package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ugeebee/EzMonitor/backend/internal/auth"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

func TestAuthMiddleware(t *testing.T) {
	jwtSecret := "my-very-secret-test-key-32b-long"
	authMW := middleware.AuthRequired(jwtSecret)

	user := &models.User{
		UID:   10,
		Email: "test@ezmonitor.dev",
		Name:  "Test User",
	}
	validToken, err := auth.GenerateToken(user, jwtSecret, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	handlerToTest := authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.GetAuthUser(r.Context())
		if !ok || claims.UID != user.UID {
			t.Errorf("Expected user UID %d in context", user.UID)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// Case 1: Missing header
	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	rec := httptest.NewRecorder()
	handlerToTest.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for missing header, got %d", rec.Code)
	}

	// Case 2: Invalid format
	req = httptest.NewRequest("GET", "/api/v1/groups", nil)
	req.Header.Set("Authorization", "Token "+validToken)
	rec = httptest.NewRecorder()
	handlerToTest.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for invalid token prefix, got %d", rec.Code)
	}

	// Case 3: Valid bearer token
	req = httptest.NewRequest("GET", "/api/v1/groups", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	rec = httptest.NewRecorder()
	handlerToTest.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for valid bearer token, got %d", rec.Code)
	}
}

