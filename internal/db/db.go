// Package db manages MongoDB connections and collections.
package db

import (
	"context" // For connection timeout/cancellation
	"fmt"     // Error formatting
	"time"    // Duration for timeouts

	"go.mongodb.org/mongo-driver/v2/mongo"          // MongoDB driver
	"go.mongodb.org/mongo-driver/v2/mongo/options"  // MongoDB options
	"go.mongodb.org/mongo-driver/v2/mongo/readpref" // MongoDB read preference
)

// Client wraps mongo.Client and exposes collections.
type Client struct {
	// client is the underlying MongoDB connection (thread-safe, can be reused)
	client *mongo.Client

	// db is reference to "chat_db" database within MongoDB
	// Collections ("users", "messages") are accessed via this db reference
	db *mongo.Database
}

// New connects to MongoDB and returns a Client.
func New(ctx context.Context, mongoURI string) (*Client, error) {
	// Create MongoDB client options from connection URI
	// SetConnectTimeout: fail fast if MongoDB is unreachable
	opts := options.Client().
		ApplyURI(mongoURI).                 // Parse connection string
		SetConnectTimeout(10 * time.Second) // Max time to connect

	// Establish connection to MongoDB server
	// This doesn't actually connect yet, just creates the client
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Create a context with timeout for the ping operation
	// If ping doesn't complete in 5 seconds, fail
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel() // Ensure context is cancelled (cleanup)

	// Ping MongoDB to verify connection is working
	// This is the actual connection test
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	// Get reference to "chat_db" database (created if doesn't exist)
	// Lazy-loaded: actual DB not created until first write
	db := client.Database("chat_db")

	// Return wrapped client with both MongoDB client and database references
	return &Client{
		client: client, // Keep reference to close connection later
		db:     db,     // Use this to access collections
	}, nil
}

// UsersCollection returns the users collection.
func (c *Client) UsersCollection() *mongo.Collection {
	// Access "users" collection in "chat_db" database
	// Created if doesn't exist (MongoDB creates on first write)
	return c.db.Collection("users")
}

// MessagesCollection returns the messages collection.
func (c *Client) MessagesCollection() *mongo.Collection {
	// Access "messages" collection in "chat_db" database
	// Created if doesn't exist (MongoDB creates on first write)
	return c.db.Collection("messages")
}

// Close disconnects from MongoDB.
func (c *Client) Close(ctx context.Context) error {
	// Disconnect closes the MongoDB connection
	// ctx can have timeout if you want to force shutdown after N seconds
	return c.client.Disconnect(ctx)
}

// CreateIndexes creates necessary indexes for users and messages collections.
func (c *Client) CreateIndexes(ctx context.Context) error {
	// ===== USERS COLLECTION INDEX =====
	// Create unique index on email field
	// Ensures: no two users can have the same email (unique constraint)
	// Used by: GetUserByEmail() queries, prevents duplicate registration
	usersIndexModel := mongo.IndexModel{
		// Create index on "email" field (1 = ascending order)
		Keys: map[string]int{"email": 1},
		// SetUnique(true) means duplicate emails not allowed
		Options: options.Index().SetUnique(true),
	}

	// Execute index creation on users collection
	_, err := c.UsersCollection().Indexes().CreateOne(ctx, usersIndexModel)
	if err != nil {
		return fmt.Errorf("failed to create users index: %w", err)
	}

	// ===== MESSAGES COLLECTION INDEXES =====
	// Create two indexes for efficient message queries
	messageIndexes := []mongo.IndexModel{
		{
			// Composite index: (from_email, to_email, sent_at)
			// Used by: GetMessageHistory() to find all messages between two users, ordered by time
			// 1 = ascending, -1 = descending (newest first for sent_at)
			Keys: map[string]int{"from_email": 1, "to_email": 1, "sent_at": -1},
		},
		{
			// Simple index: just sent_at
			// Used by: GetRecentChats() aggregation to sort by time
			Keys: map[string]int{"sent_at": -1},
		},
	}

	// Execute index creation on messages collection (creates both indexes)
	_, err = c.MessagesCollection().Indexes().CreateMany(ctx, messageIndexes)
	if err != nil {
		return fmt.Errorf("failed to create message indexes: %w", err)
	}

	// All indexes created successfully
	return nil
}
