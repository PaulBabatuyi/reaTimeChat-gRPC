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

	"strconv"
	"strings"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/db"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/middleware"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	// Read configuration from environment
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		log.Fatal("MONGODB_URI must be set")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	jwtKeysEnv := os.Getenv("JWT_KEYS") // optional: format kid:secret,kid2:secret2
	jwtActiveKid := os.Getenv("JWT_ACTIVE_KID")
	if jwtKeysEnv == "" && jwtSecret == "" {
		log.Fatal("either JWT_SECRET or JWT_KEYS must be set")
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

	// Initialize auth manager (token valid for 24 hours). If JWT_KEYS supplied
	// we parse keys so token rotation is possible; otherwise fall back to single
	// JWT_SECRET value for backward compatibility.
	var jwtMgr *auth.JWTManager
	if jwtKeysEnv != "" {
		// parse kid:key pairs
		keyMap := map[string]string{}
		pairs := strings.Split(jwtKeysEnv, ",")
		for _, p := range pairs {
			if p == "" {
				continue
			}
			parts := strings.SplitN(p, ":", 2)
			if len(parts) != 2 {
				log.Fatalf("invalid JWT_KEYS entry: %s", p)
			}
			keyMap[parts[0]] = parts[1]
		}
		jwtMgr = auth.NewJWTManagerFromKeys(keyMap, jwtActiveKid, 24*time.Hour)
	} else {
		jwtMgr = auth.NewJWTManager(jwtSecret, 24*time.Hour)
	}

	// Build a rate limiter for Register and Login endpoints, then chain interceptors.
	// RATE_LIMIT_RPM controls requests per minute for these sensitive endpoints.
	rateRPM := 10
	if v := os.Getenv("RATE_LIMIT_RPM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateRPM = n
		}
	}

	// Create limiter store (small burst to allow a couple of quick retries)
	limiterStore := middleware.NewLimiterStore(rateRPM, 3, 1*time.Minute)
	defer limiterStore.Stop()
	limited := map[string]bool{
		"/chat.v1.ChatService/Register": true,
		"/chat.v1.ChatService/Login":    true,
	}

	// assemble server opts and chain unary interceptors: rate limiter -> auth
	var serverOpts []grpc.ServerOption

	// If TLS certs are configured, create server credentials and require TLS
	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")
	requireTLS := os.Getenv("REQUIRE_TLS") == "true"
	if certFile != "" && keyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			log.Fatalf("failed to load TLS certs: %v", err)
		}
		serverOpts = append(serverOpts, grpc.Creds(creds))
	} else if requireTLS {
		log.Fatal("REQUIRE_TLS is true but TLS_CERT/TLS_KEY are not configured")
	}

	// Add the chained interceptors
	serverOpts = append(serverOpts, grpc.ChainUnaryInterceptor(
		middleware.RateLimitUnaryInterceptor(limiterStore, limited),
		authUnaryInterceptor(jwtMgr),
	))
	serverOpts = append(serverOpts, grpc.ChainStreamInterceptor(authStreamInterceptor(jwtMgr)))

	grpcServer := grpc.NewServer(serverOpts...)

	// Create connection hub, service instance and register
	hub := NewConnectionHub()
	srv := newServer(usersStore, msgsStore, jwtMgr, hub)
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
