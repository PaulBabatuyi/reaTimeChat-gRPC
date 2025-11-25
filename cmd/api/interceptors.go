package main

import (
	"context"
	"strings"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// context key type for storing auth claims in context
type authContextKey struct{}

// getClaimsFromContext extracts auth claims from the context, if present.
func getClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	v := ctx.Value(authContextKey{})
	if v == nil {
		return nil, false
	}
	c, ok := v.(*auth.Claims)
	return c, ok
}

// authUnaryInterceptor returns a UnaryServerInterceptor that enforces JWT authentication
// for all methods except the allowed unauthenticated list (Register, Login).
func authUnaryInterceptor(j *auth.JWTManager) grpc.UnaryServerInterceptor {
	// methods that don't require authentication
	allowed := map[string]bool{
		"/chat.v1.ChatService/Register": true,
		"/chat.v1.ChatService/Login":    true,
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if allowed[info.FullMethod] {
			return handler(ctx, req)
		}

		// extract Authorization header from metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
		}
		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Errorf(codes.Unauthenticated, "missing authorization header")
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeaders[0], "Bearer"))
		if token == "" {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token")
		}

		claims, err := j.VerifyToken(token)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
		}

		// attach claims into context for handlers
		ctx = context.WithValue(ctx, authContextKey{}, claims)
		return handler(ctx, req)
	}
}

// authStreamInterceptor is the stream equivalent of authUnaryInterceptor.
func authStreamInterceptor(j *auth.JWTManager) grpc.StreamServerInterceptor {
	allowed := map[string]bool{
		"/chat.v1.ChatService/Register": true,
		"/chat.v1.ChatService/Login":    true,
	}

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if allowed[info.FullMethod] {
			return handler(srv, ss)
		}

		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Errorf(codes.Unauthenticated, "missing metadata")
		}
		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return status.Errorf(codes.Unauthenticated, "missing authorization header")
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeaders[0], "Bearer"))
		if token == "" {
			return status.Errorf(codes.Unauthenticated, "invalid token")
		}

		claims, err := j.VerifyToken(token)
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "unauthenticated: %v", err)
		}

		// wrap stream context with claims
		newCtx := context.WithValue(ss.Context(), authContextKey{}, claims)
		wrapped := grpcmiddlewareServerStream{ServerStream: ss, ctx: newCtx}
		return handler(srv, wrapped)
	}
}

// grpcmiddlewareServerStream wraps grpc.ServerStream to override Context()
type grpcmiddlewareServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the wrapped context (with claims)
func (g grpcmiddlewareServerStream) Context() context.Context { return g.ctx }
