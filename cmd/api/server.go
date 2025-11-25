package main

import (
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc"
)

// Server implements the chat service and contains references to stores and auth logic.
type Server struct {
	v1.UnimplementedChatServiceServer

	users *data.UsersStore
	msgs  *data.MessagesStore
	auth  *auth.JWTManager
}

// newServer returns a ready-to-use Server wired with stores and auth manager.
func newServer(users *data.UsersStore, msgs *data.MessagesStore, authMgr *auth.JWTManager) *Server {
	return &Server{users: users, msgs: msgs, auth: authMgr}
}

// registerService registers the ChatService on the given gRPC server.
func registerService(s *grpc.Server, srv *Server) {
	v1.RegisterChatServiceServer(s, srv)
}
