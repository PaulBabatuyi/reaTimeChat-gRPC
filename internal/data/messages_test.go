package data

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/db"
)

func TestMessagesSaveAndQuery(t *testing.T) {
	// no env loader; require MONGODB_URI set externally for integration tests
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping integration test")
	}

	ctx := context.Background()
	c, err := db.New(ctx, uri)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	// ensure clean collections
	_ = c.MessagesCollection().Drop(ctx)

	msgs := NewMessagesStore(c.MessagesCollection())

	// create messages between alice and bob
	now := time.Now()
	_, err = msgs.SaveMessage(ctx, "alice@example.com", "bob@example.com", "hi bob", now)
	if err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}
	_, err = msgs.SaveMessage(ctx, "bob@example.com", "alice@example.com", "hello alice", now.Add(time.Second))
	if err != nil {
		t.Fatalf("SaveMessage 2 failed: %v", err)
	}

	// history
	history, err := msgs.GetMessageHistory(ctx, "alice@example.com", "bob@example.com", 10)
	if err != nil {
		t.Fatalf("GetMessageHistory failed: %v", err)
	}
	if len(history) < 2 {
		t.Fatalf("expected >=2 messages, got %d", len(history))
	}

	// recent chats
	partners, err := msgs.GetRecentChats(ctx, "alice@example.com", 10)
	if err != nil {
		t.Fatalf("GetRecentChats failed: %v", err)
	}
	if len(partners) == 0 {
		t.Fatalf("expected at least 1 partner")
	}
}

func TestMessagesNormalization(t *testing.T) {
	// require MONGODB_URI set externally for integration tests
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping integration test")
	}

	ctx := context.Background()
	c, err := db.New(ctx, uri)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	// ensure clean collections
	_ = c.MessagesCollection().Drop(ctx)

	msgs := NewMessagesStore(c.MessagesCollection())

	now := time.Now()

	// save using mixed-case emails
	_, err = msgs.SaveMessage(ctx, "ALICE@Example.COM", "BoB@EXample.com", "hi bob", now)
	if err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}

	// history query using different casing should still return the message
	history, err := msgs.GetMessageHistory(ctx, "alice@example.com", "BOB@example.COM", 10)
	if err != nil {
		t.Fatalf("GetMessageHistory failed: %v", err)
	}
	if len(history) < 1 {
		t.Fatalf("expected >=1 messages, got %d", len(history))
	}

	// recent chats for ALICE should show partner bob (normalized)
	partners, err := msgs.GetRecentChats(ctx, "ALICE@example.com", 10)
	if err != nil {
		t.Fatalf("GetRecentChats failed: %v", err)
	}
	if len(partners) == 0 {
		t.Fatalf("expected at least 1 partner")
	}
}
