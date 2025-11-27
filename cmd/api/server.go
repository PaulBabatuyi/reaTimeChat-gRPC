package main

import (
	"context"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc"
)

// Server implements the chat service and contains references to stores and auth logic.
// UsersStore is the subset of data.UsersStore used by the API handlers.
type UsersStore interface {
	CreateUser(ctx context.Context, email, hashedPassword string) (*data.User, error)
	GetUserByEmail(ctx context.Context, email string) (*data.User, error)
	UserExists(ctx context.Context, email string) (bool, error)
}

// MessagesStore is the subset of data.MessagesStore used by the API handlers.
type MessagesStore interface {
	SaveMessage(ctx context.Context, fromEmail, toEmail, content string, sentAt time.Time) (*data.Message, error)
	GetRecentChats(ctx context.Context, userEmail string, limit int64) ([]*data.ChatPartner, error)
	GetMessageHistory(ctx context.Context, user1, user2 string, limit int64) ([]*data.Message, error)
}

type Server struct {
	v1.UnimplementedChatServiceServer

	users UsersStore
	msgs  MessagesStore
	auth  *auth.JWTManager
	hub   *ConnectionHub
}

// newServer returns a ready-to-use Server wired with stores and auth manager.
func newServer(users UsersStore, msgs MessagesStore, authMgr *auth.JWTManager, hub *ConnectionHub) *Server {
	return &Server{users: users, msgs: msgs, auth: authMgr, hub: hub}
}

// registerService registers the ChatService on the given gRPC server.
func registerService(s *grpc.Server, srv *Server) {
	v1.RegisterChatServiceServer(s, srv)
}
