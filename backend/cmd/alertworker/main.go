// Command alertworker listens for PostgreSQL alert notifications and
// dispatches webhook payloads with HMAC-SHA256 signing.
//
// Configuration (environment variables):
//
//	ALERT_DATABASE_URL  — PostgreSQL connection string (required)
//	DATABASE_URL        — fallback if ALERT_DATABASE_URL is not set
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/ugeebee/EzMonitor/backend/internal/alerting"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	dbURL := os.Getenv("ALERT_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "fatal: set ALERT_DATABASE_URL or DATABASE_URL")
		os.Exit(1)
	}

	slog.Info("starting alert worker")

	if err := alerting.RunWithSignals(dbURL); err != nil {
		slog.Error("alert worker exited with error", "error", err)
		os.Exit(1)
	}

	slog.Info("alert worker stopped")
}
