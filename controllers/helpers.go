package controllers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/session"
)

// Controller coordinates HTTP requests by delegating to the appropriate domain model.
// It holds no business logic — that lives exclusively in the model layer.
type Controller struct {
	Sessions      *session.Store
	ResetTokens   *session.ResetStore
	LoginAttempts *session.AttemptStore
	OTPs          *session.OTPStore
	Admins        *models.AdminModel
	Users         *models.UserModel
	Rooms         *models.RoomModel
	Bookings      *models.BookingModel
}

// New wires the database connection into each domain model and returns a Controller.
func New(db *sql.DB, sessions *session.Store) *Controller {
	return &Controller{
		Sessions:      sessions,
		ResetTokens:   session.NewResetStore(),
		LoginAttempts: session.NewAttemptStore(),
		OTPs:          session.NewOTPStore(),
		Admins:        &models.AdminModel{DB: db},
		Users:         &models.UserModel{DB: db},
		Rooms:         &models.RoomModel{DB: db},
		Bookings:      &models.BookingModel{DB: db},
	}
}

// writeJSON sends a JSON-encoded response with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError sends a standard JSON error payload to the client.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse{Error: message})
}

// decodeJSON reads the request body into target.
// Returns true on success; writes a 400 error and returns false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	return true
}

// forcePasswordChangePath is the only admin-authenticated endpoint reachable
// while a session has MustResetPassword set — every other admin endpoint is
// blocked server-side until the temporary password is replaced.
const forcePasswordChangePath = "/api/admin/change-password"

// requireAdminSession does the shared cookie + session lookup for RequireAdmin
// and RequireSuperAdmin. Returns ok=false after already writing the
// appropriate error response — callers should return immediately when ok is
// false. A database error during lookup responds with 503, not 401, so a
// momentary connectivity hiccup never gets mistaken for an expired session
// and force-logs-out an admin whose session is actually still valid.
func (c *Controller) requireAdminSession(w http.ResponseWriter, r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "admin login required")
		return session.SessionData{}, false
	}

	data, found, sessErr := c.Sessions.Get(cookie.Value)
	if sessErr != nil {
		log.Printf("[SESSION ERROR] lookup failed: %v", sessErr)
		writeError(w, http.StatusServiceUnavailable, "temporary server issue, please try again in a moment")
		return session.SessionData{}, false
	}
	if !found {
		writeError(w, http.StatusUnauthorized, "session expired, please log in again")
		return session.SessionData{}, false
	}

	if data.MustResetPassword && r.URL.Path != forcePasswordChangePath {
		writeError(w, http.StatusForbidden, "you must change your temporary password before continuing")
		return session.SessionData{}, false
	}

	return data, true
}

// RequireAdmin is middleware that rejects requests without a valid session cookie.
// It also blocks every endpoint except forcePasswordChangePath for accounts
// that still need to replace a generated temporary password.
func (c *Controller) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.requireAdminSession(w, r); !ok {
			return
		}
		next(w, r)
	}
}

// RequireSuperAdmin is middleware that allows only the super_admin role through.
// Like RequireAdmin, it also enforces the temporary-password-change block.
func (c *Controller) RequireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := c.requireAdminSession(w, r)
		if !ok {
			return
		}
		if data.Role != "super_admin" {
			writeError(w, http.StatusForbidden, "super admin access required")
			return
		}
		next(w, r)
	}
}

// getSession returns the session data attached to the current request.
// A database error during lookup is treated the same as "no session" here —
// this helper is only ever called from inside a handler that RequireAdmin/
// RequireSuperAdmin already gated, so a fresh DB error on this second lookup
// within the same request is vanishingly unlikely, and failing closed
// (denying whatever privilege check the caller is making) is the safe
// default either way.
func (c *Controller) getSession(r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		return session.SessionData{}, false
	}
	data, found, _ := c.Sessions.Get(cookie.Value)
	return data, found
}

// idFromPath extracts a positive integer ID from the URL path segment after prefix.
// Example: "/api/rooms/42" with prefix "/api/rooms/" returns 42.
func idFromPath(w http.ResponseWriter, r *http.Request, prefix string) (int64, bool) {
	remaining := strings.TrimPrefix(r.URL.Path, prefix)
	remaining = strings.Trim(remaining, "/")

	// Handle paths like "42/cancel" — take only the first segment
	if strings.Contains(remaining, "/") {
		remaining = strings.Split(remaining, "/")[0]
	}

	id, err := strconv.ParseInt(remaining, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
