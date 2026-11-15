package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

type RuleHandler struct {
	db *pgxpool.Pool
}

func NewRuleHandler(db *pgxpool.Pool) *RuleHandler {
	return &RuleHandler{db: db}
}

// CreateRule inserts a new alert rule for a group.
func (h *RuleHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req models.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if err := req.Validate(); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ctx := r.Context()
	var rule models.AlertRule
	query := `
		INSERT INTO alert_rules (g_id, metric_name, operator, threshold_value, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING rule_id, g_id, metric_name, operator, threshold_value, enabled, date_created
	`
	err = h.db.QueryRow(ctx, query, groupID, req.MetricName, req.Operator, req.ThresholdValue, enabled).Scan(
		&rule.RuleID, &rule.GID, &rule.MetricName, &rule.Operator, &rule.ThresholdValue, &rule.Enabled, &rule.DateCreated,
	)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create rule: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusCreated, rule)
}

// ListRules returns all alert rules configured for a group.
func (h *RuleHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	query := `
		SELECT rule_id, g_id, metric_name, operator, threshold_value, enabled, date_created
		FROM alert_rules
		WHERE g_id = $1
		ORDER BY rule_id ASC
	`
	rows, err := h.db.Query(ctx, query, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list rules"})
		return
	}
	defer rows.Close()

	rules := make([]models.AlertRule, 0)
	for rows.Next() {
		var rule models.AlertRule
		if err := rows.Scan(
			&rule.RuleID, &rule.GID, &rule.MetricName, &rule.Operator, &rule.ThresholdValue, &rule.Enabled, &rule.DateCreated,
		); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse rules"})
			return
		}
		rules = append(rules, rule)
	}

	respondJSON(w, http.StatusOK, rules)
}

// UpdateRule updates a threshold rule.
func (h *RuleHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleId")
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil || ruleID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid rule ID"})
		return
	}

	var req models.UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	ctx := r.Context()
	query := `
		UPDATE alert_rules
		SET metric_name = COALESCE($1, metric_name),
		    operator = COALESCE($2, operator),
		    threshold_value = COALESCE($3, threshold_value),
		    enabled = COALESCE($4, enabled)
		WHERE rule_id = $5 AND g_id = $6
		RETURNING rule_id, g_id, metric_name, operator, threshold_value, enabled, date_created
	`
	var rule models.AlertRule
	err = h.db.QueryRow(ctx, query, req.MetricName, req.Operator, req.ThresholdValue, req.Enabled, ruleID, groupID).Scan(
		&rule.RuleID, &rule.GID, &rule.MetricName, &rule.Operator, &rule.ThresholdValue, &rule.Enabled, &rule.DateCreated,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Alert rule not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update rule: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, rule)
}

// DeleteRule removes an alert rule.
func (h *RuleHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	groupID, err := getTargetGroupID(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleId")
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil || ruleID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid rule ID"})
		return
	}

	ctx := r.Context()
	res, err := h.db.Exec(ctx, `DELETE FROM alert_rules WHERE rule_id = $1 AND g_id = $2`, ruleID, groupID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete rule"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Alert rule not found"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Alert rule deleted successfully"})
}

// ResolveAlert marks an active alert log entry as resolved.
func (h *RuleHandler) ResolveAlert(w http.ResponseWriter, r *http.Request) {
	alertIDStr := chi.URLParam(r, "alertId")
	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil || alertID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid alert ID"})
		return
	}

	ctx := r.Context()
	query := `
		UPDATE alert_logs
		SET status = 'resolved', resolved_at = now()
		WHERE a_id = $1 AND status = 'active'
		RETURNING a_id, rule_id, i_id, g_id, alert_value, triggered_at, resolved_at, status
	`
	var aID, ruleID, iID, gID int64
	var alertValue float64
	var triggeredAt, resolvedAt any
	var status string
	err = h.db.QueryRow(ctx, query, alertID).Scan(
		&aID, &ruleID, &iID, &gID, &alertValue, &triggeredAt, &resolvedAt, &status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Active alert not found or already resolved"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to resolve alert: " + err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"a_id":        aID,
		"rule_id":     ruleID,
		"status":      status,
		"resolved_at": resolvedAt,
		"message":     "Alert marked as resolved",
	})
}

