package controllers

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"bookroom-management-system/models"
)

// cookieName is the name of the session cookie placed in the browser.
const cookieName = "smartbook_session"

// HealthCheck confirms the server is running. Used by monitoring tools.
func (c *Controller) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Login checks credentials against the admins table using bcrypt.
// On success it creates a server-side session and sets an HttpOnly cookie.
// The browser will automatically send this cookie on every future request.
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

	// Fetch the stored bcrypt hash alongside the admin record
	var admin    models.Admin
	var hashInDB string
	var status   string
	err := c.DB.QueryRow(`
		SELECT id, username, name, role, status, password
		FROM admins
		WHERE username = $1
	`, username).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role, &status, &hashInDB)

	if err != nil {
		// Same message whether user does not exist or password is wrong —
		// prevents an attacker from discovering valid usernames.
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// bcrypt.CompareHashAndPassword is the safe way to check a password
	// against a stored hash. Returns nil if they match.
	if err := bcrypt.CompareHashAndPassword([]byte(hashInDB), []byte(password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Reject revoked accounts after verifying the password (avoids timing leak
	// that would reveal whether the username exists).
	if status == "revoked" {
		writeError(w, http.StatusForbidden, "your account access has been revoked")
		return
	}

	// Create a server-side session and store the admin info in memory
	sessionID := c.Sessions.Create(admin.ID, admin.Username, admin.Name, admin.Role)

	// Set Secure=true only in production (requires HTTPS).
	// In local development this is false so the cookie works over plain HTTP.
	secureCookie := os.Getenv("APP_ENV") == "production"

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   8 * 60 * 60, // 8 hours in seconds
		HttpOnly: true,         // JavaScript cannot read this cookie
		SameSite: http.SameSiteStrictMode,
		Secure:   secureCookie,
	})

	// Return admin info — no token, no password hash
	writeJSON(w, http.StatusOK, models.LoginResponse{Admin: admin})
}

// Logout deletes the session from the server and clears the cookie in the browser.
func (c *Controller) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		c.Sessions.Delete(cookie.Value)
	}

	// MaxAge = -1 tells the browser to delete the cookie immediately
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

// Me returns the currently logged-in admin's info.
// The frontend calls this to get the admin name to display on the page.
func (c *Controller) Me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
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
