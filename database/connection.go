// Package database handles connecting to PostgreSQL.
// Calling Connect() is all main.go needs to do — this file
// loads the .env file and reads DATABASE_URL on its own.
package database

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver — the blank import registers it
)

// Connect loads the .env file, reads DATABASE_URL, opens a connection to
// PostgreSQL, and verifies it with a ping.
// Returns a ready-to-use *sql.DB or an error.
func Connect() (*sql.DB, error) {
	// Load .env so environment variables are available before anything else
	loadDotEnv(".env")

	// Read the database connection string from the environment
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Fall back to a sensible local default when DATABASE_URL is not set
		databaseURL = "postgres://postgres:postgres@localhost:5432/bookroom_db?sslmode=disable"
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
// Call this after Connect() so the .env file has already been loaded.
func GetEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// loadDotEnv reads a .env file and sets each KEY=VALUE pair as an
// environment variable. Lines starting with # and empty lines are ignored.
// Variables already set in the environment are not overwritten.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return // .env is optional — silently skip if missing
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip lines that have no = sign
		if !strings.Contains(line, "=") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		key   := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		// Only set the variable if it is not already in the environment
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
