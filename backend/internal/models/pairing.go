package models

import (
	"errors"
	"strings"
	"time"
)

// WebSocket message types between Agent and Server
const (
	WSMsgTypeInit    = "init"
	WSMsgTypePaired  = "paired"
	WSMsgTypeAck     = "ack"
	WSMsgTypeError   = "error"
	WSMsgTypeTimeout = "timeout"
)

// AgentInitMessage is sent by the agent when opening the WebSocket.
type AgentInitMessage struct {
	Type        string `json:"type"` // "init"
	Fingerprint string `json:"fingerprint"`
	OTP         string `json:"otp"`
	CPUModel    string `json:"cpu_model,omitempty"`
	RAMConfig   string `json:"ram_config,omitempty"`
	GPUModel    string `json:"gpu_model,omitempty"`
}

// ServerPairedMessage is sent by the server to the agent once the admin verifies the OTP.
type ServerPairedMessage struct {
	Type       string `json:"type"` // "paired"
	InstanceID int64  `json:"instance_id"`
	Token      string `json:"token"`
	Label      string `json:"label"`
	GID        int64  `json:"g_id"`
}

// PairInstanceRequest is sent by Admin/Subadmin via REST API to verify OTP and pair the instance.
type PairInstanceRequest struct {
	Label       string `json:"label"`
	Fingerprint string `json:"fingerprint"`
	OTP         string `json:"otp"`
}

func (r *PairInstanceRequest) Validate() error {
	r.Label = strings.TrimSpace(r.Label)
	if r.Label == "" {
		return errors.New("instance label is required")
	}
	if len(r.Label) > 100 {
		return errors.New("instance label cannot exceed 100 characters")
	}

	r.Fingerprint = strings.ToLower(strings.TrimSpace(r.Fingerprint))
	if !fingerprintRegex.MatchString(r.Fingerprint) {
		return errors.New("invalid fingerprint format: must be 16 lowercase hex characters")
	}

	r.OTP = strings.TrimSpace(r.OTP)
	if r.OTP == "" {
		return errors.New("pairing OTP is required")
	}
	if len(r.OTP) != 6 {
		return errors.New("pairing OTP must be exactly 6 characters")
	}

	return nil
}

type PairInstanceResponse struct {
	IID         int64     `json:"i_id"`
	GID         int64     `json:"g_id"`
	Label       string    `json:"label"`
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"`
	Enabled     bool      `json:"enabled"`
	DateCreated time.Time `json:"date_created"`
	Message     string    `json:"message"`
}

