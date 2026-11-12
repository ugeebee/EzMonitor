package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type InstanceHandler struct {
	db *pgxpool.Pool
}

func NewInstanceHandler(db *pgxpool.Pool) *InstanceHandler {
	return &InstanceHandler{db: db}
}

// AddInstance allows Admin or Subadmin to add an instance to a specific subgroup.
// Returns the instance ID and the secret pairing token for the agent.
func (h *InstanceHandler) AddInstance(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	var req models.AddInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// Verify subgroup exists
	var groupExists bool
	err = h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE g_id = $1)`, subgroupID).Scan(&groupExists)
	if err != nil || !groupExists {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Subgroup not found"})
		return
	}

	// Generate a cryptographically secure random secret pairing token (32 hex characters)
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate pairing token"})
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)

	// Hash token with SHA-256 for storage (token_hash)
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])

	query := `
		INSERT INTO instances (g_id, label, fingerprint, token_hash, cpu_model, ram_config, gpu_model, enabled, status)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), true, 'unknown')
		RETURNING i_id, g_id, label, fingerprint, status, enabled, date_created
	`

	var resp models.InstanceCreatedResponse
	err = h.db.QueryRow(ctx, query,
		subgroupID,
		req.Label,
		req.Fingerprint,
		tokenHash,
		req.CPUModel,
		req.RAMConfig,
		req.GPUModel,
	).Scan(
		&resp.IID,
		&resp.GID,
		&resp.Label,
		&resp.Fingerprint,
		&resp.Status,
		&resp.Enabled,
		&resp.DateCreated,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// 23505: unique_violation (e.g. fingerprint or (g_id, label))
			if pgErr.Code == "23505" {
				if pgErr.ConstraintName == "instances_fingerprint_key" {
					respondJSON(w, http.StatusConflict, map[string]string{"error": "An instance with this hardware fingerprint is already registered"})
					return
				}
				respondJSON(w, http.StatusConflict, map[string]string{"error": "An instance with this label already exists in this subgroup"})
				return
			}
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to register instance: " + err.Error()})
		return
	}

	// Plaintext token returned ONCE so operator can copy it into agent setup
	resp.Token = rawToken

	respondJSON(w, http.StatusCreated, resp)
}

// ListInstances lists all instances in a subgroup.
func (h *InstanceHandler) ListInstances(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	ctx := r.Context()
	query := `
		SELECT i_id, g_id, label, fingerprint, cpu_model, ram_config, gpu_model, enabled, status, date_created, last_seen_at
		FROM instances
		WHERE g_id = $1
		ORDER BY i_id ASC
	`
	rows, err := h.db.Query(ctx, query, subgroupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query instances"})
		return
	}
	defer rows.Close()

	instances := make([]models.Instance, 0)
	for rows.Next() {
		var inst models.Instance
		if err := rows.Scan(
			&inst.IID,
			&inst.GID,
			&inst.Label,
			&inst.Fingerprint,
			&inst.CPUModel,
			&inst.RAMConfig,
			&inst.GPUModel,
			&inst.Enabled,
			&inst.Status,
			&inst.DateCreated,
			&inst.LastSeenAt,
		); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse instance data"})
			return
		}
		instances = append(instances, inst)
	}

	respondJSON(w, http.StatusOK, instances)
}

// RenameInstance allows Admin or Subadmin to rename an instance.
func (h *InstanceHandler) RenameInstance(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	instanceIDStr := chi.URLParam(r, "instanceId")
	instanceID, err := strconv.ParseInt(instanceIDStr, 10, 64)
	if err != nil || instanceID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid instance ID"})
		return
	}

	var req models.RenameInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	query := `
		UPDATE instances 
		SET label = $1 
		WHERE i_id = $2 AND g_id = $3
		RETURNING i_id, g_id, label, fingerprint, cpu_model, ram_config, gpu_model, enabled, status, date_created, last_seen_at
	`
	var inst models.Instance
	err = h.db.QueryRow(ctx, query, req.Label, instanceID, subgroupID).Scan(
		&inst.IID,
		&inst.GID,
		&inst.Label,
		&inst.Fingerprint,
		&inst.CPUModel,
		&inst.RAMConfig,
		&inst.GPUModel,
		&inst.Enabled,
		&inst.Status,
		&inst.DateCreated,
		&inst.LastSeenAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "An instance with this label already exists in this subgroup"})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found in this subgroup"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to rename instance: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, inst)
}

// DeleteInstance allows Admin or Subadmin to remove an instance.
func (h *InstanceHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	instanceIDStr := chi.URLParam(r, "instanceId")
	instanceID, err := strconv.ParseInt(instanceIDStr, 10, 64)
	if err != nil || instanceID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid instance ID"})
		return
	}

	ctx := r.Context()
	res, err := h.db.Exec(ctx, `DELETE FROM instances WHERE i_id = $1 AND g_id = $2`, instanceID, subgroupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete instance"})
		return
	}

	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found in this subgroup"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Instance deleted successfully"})
}

// GetInstanceMetrics allows Viewers, Subadmins, and Admins to inspect time-series measurements.
func (h *InstanceHandler) GetInstanceMetrics(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	instanceIDStr := chi.URLParam(r, "instanceId")
	instanceID, err := strconv.ParseInt(instanceIDStr, 10, 64)
	if err != nil || instanceID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid instance ID"})
		return
	}

	// Verify instance belongs to this subgroup
	ctx := r.Context()
	var exists bool
	err = h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE i_id = $1 AND g_id = $2)`, instanceID, subgroupID).Scan(&exists)
	if err != nil || !exists {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found in this subgroup"})
		return
	}

	limit := 60
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	query := `
		SELECT recorded_at, cpu_util, gpu_util, ram_util, uptime, net_up_kbps, net_down_kbps
		FROM metrics_data
		WHERE i_id = $1
		ORDER BY recorded_at DESC
		LIMIT $2
	`
	rows, err := h.db.Query(ctx, query, instanceID, limit)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to query metrics: " + err.Error()})
		return
	}
	defer rows.Close()

	samples := make([]models.MetricSample, 0)
	for rows.Next() {
		var s models.MetricSample
		var cpu, ram, netUp, netDown float32
		var gpu *float32
		var uptime int64
		var recordedAt time.Time

		if err := rows.Scan(&recordedAt, &cpu, &gpu, &ram, &uptime, &netUp, &netDown); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse metric sample"})
			return
		}

		s.RecordedAt = recordedAt
		s.CPUUtil = float64(cpu)
		if gpu != nil {
			g := float64(*gpu)
			s.GPUUtil = &g
		}
		s.RAMUtil = float64(ram)
		s.Uptime = uptime
		s.NetUpKBPS = float64(netUp)
		s.NetDownKBPS = float64(netDown)

		samples = append(samples, s)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"i_id":     instanceID,
		"g_id":     subgroupID,
		"count":    len(samples),
		"samples":  samples,
	})
}
