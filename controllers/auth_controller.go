package controllers

import (
	"errors"
	"net/http"

	"bookroom-management-system/models"
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

	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	admin, hash, status, err := c.Admins.GetByUsername(req.Username)
	if errors.Is(err, models.ErrNotFound) || err != nil {
		// Identical message for missing user and wrong password — prevents username enumeration.
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	if err := c.Admins.VerifyPassword(hash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Reject revoked accounts only after verifying the password so that the response
	// time for a revoked account with a correct password is indistinguishable from a
	// failed login (timing-attack safe).
	if status == "revoked" {
		writeError(w, http.StatusForbidden, "your account access has been revoked")
		return
	}

	sessionID := c.Sessions.Create(admin.ID, admin.Username, admin.Name, admin.Role)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(), // __Host- prefix in production for maximum host binding
		Value:    sessionID,
		Path:     "/",
		MaxAge:   8 * 60 * 60,         // 8 hours in seconds
		HttpOnly: true,                 // JavaScript cannot access this cookie
		SameSite: http.SameSiteStrictMode,
		Secure:   isProduction(),       // HTTPS-only in production; allows HTTP in development
	})

	writeJSON(w, http.StatusOK, models.LoginResponse{Admin: admin})
}

// Logout deletes the server-side session and instructs the browser to expire the cookie.
func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName())
	if err == nil {
		c.Sessions.Delete(cookie.Value)
	}

	// Expire the cookie immediately by setting MaxAge to -1.
	// The Secure and SameSite attributes must match the original Set-Cookie so
	// that the browser treats this as the same cookie and actually removes it.
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
