package controllers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"bookroom-management-system/models"
	"bookroom-management-system/session"
)

// Controller holds the shared dependencies that all HTTP handlers need.
type Controller struct {
	DB       *sql.DB
	Sessions *session.Store
}

// New creates a Controller with a database connection and a session store.
func New(db *sql.DB, sessions *session.Store) *Controller {
	return &Controller{DB: db, Sessions: sessions}
}

// writeJSON sends a JSON response back to the client.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError sends a standard JSON error message to the client.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse{Error: message})
}

// decodeJSON reads the JSON body of a request into target.
// Returns true on success, or writes a 400 error and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	return true
}

// RequireAdmin is a middleware wrapper that checks for a valid session cookie.
// If the cookie is missing or the session is expired it returns 401.
// Otherwise it runs the real handler.
func (c *Controller) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("smartbook_session")
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

// RequireSuperAdmin is a middleware that allows only super_admin role through.
// Returns 403 if the logged-in admin is a general_admin.
func (c *Controller) RequireSuperAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("smartbook_session")
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

// getSession returns the session data for the current request.
func (c *Controller) getSession(r *http.Request) (session.SessionData, bool) {
	cookie, err := r.Cookie("smartbook_session")
	if err != nil {
		return session.SessionData{}, false
	}
	return c.Sessions.Get(cookie.Value)
}

// idFromPath extracts a positive integer ID from the URL path.
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

// isValidEmail returns true if the address is a properly formatted email (RFC 5322).
func isValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// normalizeEmail lower-cases an email and trims surrounding spaces.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeRoomStatus returns "Active" if status is empty, otherwise returns it as-is.
func normalizeRoomStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "Active"
	}
	return status
}

// normalizeBookingInput cleans all fields in a booking request:
// trims whitespace, normalises the email, and converts times to 24-hour format.
func normalizeBookingInput(req *models.BookingRequest) {
	req.User    = strings.TrimSpace(req.User)
	req.Email   = normalizeEmail(req.Email)
	req.Room    = strings.TrimSpace(req.Room)
	req.Date    = strings.TrimSpace(req.Date)
	req.Purpose = strings.TrimSpace(req.Purpose)
	req.Status  = strings.TrimSpace(req.Status)

	// Accept startTime/endTime (12-hour AM/PM) as fallbacks for start/end
	if req.Start == "" {
		req.Start = req.StartTime
	}
	if req.End == "" {
		req.End = req.EndTime
	}

	// Always store times in 24-hour HH:MM format
	req.Start = to24HourTime(req.Start)
	req.End   = to24HourTime(req.End)
}

// to24HourTime converts a time string to 24-hour HH:MM format.
// Handles three input formats:
//   - "14:30"     — already 24-hour, kept as-is
//   - "14:30:00"  — 24-hour with seconds, seconds dropped
//   - "02:30 PM"  — 12-hour AM/PM, converted to 24-hour
func to24HourTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if t, err := time.Parse("15:04", value); err == nil {
		return t.Format("15:04")
	}
	if t, err := time.Parse("15:04:05", value); err == nil {
		return t.Format("15:04")
	}
	if t, err := time.Parse("03:04 PM", strings.ToUpper(value)); err == nil {
		return t.Format("15:04")
	}

	return value
}

// toDisplayTime converts a 24-hour time string to 12-hour AM/PM format.
// Example: "14:30" → "02:30 PM"
func toDisplayTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if t, err := time.Parse("15:04", value); err == nil {
		return t.Format("03:04 PM")
	}
	if t, err := time.Parse("15:04:05", value); err == nil {
		return t.Format("03:04 PM")
	}
	if t, err := time.Parse("03:04 PM", strings.ToUpper(value)); err == nil {
		return t.Format("03:04 PM")
	}

	return value
}

// minutesFromTime converts a time string to total minutes since midnight.
// Example: "09:30" → 570 (9×60 + 30).
// Used to compare start and end times as plain numbers.
func minutesFromTime(value string) (int, error) {
	value = to24HourTime(value)
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// fillBookingDisplayFields adds the extra fields the frontend expects:
//   - Room      = copy of RoomName (legacy alias)
//   - StartTime / EndTime in 12-hour AM/PM format
//   - Status recomputed from the current time
func fillBookingDisplayFields(b *models.Booking) {
	b.Room      = b.RoomName
	b.StartTime = toDisplayTime(b.Start)
	b.EndTime   = toDisplayTime(b.End)
	b.Status    = computeBookingStatus(b.Date, b.Start, b.End, b.Status)
}

// computeBookingStatus returns the live display status based on the current time:
//   - "Cancelled"   → never changes
//   - "Booked"      → meeting has not started yet
//   - "In Progress" → current time is between start and end
//   - "Completed"   → meeting has ended
func computeBookingStatus(dateStr, startStr, endStr, savedStatus string) string {
	if strings.EqualFold(strings.TrimSpace(savedStatus), "Cancelled") {
		return "Cancelled"
	}

	bookingDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), time.Local)
	if err != nil {
		return "Booked"
	}

	startMinutes, err := minutesFromTime(startStr)
	if err != nil {
		return "Booked"
	}
	endMinutes, err := minutesFromTime(endStr)
	if err != nil {
		return "Booked"
	}

	startAt := bookingDate.Add(time.Duration(startMinutes) * time.Minute)
	endAt   := bookingDate.Add(time.Duration(endMinutes)   * time.Minute)
	now     := time.Now()

	switch {
	case now.Before(startAt):
		return "Booked"
	case now.Before(endAt):
		return "In Progress"
	default:
		return "Completed"
	}
}

// isUniqueViolation returns true when a database error is a unique-constraint failure.
func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique")
}

// isForeignKeyViolation returns true when a database error is a foreign-key failure.
func isForeignKeyViolation(err error) bool {
	return strings.Contains(err.Error(), "foreign key")
}
