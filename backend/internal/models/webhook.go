package models

import (
	"errors"
	"strings"
	"time"
)

type Webhook struct {
	WebID       int64     `json:"web_id"`
	GID         int64     `json:"g_id"`
	WebhookURL  string    `json:"webhook_url"`
	Secret      string    `json:"secret,omitempty"`
	Status      string    `json:"status"`
	DateCreated time.Time `json:"date_created"`
}

type CreateWebhookRequest struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret,omitempty"` // optional; generated if empty
}

func (r *CreateWebhookRequest) Validate() error {
	r.WebhookURL = strings.TrimSpace(r.WebhookURL)
	if !strings.HasPrefix(r.WebhookURL, "https://") {
		return errors.New("webhook_url must begin with https://")
	}
	if len(r.WebhookURL) > 2048 {
		return errors.New("webhook_url exceeds 2048 characters")
	}
	return nil
}

