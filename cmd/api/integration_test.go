package main

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/db"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func TestRegisterAndLogin(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping integration test")
	}

	ctx := context.Background()
	dbClient, err := db.New(ctx, uri)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}
	defer func() {
		_ = dbClient.UsersCollection().Drop(context.Background())
		_ = dbClient.MessagesCollection().Drop(context.Background())
		_ = dbClient.Close(context.Background())
	}()

	usersStore := data.NewUsersStore(dbClient.UsersCollection())
	msgsStore := data.NewMessagesStore(dbClient.MessagesCollection())
	jwtMgr := auth.NewJWTManager("test-secret", time.Hour)

	// set up bufconn server
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer(
		grpc.UnaryInterceptor(authUnaryInterceptor(jwtMgr)),
		grpc.StreamInterceptor(authStreamInterceptor(jwtMgr)),
	)

	srv := newServer(usersStore, msgsStore, jwtMgr)
	v1.RegisterChatServiceServer(s, srv)

	go func() {
		_ = s.Serve(lis)
	}()

	// Dialer via bufconn
	dialer := func(ctx context.Context, _ string) (net.Conn, error) { return lis.Dial() }

	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(dialer), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := v1.NewChatServiceClient(conn)

	email := time.Now().UTC().Format("20060102-150405") + "-it@example.com"
	pwd := "testPass123"

	// Register
	regResp, err := client.Register(ctx, &v1.RegisterRequest{Email: email, Password: pwd})
	if err != nil {
		t.Fatalf("Register RPC failed: %v", err)
	}
	if regResp.GetToken() == "" || regResp.GetUserId() == "" {
		t.Fatalf("Register response missing token or user_id")
	}

	// Login
	loginResp, err := client.Login(ctx, &v1.LoginRequest{Email: email, Password: pwd})
	if err != nil {
		t.Fatalf("Login RPC failed: %v", err)
	}
	if loginResp.GetToken() == "" {
		t.Fatalf("Login response missing token")
	}

	// shutdown server
	s.GracefulStop()
}
