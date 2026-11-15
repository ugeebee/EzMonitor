// Package alerting implements the alert worker that listens for PostgreSQL
// NOTIFY events on the "alert_fired" channel and dispatches webhook
// notifications to configured endpoints with HMAC-SHA256 signing.
package alerting

// AlertNotification is the JSON payload received from pg_notify('alert_fired', ...).
type AlertNotification struct {
	AlertID     int64   `json:"a_id"`
	RuleID      int64   `json:"rule_id"`
	InstanceID  int64   `json:"i_id"`
	GroupID     int64   `json:"g_id"`
	AlertValue  float64 `json:"alert_value"`
	TriggeredAt string  `json:"triggered_at"`
}

// WebhookTarget holds the destination and signing secret for a single webhook.
type WebhookTarget struct {
	WebID      int64
	WebhookURL string
	Secret     string
}

// WebhookPayload is the JSON body POSTed to each webhook endpoint.
type WebhookPayload struct {
	Event       string  `json:"event"`
	AlertID     int64   `json:"alert_id"`
	RuleID      int64   `json:"rule_id"`
	InstanceID  int64   `json:"instance_id"`
	GroupID     int64   `json:"group_id"`
	MetricValue float64 `json:"metric_value"`
	TriggeredAt string  `json:"triggered_at"`
	Instance    string  `json:"instance_label"`
	MetricName  string  `json:"metric_name"`
	Operator    string  `json:"operator"`
	Threshold   float64 `json:"threshold"`
}
