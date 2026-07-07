package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Character classes exclude visually ambiguous characters (0/O, 1/l/I) so a human
// can retype a generated temporary password without confusion.
const (
	upperChars      = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerChars      = "abcdefghijkmnopqrstuvwxyz"
	digitChars      = "23456789"
	symbolChars     = "!@#$%*"
	passwordCharset = upperChars + lowerChars + digitChars + symbolChars
)

// GenerateRandomPassword returns a cryptographically random password of the given
// length (minimum 12). At least one character from each class is guaranteed, so the
// result always satisfies models.ValidatePasswordComplexity without relying on chance.
func GenerateRandomPassword(length int) (string, error) {
	if length < 12 {
		length = 12
	}

	result := make([]byte, length)
	classes := []string{upperChars, lowerChars, digitChars, symbolChars}
	for i, class := range classes {
		c, err := randomCharFrom(class)
		if err != nil {
			return "", err
		}
		result[i] = c
	}
	for i := len(classes); i < length; i++ {
		c, err := randomCharFrom(passwordCharset)
		if err != nil {
			return "", err
		}
		result[i] = c
	}

	if err := shuffleBytes(result); err != nil {
		return "", err
	}
	return string(result), nil
}

func randomCharFrom(set string) (byte, error) {
	buf := make([]byte, 1)
	if _, err := rand.Read(buf); err != nil {
		return 0, fmt.Errorf("crypto/rand: %w", err)
	}
	return set[buf[0]%byte(len(set))], nil
}

// shuffleBytes randomizes order in place (Fisher-Yates) so the guaranteed
// one-per-class characters aren't always in the same leading positions.
func shuffleBytes(b []byte) error {
	for i := len(b) - 1; i > 0; i-- {
		buf := make([]byte, 1)
		if _, err := rand.Read(buf); err != nil {
			return fmt.Errorf("crypto/rand: %w", err)
		}
		j := int(buf[0]) % (i + 1)
		b[i], b[j] = b[j], b[i]
	}
	return nil
}

// GenerateSecureToken returns a cryptographically random 64-character hex string.
// Used for single-use links (registration confirmation) and trusted-device cookies.
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hex digest of a token, for storing a verifier in the
// database without persisting the raw secret. Tokens here are high-entropy random values
// so a fast hash is appropriate — unlike passwords, which use bcrypt to resist brute-forcing.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
