package session

import (
	"sync"
	"time"
)

const MaxLoginAttempts = 3

// attemptTTL bounds how long a failed-login record is kept before the cleanup loop evicts
// it, so repeatedly submitting distinct usernames can't grow the in-memory map without bound.
const attemptTTL = 30 * time.Minute

type loginRecord struct {
	failures    int
	lastFailure time.Time
}

// AttemptStore tracks consecutive failed login attempts per username (in-memory only).
// Lockout state is persisted to the DB by the auth controller; this store is the
// fast in-session layer that also survives the check before the DB lookup.
type AttemptStore struct {
	mu      sync.Mutex
	records map[string]*loginRecord
}

// NewAttemptStore creates an empty AttemptStore and starts a cleanup goroutine.
func NewAttemptStore() *AttemptStore {
	s := &AttemptStore{records: make(map[string]*loginRecord)}
	go s.cleanupLoop()
	return s
}

// RecordFailure increments the failure counter; returns true when the account just reached the lock threshold.
func (s *AttemptStore) RecordFailure(username string) (nowLocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.getOrCreate(username)
	r.failures++
	r.lastFailure = time.Now()
	return r.failures >= MaxLoginAttempts
}

// Remaining returns how many more failures are allowed before the account locks.
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

// IsLocked reports whether the username has hit the failure threshold in this server session.
func (s *AttemptStore) IsLocked(username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.records[username]
	return ok && r.failures >= MaxLoginAttempts
}

// Reset clears all failure records for username. Call on successful login or admin unlock.
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

// cleanupLoop evicts failure records that have been idle past attemptTTL to keep memory bounded.
func (s *AttemptStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-attemptTTL)
		s.mu.Lock()
		for username, r := range s.records {
			if r.lastFailure.Before(cutoff) {
				delete(s.records, username)
			}
		}
		s.mu.Unlock()
	}
}
