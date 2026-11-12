package models

import (
	"errors"
	"strings"
	"time"
)

type Group struct {
	GID         int64     `json:"g_id"`
	ParentID    *int64    `json:"parent_id,omitempty"`
	Label       string    `json:"label"`
	DateCreated time.Time `json:"date_created"`
}

type GroupWithRole struct {
	GID         int64     `json:"g_id"`
	ParentID    *int64    `json:"parent_id,omitempty"`
	Label       string    `json:"label"`
	DateCreated time.Time `json:"date_created"`
	RoleID      int16     `json:"role_id"`
	RoleName    string    `json:"role_name"`
}

type SubgroupDetail struct {
	GID            int64     `json:"g_id"`
	ParentID       int64     `json:"parent_id"`
	Label          string    `json:"label"`
	DateCreated    time.Time `json:"date_created"`
	UserRoleID     int16     `json:"user_role_id"`
	UserRoleName   string    `json:"user_role_name"`
	InstancesCount int64     `json:"instances_count"`
	MembersCount   int64     `json:"members_count"`
}

type CreateGroupRequest struct {
	Label string `json:"label"`
}

func (r *CreateGroupRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("group label is required and cannot be empty")
	}
	if len(r.Label) > 100 {
		return errors.New("group label cannot exceed 100 characters")
	}
	return nil
}

type CreateSubgroupRequest struct {
	Label string `json:"label"`
}

func (r *CreateSubgroupRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("subgroup label is required and cannot be empty")
	}
	if len(r.Label) > 100 {
		return errors.New("subgroup label cannot exceed 100 characters")
	}
	return nil
}

type RenameRequest struct {
	Label string `json:"label"`
}

func (r *RenameRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("new label is required and cannot be empty")
	}
	if len(r.Label) > 100 {
		return errors.New("label cannot exceed 100 characters")
	}
	return nil
}

type GroupMember struct {
	UID        int64     `json:"u_id"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	RoleID     int16     `json:"role_id"`
	RoleName   string    `json:"role_name"`
	DateJoined time.Time `json:"date_joined,omitempty"`
}

type AdmitMemberRequest struct {
	Email  string `json:"email,omitempty"`
	UID    int64  `json:"u_id,omitempty"`
	Role   string `json:"role,omitempty"`    // "admin", "subadmin", "viewer"
	RoleID int16  `json:"role_id,omitempty"` // 1, 2, 3
}

func (r *AdmitMemberRequest) ValidateAndResolveRole() (int16, error) {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if r.Email == "" && r.UID <= 0 {
		return 0, errors.New("either email or u_id must be provided")
	}

	if r.RoleID != 0 {
		if !IsValidRoleID(r.RoleID) {
			return 0, errors.New("invalid role_id: must be 1 (admin), 2 (subadmin), or 3 (viewer)")
		}
		return r.RoleID, nil
	}

	if r.Role != "" {
		roleID, err := ParseRole(strings.ToLower(strings.TrimSpace(r.Role)))
		if err != nil {
			return 0, err
		}
		return roleID, nil
	}

	// Default role is viewer (3)
	return RoleViewer, nil
}

type UpdateMemberRoleRequest struct {
	Role   string `json:"role,omitempty"`
	RoleID int16  `json:"role_id,omitempty"`
}

func (r *UpdateMemberRoleRequest) ValidateAndResolveRole() (int16, error) {
	if r.RoleID != 0 {
		if !IsValidRoleID(r.RoleID) {
			return 0, errors.New("invalid role_id: must be 1 (admin), 2 (subadmin), or 3 (viewer)")
		}
		return r.RoleID, nil
	}

	if r.Role != "" {
		roleID, err := ParseRole(strings.ToLower(strings.TrimSpace(r.Role)))
		if err != nil {
			return 0, err
		}
		return roleID, nil
	}

	return 0, errors.New("role or role_id is required")
}
