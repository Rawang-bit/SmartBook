package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// passwordCharset deliberately excludes visually ambiguous characters
// (0/O, 1/l/I) since a human may need to retype this temporary password.
const passwordCharset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%*"

// GenerateRandomPassword returns a cryptographically random password of the
// given length (minimum 12, to satisfy the application's password policy).
// Intended for one-time temporary passwords that the recipient must change
// on first login — see AdminModel.CreateWithGeneratedPassword.
func GenerateRandomPassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}

	charsetLen := byte(len(passwordCharset))
	result := make([]byte, length)
	for i, b := range buf {
		result[i] = passwordCharset[b%charsetLen]
	}
	return string(result), nil
}

// GenerateSecureToken returns a cryptographically random 64-character hex
// string. Used for single-use links (registration confirmation) and for the
// trusted-device cookie issued when a user opts in to "remember this device".
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a token, for storing a
// verifier in the database without persisting the raw secret. Tokens here
// are high-entropy random values, not human-chosen secrets, so a fast
// deterministic hash is appropriate — unlike passwords, which use bcrypt
// specifically to resist brute-forcing low-entropy input.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
