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

func TestJWTManager_NormalizeEmailClaim(t *testing.T) {
	m := NewJWTManager("test-secret", 5*time.Minute)

	var id bson.ObjectID
	token, _, err := m.GenerateToken(id, "User.Case@Example.COM")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := m.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if claims.Email != "user.case@example.com" {
		t.Fatalf("expected normalized email in claims, got %s", claims.Email)
	}
}

func TestJWTManager_Rotation(t *testing.T) {
	// create a manager with two keys and active kid "k2"
	keys := map[string]string{"k1": "secret-one", "k2": "secret-two"}
	m := NewJWTManagerFromKeys(keys, "k2", 5*time.Minute)

	var id bson.ObjectID

	// token created with active kid (k2)
	tkn2, _, err := m.GenerateToken(id, "rot@example.com")
	if err != nil {
		t.Fatalf("GenerateToken (k2) failed: %v", err)
	}

	// verify works (should pick k2 via kid header)
	if _, err := m.VerifyToken(tkn2); err != nil {
		t.Fatalf("VerifyToken (k2) failed: %v", err)
	}

	// Create a token signed by the older key (k1) to emulate previously-issued tokens.
	// We'll produce it by temporarily switching active kid (similar to how a rotated key
	// may have been active in the past).
	mOld := NewJWTManagerFromKeys(keys, "k1", 5*time.Minute)
	tkn1, _, err := mOld.GenerateToken(id, "rot@example.com")
	if err != nil {
		t.Fatalf("GenerateToken (k1) failed: %v", err)
	}

	// Current manager should still verify tokens signed with older key k1
	if _, err := m.VerifyToken(tkn1); err != nil {
		t.Fatalf("VerifyToken (old k1) failed: %v", err)
	}
}
