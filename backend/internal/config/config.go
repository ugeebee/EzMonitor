package config

import (
	"os"
)

type Config struct {
	APIDatabaseURL    string
	IngestDatabaseURL string
	ListenAddr        string
	JWTSecret         string
	Environment       string
}

func Load() *Config {
	apiDB := os.Getenv("API_DATABASE_URL")
	if apiDB == "" {
		// Fallback to generic DATABASE_URL or default dev credentials
		apiDB = os.Getenv("DATABASE_URL")
		if apiDB == "" {
			apiDB = "postgres://ezm_api:change_me_api@localhost:5432/ezmonitor?sslmode=disable"
		}
	}

	ingestDB := os.Getenv("INGEST_DATABASE_URL")
	if ingestDB == "" {
		ingestDB = os.Getenv("DATABASE_URL")
		if ingestDB == "" {
			ingestDB = "postgres://ezm_ingest:change_me_ingest@localhost:5432/ezmonitor?sslmode=disable"
		}
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "ezmonitor-default-development-jwt-secret-key-32b"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	return &Config{
		APIDatabaseURL:    apiDB,
		IngestDatabaseURL: ingestDB,
		ListenAddr:        listenAddr,
		JWTSecret:         jwtSecret,
		Environment:       env,
	}
}

