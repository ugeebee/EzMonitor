package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type IngestHandler struct {
	db *pgxpool.Pool
}

func NewIngestHandler(db *pgxpool.Pool) *IngestHandler {
	return &IngestHandler{db: db}
}

// IngestBatch receives a batch of metrics from an agent daemon and writes them to metrics_data.
func (h *IngestHandler) IngestBatch(w http.ResponseWriter, r *http.Request) {
	var req models.IngestBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if req.InstanceID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid or missing instance ID"})
		return
	}

	if len(req.Samples) == 0 {
		respondJSON(w, http.StatusOK, map[string]string{"message": "No samples in batch"})
		return
	}

	// Extract token from Header or Payload
	token := req.Token
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = parts[1]
		}
	}

	if token == "" {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing agent secret token"})
		return
	}

	// Compute hex SHA-256 of the plaintext token
	hBytes := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hBytes[:])

	ctx := r.Context()

	// 1. Authenticate instance: SELECT i_id, enabled FROM instances WHERE i_id = $1 AND token_hash = $2
	var instanceID int64
	var enabled bool
	authQuery := `SELECT i_id, enabled FROM instances WHERE i_id = $1 AND token_hash = $2`
	err := h.db.QueryRow(ctx, authQuery, req.InstanceID, tokenHash).Scan(&instanceID, &enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid instance ID or secret token"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to authenticate agent"})
		return
	}

	if !enabled {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "Instance is disabled (Trigger T1 rejection)"})
		return
	}

	// 2. Prepare unnest arrays for batch insert
	n := len(req.Samples)
	recordedAtArr := make([]time.Time, n)
	cpuUtilArr := make([]float32, n)
	gpuUtilArr := make([]*float32, n)
	ramUtilArr := make([]float32, n)
	uptimeArr := make([]int64, n)
	netUpArr := make([]float32, n)
	netDownArr := make([]float32, n)

	for i, s := range req.Samples {
		recordedAtArr[i] = s.RecordedAt
		cpuUtilArr[i] = float32(s.CPUUtil)
		if s.GPUUtil != nil {
			g := float32(*s.GPUUtil)
			gpuUtilArr[i] = &g
		} else {
			gpuUtilArr[i] = nil
		}
		ramUtilArr[i] = float32(s.RAMUtil)
		uptimeArr[i] = s.Uptime
		netUpArr[i] = float32(s.NetUpKBPS)
		netDownArr[i] = float32(s.NetDownKBPS)
	}

	// 3. Batch insert using UNNEST with ON CONFLICT DO UPDATE (BR4 semantics)
	upsertQuery := `
		INSERT INTO metrics_data (i_id, recorded_at, cpu_util, gpu_util, ram_util, uptime, net_up_kbps, net_down_kbps)
		SELECT $1::bigint, * FROM unnest(
			$2::timestamptz[], 
			$3::real[], 
			$4::real[], 
			$5::real[],
			$6::bigint[], 
			$7::real[], 
			$8::real[]
		)
		ON CONFLICT (i_id, recorded_at) DO UPDATE SET
			cpu_util = EXCLUDED.cpu_util,
			gpu_util = EXCLUDED.gpu_util,
			ram_util = EXCLUDED.ram_util,
			uptime = EXCLUDED.uptime,
			net_up_kbps = EXCLUDED.net_up_kbps,
			net_down_kbps = EXCLUDED.net_down_kbps;
	`

	_, err = h.db.Exec(ctx, upsertQuery,
		req.InstanceID,
		recordedAtArr,
		cpuUtilArr,
		gpuUtilArr,
		ramUtilArr,
		uptimeArr,
		netUpArr,
		netDownArr,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			// SQLSTATE 23514: check_violation (bad value, disabled instance, or missing partition)
			if pgErr.Code == "23514" {
				respondJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"error":  "Constraint violation: " + pgErr.Message,
					"detail": pgErr.Detail,
				})
				return
			}
			// SQLSTATE 23503: foreign_key_violation
			if pgErr.Code == "23503" {
				respondJSON(w, http.StatusNotFound, map[string]string{"error": "Foreign key not found"})
				return
			}
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to persist metrics batch: " + err.Error()})
		return
	}

	// 4. Update instance last_seen_at and status = 'up'
	updateQuery := `UPDATE instances SET last_seen_at = now(), status = 'up' WHERE i_id = $1`
	_, _ = h.db.Exec(ctx, updateQuery, req.InstanceID)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "success",
		"ingested": len(req.Samples),
	})
}

