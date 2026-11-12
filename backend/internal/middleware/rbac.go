package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type groupRoleKey string

const GroupRoleContextKey groupRoleKey = "groupRole"

// GetUserRoleInGroup queries user_group_roles for the given user and group.
func GetUserRoleInGroup(ctx context.Context, db *pgxpool.Pool, uID, gID int64) (int16, error) {
	var roleID int16
	query := `SELECT role_id FROM user_group_roles WHERE u_id = $1 AND g_id = $2`
	err := db.QueryRow(ctx, query, uID, gID).Scan(&roleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil // Not a member
		}
		return 0, err
	}
	return roleID, nil
}

// RequireGroupAdmin enforces that the authenticated user is an Admin of the group in URL param {groupId}.
func RequireGroupAdmin(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthUser(r.Context())
			if !ok {
				http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			groupIDStr := chi.URLParam(r, "groupId")
			groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
			if err != nil || groupID <= 0 {
				http.Error(w, `{"error": "Invalid group ID"}`, http.StatusBadRequest)
				return
			}

			roleID, err := GetUserRoleInGroup(r.Context(), db, claims.UID, groupID)
			if err != nil {
				http.Error(w, `{"error": "Failed to verify group membership"}`, http.StatusInternalServerError)
				return
			}

			if roleID == 0 {
				http.Error(w, `{"error": "Forbidden: you are not a member of this group"}`, http.StatusForbidden)
				return
			}

			if roleID != models.RoleAdmin {
				http.Error(w, `{"error": "Forbidden: only group owner/admin can perform this action"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), GroupRoleContextKey, roleID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireGroupMember enforces that the authenticated user has at least membership in the group.
func RequireGroupMember(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthUser(r.Context())
			if !ok {
				http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			groupIDStr := chi.URLParam(r, "groupId")
			groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
			if err != nil || groupID <= 0 {
				http.Error(w, `{"error": "Invalid group ID"}`, http.StatusBadRequest)
				return
			}

			roleID, err := GetUserRoleInGroup(r.Context(), db, claims.UID, groupID)
			if err != nil {
				http.Error(w, `{"error": "Failed to verify group membership"}`, http.StatusInternalServerError)
				return
			}

			if roleID == 0 {
				http.Error(w, `{"error": "Forbidden: you are not a member of this group"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), GroupRoleContextKey, roleID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

