// Package session provides a simple in-memory session store.
// When an admin logs in, a random session ID is generated and their info is
// saved here on the server. The session ID is sent to the browser as an
// HttpOnly cookie so the browser can prove who it is on future requests.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionDuration is how long an admin session stays valid after login.
// The browser cookie's MaxAge is set to match this value (see auth_controller.go).
const SessionDuration = 30 * time.Minute

// SessionData holds the information we keep for one logged-in admin session.
type SessionData struct {
	AdminID           int64
	Username          string
	Name              string
	Role              string // "super_admin" or "general_admin"
	MustResetPassword bool   // true until a temporary password is replaced
	ExpiresAt         time.Time
}

// Store is an in-memory session map protected by a mutex so it is safe
// when many HTTP requests arrive at the same time.
type Store struct {
	mu       sync.Mutex
	sessions map[string]SessionData
}

// New creates an empty session store and starts a background goroutine
// that removes expired sessions every hour to free memory.
func New() *Store {
	s := &Store{
		sessions: make(map[string]SessionData),
	}
	go s.cleanupLoop()
	return s
}

// Create saves a new session and returns a random 64-char hex session ID.
// Sessions expire after SessionDuration. mustResetPassword should mirror the
// admin's current Admin.MustResetPassword value at login time.
func (s *Store) Create(adminID int64, username, name, role string, mustResetPassword bool) string {
	// 32 random bytes → 64-character hex string, impossible to guess
	randomBytes := make([]byte, 32)
	_, _ = rand.Read(randomBytes)
	sessionID := hex.EncodeToString(randomBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[sessionID] = SessionData{
		AdminID:           adminID,
		Username:          username,
		Name:              name,
		Role:              role,
		MustResetPassword: mustResetPassword,
		ExpiresAt:         time.Now().Add(SessionDuration),
	}

	return sessionID
}

// Get looks up a session by ID.
// Returns the data and true if found and not expired, or empty and false otherwise.
func (s *Store) Get(sessionID string) (SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, found := s.sessions[sessionID]
	if !found {
		return SessionData{}, false
	}

	// Reject and delete expired sessions
	if time.Now().After(data.ExpiresAt) {
		delete(s.sessions, sessionID)
		return SessionData{}, false
	}

	return data, true
}

// Delete removes a session immediately. Called when the admin logs out.
func (s *Store) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// ClearMustResetPassword removes the forced-password-reset flag from an
// active session in memory. Called immediately after ChangeOwnPassword
// succeeds so the admin doesn't have to log out and back in to get unblocked.
func (s *Store) ClearMustResetPassword(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if data, ok := s.sessions[sessionID]; ok {
		data.MustResetPassword = false
		s.sessions[sessionID] = data
	}
}

// cleanupLoop runs forever, waking every 10 minutes to remove expired sessions.
// This prevents memory from growing if sessions are created but never logged out.
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for id, data := range s.sessions {
			if now.After(data.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
