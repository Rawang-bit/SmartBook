package session

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// OTPExpiry is how long a verification code remains valid after it is issued.
const OTPExpiry = 10 * time.Minute

type otpRecord struct {
	code      string
	expiresAt time.Time
}

// OTPStore is an in-memory store of one-time verification codes keyed by email.
type OTPStore struct {
	mu    sync.Mutex
	codes map[string]otpRecord
}

// NewOTPStore creates an empty OTPStore and starts a background cleanup goroutine.
func NewOTPStore() *OTPStore {
	s := &OTPStore{codes: make(map[string]otpRecord)}
	go s.cleanupLoop()
	return s
}

// Create generates a random 6-digit code for email, valid for OTPExpiry.
// A new call for the same email overwrites any previous unused code.
func (s *OTPStore) Create(email string) string {
	code := generateCode()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[email] = otpRecord{code: code, expiresAt: time.Now().Add(OTPExpiry)}

	return code
}

// Verify checks code against the stored value for email.
// The record is consumed (deleted) regardless of outcome — a code can only be used once.
func (s *OTPStore) Verify(email, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.codes[email]
	delete(s.codes, email)

	if !ok || time.Now().After(r.expiresAt) {
		return false
	}
	return r.code == code
}

// generateCode returns a cryptographically random 6-digit numeric string.
func generateCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	n := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
	return fmt.Sprintf("%06d", n)
}

// cleanupLoop removes expired codes every 5 minutes to keep memory bounded.
func (s *OTPStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for email, r := range s.codes {
			if now.After(r.expiresAt) {
				delete(s.codes, email)
			}
		}
		s.mu.Unlock()
	}
}
