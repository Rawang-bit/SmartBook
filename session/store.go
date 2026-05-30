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

// SessionData holds the information we keep for one logged-in admin session.
type SessionData struct {
	AdminID   int64
	Username  string
	Name      string
	Role      string // "super_admin" or "general_admin"
	ExpiresAt time.Time
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
// Sessions expire after 8 hours.
func (s *Store) Create(adminID int64, username, name, role string) string {
	// 32 random bytes → 64-character hex string, impossible to guess
	randomBytes := make([]byte, 32)
	_, _ = rand.Read(randomBytes)
	sessionID := hex.EncodeToString(randomBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[sessionID] = SessionData{
		AdminID:   adminID,
		Username:  username,
		Name:      name,
		Role:      role,
		ExpiresAt: time.Now().Add(8 * time.Hour),
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

// cleanupLoop runs forever, waking every hour to remove expired sessions.
// This prevents memory from growing if sessions are created but never logged out.
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
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
