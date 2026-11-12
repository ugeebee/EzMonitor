package models

import (
	"fmt"
	"strings"
)

const (
	RoleAdmin    int16 = 1
	RoleSubadmin int16 = 2
	RoleViewer   int16 = 3

	RoleNameAdmin    = "admin"
	RoleNameSubadmin = "subadmin"
	RoleNameViewer   = "viewer"
)

type Role struct {
	RoleID   int16  `json:"role_id"`
	RoleName string `json:"role_name"`
}

// ParseRole parses a role name or returns an error if invalid.
// Accepts 'admin', 'subadmin', and 'viewer' (also accepts 'editor' as legacy alias for subadmin).
func ParseRole(name string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case RoleNameAdmin:
		return RoleAdmin, nil
	case RoleNameSubadmin, "editor":
		return RoleSubadmin, nil
	case RoleNameViewer:
		return RoleViewer, nil
	default:
		return 0, fmt.Errorf("invalid role name: '%s', expected 'admin', 'subadmin', or 'viewer'", name)
	}
}

// RoleName returns the string representation of a role ID.
func RoleName(roleID int16) string {
	switch roleID {
	case RoleAdmin:
		return RoleNameAdmin
	case RoleSubadmin:
		return RoleNameSubadmin
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
