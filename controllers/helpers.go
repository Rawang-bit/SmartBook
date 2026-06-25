package controllers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net"
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
	Audit         *models.AuditModel
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
		Audit:         &models.AuditModel{DB: db},
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

// RequireGeneralAdmin is middleware for day-to-day operational endpoints
// (rooms, bookings) that deliberately exclude super_admin. Per the role
// design, Super Admin's responsibilities are security, users, roles,
// permissions, and audit monitoring — not daily operations — so this is the
// inverse of RequireSuperAdmin rather than a relaxation of RequireAdmin.
func (c *Controller) RequireGeneralAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := c.requireAdminSession(w, r)
		if !ok {
			return
		}
		if data.Role != "general_admin" {
			writeError(w, http.StatusForbidden, "this is a general admin operation — super admin is limited to security, users, roles, and audit monitoring")
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

// clientIP extracts the caller's address for the audit trail, preferring
// X-Forwarded-For (set by the reverse proxy in front of the app in
// production — see HTTPSRedirect's identical reasoning for X-Forwarded-Proto)
// over r.RemoteAddr, which behind a proxy would otherwise just be the
// proxy's own address for every request.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// May be a comma-separated chain ("client, proxy1, proxy2") — the
		// original client is always first.
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// audit records one audit-trail entry attributed to the currently
// logged-in admin (or "system" if, unusually, there isn't one — e.g. a
// handler reachable before RequireAdmin's gate). See controllers/auth_controller.go
// for login/logout, which have no session yet and call c.Audit.Record directly.
func (c *Controller) audit(r *http.Request, action, targetType, targetLabel string, targetID int64, details string) {
	sess, ok := c.getSession(r)

	actorLabel := "system"
	var actorID int64
	if ok {
		actorLabel = sess.Username
		actorID = sess.AdminID
	}

	c.Audit.Record(models.AuditEntry{
		ActorType:   "admin",
		ActorID:     actorID,
		ActorLabel:  actorLabel,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		TargetLabel: targetLabel,
		Details:     details,
		IPAddress:   clientIP(r),
		UserAgent:   r.UserAgent(),
	})
}

// auditPublic records an audit-trail entry for an action with no admin
// session at all — a public, unauthenticated caller proving ownership of
// actorEmail (self-registration, booking create/cancel, Minutes of Meeting)
// rather than an authenticated admin.
func (c *Controller) auditPublic(r *http.Request, actorEmail, action, targetType, targetLabel string, targetID int64, details string) {
	c.Audit.Record(models.AuditEntry{
		ActorType:   "system",
		ActorLabel:  actorEmail,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		TargetLabel: targetLabel,
		Details:     details,
		IPAddress:   clientIP(r),
		UserAgent:   r.UserAgent(),
	})
}
