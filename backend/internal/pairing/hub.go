package pairing

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

var (
	ErrSessionNotFound = errors.New("no active agent session found with this fingerprint (make sure the agent is running)")
	ErrSessionExpired  = errors.New("pairing session has expired (5-minute limit exceeded)")
	ErrInvalidOTP      = errors.New("invalid pairing OTP code")
)

type AgentSession struct {
	Fingerprint string
	OTP         string
	CPUModel    string
	RAMConfig   string
	GPUModel    string
	Conn        *websocket.Conn
	ExpiresAt   time.Time
	PairedChan  chan models.ServerPairedMessage
	DoneChan    chan struct{}
}

type Hub struct {
	mu       sync.RWMutex
	sessions map[string]*AgentSession
}

func NewHub() *Hub {
	h := &Hub{
		sessions: make(map[string]*AgentSession),
	}
	// Periodically clean up expired sessions
	go h.cleanupLoop()
	return h
}

// Register adds a new waiting agent session.
func (h *Hub) Register(session *AgentSession) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// If a previous session exists for this fingerprint, close it
	if old, exists := h.sessions[session.Fingerprint]; exists {
		close(old.DoneChan)
		_ = old.Conn.Close()
	}

	h.sessions[session.Fingerprint] = session
}

// Unregister removes a session.
func (h *Hub) Unregister(fingerprint string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.sessions, fingerprint)
}

// VerifyAndComplete verifies the OTP and transmits the paired credentials to the waiting agent.
func (h *Hub) VerifyAndComplete(fingerprint, otp string, pairedMsg models.ServerPairedMessage) (*AgentSession, error) {
	h.mu.RLock()
	session, exists := h.sessions[fingerprint]
	h.mu.RUnlock()

	if !exists {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		h.Unregister(fingerprint)
		return nil, ErrSessionExpired
	}

	if session.OTP != otp {
		return nil, ErrInvalidOTP
	}

	// Deliver credentials to the agent's channel
	select {
	case session.PairedChan <- pairedMsg:
		h.Unregister(fingerprint)
		return session, nil
	default:
		return nil, errors.New("failed to deliver credentials to agent WebSocket")
	}
}

func (h *Hub) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		h.mu.Lock()
		for fp, s := range h.sessions {
			if now.After(s.ExpiresAt) {
				_ = s.Conn.WriteJSON(map[string]string{
					"type":  models.WSMsgTypeTimeout,
					"error": "Pairing session timed out (5 minutes exceeded)",
				})
				close(s.DoneChan)
				_ = s.Conn.Close()
				delete(h.sessions, fp)
			}
		}
		h.mu.Unlock()
	}
}

