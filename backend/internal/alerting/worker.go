package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// listenChannel is the PostgreSQL NOTIFY channel for alert events.
	listenChannel = "alert_fired"

	// webhookTimeout is the HTTP timeout for each webhook delivery attempt.
	webhookTimeout = 10 * time.Second

	// maxRetries is the number of delivery attempts per webhook.
	maxRetries = 3

	// baseBackoff is the initial backoff duration; doubled on each retry.
	baseBackoff = 1 * time.Second
)

// Run starts the alert worker. It connects to PostgreSQL, issues
// LISTEN on the alert_fired channel, and dispatches webhooks for
// every incoming notification. It blocks until ctx is cancelled or
// a fatal error occurs.
func Run(ctx context.Context, databaseURL string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// ── Connect ─────────────────────────────────────────────────────
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("alertworker: connect to database: %w", err)
	}
	defer conn.Close(context.Background())

	slog.Info("connected to database")

	// ── LISTEN ──────────────────────────────────────────────────────
	if _, err := conn.Exec(ctx, "LISTEN "+listenChannel); err != nil {
		return fmt.Errorf("alertworker: LISTEN %s: %w", listenChannel, err)
	}
	slog.Info("listening for notifications", "channel", listenChannel)

	// Shared HTTP client for webhook delivery (connection pooling).
	httpClient := &http.Client{Timeout: webhookTimeout}

	// WaitGroup tracks in-flight webhook goroutines for graceful shutdown.
	var wg sync.WaitGroup

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			// Context cancelled → graceful shutdown.
			if ctx.Err() != nil {
				slog.Info("context cancelled, waiting for in-flight deliveries")
				wg.Wait()
				slog.Info("shutdown complete")
				return nil
			}
			return fmt.Errorf("alertworker: wait for notification: %w", err)
		}

		slog.Info("notification received",
			"channel", notification.Channel,
			"payload_len", len(notification.Payload),
		)

		// ── Parse payload ───────────────────────────────────────────
		var alert AlertNotification
		if err := json.Unmarshal([]byte(notification.Payload), &alert); err != nil {
			slog.Error("failed to parse notification payload",
				"error", err,
				"payload", notification.Payload,
			)
			continue
		}

		slog.Info("alert received",
			"a_id", alert.AlertID,
			"rule_id", alert.RuleID,
			"i_id", alert.InstanceID,
			"g_id", alert.GroupID,
			"alert_value", alert.AlertValue,
		)

		// ── Fetch webhook targets ───────────────────────────────────
		targets, err := fetchWebhookTargets(ctx, conn, alert.RuleID)
		if err != nil {
			slog.Error("failed to fetch webhook targets",
				"error", err,
				"rule_id", alert.RuleID,
			)
			continue
		}

		if len(targets) == 0 {
			slog.Info("no active webhooks for rule", "rule_id", alert.RuleID)
			continue
		}

		// ── Build payload with context ──────────────────────────────
		payload, err := buildPayload(ctx, conn, alert)
		if err != nil {
			slog.Error("failed to build webhook payload",
				"error", err,
				"a_id", alert.AlertID,
			)
			continue
		}

		body, err := json.Marshal(payload)
		if err != nil {
			slog.Error("failed to marshal webhook payload", "error", err)
			continue
		}

		// ── Dispatch to each target ─────────────────────────────────
		for _, target := range targets {
			wg.Add(1)
			go func(t WebhookTarget) {
				defer wg.Done()
				deliverWebhook(ctx, httpClient, t, body, alert.AlertID)
			}(target)
		}
	}
}

// fetchWebhookTargets returns the active webhook endpoints linked to a rule.
func fetchWebhookTargets(ctx context.Context, conn *pgx.Conn, ruleID int64) ([]WebhookTarget, error) {
	const query = `
		SELECT w.web_id, w.webhook_url, w.secret
		FROM alert_rule_webhooks arw
		JOIN webhooks w ON w.web_id = arw.web_id AND w.g_id = arw.g_id
		WHERE arw.rule_id = $1 AND w.status = 'active'
	`
	rows, err := conn.Query(ctx, query, ruleID)
	if err != nil {
		return nil, fmt.Errorf("query webhook targets: %w", err)
	}
	defer rows.Close()

	var targets []WebhookTarget
	for rows.Next() {
		var t WebhookTarget
		if err := rows.Scan(&t.WebID, &t.WebhookURL, &t.Secret); err != nil {
			return nil, fmt.Errorf("scan webhook target: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// buildPayload enriches the alert notification with rule and instance context.
func buildPayload(ctx context.Context, conn *pgx.Conn, alert AlertNotification) (WebhookPayload, error) {
	var metricName, operator string
	var threshold float64
	err := conn.QueryRow(ctx,
		`SELECT metric_name::text, operator::text, threshold_value
		 FROM alert_rules WHERE rule_id = $1`,
		alert.RuleID,
	).Scan(&metricName, &operator, &threshold)
	if err != nil {
		return WebhookPayload{}, fmt.Errorf("query alert_rules: %w", err)
	}

	var instanceLabel string
	err = conn.QueryRow(ctx,
		`SELECT label FROM instances WHERE i_id = $1`,
		alert.InstanceID,
	).Scan(&instanceLabel)
	if err != nil {
		return WebhookPayload{}, fmt.Errorf("query instances: %w", err)
	}

	return WebhookPayload{
		Event:       "alert.fired",
		AlertID:     alert.AlertID,
		RuleID:      alert.RuleID,
		InstanceID:  alert.InstanceID,
		GroupID:     alert.GroupID,
		MetricValue: alert.AlertValue,
		TriggeredAt: alert.TriggeredAt,
		Instance:    instanceLabel,
		MetricName:  metricName,
		Operator:    operator,
		Threshold:   threshold,
	}, nil
}

// deliverWebhook POSTs the body to a single webhook endpoint with HMAC-SHA256
// signing and exponential-backoff retry.
func deliverWebhook(ctx context.Context, client *http.Client, target WebhookTarget, body []byte, alertID int64) {
	// Compute HMAC-SHA256 signature.
	mac := hmac.New(sha256.New, []byte(target.Secret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	backoff := baseBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			slog.Warn("delivery cancelled",
				"web_id", target.WebID,
				"a_id", alertID,
				"attempt", attempt,
			)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.WebhookURL, bytes.NewReader(body))
		if err != nil {
			slog.Error("failed to create request",
				"error", err,
				"web_id", target.WebID,
				"a_id", alertID,
			)
			return // non-retryable
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-EzMonitor-Signature", signature)

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("webhook delivery failed",
				"error", err,
				"web_id", target.WebID,
				"a_id", alertID,
				"attempt", attempt,
				"max_retries", maxRetries,
			)
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				slog.Info("webhook delivered",
					"web_id", target.WebID,
					"a_id", alertID,
					"status", resp.StatusCode,
					"attempt", attempt,
				)
				return
			}
			slog.Warn("webhook non-2xx response",
				"web_id", target.WebID,
				"a_id", alertID,
				"status", resp.StatusCode,
				"attempt", attempt,
				"max_retries", maxRetries,
			)
		}

		// Exponential backoff before retry (unless it's the last attempt).
		if attempt < maxRetries {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			backoff *= 2
		}
	}

	slog.Error("webhook delivery exhausted all retries",
		"web_id", target.WebID,
		"a_id", alertID,
		"url", target.WebhookURL,
	)
}

// RunWithSignals is a convenience wrapper that sets up OS signal handling
// and calls Run. Used by cmd/alertworker/main.go.
func RunWithSignals(databaseURL string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return Run(ctx, databaseURL)
}
