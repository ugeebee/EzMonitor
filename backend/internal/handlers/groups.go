package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type GroupHandler struct {
	db *pgxpool.Pool
}

func NewGroupHandler(db *pgxpool.Pool) *GroupHandler {
	return &GroupHandler{db: db}
}

// CreateGroup creates a new group and automatically makes the creator the Admin (owner).
func (h *GroupHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	var req models.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to initiate transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// 1. Insert into groups table
	var group models.GroupWithRole
	queryGroup := `
		INSERT INTO groups (label)
		VALUES ($1)
		RETURNING g_id, label, date_created
	`
	err = tx.QueryRow(ctx, queryGroup, req.Label).Scan(&group.GID, &group.Label, &group.DateCreated)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create group"})
		return
	}

	// 2. Insert creator into user_group_roles as admin (owner)
	queryRole := `
		INSERT INTO user_group_roles (u_id, g_id, role_id)
		VALUES ($1, $2, $3)
	`
	_, err = tx.Exec(ctx, queryRole, claims.UID, group.GID, models.RoleAdmin)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to assign group owner role"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to commit group creation"})
		return
	}

	group.RoleID = models.RoleAdmin
	group.RoleName = models.RoleNameAdmin

	respondJSON(w, http.StatusCreated, group)
}

// ListGroups returns all groups the authenticated user belongs to.
func (h *GroupHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	query := `
		SELECT g.g_id, g.label, g.date_created, ugr.role_id, r.role_name
		FROM groups g
		JOIN user_group_roles ugr ON g.g_id = ugr.g_id
		JOIN roles r ON ugr.role_id = r.role_id
		WHERE ugr.u_id = $1
		ORDER BY g.g_id ASC
	`
	rows, err := h.db.Query(r.Context(), query, claims.UID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query groups"})
		return
	}
	defer rows.Close()

	groups := make([]models.GroupWithRole, 0)
	for rows.Next() {
		var g models.GroupWithRole
		if err := rows.Scan(&g.GID, &g.Label, &g.DateCreated, &g.RoleID, &g.RoleName); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse group data"})
			return
		}
		groups = append(groups, g)
	}

	respondJSON(w, http.StatusOK, groups)
}

// GetGroup returns details of a single group if the user is a member.
func (h *GroupHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	groupIDStr := chi.URLParam(r, "groupId")
	groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
	if err != nil || groupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid group ID"})
		return
	}

	type GroupDetailResponse struct {
		GID            int64     `json:"g_id"`
		Label          string    `json:"label"`
		DateCreated    time.Time `json:"date_created"`
		UserRoleID     int16     `json:"user_role_id"`
		UserRoleName   string    `json:"user_role_name"`
		MembersCount   int64     `json:"members_count"`
		InstancesCount int64     `json:"instances_count"`
	}

	query := `
		SELECT 
			g.g_id, 
			g.label, 
			g.date_created, 
			ugr.role_id, 
			r.role_name,
			(SELECT COUNT(*) FROM user_group_roles WHERE g_id = g.g_id) AS members_count,
			(SELECT COUNT(*) FROM instances WHERE g_id = g.g_id) AS instances_count
		FROM groups g
		JOIN user_group_roles ugr ON g.g_id = ugr.g_id
		JOIN roles r ON ugr.role_id = r.role_id
		WHERE g.g_id = $1 AND ugr.u_id = $2
	`

	var detail GroupDetailResponse
	err = h.db.QueryRow(r.Context(), query, groupID, claims.UID).Scan(
		&detail.GID,
		&detail.Label,
		&detail.DateCreated,
		&detail.UserRoleID,
		&detail.UserRoleName,
		&detail.MembersCount,
		&detail.InstancesCount,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Group not found or access denied"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	respondJSON(w, http.StatusOK, detail)
}
