package utils

import (
	"crypto/rand"
	"fmt"
	"strings"
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

// DeriveUsernameFromEmail builds a candidate admin username from the
// local-part of an email address (e.g. "john.doe@dhi.bt" → "john.doe"),
// keeping only letters, digits, dots, underscores, and hyphens.
// Falls back to "admin" if nothing usable remains.
func DeriveUsernameFromEmail(email string) string {
	local := email
	if at := strings.IndexByte(email, '@'); at >= 0 {
		local = email[:at]
	}

	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}

	username := b.String()
	if username == "" {
		return "admin"
	}
	return username
}
