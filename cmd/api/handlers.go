package main

import (
	"context"
	"html"
	"io"
	"log"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Register handles user registration: hashes password, stores user, returns JWT token
func (s *Server) Register(ctx context.Context, req *v1.RegisterRequest) (*v1.RegisterResponse, error) {
	// Hash password using auth utility
	hashed, err := auth.HashPassword(req.GetPassword())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to hash password: %v", err)
	}

	// Create user in DB
	user, err := s.users.CreateUser(ctx, req.GetEmail(), hashed)
	if err != nil {
		log.Printf("create user failed: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to create user")
	}

	// Generate JWT token for newly created user
	token, expiresAt, err := s.auth.GenerateToken(user.ID, user.Email)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}

	// Build response using proto types
	return &v1.RegisterResponse{
		Token:     token,
		UserId:    user.ID.Hex(),
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

// Login authenticates a user and returns a JWT token
func (s *Server) Login(ctx context.Context, req *v1.LoginRequest) (*v1.LoginResponse, error) {
	// Lookup user by email
	user, err := s.users.GetUserByEmail(ctx, req.GetEmail())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "user not found")
	}

	// Verify password
	if err := auth.CheckPassword(user.Password, req.GetPassword()); err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "invalid credentials")
	}

	// Generate token
	token, expiresAt, err := s.auth.GenerateToken(user.ID, user.Email)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate token: %v", err)
	}

	return &v1.LoginResponse{
		Token:     token,
		UserId:    user.ID.Hex(),
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

// ListChats streams recent chat partners for the authenticated user
func (s *Server) ListChats(req *v1.ListChatsRequest, stream v1.ChatService_ListChatsServer) error {
	// Get claims from context (injected by interceptor)
	claims, ok := getClaimsFromContext(stream.Context())
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing auth claims")
	}

	// Get recent chat partners (use request limit or default value)
	var limit int64 = int64(req.GetLimit())
	if limit == 0 {
		limit = 50
	}
	partners, err := s.msgs.GetRecentChats(stream.Context(), claims.Email, limit)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to read recent chats: %v", err)
	}

	for _, p := range partners {
		// Stream each partner to client
		if err := stream.Send(&v1.ListChatsResponse{
			Email:         p.Email,
			LastMessage:   p.LastMessage,
			LastMessageAt: timestamppb.New(p.LastMessageTime),
		}); err != nil {
			return status.Errorf(codes.Internal, "failed to send partner: %v", err)
		}
	}
	return nil
}

// GetHistory streams conversation history with the requested user
func (s *Server) GetHistory(req *v1.GetHistoryRequest, stream v1.ChatService_GetHistoryServer) error {
	claims, ok := getClaimsFromContext(stream.Context())
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing auth claims")
	}

	// Retrieve recent messages between the authenticated user and the requested partner
	msgs, err := s.msgs.GetMessageHistory(stream.Context(), claims.Email, req.GetWithEmail(), 100)
	if err != nil {
		return status.Errorf(codes.Internal, "failed to get history: %v", err)
	}

	for _, m := range msgs {
		if err := stream.Send(&v1.GetHistoryResponse{
			MsgId:     m.ID.Hex(),
			FromEmail: m.FromEmail,
			ToEmail:   m.ToEmail,
			Content:   m.Content,
			SentAt:    timestamppb.New(m.SentAt),
		}); err != nil {
			return status.Errorf(codes.Internal, "failed to send message: %v", err)
		}
	}

	return nil
}

// ChatStream handles bidirectional real-time messaging - saves messages and replies with message metadata.
func (s *Server) ChatStream(stream v1.ChatService_ChatStreamServer) error {
	// Extract claims once from stream context for sender identity
	claims, ok := getClaimsFromContext(stream.Context())
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing auth claims")
	}

	// Register this stream in the hub so other connected clients can receive messages.
	// We register under the authenticated user's email and ensure we unregister when
	// the stream returns/exits.
	var connID int64
	if s.hub != nil {
		connID = s.hub.Register(claims.Email, stream)
		defer s.hub.Unregister(claims.Email, connID)
	}

	for {
		// Receive message from client
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "receive error: %v", err)
		}

		// Optionally verify recipient exists
		exists, err := s.users.UserExists(stream.Context(), req.GetToEmail())
		if err != nil {
			return status.Errorf(codes.Internal, "failed to verify recipient: %v", err)
		}
		if !exists {
			return status.Errorf(codes.NotFound, "recipient not found")
		}

		// Save message in DB
		saved, err := s.msgs.SaveMessage(stream.Context(), claims.Email, req.GetToEmail(), html.EscapeString(req.GetContent()), time.Now())
		if err != nil {
			return status.Errorf(codes.Internal, "failed to save message: %v", err)
		}

		// Build response with the persisted message metadata
		resp := &v1.ChatStreamResponse{
			MsgId:     saved.ID.Hex(),
			FromEmail: saved.FromEmail,
			Content:   saved.Content,
			SentAt:    timestamppb.New(saved.SentAt),
		}

		// Send acknowledgement back to sender
		if err := stream.Send(resp); err != nil {
			return status.Errorf(codes.Internal, "failed to send response to sender: %v", err)
		}

		// Try to deliver the saved message to the recipient's active streams.
		// This is best-effort — if the recipient isn't connected, the message is persisted
		// and will be available via GetHistory when they reconnect.
		if s.hub != nil {
			if err := s.hub.SendToUser(req.GetToEmail(), resp); err != nil {
				// Not connected or send failed — log and continue. This is deliberate: we don't
				// want a single failing recipient stream to bring down the sender's stream.
				log.Printf("delivery to %s failed (or user offline): %v", req.GetToEmail(), err)
			}
		}
	}
}
