package models

import (
	"fmt"
	"unicode"
)

// RoleLabel converts an internal role key to a human-readable label for audit entries.
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

// MinPasswordLength is the minimum character count for any admin password.
const MinPasswordLength = 12

// ValidatePasswordComplexity enforces minimum length plus a mix of character classes.
// Applied everywhere an admin password is set — creation, reset, and self-service change.
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
	Email     string `json:"email"`
	Status    string `json:"status"` // "active" or "revoked"
	CreatedAt string `json:"createdAt"`
}

// AdminRequest is the JSON body for creating or updating an admin account.
// Email doubles as the login username; Password is ignored on updates.
type AdminRequest struct {
	Password string `json:"password"`
	Name     string `json:"name"`
	Role     string `json:"role"` // "super_admin" or "general_admin"
	Email    string `json:"email"`
}

// LoginRequest is the JSON body sent by the admin login form.
type LoginRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captchaToken"`
}

// LoginResponse is returned after a successful login.
type LoginResponse struct {
	Admin Admin `json:"admin"`
}

// ForgotPasswordRequest is the JSON body sent by the forgot-password form.
// Both username and email must match a single active admin account.
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

// User is a registered person who may make room bookings.
// Self-registered users start as "pending" until an admin approves or rejects them.
// Admin-added users also start as "pending" with AwaitingConfirmation=true until
// the recipient clicks the emailed confirmation link.
// Active users can be set to "revoked" to pull booking access without deleting the record.
type User struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Email                string `json:"email"`
	Phone                string `json:"phone,omitempty"`      // optional contact number shown to reviewing admin
	Status               string `json:"status"`               // "pending", "active", "rejected", or "revoked"
	IntendedRole         string `json:"intendedRole"`         // "normal_user", "general_admin", or "super_admin"
	AwaitingConfirmation bool   `json:"awaitingConfirmation"` // true only for admin-added rows not yet email-confirmed
	RejectionReason      string `json:"rejectionReason,omitempty"`
	CreatedAt            string `json:"createdAt,omitempty"` // YYYY-MM-DD HH:MM
	AdminRole            string `json:"adminRole,omitempty"` // "general_admin" if also an admin (Normal User + General Admin combo), else empty
}

// UserRequest is the JSON body for creating or updating a user.
type UserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"` // intended role at confirmation time
}

// RejectUserRequest is the JSON body when rejecting a pending registration.
type RejectUserRequest struct {
	Reason string `json:"reason"`
}

// ConfirmRegistrationRequest is the JSON body when a newly admin-added user clicks
// the confirmation link emailed to them.
type ConfirmRegistrationRequest struct {
	Token string `json:"token"`
}

// ApproveUserRequest is the JSON body when an admin approves a pending self-registration.
// Role defaults to "normal_user"; any admin role promotes to a new admin account.
type ApproveUserRequest struct {
	Role string `json:"role"`
}

// CheckEmailRequest is the JSON body for the public access gate's email lookup.
// CaptchaToken is used only by the OTP-send path, not by the email-check path.
type CheckEmailRequest struct {
	Email        string `json:"email"`
	CaptchaToken string `json:"captchaToken"`
}

// SendOTPRequest is the JSON body to request a registration verification code.
type SendOTPRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	CaptchaToken string `json:"captchaToken"`
}

// VerifyOTPRequest is the JSON body to verify a one-time code — for self-registration
// (Name required) or re-verifying an active user on an unrecognized device (Name ignored).
// RememberDevice=true issues a trusted-device cookie valid for 30 days; never assumed.
type VerifyOTPRequest struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"` // only meaningful for self-registration
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

// RoomRequest is the JSON body for creating or updating a room.
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
	Agenda           string `json:"agenda"`
	Participants     string `json:"participants"`     // comma-separated participant emails
	Status           string `json:"status"`           // "Booked", "In Progress", "Completed", or "Cancelled"
	MinutesOfMeeting string `json:"minutesOfMeeting"` // set by booking owner after meeting ends
	MinutesEditable  bool   `json:"minutesEditable"`  // true if SetMinutesOfMeeting would currently accept a save
}

// BookingRequest is the JSON body for creating or updating a booking.
type BookingRequest struct {
	User         string `json:"user"`
	Email        string `json:"email"`
	RoomID       int64  `json:"roomId"`
	Room         string `json:"room"`      // room name — used if roomId is not provided
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

// CancelBookingRequest is the JSON body when a public user cancels their booking.
type CancelBookingRequest struct {
	Email string `json:"email"`
}

// MinutesOfMeetingRequest is the JSON body when a booking's owner adds or edits Minutes of Meeting.
type MinutesOfMeetingRequest struct {
	Email   string `json:"email"`
	Minutes string `json:"minutes"`
}

// ErrorResponse is the standard JSON format for all error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}

// AuditEntry is what a controller records for one audit-trail event.
// ActorLabel/TargetLabel are denormalized snapshots taken at action time so entries stay
// meaningful even if the underlying actor or target is later renamed or deleted.
type AuditEntry struct {
	ActorType   string // "admin" or "system"
	ActorID     int64  // 0 when there is no authenticated actor
	ActorLabel  string
	Action      string // e.g. "login_success", "user_approved", "room_deleted"
	TargetType  string // "user", "admin", "room", "booking", or ""
	TargetID    int64
	TargetLabel string
	Details     string // free-text, e.g. "role: normal_user -> general_admin"
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

// AuditFilter narrows ListAuditLogs. Every field is optional.
// Page is 1-based; Page<=0 means "no pagination" (full export, not on-screen list).
type AuditFilter struct {
	ActorLabel string // case-insensitive substring match on actor or target label
	Action     string // exact match
	From       string // YYYY-MM-DD, inclusive
	To         string // YYYY-MM-DD, inclusive
	Page       int
}

// AuditPage is one page of audit-trail results plus pagination metadata.
type AuditPage struct {
	Logs       []AuditLog `json:"logs"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
	TotalPages int        `json:"totalPages"`
}
