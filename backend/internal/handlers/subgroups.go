package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type SubgroupHandler struct {
	db *pgxpool.Pool
}

func NewSubgroupHandler(db *pgxpool.Pool) *SubgroupHandler {
	return &SubgroupHandler{db: db}
}

// CreateSubgroup creates a subgroup under the specified parent group.
// Only Group Admin can create subgroups.
func (h *SubgroupHandler) CreateSubgroup(w http.ResponseWriter, r *http.Request) {
	parentIDStr := chi.URLParam(r, "groupId")
	parentID, err := strconv.ParseInt(parentIDStr, 10, 64)
	if err != nil || parentID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid parent group ID"})
		return
	}

	var req models.CreateSubgroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Verify parent group exists
	var parentExists bool
	err = h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE g_id = $1)`, parentID).Scan(&parentExists)
	if err != nil || !parentExists {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Parent group not found"})
		return
	}

	// Insert subgroup
	var subgroup models.Group
	query := `
		INSERT INTO groups (label, parent_id)
		VALUES ($1, $2)
		RETURNING g_id, label, parent_id, date_created
	`
	err = h.db.QueryRow(ctx, query, req.Label, parentID).Scan(
		&subgroup.GID,
		&subgroup.Label,
		&subgroup.ParentID,
		&subgroup.DateCreated,
	)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create subgroup: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, subgroup)
}

// ListSubgroups returns all subgroups under a parent group.
func (h *SubgroupHandler) ListSubgroups(w http.ResponseWriter, r *http.Request) {
	parentIDStr := chi.URLParam(r, "groupId")
	parentID, err := strconv.ParseInt(parentIDStr, 10, 64)
	if err != nil || parentID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid parent group ID"})
		return
	}

	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	ctx := r.Context()

	// List subgroups with instance and member counts
	query := `
		SELECT 
			g.g_id, 
			g.parent_id, 
			g.label, 
			g.date_created,
			(SELECT COUNT(*) FROM instances WHERE g_id = g.g_id) AS instances_count,
			(SELECT COUNT(*) FROM user_group_roles WHERE g_id = g.g_id) AS members_count
		FROM groups g
		WHERE g.parent_id = $1
		ORDER BY g.g_id ASC
	`
	rows, err := h.db.Query(ctx, query, parentID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query subgroups"})
		return
	}
	defer rows.Close()

	type SubgroupItem struct {
		GID            int64  `json:"g_id"`
		ParentID       int64  `json:"parent_id"`
		Label          string `json:"label"`
		DateCreated    string `json:"date_created"`
		UserRoleID     int16  `json:"user_role_id"`
		UserRoleName   string `json:"user_role_name"`
		InstancesCount int64  `json:"instances_count"`
		MembersCount   int64  `json:"members_count"`
	}

	subgroups := make([]SubgroupItem, 0)
	for rows.Next() {
		var item SubgroupItem
		var parentRef *int64
		var dateStr string

		if err := rows.Scan(&item.GID, &parentRef, &item.Label, &dateStr, &item.InstancesCount, &item.MembersCount); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse subgroup data"})
			return
		}
		if parentRef != nil {
			item.ParentID = *parentRef
		}
		item.DateCreated = dateStr

		// Resolve caller's effective role in this subgroup
		roleID, _ := middleware.GetEffectiveRoleInGroup(ctx, h.db, claims.UID, item.GID)
		item.UserRoleID = roleID
		item.UserRoleName = models.RoleName(roleID)

		subgroups = append(subgroups, item)
	}

	respondJSON(w, http.StatusOK, subgroups)
}

// GetSubgroup retrieves details of a specific subgroup.
func (h *SubgroupHandler) GetSubgroup(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	claims, ok := middleware.GetAuthUser(r.Context())
	if !ok {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}

	ctx := r.Context()

	query := `
		SELECT 
			g.g_id, 
			COALESCE(g.parent_id, 0), 
			g.label, 
			g.date_created,
			(SELECT COUNT(*) FROM instances WHERE g_id = g.g_id) AS instances_count,
			(SELECT COUNT(*) FROM user_group_roles WHERE g_id = g.g_id) AS members_count
		FROM groups g
		WHERE g.g_id = $1
	`
	var detail models.SubgroupDetail
	err = h.db.QueryRow(ctx, query, subgroupID).Scan(
		&detail.GID,
		&detail.ParentID,
		&detail.Label,
		&detail.DateCreated,
		&detail.InstancesCount,
		&detail.MembersCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Subgroup not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	roleID, _ := middleware.GetEffectiveRoleInGroup(ctx, h.db, claims.UID, subgroupID)
	detail.UserRoleID = roleID
	detail.UserRoleName = models.RoleName(roleID)

	respondJSON(w, http.StatusOK, detail)
}

// RenameSubgroup allows Admin or Subadmin to rename a subgroup.
func (h *SubgroupHandler) RenameSubgroup(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	var req models.RenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	query := `UPDATE groups SET label = $1 WHERE g_id = $2 RETURNING g_id, label, parent_id, date_created`
	var updated models.Group
	err = h.db.QueryRow(ctx, query, req.Label, subgroupID).Scan(
		&updated.GID,
		&updated.Label,
		&updated.ParentID,
		&updated.DateCreated,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Subgroup not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to rename subgroup: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, updated)
}

// DeleteSubgroup allows Admin to delete a subgroup (cascading its instances, rules, etc.).
func (h *SubgroupHandler) DeleteSubgroup(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	ctx := r.Context()
	res, err := h.db.Exec(ctx, `DELETE FROM groups WHERE g_id = $1 AND parent_id IS NOT NULL`, subgroupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete subgroup"})
		return
	}

	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Subgroup not found or cannot delete top-level group here"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Subgroup deleted successfully"})
}
