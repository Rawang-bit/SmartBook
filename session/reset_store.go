package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ResetTokenData holds the admin ID associated with a password-reset token.
type ResetTokenData struct {
	AdminID   int64
	ExpiresAt time.Time
}

// ResetStore is an in-memory store for single-use password-reset tokens; tokens are valid 15 minutes and deleted on first use.
type ResetStore struct {
	mu     sync.Mutex
	tokens map[string]ResetTokenData
}

// NewResetStore creates an empty ResetStore and starts a cleanup goroutine.
func NewResetStore() *ResetStore {
	rs := &ResetStore{tokens: make(map[string]ResetTokenData)}
	go rs.cleanupLoop()
	return rs
}

// Create generates a random 64-char hex token for adminID, expiring in 15 minutes.
func (rs *ResetStore) Create(adminID int64) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.tokens[token] = ResetTokenData{
		AdminID:   adminID,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	return token
}

// Consume atomically validates and deletes a token; returns (adminID, true) on success, (0, false) if unknown/used/expired.
func (rs *ResetStore) Consume(token string) (int64, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	data, found := rs.tokens[token]
	// Always delete to prevent replay even if the token has expired.
	delete(rs.tokens, token)

	if !found || time.Now().After(data.ExpiresAt) {
		return 0, false
	}
	return data.AdminID, true
}

// cleanupLoop removes expired tokens every 5 minutes to keep memory bounded.
func (rs *ResetStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		rs.mu.Lock()
		for token, data := range rs.tokens {
			if now.After(data.ExpiresAt) {
				delete(rs.tokens, token)
			}
		}
		rs.mu.Unlock()
	}
}
