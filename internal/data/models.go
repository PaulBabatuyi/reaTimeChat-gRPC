package data

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// User maps to users collection (id, email, password hash, timestamps)
type User struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Email     string        `bson:"email,unique"`
	Password  string        `bson:"password"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

// Message maps to messages collection (sender, recipient, content, sent_at)
type Message struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	FromEmail string        `bson:"from_email"`
	ToEmail   string        `bson:"to_email"`
	Content   string        `bson:"content"`
	SentAt    time.Time     `bson:"sent_at"`
	CreatedAt time.Time     `bson:"created_at"`
}

// ChatPartner is a minimal struct used by ListChats responses
type ChatPartner struct {
	Email           string
	LastMessage     string
	LastMessageTime time.Time
}
