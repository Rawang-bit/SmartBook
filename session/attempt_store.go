package session

import "sync"

const MaxLoginAttempts = 3

type loginRecord struct {
	failures int
}

// AttemptStore tracks consecutive failed login attempts per username (in-memory only).
// Lockout state is persisted to the DB by the auth controller; this store is the
// fast in-session layer that also survives the check before the DB lookup.
type AttemptStore struct {
	mu      sync.Mutex
	records map[string]*loginRecord
}

// NewAttemptStore creates an empty AttemptStore.
func NewAttemptStore() *AttemptStore {
	return &AttemptStore{records: make(map[string]*loginRecord)}
}

// RecordFailure increments the failure counter; returns true when the account just reached the lock threshold.
func (s *AttemptStore) RecordFailure(username string) (nowLocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.getOrCreate(username)
	r.failures++
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
