package controllers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/session"
	"bookroom-management-system/utils"
)

// HealthCheck confirms the server is running.
func (c *Controller) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PublicConfig exposes TURNSTILE_SITE_KEY to the frontend; blank when unset tells the frontend to skip the widget.
func (c *Controller) PublicConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"turnstileSiteKey": os.Getenv("TURNSTILE_SITE_KEY"),
	})
}

// rejectLockedLogin records the audit entry and writes the 429 response for a locked-account
// login attempt, whether the lock was detected via in-memory session state or the DB status.
func (c *Controller) rejectLockedLogin(w http.ResponseWriter, r *http.Request, username string) {
	c.Audit.Record(models.AuditEntry{
		ActorType:  "system",
		ActorLabel: username,
		Action:     "login_blocked_locked",
		Details:    "account is locked — requires admin to unlock",
		IPAddress:  clientIP(r),
		UserAgent:  r.UserAgent(),
	})
	writeError(w, http.StatusTooManyRequests,
		fmt.Sprintf("account locked after %d failed attempts — contact an administrator to unlock your account",
			session.MaxLoginAttempts))
}

// recordFailedLoginAttempt increments the counter; same error for bad username or bad password prevents enumeration.
// adminID is 0 when the username does not match any account (nothing to persist to DB in that case).
// currentStatus is the admin's status at the time of the attempt; only 'active' accounts get locked.
func (c *Controller) recordFailedLoginAttempt(w http.ResponseWriter, r *http.Request, username string, adminID int64, currentStatus string) {
	nowLocked := c.LoginAttempts.RecordFailure(username)
	remaining := c.LoginAttempts.Remaining(username)

	details := fmt.Sprintf("attempt %d of %d", session.MaxLoginAttempts-remaining, session.MaxLoginAttempts)
	c.Audit.Record(models.AuditEntry{
		ActorType:  "system",
		ActorLabel: username,
		Action:     "login_failed",
		Details:    details,
		IPAddress:  clientIP(r),
		UserAgent:  r.UserAgent(),
	})

	if nowLocked {
		if adminID > 0 && currentStatus == "active" {
			if err := c.Admins.SetStatus(adminID, "locked"); err != nil {
				log.Printf("[LOGIN LOCK] failed to persist lockout for %s (id=%d): %v", username, adminID, err)
			}
		}
		c.Audit.Record(models.AuditEntry{
			ActorType:  "system",
			ActorLabel: username,
			Action:     "account_locked",
			Details:    fmt.Sprintf("locked after %d failed attempts — requires admin to unlock", session.MaxLoginAttempts),
			IPAddress:  clientIP(r),
			UserAgent:  r.UserAgent(),
		})
		c.rejectLockedLogin(w, r, username)
		return
	}
	writeError(w, http.StatusUnauthorized,
		fmt.Sprintf("invalid username or password — %d attempt(s) remaining before lockout", remaining))
}

// Login validates credentials and creates a server-side session with a hardened cookie.
func (c *Controller) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	username := utils.NormalizeEmail(req.Username)
	password := strings.TrimSpace(req.Password)

	if username == "" || password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	if err := utils.VerifyTurnstile(req.CaptchaToken, clientIP(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fast in-session lockout check — rejects immediately if this server session
	// already tracked 3 failures for this username (before any DB work).
	if c.LoginAttempts.IsLocked(username) {
		c.rejectLockedLogin(w, r, username)
		return
	}

	admin, hash, status, err := c.Admins.GetByUsername(username)
	if errors.Is(err, models.ErrNotFound) || err != nil {
		// Count the failure even for unknown usernames — error message is identical either way.
		c.recordFailedLoginAttempt(w, r, username, 0, "")
		return
	}

	// DB lock check — catches accounts locked before a server restart (in-memory state was cleared).
	if status == "locked" {
		c.rejectLockedLogin(w, r, username)
		return
	}

	if err := c.Admins.VerifyPassword(hash, password); err != nil {
		c.recordFailedLoginAttempt(w, r, username, admin.ID, status)
		return
	}

	// Check revoked after password verification so timing is indistinguishable from a
	// failed login — prevents an attacker from detecting revoked accounts by response time.
	if status == "revoked" {
		c.Audit.Record(models.AuditEntry{
			ActorType:  "admin",
			ActorID:    admin.ID,
			ActorLabel: admin.Username,
			Action:     "login_blocked_revoked",
			Details:    "login attempt on a revoked account",
			IPAddress:  clientIP(r),
			UserAgent:  r.UserAgent(),
		})
		writeError(w, http.StatusForbidden, "your account access has been revoked")
		return
	}

	// Successful login — clear the failure counter so the user starts fresh next time.
	c.LoginAttempts.Reset(username)

	sessionID, err := c.Sessions.Create(admin.ID, admin.Username, admin.Name, admin.Role, admin.MustResetPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session, please try again")
		return
	}

	setSessionCookie(w, sessionID, int(session.SessionDuration.Seconds()))

	c.Audit.Record(models.AuditEntry{
		ActorType:  "admin",
		ActorID:    admin.ID,
		ActorLabel: admin.Username,
		Action:     "login_success",
		IPAddress:  clientIP(r),
		UserAgent:  r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, models.LoginResponse{Admin: admin})
}

// Logout deletes the server-side session and instructs the browser to expire the cookie.
func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName())
	if err == nil {
		if sess, found, sessErr := c.Sessions.Get(cookie.Value); sessErr == nil && found {
			c.Audit.Record(models.AuditEntry{
				ActorType:  "admin",
				ActorID:    sess.AdminID,
				ActorLabel: sess.Username,
				Action:     "logout",
				IPAddress:  clientIP(r),
				UserAgent:  r.UserAgent(),
			})
		}
		c.Sessions.Delete(cookie.Value)
	}

	setSessionCookie(w, "", -1)

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// Me returns the currently logged-in admin's info from the active session.
func (c *Controller) Me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}

	data, ok, sessErr := c.Sessions.Get(cookie.Value)
	if sessErr != nil {
		log.Printf("[SESSION ERROR] lookup failed: %v", sessErr)
		writeError(w, http.StatusServiceUnavailable, "temporary server issue, please try again in a moment")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "session expired, please log in again")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                data.AdminID,
		"username":          data.Username,
		"name":              data.Name,
		"role":              data.Role,
		"mustResetPassword": data.MustResetPassword,
	})
}

// ForgotPassword issues a reset token only when username+email match; always responds 200 OK to prevent enumeration.
func (c *Controller) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req models.ForgotPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	email := utils.NormalizeEmail(req.Email)

	if username == "" || email == "" {
		writeError(w, http.StatusBadRequest, "username and email are required")
		return
	}
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}
	if err := utils.VerifyTurnstile(req.CaptchaToken, clientIP(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	const neutralMsg = "Success: Password reset link has been sent to your email."

	adminID, adminName, adminEmail, err := c.Admins.GetByUsernameAndEmail(username, email)
	if err != nil {
		// No match — log for security monitoring but return the neutral message.
		c.Audit.Record(models.AuditEntry{
			ActorType:  "system",
			ActorLabel: username,
			Action:     "password_reset_failed",
			Details:    "no active account matched the provided email",
			IPAddress:  clientIP(r),
			UserAgent:  r.UserAgent(),
		})
		writeJSON(w, http.StatusOK, map[string]string{"message": neutralMsg})
		return
	}

	c.Audit.Record(models.AuditEntry{
		ActorType:  "admin",
		ActorID:    adminID,
		ActorLabel: adminEmail,
		Action:     "password_reset_requested",
		Details:    "reset link sent to " + adminEmail,
		IPAddress:  clientIP(r),
		UserAgent:  r.UserAgent(),
	})

	token := c.ResetTokens.Create(adminID)
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		if p := os.Getenv("PORT"); p != "" {
			appURL = "http://localhost:" + p
		} else {
			appURL = "http://localhost:8080"
		}
	}
	resetURL := appURL + "/login.html?token=" + token

	// Dev shortcut: when no email API key is configured, return the URL directly
	// so it's usable without a live email service. Never present in production.
	if !isProduction() && os.Getenv("RESEND_API_KEY") == "" {
		log.Printf("[PASSWORD RESET DEV] reset link for %q: %s", adminName, resetURL)
		writeJSON(w, http.StatusOK, map[string]string{
			"message":     neutralMsg,
			"devResetUrl": resetURL,
		})
		return
	}

	// Send email synchronously so delivery failures are immediately visible in the log.
	// Response is always the neutral message — never reveals whether delivery failed.
	if err := utils.SendPasswordResetEmail(adminEmail, adminName, resetURL); err != nil {
		log.Printf("[PASSWORD RESET ERROR] could not deliver to %s: %v", adminEmail, err)
	} else {
		log.Printf("[PASSWORD RESET] reset email sent to %s", adminEmail)
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": neutralMsg})
}

// ResetPassword validates and atomically consumes a reset token, then updates the password.
func (c *Controller) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req models.ResetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	token := strings.TrimSpace(req.Token)
	password := strings.TrimSpace(req.Password)

	if token == "" {
		writeError(w, http.StatusBadRequest, "reset token is required")
		return
	}
	if err := models.ValidatePasswordComplexity(password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	adminID, ok := c.ResetTokens.Consume(token)
	if !ok {
		c.Audit.Record(models.AuditEntry{
			ActorType:  "system",
			ActorLabel: "unknown",
			Action:     "password_reset_token_invalid",
			Details:    "reset token was invalid or already expired",
			IPAddress:  clientIP(r),
			UserAgent:  r.UserAgent(),
		})
		writeError(w, http.StatusBadRequest, "this reset link is invalid or has expired — please request a new one")
		return
	}

	if err := c.Admins.ApplyPasswordReset(adminID, password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Best-effort label lookup — the reset completed regardless of whether this succeeds.
	resetAdmin, _, _ := c.Admins.GetByID(adminID)
	actorLabel := resetAdmin.Username
	if actorLabel == "" {
		actorLabel = fmt.Sprintf("admin#%d", adminID)
	}
	c.Audit.Record(models.AuditEntry{
		ActorType:  "admin",
		ActorID:    adminID,
		ActorLabel: actorLabel,
		Action:     "password_reset_completed",
		Details:    "password changed via reset link",
		IPAddress:  clientIP(r),
		UserAgent:  r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successfully. You can now log in with your new password.",
	})
}
