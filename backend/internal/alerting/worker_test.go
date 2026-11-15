package alerting

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliverWebhook_HMACSignature(t *testing.T) {
	secret := "super-secret-test-key"
	payload := []byte(`{"event":"alert.fired","alert_id":42,"metric":"cpu_util"}`)

	// Expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	receivedSig := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-EzMonitor-Signature")
		body, _ := io.ReadAll(r.Body)
		if string(body) != string(payload) {
			t.Errorf("expected body %s, got %s", payload, body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := WebhookTarget{
		WebID:      1,
		WebhookURL: server.URL,
		Secret:     secret,
	}

	client := &http.Client{Timeout: 5 * time.Second}
	deliverWebhook(context.Background(), client, target, payload, 42)

	if receivedSig != expectedSig {
		t.Errorf("HMAC mismatch: expected %q, got %q", expectedSig, receivedSig)
	}
}

func TestDeliverWebhook_RetryOnFailure(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := WebhookTarget{
		WebID:      2,
		WebhookURL: server.URL,
		Secret:     "key",
	}

	// Use cancelled or fast context so backoff does not take too long in test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 1 * time.Second}
	deliverWebhook(ctx, client, target, []byte(`{"test":true}`), 99)

	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("expected 3 delivery attempts, got %d", finalAttempts)
	}
}

