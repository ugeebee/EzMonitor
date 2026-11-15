package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type WebhookHandler struct {
	db *pgxpool.Pool
}

func NewWebhookHandler(db *pgxpool.Pool) *WebhookHandler {
	return &WebhookHandler{db: db}
}

// CreateWebhook creates a new notification webhook endpoint for a group.
func (h *WebhookHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req models.CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	secret := req.Secret
	if secret == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		secret = hex.EncodeToString(b)
	}

	ctx := r.Context()
	var web models.Webhook
	query := `
		INSERT INTO webhooks (g_id, webhook_url, secret, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING web_id, g_id, webhook_url, secret, status, date_created
	`
	err = h.db.QueryRow(ctx, query, groupID, req.WebhookURL, secret).Scan(
		&web.WebID, &web.GID, &web.WebhookURL, &web.Secret, &web.Status, &web.DateCreated,
	)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create webhook: " + err.Error()})
		return
	}

	// Auto-link new webhook to all alert rules in this group
	_, _ = h.db.Exec(ctx, `
		INSERT INTO alert_rule_webhooks (rule_id, web_id, g_id)
		SELECT rule_id, $1, $2 FROM alert_rules WHERE g_id = $2
		ON CONFLICT DO NOTHING
	`, web.WebID, groupID)

	respondJSON(w, http.StatusCreated, web)
}

// ListWebhooks returns all configured webhooks for a group.
func (h *WebhookHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	query := `
		SELECT web_id, g_id, webhook_url, secret, status, date_created
		FROM webhooks
		WHERE g_id = $1
		ORDER BY web_id ASC
	`
	rows, err := h.db.Query(ctx, query, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list webhooks"})
		return
	}
	defer rows.Close()

	webhooks := make([]models.Webhook, 0)
	for rows.Next() {
		var web models.Webhook
		if err := rows.Scan(
			&web.WebID, &web.GID, &web.WebhookURL, &web.Secret, &web.Status, &web.DateCreated,
		); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse webhooks"})
			return
		}
		webhooks = append(webhooks, web)
	}

	respondJSON(w, http.StatusOK, webhooks)
}

// DeleteWebhook removes a webhook.
func (h *WebhookHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	webIDStr := chi.URLParam(r, "webId")
	webID, err := strconv.ParseInt(webIDStr, 10, 64)
	if err != nil || webID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid webhook ID"})
		return
	}

	ctx := r.Context()
	res, err := h.db.Exec(ctx, `DELETE FROM webhooks WHERE web_id = $1 AND g_id = $2`, webID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete webhook"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Webhook not found"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Webhook deleted successfully"})
}

// LinkRuleWebhook associates a webhook with an alert rule so it gets notified when the rule fires.
func (h *WebhookHandler) LinkRuleWebhook(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ruleID, _ := strconv.ParseInt(chi.URLParam(r, "ruleId"), 10, 64)
	webID, _ := strconv.ParseInt(chi.URLParam(r, "webId"), 10, 64)
	if ruleID <= 0 || webID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid rule ID or webhook ID"})
		return
	}

	ctx := r.Context()
	query := `
		INSERT INTO alert_rule_webhooks (rule_id, web_id, g_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`
	_, err = h.db.Exec(ctx, query, ruleID, webID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to link webhook to rule: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Webhook linked to rule"})
}

// UnlinkRuleWebhook dissociates a webhook from an alert rule.
func (h *WebhookHandler) UnlinkRuleWebhook(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ruleID, _ := strconv.ParseInt(chi.URLParam(r, "ruleId"), 10, 64)
	webID, _ := strconv.ParseInt(chi.URLParam(r, "webId"), 10, 64)
	if ruleID <= 0 || webID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid rule ID or webhook ID"})
		return
	}

	ctx := r.Context()
	res, err := h.db.Exec(ctx, `DELETE FROM alert_rule_webhooks WHERE rule_id = $1 AND web_id = $2 AND g_id = $3`, ruleID, webID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to unlink webhook: " + err.Error()})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Link not found"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Webhook unlinked from rule"})
}
