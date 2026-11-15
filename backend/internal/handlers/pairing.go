package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
	"github.com/ugeebee/EzMonitor/backend/internal/pairing"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow agent CLI connection from anywhere
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type PairingHandler struct {
	db  *pgxpool.Pool
	hub *pairing.Hub
}

func NewPairingHandler(db *pgxpool.Pool, hub *pairing.Hub) *PairingHandler {
	return &PairingHandler{
		db:  db,
		hub: hub,
	}
}

// AgentWebSocket handles the incoming pairing connection from the agent CLI.
// It keeps the connection alive for up to 5 minutes waiting for Admin verification.
func (h *PairingHandler) AgentWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// 1. Read initial handshake message from Agent
	var initMsg models.AgentInitMessage
	if err := conn.ReadJSON(&initMsg); err != nil {
		log.Printf("Failed to read agent init message: %v", err)
		_ = conn.WriteJSON(map[string]string{
			"type":  models.WSMsgTypeError,
			"error": "Expected initial JSON message with fingerprint and OTP",
		})
		return
	}

	if initMsg.Type != models.WSMsgTypeInit || initMsg.Fingerprint == "" || initMsg.OTP == "" {
		_ = conn.WriteJSON(map[string]string{
			"type":  models.WSMsgTypeError,
			"error": "Invalid init message: type, fingerprint, and otp are required",
		})
		return
	}

	// 2. Register session in Hub with 5-minute timeout
	timeout := 5 * time.Minute
	session := &pairing.AgentSession{
		Fingerprint: initMsg.Fingerprint,
		OTP:         initMsg.OTP,
		CPUModel:    initMsg.CPUModel,
		RAMConfig:   initMsg.RAMConfig,
		GPUModel:    initMsg.GPUModel,
		Conn:        conn,
		ExpiresAt:   time.Now().Add(timeout),
		PairedChan:  make(chan models.ServerPairedMessage, 1),
		DoneChan:    make(chan struct{}),
	}
	h.hub.Register(session)
	defer h.hub.Unregister(initMsg.Fingerprint)

	log.Printf("[Pairing] Agent registered (Fingerprint: %s, OTP: %s, Expires in 5m)", initMsg.Fingerprint, initMsg.OTP)

	// Notify agent that registration is received and timer started
	_ = conn.WriteJSON(map[string]interface{}{
		"type":       "waiting",
		"message":    "Fingerprint and OTP registered. Waiting for admin approval.",
		"expires_in": int(timeout.Seconds()),
	})

	// 3. Wait for Admin approval, timeout, or disconnect
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Listen for possible client close in background
	closeChan := make(chan struct{})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(closeChan)
				return
			}
		}
	}()

	select {
	case pairedMsg := <-session.PairedChan:
		// Admin verified OTP! Send instance_id and token to agent
		log.Printf("[Pairing] Successfully verified agent %s! Sending Instance ID %d", initMsg.Fingerprint, pairedMsg.InstanceID)
		_ = conn.WriteJSON(pairedMsg)

		// Give agent a brief moment to process before closing
		time.Sleep(500 * time.Millisecond)

		// Send normal closure
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Pairing complete"))
		return

	case <-timer.C:
		log.Printf("[Pairing] Session timed out for agent %s", initMsg.Fingerprint)
		_ = conn.WriteJSON(map[string]string{
			"type":  models.WSMsgTypeTimeout,
			"error": "Pairing session timed out (5-minute window expired). Please restart agent setup.",
		})
		return

	case <-closeChan:
		log.Printf("[Pairing] Agent %s disconnected early", initMsg.Fingerprint)
		return
	}
}

// AdminPairInstance verifies the OTP and assigns the instance to the subgroup.
func (h *PairingHandler) AdminPairInstance(w http.ResponseWriter, r *http.Request) {
	subgroupIDStr := chi.URLParam(r, "subgroupId")
	subgroupID, err := strconv.ParseInt(subgroupIDStr, 10, 64)
	if err != nil || subgroupID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid subgroup ID"})
		return
	}

	var req models.PairInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()

	// 1. Verify subgroup exists
	var groupExists bool
	err = h.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE g_id = $1)`, subgroupID).Scan(&groupExists)
	if err != nil || !groupExists {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Subgroup not found"})
		return
	}

	// 2. Generate secret token (32 hex characters)
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to generate pairing token"})
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)
	hashBytes := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hashBytes[:])

	// 3. Check hub for waiting agent session and peek hardware metadata
	h.hub.VerifyAndComplete("", "", models.ServerPairedMessage{}) // typecheck helper (no-op)

	// First verify session exists and OTP matches before inserting into DB
	var tempMsg models.ServerPairedMessage
	session, err := h.hub.VerifyAndComplete(req.Fingerprint, req.OTP, tempMsg)
	if err != nil {
		if errors.Is(err, pairing.ErrSessionNotFound) {
			respondJSON(w, http.StatusNotFound, map[string]string{
				"error": "No active agent session found with this fingerprint. Ensure the agent CLI is currently running.",
			})
			return
		}
		if errors.Is(err, pairing.ErrSessionExpired) {
			respondJSON(w, http.StatusGone, map[string]string{
				"error": "Pairing session has expired (5-minute window). Please restart agent setup.",
			})
			return
		}
		if errors.Is(err, pairing.ErrInvalidOTP) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "Invalid OTP code. Please enter the exact 6-digit OTP displayed in the agent terminal.",
			})
			return
		}
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// 4. Insert instance into database
	query := `
		INSERT INTO instances (g_id, label, fingerprint, token_hash, cpu_model, ram_config, gpu_model, enabled, status)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), true, 'up')
		RETURNING i_id, date_created
	`

	var instanceID int64
	var dateCreated time.Time
	err = h.db.QueryRow(ctx, query,
		subgroupID,
		req.Label,
		req.Fingerprint,
		tokenHash,
		session.CPUModel,
		session.RAMConfig,
		session.GPUModel,
	).Scan(&instanceID, &dateCreated)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "instances_fingerprint_key" {
				respondJSON(w, http.StatusConflict, map[string]string{"error": "An instance with this hardware fingerprint is already registered in the system"})
				return
			}
			respondJSON(w, http.StatusConflict, map[string]string{"error": "An instance with this label already exists in this subgroup"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create instance record: " + err.Error()})
		return
	}

	// 5. Send credentials to waiting agent WebSocket
	pairedMsg := models.ServerPairedMessage{
		Type:       models.WSMsgTypePaired,
		InstanceID: instanceID,
		Token:      rawToken,
		Label:      req.Label,
		GID:        subgroupID,
	}

	// Write directly to agent connection
	_ = session.Conn.WriteJSON(pairedMsg)

	respondJSON(w, http.StatusCreated, models.PairInstanceResponse{
		IID:         instanceID,
		GID:         subgroupID,
		Label:       req.Label,
		Fingerprint: req.Fingerprint,
		Status:      "up",
		Enabled:     true,
		DateCreated: dateCreated,
		Message:     "Instance successfully verified and paired! Credentials transmitted to the agent.",
	})
}

