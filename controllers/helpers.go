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

// Controller delegates HTTP requests to domain models; holds no business logic.
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

// decodeJSON reads the request body into target; writes 400 and returns false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	return true
}

// forcePasswordChangePath is the only endpoint allowed for temporary-password accounts.
const forcePasswordChangePath = "/api/admin/change-password"

// requireAdminSession looks up the session cookie; DB errors return 503 so transient failures don't force-logout valid sessions.
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

// RequireAdmin rejects unauthenticated requests and blocks all endpoints except forcePasswordChangePath for temp-password accounts.
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

// RequireGeneralAdmin allows only general_admin through (super_admin excluded from operational endpoints).
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

// getSession returns session data for the current request; treats DB errors as "no session".
func (c *Controller) getSession(r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil {
		return session.SessionData{}, false
	}
	data, found, _ := c.Sessions.Get(cookie.Value)
	return data, found
}

// idFromPath extracts the integer ID after prefix (e.g. "/api/rooms/42" → 42).
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

// clientIP prefers X-Forwarded-For over RemoteAddr (behind a proxy RemoteAddr is the proxy's IP).
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

// audit records an audit entry for the logged-in admin; falls back to "system" if no session.
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

// auditPublic records an audit entry for a public caller identified by email (registration, booking).
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
