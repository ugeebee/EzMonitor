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

// GetEffectiveRoleInGroup returns a user's role in a group or subgroup, or 0
// if they have none. The rule lives in the database view effective_group_roles
// (migration 000006) so the dashboard queries use exactly the same one:
// a direct role wins; otherwise an Admin of the parent group is Admin of the
// subgroup.
func GetEffectiveRoleInGroup(ctx context.Context, db *pgxpool.Pool, uID, gID int64) (int16, error) {
	var role int16
	err := db.QueryRow(ctx,
		`SELECT role_id FROM effective_group_roles WHERE u_id = $1 AND g_id = $2`,
		uID, gID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // not a member
	}
	if err != nil {
		return 0, err
	}
	return role, nil
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

			roleID, err := GetEffectiveRoleInGroup(r.Context(), db, claims.UID, groupID)
			if err != nil {
				http.Error(w, `{"error": "Failed to verify group membership"}`, http.StatusInternalServerError)
				return
			}

			if roleID == 0 {
				http.Error(w, `{"error": "Forbidden: you are not a member of this group"}`, http.StatusForbidden)
				return
			}

			if roleID != models.RoleAdmin {
				http.Error(w, `{"error": "Forbidden: only group admin can perform this action"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), GroupRoleContextKey, roleID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireGroupMember enforces that user is at least a member of the group.
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

			roleID, err := GetEffectiveRoleInGroup(r.Context(), db, claims.UID, groupID)
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

// RequireSubgroupAdminOrSubadmin enforces that user is Admin (top-level or subgroup) or Subadmin.
// Viewers are strictly rejected.
func RequireSubgroupAdminOrSubadmin(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthUser(r.Context())
			if !ok {
				http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			subgroupIDStr := chi.URLParam(r, "subgroupId")
			if subgroupIDStr == "" {
				subgroupIDStr = chi.URLParam(r, "groupId")
			}
			subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
			if err != nil || subgroupID <= 0 {
				http.Error(w, `{"error": "Invalid subgroup ID"}`, http.StatusBadRequest)
				return
			}

			roleID, err := GetEffectiveRoleInGroup(r.Context(), db, claims.UID, subgroupID)
			if err != nil {
				http.Error(w, `{"error": "Failed to verify subgroup permissions"}`, http.StatusInternalServerError)
				return
			}

			if roleID == 0 {
				http.Error(w, `{"error": "Forbidden: you are not a member of this subgroup"}`, http.StatusForbidden)
				return
			}

			if roleID != models.RoleAdmin && roleID != models.RoleSubadmin {
				http.Error(w, `{"error": "Forbidden: viewers can only see metrics; admin or subadmin permission required"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), GroupRoleContextKey, roleID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireSubgroupAccess ensures user is at least a Viewer, Subadmin, or Admin of the subgroup.
func RequireSubgroupAccess(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthUser(r.Context())
			if !ok {
				http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
				return
			}

			subgroupIDStr := chi.URLParam(r, "subgroupId")
			if subgroupIDStr == "" {
				subgroupIDStr = chi.URLParam(r, "groupId")
			}
			subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
			if err != nil || subgroupID <= 0 {
				http.Error(w, `{"error": "Invalid subgroup ID"}`, http.StatusBadRequest)
				return
			}

			roleID, err := GetEffectiveRoleInGroup(r.Context(), db, claims.UID, subgroupID)
			if err != nil {
				http.Error(w, `{"error": "Failed to verify subgroup membership"}`, http.StatusInternalServerError)
				return
			}

			if roleID == 0 {
				http.Error(w, `{"error": "Forbidden: you are not a member of this subgroup"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), GroupRoleContextKey, roleID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
