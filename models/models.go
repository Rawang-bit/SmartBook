package models

// MinPasswordLength is the minimum number of characters required for any
// admin password — set at login, reset, self-service change, or creation.
// Centralized so a future policy change only needs to happen in one place.
const MinPasswordLength = 12

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
	Email     string `json:"email"`     // empty string when not set
	Status    string `json:"status"`    // "active" or "revoked"
	CreatedAt string `json:"createdAt"`
}

// AdminRequest is the JSON body for creating or updating an admin account.
// There is no separate username field — Email doubles as the login username
// for every admin (see AdminModel.Create) — and Password is only meaningful
// when creating a new account; Update ignores it.
type AdminRequest struct {
	Password string `json:"password"`
	Name     string `json:"name"`
	Role     string `json:"role"`  // "super_admin" or "general_admin"
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
	Username string `json:"username"`
	Email    string `json:"email"`
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
	Status               string `json:"status"`               // "pending", "active", "rejected", or "revoked"
	IntendedRole         string `json:"intendedRole"`         // "normal_user", "general_admin", or "super_admin"
	AwaitingConfirmation bool   `json:"awaitingConfirmation"` // true only for admin-added rows still awaiting the recipient's email click
}

// UserRequest is the JSON body sent when creating or updating a user.
// Role is only meaningful when an admin creates a new user — it records what
// should happen once the recipient confirms their email.
type UserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
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
	Name  string `json:"name"`
	Email string `json:"email"`
}

// VerifyOTPRequest is the JSON body sent to verify a registration code and
// create the user record on success.
type VerifyOTPRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	OTP   string `json:"otp"`
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
	ID           int64  `json:"id"`
	User         string `json:"user"`
	Email        string `json:"email"`
	RoomID       int64  `json:"roomId"`
	RoomName     string `json:"roomName"`
	Room         string `json:"room,omitempty"`     // alias for RoomName (legacy frontend)
	Location     string `json:"location,omitempty"` // room location
	Date         string `json:"date"`               // YYYY-MM-DD
	Start        string `json:"start"`              // HH:MM 24-hour (stored in DB)
	End          string `json:"end"`                // HH:MM 24-hour (stored in DB)
	StartTime    string `json:"startTime"`          // HH:MM AM/PM (for display)
	EndTime      string `json:"endTime"`            // HH:MM AM/PM (for display)
	Purpose      string `json:"purpose"`
	Agenda       string `json:"agenda"`              // optional free-text meeting agenda
	Participants string `json:"participants"`        // optional comma-separated participant emails
	Status       string `json:"status"`              // "Booked", "In Progress", "Completed", or "Cancelled"
}

// BookingRequest is the JSON body sent when creating or updating a booking.
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

// CancelBookingRequest is the JSON body sent when a public user cancels their booking.
type CancelBookingRequest struct {
	Email string `json:"email"`
}

// ErrorResponse is the standard JSON format for all error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}
