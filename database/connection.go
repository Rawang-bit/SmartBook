// Package database handles connecting to PostgreSQL.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver registered as "pgx" for database/sql
)

// Connect reads DATABASE_URL from the environment, opens a connection to
// PostgreSQL, and verifies it with a ping.
// Returns a ready-to-use *sql.DB or an error.
func Connect() (*sql.DB, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	// Open the pgx driver (does not actually dial the server yet)
	db, err := sql.Open("pgx", databaseURL)
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

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return db, nil
}

// migrate applies any schema changes that must run at startup.
// Each statement is idempotent (IF NOT EXISTS guards) so it is safe to run on
// every boot against both fresh and existing databases.
// Statements are executed individually because pgx does not support multiple
// statements in a single Exec call.
func migrate(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE admins ADD COLUMN IF NOT EXISTS email TEXT UNIQUE`,
		`CREATE INDEX IF NOT EXISTS idx_admins_email_lower
		     ON admins (LOWER(TRIM(email)))
		     WHERE email IS NOT NULL`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE admins ADD COLUMN IF NOT EXISTS must_reset_password BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE bookings ADD COLUMN IF NOT EXISTS agenda TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE bookings ADD COLUMN IF NOT EXISTS participants TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS sessions (
		    id                  TEXT PRIMARY KEY,
		    admin_id            BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
		    username            TEXT NOT NULL,
		    name                TEXT NOT NULL,
		    role                TEXT NOT NULL,
		    must_reset_password BOOLEAN NOT NULL DEFAULT FALSE,
		    expires_at          TIMESTAMPTZ NOT NULL,
		    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS intended_role TEXT NOT NULL DEFAULT 'normal_user'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS confirm_token TEXT UNIQUE`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS confirm_token_expires_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_users_confirm_token ON users(confirm_token) WHERE confirm_token IS NOT NULL`,
		// One-time rename: "approved" became "active" as the status value for
		// a user cleared to book rooms. Harmless to re-run — once no row has
		// status 'approved' this is a no-op on every subsequent boot.
		`UPDATE users SET status = 'active' WHERE status = 'approved'`,
		// Trusted-device cookie support for the public access gate: a user who
		// opts in to "remember this device" after OTP verification gets a
		// hashed token stored here with an expiry, so a recognized browser can
		// skip OTP next time (see UserModel.DeviceTokenMatches).
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS device_token_hash TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS device_token_expires_at TIMESTAMPTZ`,
		// Minutes of Meeting: the booking's owner can record what was actually
		// discussed once a meeting has ended, within a 24-hour edit window
		// (see BookingModel.MinutesEditWindow).
		`ALTER TABLE bookings ADD COLUMN IF NOT EXISTS minutes_of_meeting TEXT NOT NULL DEFAULT ''`,
		// Optional contact number shown to an admin reviewing a pending
		// registration, and the reason recorded when a registration is rejected.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS rejection_reason TEXT NOT NULL DEFAULT ''`,
		// Audit trail: every admin-initiated action (login/logout, user/admin/
		// room/booking management) is recorded here. Append-only — no UPDATE or
		// DELETE endpoint is ever exposed for this table (see AuditModel).
		`CREATE TABLE IF NOT EXISTS audit_logs (
		    id           BIGSERIAL PRIMARY KEY,
		    actor_type   TEXT NOT NULL,
		    actor_id     BIGINT,
		    actor_label  TEXT NOT NULL,
		    action       TEXT NOT NULL,
		    target_type  TEXT NOT NULL DEFAULT '',
		    target_id    BIGINT,
		    target_label TEXT NOT NULL DEFAULT '',
		    details      TEXT NOT NULL DEFAULT '',
		    ip_address   TEXT NOT NULL DEFAULT '',
		    user_agent   TEXT NOT NULL DEFAULT '',
		    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs(actor_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// GetEnv reads an environment variable and returns fallback if it is empty.
func GetEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
