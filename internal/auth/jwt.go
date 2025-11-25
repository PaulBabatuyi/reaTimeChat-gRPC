package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

// JWTManager signs and validates JWT tokens used by the API.
type JWTManager struct {
	secretKey string        // Secret key for HMAC signing (should be from environment)
	duration  time.Duration // How long tokens are valid (e.g., 24 hours)
}

// Claims is the custom JWT payload (user id + email).
type Claims struct {
	UserID               string `json:"user_id"` // MongoDB ObjectID converted to hex string
	Email                string `json:"email"`   // User email from database
	jwt.RegisteredClaims        // Includes ExpiresAt, IssuedAt, etc.
}

// NewJWTManager returns a configured JWTManager.
func NewJWTManager(secretKey string, duration time.Duration) *JWTManager {
	return &JWTManager{
		secretKey: secretKey, // Secret from environment variable
		duration:  duration,  // Token validity period
	}
}

// GenerateToken issues a signed JWT token for a user.
func (m *JWTManager) GenerateToken(userID bson.ObjectID, email string) (string, time.Time, error) {
	// Calculate when this token will expire (current time + duration)
	expiresAt := time.Now().Add(m.duration)

	// Create claims struct with user info and expiration
	claims := &Claims{
		UserID: userID.Hex(), // Convert MongoDB ObjectID to hex string for JSON
		Email:  email,        // User email from database
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),  // Set expiration time
			IssuedAt:  jwt.NewNumericDate(time.Now()), // Set creation time
		},
	}

	// Create new token with HS256 signing method (HMAC with SHA-256)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign the token using the secret key to create the final JWT string
	tokenString, err := token.SignedString([]byte(m.secretKey))
	if err != nil {
		return "", time.Time{}, err // Return empty string and zero time on error
	}

	// Return the signed token string, expiration time, and no error
	return tokenString, expiresAt, nil
}

// VerifyToken parses and validates a token and returns its claims.
func (m *JWTManager) VerifyToken(tokenString string) (*Claims, error) {
	// Initialize empty Claims struct to hold decoded data
	claims := &Claims{}

	// ParseWithClaims parses the token and validates the signature
	// The third argument is a callback that validates the signing method
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Security check: ensure token was signed with HMAC (not asymmetric key)
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// Return the secret key used to verify the signature
		return []byte(m.secretKey), nil
	})

	// Check if there was an error during parsing (malformed, expired, etc)
	if err != nil {
		return nil, err
	}

	// Verify token is actually valid (checks signature and expiration)
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Return extracted claims so handler can identify the user
	return claims, nil
}

// HashPassword returns a bcrypt hash for the provided plaintext.
func HashPassword(password string) (string, error) {
	// GenerateFromPassword creates a bcrypt hash with default cost (10 rounds)
	// More rounds = slower but more secure; default balances security and speed
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err // Return empty string if hashing fails
	}
	// Return the hash as string for storage in MongoDB
	return string(hashedPassword), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(hash, password string) error {
	// CompareHashAndPassword returns nil if password matches hash, error otherwise
	// This is timing-safe against brute-force attacks
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
