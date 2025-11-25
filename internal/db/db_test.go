package db

import (
	"context"
	"os"
	"testing"

	"time"
)

// These tests are integration tests and require a running MongoDB instance.
// Set MONGODB_URI in the environment before running them.

func TestNewAndCreateIndexes(t *testing.T) {
	uri := os.Getenv("MONGODB_URI")
	if uri == "" {
		t.Skip("MONGODB_URI not set; skipping integration test")
	}

	ctx := context.Background()
	c, err := New(ctx, uri)
	if err != nil {
		t.Fatalf("failed to connect to DB: %v", err)
	}
	defer func() {
		// drop the testing collections and close connection
		_ = c.db.Collection("users").Drop(context.Background())
		_ = c.db.Collection("messages").Drop(context.Background())
		_ = c.Close(context.Background())
	}()

	// should be able to create indexes without error
	if err := c.CreateIndexes(ctx); err != nil {
		t.Fatalf("CreateIndexes failed: %v", err)
	}

	// quick sanity sleep to allow DB to finalize
	time.Sleep(100 * time.Millisecond)
}
