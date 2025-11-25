package auth

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestHashAndCheckPassword(t *testing.T) {
	pwd := "s3cr3t-password"
	hash, err := HashPassword(pwd)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if err := CheckPassword(hash, pwd); err != nil {
		t.Fatalf("CheckPassword failed when password should match: %v", err)
	}

	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Fatal("CheckPassword succeeded when it should have failed")
	}
}

func TestJWTManager_GenerateAndVerify(t *testing.T) {
	m := NewJWTManager("test-secret", 5*time.Minute)

	// use zero ObjectID (valid type) — hex string will still be produced
	var id bson.ObjectID
	token, _, err := m.GenerateToken(id, "test@example.com")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := m.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if claims.Email != "test@example.com" {
		t.Fatalf("claims.Email mismatch: got %s", claims.Email)
	}
}
