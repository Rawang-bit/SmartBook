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

// forcePasswordChangePath is the only admin endpoint reachable while MustResetPassword
// is set — every other admin endpoint is blocked until the temporary password is replaced.
const forcePasswordChangePath = "/api/admin/change-password"

// requireAdminSession does the shared cookie + session lookup for RequireAdmin and
// RequireSuperAdmin. Returns ok=false after writing the error — callers must return
// immediately. A DB error responds 503 (not 401) so a transient hiccup doesn't
// force-logout an admin whose session is actually still valid.
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
// Also blocks all endpoints except forcePasswordChangePath for temporary-password accounts.
func (c *Controller) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.requireAdminSession(w, r); !ok {
			return
		}
		next(w, r)
	}
}

// forbiddenModuleMsg is the standard 403 body for all role-mismatch rejections.
const forbiddenModuleMsg = "You do not have permission to access this module."

// RequireSuperAdmin allows only super_admin through, and enforces the temp-password block.
func (c *Controller) RequireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := c.requireAdminSession(w, r)
		if !ok {
			return
		}
		if data.Role != "super_admin" {
			writeError(w, http.StatusForbidden, forbiddenModuleMsg)
			return
		}
		next(w, r)
	}
}

// RequireGeneralAdmin allows only general_admin through. Super Admin is deliberately
// excluded from operational endpoints (rooms, bookings) — its scope is security,
// users, roles, and audit monitoring, not day-to-day operations.
func (c *Controller) RequireGeneralAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, ok := c.requireAdminSession(w, r)
		if !ok {
			return
		}
		if data.Role != "general_admin" {
			writeError(w, http.StatusForbidden, forbiddenModuleMsg)
			return
		}
		next(w, r)
	}
}

// BlockSuperAdmin rejects requests from an authenticated super_admin but passes
// everyone else through (including anonymous callers with no session at all).
// Used on endpoints that serve both the public calendar and general_admin.
func (c *Controller) BlockSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sess, ok := c.getSession(r); ok && sess.Role == "super_admin" {
			writeError(w, http.StatusForbidden, forbiddenModuleMsg)
			return
		}
		next(w, r)
	}
}

// getSession returns the session data for the current request.
// A DB error is treated as "no session" — this is only called inside handlers already
// gated by RequireAdmin, so a second DB error within the same request is vanishingly
// unlikely; failing closed (denying the privilege check) is the safe default.
func (c *Controller) getSession(r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		return session.SessionData{}, false
	}
	data, found, _ := c.Sessions.Get(cookie.Value)
	return data, found
}

// idFromPath extracts a positive integer ID from the URL path segment after prefix.
// Example: "/api/rooms/42" with prefix "/api/rooms/" → 42.
func idFromPath(w http.ResponseWriter, r *http.Request, prefix string) (int64, bool) {
	remaining := strings.TrimPrefix(r.URL.Path, prefix)
	remaining = strings.Trim(remaining, "/")

	// Handle paths like "42/cancel" — take only the first segment.
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

// clientIP extracts the caller's address for the audit trail. Prefers X-Forwarded-For
// (set by the reverse proxy) over r.RemoteAddr, which behind a proxy would otherwise
// always be the proxy's own address.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		// May be "client, proxy1, proxy2" — the original client is always first.
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// audit records one audit-trail entry attributed to the currently logged-in admin.
// Falls back to "system" actor when no session is present (unusual for gated handlers).
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

// auditPublic records an audit entry for a public, unauthenticated caller who proves
// ownership of actorEmail (self-registration, booking create/cancel, Minutes of Meeting).
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
