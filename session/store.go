// Package session manages admin login sessions persisted in PostgreSQL (survives restarts).
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

// SessionData is a snapshot of admin identity at login; role changes only apply after re-login.
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

// Create inserts a session row and returns a random 64-char hex ID; error means login must fail.
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

// Get looks up a session; DB errors return (zero, false, err) — MUST NOT be treated as "not authenticated".
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

// DeleteByAdminID invalidates all sessions for adminID — ensures revoke/password-reset takes effect immediately.
func (s *Store) DeleteByAdminID(adminID int64) {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE admin_id = $1`, adminID); err != nil {
		log.Printf("[SESSION] failed to delete sessions for admin %d: %v", adminID, err)
	}
}

// ClearMustResetPassword clears the forced-reset flag from the active session after password change.
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
