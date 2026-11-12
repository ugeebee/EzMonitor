package models_test

import (
	"testing"

	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

func TestRoleParsingAndMapping(t *testing.T) {
	// Name to ID
	adminID, err := models.ParseRole("admin")
	if err != nil || adminID != models.RoleAdmin {
		t.Errorf("Expected role ID %d for admin, got %d (err: %v)", models.RoleAdmin, adminID, err)
	}

	editorID, err := models.ParseRole("editor")
	if err != nil || editorID != models.RoleEditor {
		t.Errorf("Expected role ID %d for editor, got %d (err: %v)", models.RoleEditor, editorID, err)
	}

	viewerID, err := models.ParseRole("viewer")
	if err != nil || viewerID != models.RoleViewer {
		t.Errorf("Expected role ID %d for viewer, got %d (err: %v)", models.RoleViewer, viewerID, err)
	}

	// Invalid name
	_, err = models.ParseRole("superadmin")
	if err == nil {
		t.Errorf("Expected error parsing invalid role 'superadmin'")
	}

	// ID to Name
	if models.RoleName(models.RoleAdmin) != "admin" {
		t.Errorf("Expected role name 'admin'")
	}
	if models.RoleName(models.RoleEditor) != "editor" {
		t.Errorf("Expected role name 'editor'")
	}
	if models.RoleName(models.RoleViewer) != "viewer" {
		t.Errorf("Expected role name 'viewer'")
	}
	if models.RoleName(99) != "unknown" {
		t.Errorf("Expected 'unknown' for invalid role ID")
	}

	// Validation
	if !models.IsValidRoleID(1) || !models.IsValidRoleID(2) || !models.IsValidRoleID(3) {
		t.Errorf("Expected roles 1, 2, 3 to be valid")
	}
	if models.IsValidRoleID(0) || models.IsValidRoleID(4) {
		t.Errorf("Expected role 0 and 4 to be invalid")
	}
}

func TestRegisterRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     models.RegisterRequest
		wantErr bool
	}{
		{
			name: "Valid user",
			req: models.RegisterRequest{
				Name:     "Alice Smith",
				Email:    "alice@example.com",
				Password: "password123",
			},
			wantErr: false,
		},
		{
			name: "Empty name",
			req: models.RegisterRequest{
				Name:     "   ",
				Email:    "alice@example.com",
				Password: "password123",
			},
			wantErr: true,
		},
		{
			name: "Invalid email",
			req: models.RegisterRequest{
				Name:     "Alice Smith",
				Email:    "not-an-email",
				Password: "password123",
			},
			wantErr: true,
		},
		{
			name: "Short password",
			req: models.RegisterRequest{
				Name:     "Alice Smith",
				Email:    "alice@example.com",
				Password: "123",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestGroupAndMemberRequests(t *testing.T) {
	// Create group
	grpReq := models.CreateGroupRequest{Label: "   "}
	if err := grpReq.Validate(); err == nil {
		t.Errorf("Expected empty group label to fail validation")
	}

	validGrp := models.CreateGroupRequest{Label: "DevOps Team"}
	if err := validGrp.Validate(); err != nil {
		t.Errorf("Expected valid label to pass: %v", err)
	}

	// Admit member by role name
	admitReq := models.AdmitMemberRequest{
		Email: "bob@example.com",
		Role:  "editor",
	}
	roleID, err := admitReq.ValidateAndResolveRole()
	if err != nil || roleID != models.RoleEditor {
		t.Errorf("Expected role editor (2), got %d (err: %v)", roleID, err)
	}

	// Admit member by default role (viewer)
	admitDefault := models.AdmitMemberRequest{
		Email: "charlie@example.com",
	}
	roleID, err = admitDefault.ValidateAndResolveRole()
	if err != nil || roleID != models.RoleViewer {
		t.Errorf("Expected default role viewer (3), got %d (err: %v)", roleID, err)
	}

	// Admit member with invalid role
	admitInvalid := models.AdmitMemberRequest{
		Email: "dave@example.com",
		Role:  "invalid-role",
	}
	_, err = admitInvalid.ValidateAndResolveRole()
	if err == nil {
		t.Errorf("Expected invalid role to fail")
	}

	// Update role
	updateReq := models.UpdateMemberRoleRequest{Role: "admin"}
	roleID, err = updateReq.ValidateAndResolveRole()
	if err != nil || roleID != models.RoleAdmin {
		t.Errorf("Expected role admin (1), got %d (err: %v)", roleID, err)
	}
}

