// Package session manages admin login sessions, persisted in PostgreSQL so active
// logins survive process restarts (deploys, crashes, free-tier idle spin-down).
// An in-memory map would be wiped on every restart, logging admins out prematurely.
package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"time"
)

// SessionDuration is how long an admin session stays valid after login.
const SessionDuration = 30 * time.Minute

// SessionData holds a snapshot of the admin's identity taken at login time.
// A role change does not apply to an already-active session until the admin re-logs in.
type SessionData struct {
	AdminID           int64
	Username          string
	Name              string
	Role              string // "super_admin" or "general_admin"
	MustResetPassword bool   // true until a temporary password is replaced
	ExpiresAt         time.Time
}

// Store persists sessions in the sessions table.
type Store struct {
	db *sql.DB
}

// New wraps db and starts a background goroutine that purges expired sessions every 10 minutes.
func New(db *sql.DB) *Store {
	s := &Store{db: db}
	go s.cleanupLoop()
	return s
}

// Create inserts a new session row and returns a random 64-char hex session ID.
// Returns an error if the row could not be written — the caller must not treat
// the login as successful since the returned ID would point to a non-existent session.
func (s *Store) Create(adminID int64, username, name, role string, mustResetPassword bool) (string, error) {
	// 32 random bytes → 64-character hex string, impossible to guess.
	randomBytes := make([]byte, 32)
	_, _ = rand.Read(randomBytes)
	sessionID := hex.EncodeToString(randomBytes)

	expiresAt := time.Now().Add(SessionDuration)

	_, err := s.db.Exec(`
		INSERT INTO sessions(id, admin_id, username, name, role, must_reset_password, expires_at)
		VALUES($1, $2, $3, $4, $5, $6, $7)
	`, sessionID, adminID, username, name, role, mustResetPassword, expiresAt)
	if err != nil {
		return "", err
	}

	return sessionID, nil
}

// Get looks up a session by ID.
// Returns (data, true, nil) if found and not expired.
// Returns (zero, false, nil) if the session does not exist or has expired — treat as unauthenticated.
// Returns (zero, false, err) on a transient DB error — callers MUST NOT treat this as
// "not authenticated"; the session may still be valid and forcing a logout is confusing.
func (s *Store) Get(sessionID string) (SessionData, bool, error) {
	var data SessionData
	var expiresAt time.Time

	err := s.db.QueryRow(`
		SELECT admin_id, username, name, role, must_reset_password, expires_at
		FROM sessions WHERE id = $1
	`, sessionID).Scan(&data.AdminID, &data.Username, &data.Name, &data.Role, &data.MustResetPassword, &expiresAt)
	if err == sql.ErrNoRows {
		return SessionData{}, false, nil
	}
	if err != nil {
		return SessionData{}, false, err
	}

	if time.Now().After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
		return SessionData{}, false, nil
	}

	data.ExpiresAt = expiresAt
	return data, true, nil
}

// Delete removes a session immediately. Called when the admin logs out.
func (s *Store) Delete(sessionID string) {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID); err != nil {
		log.Printf("[SESSION] failed to delete session: %v", err)
	}
}

// DeleteByAdminID immediately invalidates every active session for adminID.
// Without this, revoking an admin or resetting their password only takes effect
// at next login — their current session would keep working until natural expiry.
func (s *Store) DeleteByAdminID(adminID int64) {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE admin_id = $1`, adminID); err != nil {
		log.Printf("[SESSION] failed to delete sessions for admin %d: %v", adminID, err)
	}
}

// ClearMustResetPassword removes the forced-password-reset flag from an active session.
// Called immediately after ChangeOwnPassword so the admin doesn't have to re-login.
func (s *Store) ClearMustResetPassword(sessionID string) {
	if _, err := s.db.Exec(`UPDATE sessions SET must_reset_password = FALSE WHERE id = $1`, sessionID); err != nil {
		log.Printf("[SESSION] failed to clear must_reset_password: %v", err)
	}
}

// cleanupLoop purges expired sessions every 10 minutes to bound table growth.
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`); err != nil {
			log.Printf("[SESSION] cleanup failed: %v", err)
		}
	}
}
