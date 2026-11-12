package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type MemberHandler struct {
	db *pgxpool.Pool
}

func NewMemberHandler(db *pgxpool.Pool) *MemberHandler {
	return &MemberHandler{db: db}
}

func getTargetGroupID(r *http.Request) (int64, error) {
	idStr := chi.URLParam(r, "subgroupId")
	if idStr == "" {
		idStr = chi.URLParam(r, "groupId")
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid group or subgroup ID")
	}
	return id, nil
}

// ListMembers returns all users belonging to a group/subgroup along with their roles.
func (h *MemberHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT u.u_id, u.name, u.email, ugr.role_id, r.role_name
		FROM user_group_roles ugr
		JOIN users u ON ugr.u_id = u.u_id
		JOIN roles r ON ugr.role_id = r.role_id
		WHERE ugr.g_id = $1
		ORDER BY ugr.role_id ASC, u.name ASC
	`
	rows, err := h.db.Query(r.Context(), query, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query group members"})
		return
	}
	defer rows.Close()

	members := make([]models.GroupMember, 0)
	for rows.Next() {
		var m models.GroupMember
		if err := rows.Scan(&m.UID, &m.Name, &m.Email, &m.RoleID, &m.RoleName); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse member data"})
			return
		}
		members = append(members, m)
	}

	respondJSON(w, http.StatusOK, members)
}

// AdmitMember allows a group Owner (Admin) or Subadmin to admit a user to the group/subgroup and assign them a role.
func (h *MemberHandler) AdmitMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req models.AdmitMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	roleID, err := req.ValidateAndResolveRole()
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// 1. Find target user
	var targetUser models.User
	if req.UID > 0 {
		queryUser := `SELECT u_id, name, email FROM users WHERE u_id = $1`
		err = h.db.QueryRow(ctx, queryUser, req.UID).Scan(&targetUser.UID, &targetUser.Name, &targetUser.Email)
	} else {
		queryUser := `SELECT u_id, name, email FROM users WHERE email = $1`
		err = h.db.QueryRow(ctx, queryUser, req.Email).Scan(&targetUser.UID, &targetUser.Name, &targetUser.Email)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{
				"error": "User not found. The user must first register an account before being admitted.",
			})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error looking up user"})
		return
	}

	// 2. Insert or update user_group_roles
	// Using ON CONFLICT to update role if user is already in the group
	queryInsert := `
		INSERT INTO user_group_roles (u_id, g_id, role_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (u_id, g_id) DO UPDATE SET role_id = EXCLUDED.role_id
		RETURNING role_id
	`

	var assignedRoleID int16
	err = h.db.QueryRow(ctx, queryInsert, targetUser.UID, groupID, roleID).Scan(&assignedRoleID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to admit user to group: %v", err)})
		return
	}

	respondJSON(w, http.StatusOK, models.GroupMember{
		UID:      targetUser.UID,
		Name:     targetUser.Name,
		Email:    targetUser.Email,
		RoleID:   assignedRoleID,
		RoleName: models.RoleName(assignedRoleID),
	})
}

// UpdateMemberRole allows a group Owner (Admin) or Subadmin to change a member's role (admin, subadmin, viewer).
func (h *MemberHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	targetUIDStr := chi.URLParam(r, "userId")
	targetUID, err := strconv.ParseInt(targetUIDStr, 10, 64)
	if err != nil || targetUID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	var req models.UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	newRoleID, err := req.ValidateAndResolveRole()
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Check if target member exists in group
	var currentRoleID int16
	err = h.db.QueryRow(ctx, `SELECT role_id FROM user_group_roles WHERE u_id = $1 AND g_id = $2`, targetUID, groupID).Scan(&currentRoleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "User is not a member of this group"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	// Guard: If demoting an admin, ensure at least one other admin remains
	if currentRoleID == models.RoleAdmin && newRoleID != models.RoleAdmin {
		var adminCount int
		err = h.db.QueryRow(ctx, `SELECT COUNT(*) FROM user_group_roles WHERE g_id = $1 AND role_id = $2`, groupID, models.RoleAdmin).Scan(&adminCount)
		if err == nil && adminCount <= 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Cannot demote the only admin of this group"})
			return
		}
	}

	// Update role
	queryUpdate := `
		UPDATE user_group_roles 
		SET role_id = $1 
		WHERE u_id = $2 AND g_id = $3
	`
	_, err = h.db.Exec(ctx, queryUpdate, newRoleID, targetUID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update member role"})
		return
	}

	// Fetch updated user info
	var member models.GroupMember
	err = h.db.QueryRow(ctx, `
		SELECT u.u_id, u.name, u.email, ugr.role_id, r.role_name
		FROM user_group_roles ugr
		JOIN users u ON ugr.u_id = u.u_id
		JOIN roles r ON ugr.role_id = r.role_id
		WHERE ugr.u_id = $1 AND ugr.g_id = $2
	`, targetUID, groupID).Scan(&member.UID, &member.Name, &member.Email, &member.RoleID, &member.RoleName)

	if err != nil {
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"message": "Role updated successfully",
			"u_id":    targetUID,
			"role_id": newRoleID,
			"role":    models.RoleName(newRoleID),
		})
		return
	}

	respondJSON(w, http.StatusOK, member)
}

// RemoveMember removes a user from a group/subgroup.
func (h *MemberHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	targetUIDStr := chi.URLParam(r, "userId")
	targetUID, err := strconv.ParseInt(targetUIDStr, 10, 64)
	if err != nil || targetUID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid user ID"})
		return
	}

	ctx := r.Context()

	// Guard: Cannot remove last admin
	var currentRoleID int16
	err = h.db.QueryRow(ctx, `SELECT role_id FROM user_group_roles WHERE u_id = $1 AND g_id = $2`, targetUID, groupID).Scan(&currentRoleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "User is not a member of this group"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Database error"})
		return
	}

	if currentRoleID == models.RoleAdmin {
		var adminCount int
		err = h.db.QueryRow(ctx, `SELECT COUNT(*) FROM user_group_roles WHERE g_id = $1 AND role_id = $2`, groupID, models.RoleAdmin).Scan(&adminCount)
		if err == nil && adminCount <= 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Cannot remove the only admin of this group"})
			return
		}
	}

	// Delete from user_group_roles
	_, err = h.db.Exec(ctx, `DELETE FROM user_group_roles WHERE u_id = $1 AND g_id = $2`, targetUID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to remove user from group"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Member removed from group successfully"})
}

// RequestToJoin allows any authenticated user to request admission to a group.
func (h *MemberHandler) RequestToJoin(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()

	// Check if group exists
	var groupLabel string
	err = h.db.QueryRow(ctx, `SELECT label FROM groups WHERE g_id = $1`, groupID).Scan(&groupLabel)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Group not found"})
		return
	}

	// Check if already a member
	var existingRole int16
	err = h.db.QueryRow(ctx, `SELECT role_id FROM user_group_roles WHERE u_id = $1 AND g_id = $2`, claims.UID, groupID).Scan(&existingRole)
	if err == nil {
		respondJSON(w, http.StatusConflict, map[string]string{
			"error": fmt.Sprintf("You are already a member of this group with role: %s", models.RoleName(existingRole)),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Join request noted. Please ask the group owner/admin to admit your account.",
		"g_id":     groupID,
		"group":    groupLabel,
		"u_id":     claims.UID,
		"email":    claims.Email,
		"status":   "pending_admittance",
	})
}

