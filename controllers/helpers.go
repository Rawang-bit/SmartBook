package controllers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/session"
)

// Controller coordinates HTTP requests by delegating to the appropriate domain model.
// It holds no business logic — that lives exclusively in the model layer.
type Controller struct {
	Sessions *session.Store
	Admins   *models.AdminModel
	Users    *models.UserModel
	Rooms    *models.RoomModel
	Bookings *models.BookingModel
}

// New wires the database connection into each domain model and returns a Controller.
func New(db *sql.DB, sessions *session.Store) *Controller {
	return &Controller{
		Sessions: sessions,
		Admins:   &models.AdminModel{DB: db},
		Users:    &models.UserModel{DB: db},
		Rooms:    &models.RoomModel{DB: db},
		Bookings: &models.BookingModel{DB: db},
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

// RequireAdmin is middleware that rejects requests without a valid session cookie.
func (c *Controller) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "admin login required")
			return
		}
		_, ok := c.Sessions.Get(cookie.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "session expired, please log in again")
			return
		}
		next(w, r)
	}
}

// RequireSuperAdmin is middleware that allows only the super_admin role through.
func (c *Controller) RequireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "admin login required")
			return
		}
		data, ok := c.Sessions.Get(cookie.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "session expired, please log in again")
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
func (c *Controller) getSession(r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		return session.SessionData{}, false
	}
	return c.Sessions.Get(cookie.Value)
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
