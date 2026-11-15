package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ugeebee/EzMonitor/backend/internal/auth"
)

type contextKey string

const UserContextKey contextKey = "authUser"

// AuthRequired ensures a valid JWT bearer token is present in the request.
func AuthRequired(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error": "Authorization header required"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, `{"error": "Invalid authorization header format, expected 'Bearer <token>'"}`, http.StatusUnauthorized)
				return
			}

			claims, err := auth.ValidateToken(parts[1], jwtSecret)
			if err != nil {
				http.Error(w, `{"error": "Invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAuthUser retrieves authenticated user claims from context.
func GetAuthUser(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(UserContextKey).(*auth.Claims)
	return claims, ok && claims != nil
}

