package models

import "fmt"

const (
	RoleAdmin  int16 = 1
	RoleEditor int16 = 2
	RoleViewer int16 = 3

	RoleNameAdmin  = "admin"
	RoleNameEditor = "editor"
	RoleNameViewer = "viewer"
)

type Role struct {
	RoleID   int16  `json:"role_id"`
	RoleName string `json:"role_name"`
}

// ParseRole parses a role name or returns an error if invalid.
func ParseRole(name string) (int16, error) {
	switch name {
	case RoleNameAdmin:
		return RoleAdmin, nil
	case RoleNameEditor:
		return RoleEditor, nil
	case RoleNameViewer:
		return RoleViewer, nil
	default:
		return 0, fmt.Errorf("invalid role name: '%s', expected 'admin', 'editor', or 'viewer'", name)
	}
}

// RoleName returns the string representation of a role ID.
func RoleName(roleID int16) string {
	switch roleID {
	case RoleAdmin:
		return RoleNameAdmin
	case RoleEditor:
		return RoleNameEditor
	case RoleViewer:
		return RoleNameViewer
	default:
		return "unknown"
	}
}

// IsValidRoleID checks if a role ID is valid.
func IsValidRoleID(roleID int16) bool {
	return roleID >= RoleAdmin && roleID <= RoleViewer
}

