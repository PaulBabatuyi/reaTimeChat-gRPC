// Package data provides DB models and stores.
package data

import (
	"context" // Used for cancellation and timeouts
	"errors"  // Error handling
	"time"    // Timestamps

	"go.mongodb.org/mongo-driver/v2/bson"  // MongoDB document queries
	"go.mongodb.org/mongo-driver/v2/mongo" // MongoDB driver
)

// UsersStore performs user DB operations.
type UsersStore struct {
	// coll is reference to "users" collection in MongoDB
	// Set via NewUsersStore() and used in all methods below
	coll *mongo.Collection
}

// NewUsersStore returns a UsersStore using the provided collection.
func NewUsersStore(coll *mongo.Collection) *UsersStore {
	return &UsersStore{coll: coll} // Store reference to MongoDB collection
}

// CreateUser inserts a new user document with hashed password.
func (u *UsersStore) CreateUser(ctx context.Context, email, hashedPassword string) (*User, error) {
	// Create new User struct with provided email and already-hashed password from auth.HashPassword()
	user := &User{
		Email:     email,          // From RegisterRequest.email
		Password:  hashedPassword, // Already hashed by auth.HashPassword()
		CreatedAt: time.Now(),     // Set current server time
		UpdatedAt: time.Now(),     // Initially same as CreatedAt
	}

	// InsertOne adds the document to MongoDB "users" collection
	// Returns result with InsertedID if successful
	result, err := u.coll.InsertOne(ctx, user)
	if err != nil {
		// Check if error is due to duplicate email (unique constraint violation)
		// This happens if RegisterRequest.email already exists in database
		if mongo.IsDuplicateKeyError(err) {
			return nil, errors.New("user already exists")
		}
		// Other database errors (connection, validation, etc)
		return nil, err
	}

	// MongoDB auto-generates the _id field; extract it and set on User struct
	// This ID will be used in JWT token via auth.GenerateToken()
	user.ID = result.InsertedID.(bson.ObjectID)

	// Return the created user with ID populated, used to generate JWT token
	return user, nil
}

// GetUserByEmail finds a user by email.
func (u *UsersStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	// Initialize empty User struct to hold query result
	var user User

	// FindOne queries the collection for a document matching the email
	// bson.M{"email": email} creates MongoDB query filter: {email: "provided@email.com"}
	err := u.coll.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		// Check if no document found (user doesn't exist)
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("user not found")
		}
		// Other database errors
		return nil, err
	}

	// Return populated User struct with ID, Email, Password (hash), and timestamps
	// Handler will use Password field to verify with auth.CheckPassword()
	return &user, nil
}

// GetUserByID finds a user by ObjectID.
func (u *UsersStore) GetUserByID(ctx context.Context, id bson.ObjectID) (*User, error) {
	// Initialize empty User struct
	var user User

	// FindOne queries for document matching the _id field
	// bson.M{"_id": id} creates MongoDB query: {_id: ObjectID(...)}
	err := u.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		// No document found (user was deleted)
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("user not found")
		}
		// Database errors
		return nil, err
	}

	// Return the user; at this point we know user still exists in database
	return &user, nil
}

// UserExists checks if a user exists by email.
func (u *UsersStore) UserExists(ctx context.Context, email string) (bool, error) {
	// CountDocuments returns number of documents matching the filter
	// Much faster than FindOne when you only need to know if it exists
	count, err := u.coll.CountDocuments(ctx, bson.M{"email": email})
	if err != nil {
		// Database errors
		return false, err
	}

	// Return true if at least one document found, false otherwise
	return count > 0, nil
}
