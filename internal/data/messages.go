package data

import (
	"context"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/normalize"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MessagesStore provides message database operations.
type MessagesStore struct {
	// coll is reference to "messages" collection in MongoDB
	// Set via NewMessagesStore() and used in all methods below
	coll *mongo.Collection
}

// NewMessagesStore returns a MessagesStore using given collection.
func NewMessagesStore(coll *mongo.Collection) *MessagesStore {
	return &MessagesStore{coll: coll} // Store reference to MongoDB collection
}

// SaveMessage inserts a message document and returns the saved record.
func (m *MessagesStore) SaveMessage(ctx context.Context, fromEmail, toEmail, content string, sentAt time.Time) (*Message, error) {
	// Create Message struct matching the domain model in models.go
	msg := &Message{
		// Ensure emails are stored in normalized (lowercase + trimmed) form
		FromEmail: normalize.Email(fromEmail), // Sender email from JWT claims
		ToEmail:   normalize.Email(toEmail),   // Recipient email from ChatStreamRequest.to_email
		Content:   content,                    // Message text from ChatStreamRequest.content
		SentAt:    sentAt,                     // Timestamp when client sent (for ordering)
		CreatedAt: time.Now(),                 // Server-side timestamp when saved
	}

	// InsertOne adds the message document to MongoDB collection
	result, err := m.coll.InsertOne(ctx, msg)
	if err != nil {
		return nil, err // Database error (connection, validation, etc)
	}

	// Extract MongoDB's auto-generated _id and populate in struct
	// This ID is returned to client in ChatStreamResponse.msg_id
	msg.ID = result.InsertedID.(bson.ObjectID)

	// Return the saved message with ID; handler broadcasts to stream
	return msg, nil
}

// GetMessageHistory returns recent messages between two users (ordered oldest→newest).
func (m *MessagesStore) GetMessageHistory(ctx context.Context, user1, user2 string, limit int64) ([]*Message, error) {
	// Set MongoDB Find options: sort by sent_at descending (newest first) and limit results
	opts := options.Find().
		SetSort(bson.M{"sent_at": -1}). // -1 means descending order (newest first)
		SetLimit(limit)                 // Only return N most recent messages

	// Create filter to match messages between these two users (bidirectional)
	// "$or" means either condition is true
	// Normalize the provided emails before building the query so mixed-case
	// usage still matches stored messages.
	u1 := normalize.Email(user1)
	u2 := normalize.Email(user2)

	filter := bson.M{
		"$or": bson.A{
			bson.M{
				// Messages sent FROM user1 TO user2
				"from_email": u1,
				"to_email":   u2,
			},
			bson.M{
				// Messages sent FROM user2 TO user1 (opposite direction)
				"from_email": u2,
				"to_email":   u1,
			},
		},
	}

	// Execute the query; Find returns a cursor to iterate results
	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err // Database error
	}
	// Ensure cursor is closed when done (cleanup)
	defer cursor.Close(ctx)

	// Initialize slice to hold decoded messages
	var messages []*Message

	// All() reads all documents from cursor and decodes into messages slice
	if err = cursor.All(ctx, &messages); err != nil {
		return nil, err // Error decoding documents
	}

	// Reverse the slice because MongoDB returned newest first (-1 sort)
	// But client expects chronological order: oldest message first
	// Classic two-pointer swap: i starts at 0, j starts at end
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		// Swap elements at positions i and j
		messages[i], messages[j] = messages[j], messages[i]
	}

	// Return chronologically ordered messages (oldest first, newest last)
	return messages, nil
}

// GetRecentChats aggregates recent partners and last message info.
func (m *MessagesStore) GetRecentChats(ctx context.Context, userEmail string, limit int64) ([]*ChatPartner, error) {
	// MongoDB Aggregation Pipeline: series of stages that transform data
	// Think of it like: filter → group → sort → limit
	// Normalize the user email first so the pipeline matches stored documents
	userEmail = normalize.Email(userEmail)

	pipeline := mongo.Pipeline{
		// Stage 1: $match - Filter messages where userEmail appears as sender or recipient
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "$or", Value: bson.A{
				// Messages sent BY this user
				bson.D{{Key: "from_email", Value: userEmail}},
				// Messages sent TO this user
				bson.D{{Key: "to_email", Value: userEmail}},
			}},
		}}},

		// Stage 2: $group - Group messages by conversation partner
		// For each unique partner, collect the last message
		bson.D{{Key: "$group", Value: bson.D{
			// _id: how to identify each group (by partner email)
			{Key: "_id", Value: bson.D{
				// partner: determine who the partner is
				{Key: "partner", Value: bson.D{
					// $cond: if from_email == userEmail, then partner is to_email, else from_email
					// This handles both directions of conversation
					{Key: "$cond", Value: bson.A{
						bson.D{{Key: "$eq", Value: bson.A{"$from_email", userEmail}}},
						"$to_email",   // If true, partner is recipient
						"$from_email", // If false, partner is sender
					}},
				}},
			}},
			// Accumulate the last message content in each group
			{Key: "last_message", Value: bson.D{{Key: "$last", Value: "$content"}}},
			// Accumulate the last message timestamp in each group
			{Key: "last_message_at", Value: bson.D{{Key: "$last", Value: "$sent_at"}}},
		}}},

		// Stage 3: $sort - Sort by most recent conversation first
		bson.D{{Key: "$sort", Value: bson.D{{Key: "last_message_at", Value: -1}}}},

		// Stage 4: $limit - Only return N most recent partners
		bson.D{{Key: "$limit", Value: limit}},
	}

	// Execute aggregation pipeline; Aggregate returns cursor
	cursor, err := m.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err // Pipeline execution error
	}
	// Ensure cursor is closed (cleanup)
	defer cursor.Close(ctx)

	// Initialize slice to hold raw aggregation results (BSON documents)
	var results []bson.M

	// All() reads all aggregation results into the slice
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err // Error decoding results
	}

	// Initialize slice for ChatPartner structs
	var partners []*ChatPartner

	// Convert raw BSON results into ChatPartner structs
	for _, result := range results {
		// Extract partner email from nested _id.partner field
		partner := &ChatPartner{
			// result["_id"] is a bson.M (map) with key "partner"
			Email: result["_id"].(bson.M)["partner"].(string),

			// Extract last message text
			LastMessage: result["last_message"].(string),

			// Extract last message timestamp
			LastMessageTime: result["last_message_at"].(time.Time),
		}
		// Append to partners slice
		partners = append(partners, partner)
	}

	// Return all chat partners sorted by most recent conversation
	return partners, nil
}
