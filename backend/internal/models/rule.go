package models

import (
	"errors"
	"strings"
	"time"
)

type AlertRule struct {
	RuleID         int64     `json:"rule_id"`
	GID            int64     `json:"g_id"`
	MetricName     string    `json:"metric_name"`
	Operator       string    `json:"operator"`
	ThresholdValue float64   `json:"threshold_value"`
	Enabled        bool      `json:"enabled"`
	DateCreated    time.Time `json:"date_created"`
}

type CreateRuleRequest struct {
	MetricName     string  `json:"metric_name"`
	Operator       string  `json:"operator"`
	ThresholdValue float64 `json:"threshold_value"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

func (r *CreateRuleRequest) Validate() error {
	r.MetricName = strings.TrimSpace(r.MetricName)
	r.Operator = strings.TrimSpace(r.Operator)

	validMetrics := map[string]bool{
		"cpu_util": true, "gpu_util": true, "ram_util": true,
		"uptime": true, "net_up_kbps": true, "net_down_kbps": true,
	}
	if !validMetrics[r.MetricName] {
		return errors.New("invalid metric_name; must be one of cpu_util, gpu_util, ram_util, uptime, net_up_kbps, net_down_kbps")
	}

	validOps := map[string]bool{
		">": true, ">=": true, "<": true, "<=": true,
	}
	if !validOps[r.Operator] {
		return errors.New("invalid operator; must be one of >, >=, <, <=")
	}

	if r.ThresholdValue < 0 {
		return errors.New("threshold_value must be non-negative")
	}

	return nil
}

type UpdateRuleRequest struct {
	MetricName     *string  `json:"metric_name,omitempty"`
	Operator       *string  `json:"operator,omitempty"`
	ThresholdValue *float64 `json:"threshold_value,omitempty"`
	Enabled        *bool    `json:"enabled,omitempty"`
}

