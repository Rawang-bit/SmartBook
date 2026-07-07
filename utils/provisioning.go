package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Character classes exclude ambiguous characters (0/O, 1/l/I) so temp passwords can be retyped without confusion.
const (
	upperChars      = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerChars      = "abcdefghijkmnopqrstuvwxyz"
	digitChars      = "23456789"
	symbolChars     = "!@#$%*"
	passwordCharset = upperChars + lowerChars + digitChars + symbolChars
)

// GenerateRandomPassword returns a random password of given length (min 12) with at least one char from each class.
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

// shuffleBytes randomizes byte order in place (Fisher-Yates) so class-guaranteed chars aren't always first.
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

// GenerateSecureToken returns a random 64-char hex string for single-use links and device cookies.
func GenerateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns SHA-256 hex of a token for storage; high-entropy tokens don't need bcrypt's brute-force resistance.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
