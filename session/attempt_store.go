package session

import (
	"sync"
	"time"
)

const (
	MaxLoginAttempts = 3
	LockoutDuration  = 15 * time.Minute
)

type loginRecord struct {
	failures    int
	lockedUntil time.Time
}

// AttemptStore tracks consecutive failed login attempts per username in memory.
// After MaxLoginAttempts failures the username is locked for LockoutDuration.
// The lock is lifted automatically when it expires.
type AttemptStore struct {
	mu      sync.Mutex
	records map[string]*loginRecord
}

// NewAttemptStore creates an empty AttemptStore and starts a background cleanup goroutine.
func NewAttemptStore() *AttemptStore {
	s := &AttemptStore{records: make(map[string]*loginRecord)}
	go s.cleanupLoop()
	return s
}

// RecordFailure increments the failure counter for username.
// Returns true if the account is now locked (failure threshold just reached).
func (s *AttemptStore) RecordFailure(username string) (nowLocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.getOrCreate(username)
	r.failures++

	if r.failures >= MaxLoginAttempts {
		r.lockedUntil = time.Now().Add(LockoutDuration)
		return true
	}
	return false
}

// Remaining returns how many more failures are allowed before the account is locked.
func (s *AttemptStore) Remaining(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[username]
	if !ok {
		return MaxLoginAttempts
	}
	left := MaxLoginAttempts - r.failures
	if left < 0 {
		return 0
	}
	return left
}

// IsLocked reports whether the username is currently locked and when the lock expires.
// An expired lock is cleared automatically and reported as unlocked.
func (s *AttemptStore) IsLocked(username string) (locked bool, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[username]
	if !ok || r.lockedUntil.IsZero() {
		return false, time.Time{}
	}

	if time.Now().Before(r.lockedUntil) {
		return true, r.lockedUntil
	}

	// Lock has expired — reset so the user gets a fresh set of attempts
	r.failures = 0
	r.lockedUntil = time.Time{}
	return false, time.Time{}
}

// Reset clears all failure records for username. Call this on a successful login.
func (s *AttemptStore) Reset(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, username)
}

func (s *AttemptStore) getOrCreate(username string) *loginRecord {
	r, ok := s.records[username]
	if !ok {
		r = &loginRecord{}
		s.records[username] = r
	}
	return r
}

// cleanupLoop removes expired records every 10 minutes to keep memory bounded.
func (s *AttemptStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for username, r := range s.records {
			if !r.lockedUntil.IsZero() && now.After(r.lockedUntil) {
				delete(s.records, username)
			}
		}
		s.mu.Unlock()
	}
}
