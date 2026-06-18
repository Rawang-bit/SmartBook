// Package session manages admin login sessions, persisted in PostgreSQL.
// When an admin logs in, a random session ID is generated and their info is
// saved to the sessions table. The session ID is sent to the browser as an
// HttpOnly cookie so the browser can prove who it is on future requests.
//
// Sessions are stored in the database — not in server memory — so an active
// login survives process restarts (deploys, crashes, free-tier idle
// spin-down). An in-memory map would otherwise be wiped on every restart,
// logging every admin out well before their session actually expired.
package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"time"
)

// SessionDuration is how long an admin session stays valid after login.
// The browser cookie's MaxAge is set to match this value (see auth_controller.go).
const SessionDuration = 30 * time.Minute

// SessionData holds the information we keep for one logged-in admin session.
// It is a snapshot of the admin's identity taken at login time — exactly
// like the previous in-memory design, a role change does not apply to an
// already-active session until the admin logs in again.
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

// New wraps db and starts a background goroutine that purges expired
// sessions from the table every 10 minutes.
func New(db *sql.DB) *Store {
	s := &Store{db: db}
	go s.cleanupLoop()
	return s
}

// Create inserts a new session row and returns a random 64-char hex session ID.
// The session expires after SessionDuration. mustResetPassword should mirror
// the admin's current Admin.MustResetPassword value at login time.
//
// Returns an error if the row could not be written — the caller must not
// treat the login as successful in that case, since the returned ID would
// point to a session that doesn't actually exist.
func (s *Store) Create(adminID int64, username, name, role string, mustResetPassword bool) (string, error) {
	// 32 random bytes → 64-character hex string, impossible to guess
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
// Returns the data and true if found and not expired, or empty and false otherwise.
func (s *Store) Get(sessionID string) (SessionData, bool) {
	var data SessionData
	var expiresAt time.Time

	err := s.db.QueryRow(`
		SELECT admin_id, username, name, role, must_reset_password, expires_at
		FROM sessions WHERE id = $1
	`, sessionID).Scan(&data.AdminID, &data.Username, &data.Name, &data.Role, &data.MustResetPassword, &expiresAt)
	if err != nil {
		return SessionData{}, false
	}

	if time.Now().After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
		return SessionData{}, false
	}

	data.ExpiresAt = expiresAt
	return data, true
}

// Delete removes a session immediately. Called when the admin logs out.
func (s *Store) Delete(sessionID string) {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID); err != nil {
		log.Printf("[SESSION] failed to delete session: %v", err)
	}
}

// ClearMustResetPassword removes the forced-password-reset flag from an
// active session. Called immediately after ChangeOwnPassword succeeds so the
// admin doesn't have to log out and back in to get unblocked.
func (s *Store) ClearMustResetPassword(sessionID string) {
	if _, err := s.db.Exec(`UPDATE sessions SET must_reset_password = FALSE WHERE id = $1`, sessionID); err != nil {
		log.Printf("[SESSION] failed to clear must_reset_password: %v", err)
	}
}

// cleanupLoop runs forever, waking every 10 minutes to delete expired sessions.
// This prevents the table from growing if sessions are created but never logged out.
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`); err != nil {
			log.Printf("[SESSION] cleanup failed: %v", err)
		}
	}
}
