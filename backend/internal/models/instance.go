package models

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	// fingerprintRegex: 16 hex chars (matching collector/fingerprint.go and schema)
	fingerprintRegex = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

type Instance struct {
	IID         int64      `json:"i_id"`
	GID         int64      `json:"g_id"`
	Label       string     `json:"label"`
	Fingerprint string     `json:"fingerprint"`
	CPUModel    *string    `json:"cpu_model,omitempty"`
	RAMConfig   *string    `json:"ram_config,omitempty"`
	GPUModel    *string    `json:"gpu_model,omitempty"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"` // unknown, up, down
	DateCreated time.Time  `json:"date_created"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

type AddInstanceRequest struct {
	Label       string `json:"label"`
	Fingerprint string `json:"fingerprint"`
	CPUModel    string `json:"cpu_model,omitempty"`
	RAMConfig   string `json:"ram_config,omitempty"`
	GPUModel    string `json:"gpu_model,omitempty"`
}

func (r *AddInstanceRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("instance label is required")
	}
	if len(r.Label) > 100 {
		return errors.New("instance label cannot exceed 100 characters")
	}

	r.Fingerprint = strings.ToLower(strings.TrimSpace(r.Fingerprint))
	if !fingerprintRegex.MatchString(r.Fingerprint) {
		return errors.New("invalid fingerprint format: must be 16 lowercase hex characters (e.g. from agent setup)")
	}

	return nil
}

type RenameInstanceRequest struct {
	Label string `json:"label"`
}

func (r *RenameInstanceRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("instance label is required and cannot be empty")
	}
	if len(r.Label) > 100 {
		return errors.New("instance label cannot exceed 100 characters")
	}
	return nil
}

type InstanceCreatedResponse struct {
	IID         int64     `json:"i_id"`
	GID         int64     `json:"g_id"`
	Label       string    `json:"label"`
	Fingerprint string    `json:"fingerprint"`
	Token       string    `json:"token"` // Secret token returned ONCE to pair the agent
	Status      string    `json:"status"`
	Enabled     bool      `json:"enabled"`
	DateCreated time.Time `json:"date_created"`
}

