package auth_test

import (
	"testing"
	"time"

	"github.com/ugeebee/EzMonitor/backend/internal/auth"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	rawPassword := "SecurePassword123"

	hash, err := auth.HashPassword(rawPassword)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if hash == "" || hash == rawPassword {
		t.Fatalf("Expected valid hash, got: %s", hash)
	}

	// Verify valid password
	if !auth.CheckPasswordHash(rawPassword, hash) {
		t.Errorf("Expected password to verify successfully")
	}

	// Verify wrong password fails
	if auth.CheckPasswordHash("WrongPassword123", hash) {
		t.Errorf("Expected wrong password verification to fail")
	}
}

func TestJWTGenerationAndValidation(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!!"
	user := &models.User{
		UID:   42,
		Email: "admin@ezmonitor.dev",
		Name:  "Admin User",
	}

	token, err := auth.GenerateToken(user, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate JWT: %v", err)
	}

	if token == "" {
		t.Fatal("Generated token is empty")
	}

	// Validate token
	claims, err := auth.ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	if claims.UID != user.UID {
		t.Errorf("Expected UID %d, got %d", user.UID, claims.UID)
	}
	if claims.Email != user.Email {
		t.Errorf("Expected Email %s, got %s", user.Email, claims.Email)
	}
	if claims.Name != user.Name {
		t.Errorf("Expected Name %s, got %s", user.Name, claims.Name)
	}

	// Tampered secret
	_, err = auth.ValidateToken(token, "wrong-secret-key")
	if err == nil {
		t.Errorf("Expected token validation with wrong secret to fail")
	}

	// Expired token
	expiredToken, err := auth.GenerateToken(user, secret, -1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate expired token: %v", err)
	}
	_, err = auth.ValidateToken(expiredToken, secret)
	if err == nil {
		t.Errorf("Expected expired token to be rejected")
	}
}

