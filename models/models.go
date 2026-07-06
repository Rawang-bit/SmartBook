package models

import (
	"fmt"
	"unicode"
)

// RoleLabel converts an internal role key to a human-readable label for
// audit log entries and user-facing messages.
func RoleLabel(role string) string {
	switch role {
	case "super_admin":
		return "Super Admin"
	case "general_admin":
		return "General Admin"
	default:
		return "Normal User"
	}
}

// MinPasswordLength is the minimum number of characters required for any
// admin password — set at login, reset, self-service change, or creation.
// Centralized so a future policy change only needs to happen in one place.
const MinPasswordLength = 12

// ValidatePasswordComplexity enforces the minimum length plus a mix of
// character classes, so a long password made of digits only (or letters
// only) is still rejected. Applied everywhere an admin password is set —
// creation, reset, and self-service change.
func ValidatePasswordComplexity(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case !unicode.IsSpace(r):
			hasSymbol = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSymbol {
		return fmt.Errorf("password must mix uppercase, lowercase, a number, and a special character")
	}
	return nil
}

// Admin is an authenticated administrator account used in session data and login responses.
type Admin struct {
	ID                int64  `json:"id"`
	Username          string `json:"username"`
	Name              string `json:"name"`
	Role              string `json:"role"`              // "super_admin" or "general_admin"
	MustResetPassword bool   `json:"mustResetPassword"` // true for accounts created with a generated temporary password
}

// AdminDetail is returned in admin-list responses and includes management fields.
type AdminDetail struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Email     string `json:"email"`  // empty string when not set
	Status    string `json:"status"` // "active" or "revoked"
	CreatedAt string `json:"createdAt"`
}

// AdminRequest is the JSON body for creating or updating an admin account.
// There is no separate username field — Email doubles as the login username
// for every admin (see AdminModel.Create) — and Password is only meaningful
// when creating a new account; Update ignores it.
type AdminRequest struct {
	Password string `json:"password"`
	Name     string `json:"name"`
	Role     string `json:"role"` // "super_admin" or "general_admin"
	Email    string `json:"email"`
}

// LoginRequest is the JSON body sent by the admin login form.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned after a successful login.
type LoginResponse struct {
	Admin Admin `json:"admin"`
}

// ForgotPasswordRequest is the JSON body sent by the forgot-password form.
// Both username and email must match a single admin account for the reset to proceed.
type ForgotPasswordRequest struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	CaptchaToken string `json:"captchaToken"`
}

// ResetPasswordRequest is the JSON body sent by the reset-password form.
type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// User is a registered person who is allowed to make room bookings.
// Self-registered users start as "pending" until an admin approves or
// rejects them in the Users page.
// Users added directly by an admin also start as "pending", but they are
// never reviewed by an admin in the UI — AwaitingConfirmation is true until
// the new person clicks the confirmation link emailed to them, at which
// point the account (or admin promotion, per IntendedRole) activates itself.
// An already-active user can later be set to "revoked", pulling their
// booking access without deleting their record; restoring sets it back to
// "active".
type User struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Email                string `json:"email"`
	Phone                string `json:"phone,omitempty"`      // optional contact number, shown to a reviewing admin
	Status               string `json:"status"`               // "pending", "active", "rejected", or "revoked"
	IntendedRole         string `json:"intendedRole"`         // "normal_user", "general_admin", or "super_admin"
	AwaitingConfirmation bool   `json:"awaitingConfirmation"` // true only for admin-added rows still awaiting the recipient's email click
	RejectionReason      string `json:"rejectionReason,omitempty"`
	CreatedAt            string `json:"createdAt,omitempty"` // registration date/time, YYYY-MM-DD HH:MM
	AdminRole            string `json:"adminRole,omitempty"` // "general_admin" if this person is also an admin (the Normal User + General Admin combination), else empty — computed by UserModel.List, not stored
}

// UserRequest is the JSON body sent when creating or updating a user.
// Role is only meaningful when an admin creates a new user — it records what
// should happen once the recipient confirms their email.
type UserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
}

// RejectUserRequest is the JSON body sent when an admin rejects a pending
// registration — Reason is stored on the user record for later reference.
type RejectUserRequest struct {
	Reason string `json:"reason"`
}

// ConfirmRegistrationRequest is the JSON body sent when a newly admin-added
// user clicks the confirmation link emailed to them.
type ConfirmRegistrationRequest struct {
	Token string `json:"token"`
}

// ApproveUserRequest is the JSON body sent when an admin approves a pending
// self-registration. Role defaults to "normal_user" when omitted, which
// keeps the existing behaviour (the applicant can only book/cancel rooms).
// Any other role ("general_admin" or "super_admin") promotes the request
// into a new admin account instead — only a super_admin may grant that.
type ApproveUserRequest struct {
	Role string `json:"role"`
}

// CheckEmailRequest is the JSON body sent by the public access gate to find
// out whether an email already belongs to a registered user.
type CheckEmailRequest struct {
	Email string `json:"email"`
}

// SendOTPRequest is the JSON body sent to request a registration verification code.
type SendOTPRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	CaptchaToken string `json:"captchaToken"`
}

// VerifyOTPRequest is the JSON body sent to verify a one-time code — either
// to complete self-registration (Name is required) or to re-verify an
// already-active user's identity on an unrecognized device (Name is
// ignored). RememberDevice, if true, issues a trusted-device cookie on
// success so this browser can skip OTP next time, for up to 30 days —
// the user opts in explicitly; it is never assumed.
type VerifyOTPRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"` // optional contact number, only meaningful for self-registration
	OTP            string `json:"otp"`
	RememberDevice bool   `json:"rememberDevice"`
}

// Room is a meeting room that can be booked.
type Room struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
	Location string `json:"location"`
	Status   string `json:"status"` // "Active" or "Inactive"
}

// RoomRequest is the JSON body sent when creating or updating a room.
type RoomRequest struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
	Location string `json:"location"`
	Status   string `json:"status"`
}

// Booking is a room reservation.
type Booking struct {
	ID               int64  `json:"id"`
	User             string `json:"user"`
	Email            string `json:"email"`
	RoomID           int64  `json:"roomId"`
	RoomName         string `json:"roomName"`
	Room             string `json:"room,omitempty"`     // alias for RoomName (legacy frontend)
	Location         string `json:"location,omitempty"` // room location
	Date             string `json:"date"`               // YYYY-MM-DD
	Start            string `json:"start"`              // HH:MM 24-hour (stored in DB)
	End              string `json:"end"`                // HH:MM 24-hour (stored in DB)
	StartTime        string `json:"startTime"`          // HH:MM AM/PM (for display)
	EndTime          string `json:"endTime"`            // HH:MM AM/PM (for display)
	Purpose          string `json:"purpose"`
	Agenda           string `json:"agenda"`           // optional free-text meeting agenda
	Participants     string `json:"participants"`     // optional comma-separated participant emails
	Status           string `json:"status"`           // "Booked", "In Progress", "Completed", or "Cancelled"
	MinutesOfMeeting string `json:"minutesOfMeeting"` // set by the booking owner after the meeting ends — see BookingModel.SetMinutesOfMeeting
	MinutesEditable  bool   `json:"minutesEditable"`  // true if SetMinutesOfMeeting would currently accept a save — see isWithinMinutesEditWindow
}

// BookingRequest is the JSON body sent when creating or updating a booking.
type BookingRequest struct {
	User         string `json:"user"`
	Email        string `json:"email"`
	RoomID       int64  `json:"roomId"`
	Room         string `json:"room"` // room name — used if roomId is not provided
	Date         string `json:"date"`
	Start        string `json:"start"`     // 24-hour time
	End          string `json:"end"`       // 24-hour time
	StartTime    string `json:"startTime"` // 12-hour AM/PM time
	EndTime      string `json:"endTime"`   // 12-hour AM/PM time
	Purpose      string `json:"purpose"`
	Agenda       string `json:"agenda"`
	Participants string `json:"participants"` // comma-separated email addresses
	Status       string `json:"status"`
}

// CancelBookingRequest is the JSON body sent when a public user cancels their booking.
type CancelBookingRequest struct {
	Email string `json:"email"`
}

// MinutesOfMeetingRequest is the JSON body sent when a booking's owner adds
// or edits the Minutes of Meeting for a completed meeting.
type MinutesOfMeetingRequest struct {
	Email   string `json:"email"`
	Minutes string `json:"minutes"`
}

// ErrorResponse is the standard JSON format for all error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AuditEntry is what a controller records for one audit-trail event.
// ActorLabel/TargetLabel are denormalized snapshots (username/email/name at
// the time of the action) so the log entry stays meaningful even if the
// underlying actor or target account is later renamed or deleted.
type AuditEntry struct {
	ActorType   string // "admin" or "system" (e.g. an anonymous failed login attempt)
	ActorID     int64  // 0 when there is no authenticated actor
	ActorLabel  string
	Action      string // e.g. "login_success", "user_approved", "room_deleted"
	TargetType  string // "user", "admin", "room", "booking", or "" for actions with no single target
	TargetID    int64
	TargetLabel string
	Details     string // free-text description, e.g. "role: normal_user -> general_admin"
	IPAddress   string
	UserAgent   string
}

// AuditLog is one row returned from the audit trail for display.
type AuditLog struct {
	ID          int64  `json:"id"`
	ActorType   string `json:"actorType"`
	ActorLabel  string `json:"actorLabel"`
	Action      string `json:"action"`
	TargetType  string `json:"targetType"`
	TargetLabel string `json:"targetLabel"`
	Details     string `json:"details"`
	IPAddress   string `json:"ipAddress"`
	UserAgent   string `json:"userAgent"`
	CreatedAt   string `json:"createdAt"`
}

// AuditFilter narrows ListAuditLogs. Every field is optional; an empty
// filter matches the whole audit trail.
// Page is 1-based. Page <= 0 means "no pagination" — return every matching
// row in one go, used only for exporting the full filtered result rather
// than for the paginated on-screen list.
type AuditFilter struct {
	ActorLabel string // matches actor or target label, case-insensitive substring
	Action     string // exact match
	From       string // YYYY-MM-DD, inclusive
	To         string // YYYY-MM-DD, inclusive
	Page       int
}

// AuditPage is one page of audit-trail results, plus enough metadata for
// the caller to render pagination controls.
type AuditPage struct {
	Logs       []AuditLog `json:"logs"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	TotalPages int        `json:"totalPages"`
}
