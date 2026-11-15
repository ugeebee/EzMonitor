package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	API    *pgxpool.Pool
	Ingest *pgxpool.Pool
}

func Connect(ctx context.Context, apiURL, ingestURL string) (*DB, error) {
	apiConfig, err := pgxpool.ParseConfig(apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid API_DATABASE_URL: %w", err)
	}
	apiConfig.MaxConns = 30
	apiConfig.MinConns = 5
	apiConfig.MaxConnLifetime = 1 * time.Hour
	apiConfig.MaxConnIdleTime = 15 * time.Minute

	apiPool, err := pgxpool.NewWithConfig(ctx, apiConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create API DB pool: %w", err)
	}

	if err := apiPool.Ping(ctx); err != nil {
		log.Printf("Warning: API database ping failed (%v). Pool initialized, connection will be retried on query.", err)
	}

	var ingestPool *pgxpool.Pool
	if ingestURL == apiURL {
		ingestPool = apiPool
	} else {
		ingestConfig, err := pgxpool.ParseConfig(ingestURL)
		if err != nil {
			return nil, fmt.Errorf("invalid INGEST_DATABASE_URL: %w", err)
		}
		ingestConfig.MaxConns = 50
		ingestConfig.MinConns = 5
		ingestPool, err = pgxpool.NewWithConfig(ctx, ingestConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Ingest DB pool: %w", err)
		}
		if err := ingestPool.Ping(ctx); err != nil {
			log.Printf("Warning: Ingest database ping failed (%v). Pool initialized, connection will be retried on query.", err)
		}
	}

	return &DB{
		API:    apiPool,
		Ingest: ingestPool,
	}, nil
}

func (db *DB) Close() {
	if db.API != nil {
		db.API.Close()
	}
	if db.Ingest != nil && db.Ingest != db.API {
		db.Ingest.Close()
	}
}

