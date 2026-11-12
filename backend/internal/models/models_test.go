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

	subadminID, err := models.ParseRole("subadmin")
	if err != nil || subadminID != models.RoleSubadmin {
		t.Errorf("Expected role ID %d for subadmin, got %d (err: %v)", models.RoleSubadmin, subadminID, err)
	}

	// Legacy alias "editor"
	editorID, err := models.ParseRole("editor")
	if err != nil || editorID != models.RoleSubadmin {
		t.Errorf("Expected legacy alias 'editor' to resolve to subadmin (%d), got %d", models.RoleSubadmin, editorID)
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
	if models.RoleName(models.RoleSubadmin) != "subadmin" {
		t.Errorf("Expected role name 'subadmin'")
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

func TestSubgroupAndInstanceRequests(t *testing.T) {
	// Subgroup request
	subReq := models.CreateSubgroupRequest{Label: "   "}
	if err := subReq.Validate(); err == nil {
		t.Errorf("Expected empty subgroup label to fail")
	}

	subReqValid := models.CreateSubgroupRequest{Label: "Core Services"}
	if err := subReqValid.Validate(); err != nil {
		t.Errorf("Expected valid subgroup label to pass: %v", err)
	}

	// Rename request
	renameReq := models.RenameRequest{Label: "Updated Label"}
	if err := renameReq.Validate(); err != nil {
		t.Errorf("Expected valid rename request to pass: %v", err)
	}

	// Admit member with subadmin role
	admitReq := models.AdmitMemberRequest{
		Email: "bob@example.com",
		Role:  "subadmin",
	}
	roleID, err := admitReq.ValidateAndResolveRole()
	if err != nil || roleID != models.RoleSubadmin {
		t.Errorf("Expected role subadmin (2), got %d (err: %v)", roleID, err)
	}

	// Add instance validation
	instReq := models.AddInstanceRequest{
		Label:       "worker-01",
		Fingerprint: "0123456789abcdef", // 16 hex
	}
	if err := instReq.Validate(); err != nil {
		t.Errorf("Expected valid instance request to pass: %v", err)
	}

	instReqBadFP := models.AddInstanceRequest{
		Label:       "worker-01",
		Fingerprint: "bad-fingerprint",
	}
	if err := instReqBadFP.Validate(); err == nil {
		t.Errorf("Expected invalid fingerprint to fail")
	}

	// Rename instance
	renameInst := models.RenameInstanceRequest{Label: "new-box-name"}
	if err := renameInst.Validate(); err != nil {
		t.Errorf("Expected rename instance to pass: %v", err)
	}
}
