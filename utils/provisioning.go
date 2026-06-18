package utils

import (
	"crypto/rand"
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
