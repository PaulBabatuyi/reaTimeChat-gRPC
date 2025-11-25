package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/db"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc"
)

func main() {
	// Read configuration from environment
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		log.Fatal("MONGODB_URI must be set")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET must be set")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}

	ctx := context.Background()

	// Initialize database
	dbClient, err := db.New(ctx, mongoURI)
	if err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	defer func() {
		_ = dbClient.Close(ctx)
	}()

	// Ensure indexes exist
	if err := dbClient.CreateIndexes(ctx); err != nil {
		log.Fatalf("failed to create indexes: %v", err)
	}

	// Create stores
	usersStore := data.NewUsersStore(dbClient.UsersCollection())
	msgsStore := data.NewMessagesStore(dbClient.MessagesCollection())

	// Initialize auth manager (token valid for 24 hours)
	jwtMgr := auth.NewJWTManager(jwtSecret, 24*time.Hour)

	// Build gRPC server with interceptors for authentication
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authUnaryInterceptor(jwtMgr)),
		grpc.StreamInterceptor(authStreamInterceptor(jwtMgr)),
	)

	// Create service instance and register
	srv := newServer(usersStore, msgsStore, jwtMgr)
	v1.RegisterChatServiceServer(grpcServer, srv)

	// Listen and serve
	listenAddr := fmt.Sprintf(":%s", port)
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	go func() {
		log.Printf("gRPC server listening on %s", listenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server exit: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Printf("shutting down gRPC server")
	grpcServer.GracefulStop()
}
