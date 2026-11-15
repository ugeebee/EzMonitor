package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

func TestGetTargetGroupID(t *testing.T) {
	// 1. Test {groupId} extraction
	r := httptest.NewRequest(http.MethodGet, "/groups/123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("groupId", "123")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	gid, err := getTargetGroupID(r)
	if err != nil || gid != 123 {
		t.Errorf("expected gid 123, got %d (err: %v)", gid, err)
	}

	// 2. Test fallback to {subgroupId}
	r2 := httptest.NewRequest(http.MethodGet, "/subgroups/456", nil)
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("subgroupId", "456")
	r2 = r2.WithContext(context.WithValue(r2.Context(), chi.RouteCtxKey, rctx2))

	gid2, err := getTargetGroupID(r2)
	if err != nil || gid2 != 456 {
		t.Errorf("expected gid 456, got %d (err: %v)", gid2, err)
	}

	// 3. Test missing group ID
	r3 := httptest.NewRequest(http.MethodGet, "/invalid", nil)
	r3 = r3.WithContext(context.WithValue(r3.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
	_, err = getTargetGroupID(r3)
	if err == nil {
		t.Errorf("expected error for missing group ID, got nil")
	}
}

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"status": "ok", "message": "all good"}

	respondJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status code %d, got %d", http.StatusCreated, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "ok" || res["message"] != "all good" {
		t.Errorf("unexpected body payload: %v", res)
	}
}

func TestRuleModelValidation(t *testing.T) {
	valid := models.CreateRuleRequest{
		MetricName:     "cpu_util",
		Operator:       ">",
		ThresholdValue: 80.0,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid rule, got: %v", err)
	}

	invalidMetric := models.CreateRuleRequest{
		MetricName:     "disk_read", // unsupported
		Operator:       ">",
		ThresholdValue: 50.0,
	}
	if err := invalidMetric.Validate(); err == nil {
		t.Errorf("expected validation failure on unsupported metric")
	}

	invalidOp := models.CreateRuleRequest{
		MetricName:     "cpu_util",
		Operator:       "!=",
		ThresholdValue: 50.0,
	}
	if err := invalidOp.Validate(); err == nil {
		t.Errorf("expected validation failure on unsupported operator")
	}
}

func TestWebhookModelValidation(t *testing.T) {
	valid := models.CreateWebhookRequest{
		WebhookURL: "https://hooks.slack.com/services/T00/B00/X00",
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid webhook, got: %v", err)
	}

	invalidURL := models.CreateWebhookRequest{
		WebhookURL: "ftp://invalid-scheme.com",
	}
	if err := invalidURL.Validate(); err == nil {
		t.Errorf("expected validation error for invalid URL scheme")
	}
}

