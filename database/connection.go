// Package database handles connecting to PostgreSQL.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver — the blank import registers it
)

// Connect reads DATABASE_URL from the environment, opens a connection to
// PostgreSQL, and verifies it with a ping.
// Returns a ready-to-use *sql.DB or an error.
func Connect() (*sql.DB, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	// Open the driver (does not actually dial the server yet)
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Ping the database to confirm it is reachable (times out after 10 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cannot reach PostgreSQL: %w", err)
	}

	return db, nil
}

// GetEnv reads an environment variable and returns fallback if it is empty.
func GetEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
