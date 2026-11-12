package pairing_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ugeebee/EzMonitor/backend/internal/models"
	"github.com/ugeebee/EzMonitor/backend/internal/pairing"
)

func TestPairingHub(t *testing.T) {
	hub := pairing.NewHub()

	fp := "0123456789abcdef"
	otp := "654321"

	session := &pairing.AgentSession{
		Fingerprint: fp,
		OTP:         otp,
		CPUModel:    "Intel Core i7",
		RAMConfig:   "16GB",
		GPUModel:    "NVIDIA RTX 3060",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		PairedChan:  make(chan models.ServerPairedMessage, 1),
		DoneChan:    make(chan struct{}),
	}

	hub.Register(session)

	// 1. Test invalid OTP
	pairedMsg := models.ServerPairedMessage{
		Type:       models.WSMsgTypePaired,
		InstanceID: 42,
		Token:      "dev-token-secret-test",
		Label:      "test-box",
		GID:        1,
	}

	_, err := hub.VerifyAndComplete(fp, "000000", pairedMsg)
	if !errors.Is(err, pairing.ErrInvalidOTP) {
		t.Errorf("Expected ErrInvalidOTP, got %v", err)
	}

	// 2. Test unknown fingerprint
	_, err = hub.VerifyAndComplete("ffffffffffffffff", otp, pairedMsg)
	if !errors.Is(err, pairing.ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}

	// 3. Test valid OTP verification
	completed, err := hub.VerifyAndComplete(fp, otp, pairedMsg)
	if err != nil {
		t.Fatalf("Expected successful verification, got %v", err)
	}
	if completed.Fingerprint != fp {
		t.Errorf("Expected fingerprint %s, got %s", fp, completed.Fingerprint)
	}

	// Verify message was delivered to channel
	select {
	case msg := <-completed.PairedChan:
		if msg.InstanceID != 42 || msg.Token != "dev-token-secret-test" {
			t.Errorf("Unexpected paired message: %+v", msg)
		}
	default:
		t.Errorf("Expected paired message on PairedChan")
	}

	// 4. Test expired session
	expiredFP := "1111222233334444"
	expiredSession := &pairing.AgentSession{
		Fingerprint: expiredFP,
		OTP:         "123456",
		ExpiresAt:   time.Now().Add(-1 * time.Minute), // already expired
		PairedChan:  make(chan models.ServerPairedMessage, 1),
		DoneChan:    make(chan struct{}),
	}
	hub.Register(expiredSession)

	_, err = hub.VerifyAndComplete(expiredFP, "123456", pairedMsg)
	if !errors.Is(err, pairing.ErrSessionExpired) {
		t.Errorf("Expected ErrSessionExpired, got %v", err)
	}
}

