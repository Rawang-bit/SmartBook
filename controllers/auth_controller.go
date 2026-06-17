package controllers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bookroom-management-system/models"
	"bookroom-management-system/session"
	"bookroom-management-system/utils"
)

// HealthCheck confirms the server is running. Used by monitoring tools.
func (c *Controller) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Login validates credentials via the AdminModel, creates a server-side session,
// and sets an HttpOnly cookie. The browser sends this cookie on every future request.
//
// Cookie security attributes applied:
//   - HttpOnly:   JavaScript cannot read the cookie (mitigates XSS token theft)
//   - Secure:     HTTPS-only delivery in production (set via APP_ENV=production)
//   - SameSite:   Strict — cookie is never sent on cross-site requests (CSRF defence)
//   - __Host-:    prefix applied in production — binds the cookie to the exact host,
//                 no Domain attribute allowed, path must be "/" (prevents subdomain injection)
func (c *Controller) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)

	if username == "" || password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	// ── Lockout check ─────────────────────────────────────────────────────────
	// Must happen before any database work so locked accounts are rejected
	// immediately, regardless of whether the password would have been correct.
	if locked, until := c.LoginAttempts.IsLocked(username); locked {
		mins := int(time.Until(until).Minutes()) + 1
		writeError(w, http.StatusTooManyRequests,
			fmt.Sprintf("account locked after %d failed attempts — try again in %d minute(s)",
				session.MaxLoginAttempts, mins))
		return
	}

	admin, hash, status, err := c.Admins.GetByUsername(username)
	if errors.Is(err, models.ErrNotFound) || err != nil {
		// Count the failure even for unknown usernames to prevent brute-forcing
		// the username space; the error message is identical either way.
		nowLocked := c.LoginAttempts.RecordFailure(username)
		if nowLocked {
			writeError(w, http.StatusTooManyRequests,
				fmt.Sprintf("account locked after %d failed attempts — try again in 15 minutes",
					session.MaxLoginAttempts))
			return
		}
		remaining := c.LoginAttempts.Remaining(username)
		writeError(w, http.StatusUnauthorized,
			fmt.Sprintf("invalid username or password — %d attempt(s) remaining before lockout", remaining))
		return
	}

	if err := c.Admins.VerifyPassword(hash, password); err != nil {
		nowLocked := c.LoginAttempts.RecordFailure(username)
		if nowLocked {
			writeError(w, http.StatusTooManyRequests,
				fmt.Sprintf("account locked after %d failed attempts — try again in 15 minutes",
					session.MaxLoginAttempts))
			return
		}
		remaining := c.LoginAttempts.Remaining(username)
		writeError(w, http.StatusUnauthorized,
			fmt.Sprintf("invalid username or password — %d attempt(s) remaining before lockout", remaining))
		return
	}

	// Reject revoked accounts only after verifying the password so that the response
	// time for a revoked account with a correct password is indistinguishable from a
	// failed login (timing-attack safe).
	if status == "revoked" {
		writeError(w, http.StatusForbidden, "your account access has been revoked")
		return
	}

	// Successful login — clear the failure counter so the user starts fresh next time.
	c.LoginAttempts.Reset(username)

	sessionID := c.Sessions.Create(admin.ID, admin.Username, admin.Name, admin.Role)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(),
		Value:    sessionID,
		Path:     "/",
		MaxAge:   8 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isProduction(),
	})

	writeJSON(w, http.StatusOK, models.LoginResponse{Admin: admin})
}

// Logout deletes the server-side session and instructs the browser to expire the cookie.
func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName())
	if err == nil {
		c.Sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isProduction(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// Me returns the currently logged-in admin's info from the active session.
func (c *Controller) Me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}

	data, ok := c.Sessions.Get(cookie.Value)
	if !ok {
		writeError(w, http.StatusUnauthorized, "session expired, please log in again")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":       data.AdminID,
		"username": data.Username,
		"name":     data.Name,
		"role":     data.Role,
	})
}

// ForgotPassword initiates a password-reset flow.
// It requires both username and email to match the same active admin account
// before issuing a token, preventing account enumeration via either field alone.
//
// The response is always 200 OK regardless of whether the combination matched —
// this prevents an attacker from discovering valid username/email pairs by observing
// different responses.
func (c *Controller) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req models.ForgotPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	email    := utils.NormalizeEmail(req.Email)

	if username == "" || email == "" {
		writeError(w, http.StatusBadRequest, "username and email are required")
		return
	}
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	const neutralMsg = "If your username and email match a registered admin account, a password reset link has been sent to that address."

	adminID, adminName, adminEmail, err := c.Admins.GetByUsernameAndEmail(username, email)
	if err != nil {
		// No match (or DB error) — return the neutral message to prevent enumeration.
		writeJSON(w, http.StatusOK, map[string]string{"message": neutralMsg})
		return
	}

	token  := c.ResetTokens.Create(adminID)
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		if p := os.Getenv("PORT"); p != "" {
			appURL = "http://localhost:" + p
		} else {
			appURL = "http://localhost:8080"
		}
	}
	resetURL := appURL + "/login.html?token=" + token

	// ── Development shortcut ────────────────────────────────────────────────
	// When no email API key is configured and we are not in production, return
	// the reset URL directly in the response so the admin can use it without
	// needing a live email service. This field is never present in production.
	if !isProduction() && os.Getenv("RESEND_API_KEY") == "" {
		log.Printf("[PASSWORD RESET DEV] reset link for %q: %s", adminName, resetURL)
		writeJSON(w, http.StatusOK, map[string]string{
			"message":     neutralMsg,
			"devResetUrl": resetURL,
		})
		return
	}

	// ── Production / API key configured ────────────────────────────────────
	// Send the email synchronously so that delivery failures are visible in
	// the server log immediately. The response is still the neutral message —
	// we never tell the caller whether the address matched or delivery failed,
	// which prevents username/email enumeration.
	if err := utils.SendPasswordResetEmail(adminEmail, adminName, resetURL); err != nil {
		log.Printf("[PASSWORD RESET ERROR] could not deliver to %s: %v", adminEmail, err)
	} else {
		log.Printf("[PASSWORD RESET] reset email sent to %s", adminEmail)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": neutralMsg})
}

// ResetPassword consumes a single-use reset token and updates the admin's password.
// The token is validated and deleted atomically — replaying the same token always fails.
func (c *Controller) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req models.ResetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	token    := strings.TrimSpace(req.Token)
	password := strings.TrimSpace(req.Password)

	if token == "" {
		writeError(w, http.StatusBadRequest, "reset token is required")
		return
	}
	if len(password) < 12 {
		writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}

	adminID, ok := c.ResetTokens.Consume(token)
	if !ok {
		writeError(w, http.StatusBadRequest, "this reset link is invalid or has expired — please request a new one")
		return
	}

	if err := c.Admins.ResetPassword(adminID, password); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password, please try again")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successfully. You can now log in with your new password.",
	})
}
