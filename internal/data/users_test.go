package data

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/db"
)

func setupDB(t *testing.T) *db.Client {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping integration test")
	}

	ctx := context.Background()
	c, err := db.New(ctx, uri)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}

	// ensure clean collections in case previous runs left data
	_ = c.UsersCollection().Drop(ctx)
	_ = c.MessagesCollection().Drop(ctx)

	return c
}

func TestUsersCreateAndGet(t *testing.T) {
	// no env loader; require MONGODB_URI to be set externally

	c := setupDB(t)
	defer func() { _ = c.Close(context.Background()) }()

	users := NewUsersStore(c.UsersCollection())

	ctx := context.Background()
	email := time.Now().UTC().Format("20060102-150405") + "-integration@example.com"

	// create
	user, err := users.CreateUser(ctx, email, "hashed-password")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.Email != email {
		t.Fatalf("expected email %s got %s", email, user.Email)
	}

	// Exists
	ok, err := users.UserExists(ctx, email)
	if err != nil || !ok {
		t.Fatalf("UserExists failed: ok=%v err=%v", ok, err)
	}

	// Get by email
	u2, err := users.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if u2.Email != email {
		t.Fatalf("GetUserByEmail returned wrong email: %s", u2.Email)
	}

	// Get by id
	got, err := users.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if got.Email != email {
		t.Fatalf("GetUserByID returned wrong email: %s", got.Email)
	}
}
