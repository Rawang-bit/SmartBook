package models

// Admin is an authenticated administrator account. Session data, login response
type Admin struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"` // "super_admin" or "general_admin"
}

// AdminDetail is returned in admin list responses (includes created_at and status).
type AdminDetail struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"` // "active" or "revoked"
	CreatedAt string `json:"createdAt"`
}

// AdminRequest is the JSON body for creating or updating an admin account.
type AdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Role     string `json:"role"` // "super_admin" or "general_admin"
}

// LoginRequest is the JSON body sent by the admin login form.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned after a successful login.
// The session is stored server-side; the browser receives it as an HttpOnly cookie.
type LoginResponse struct {
	Admin Admin `json:"admin"`
}

// User is a pre-registered person who is allowed to make room bookings.
// Admin must add a user here before they can book.
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserRequest is the JSON body sent when creating or updating a user.
type UserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Room is a meeting room that can be booked.
type Room struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Capacity int    `json:"capacity"` // Maximum number of people
	Location string `json:"location"` // Building or floor
	Status   string `json:"status"`   // "Active" or "Inactive"
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
	ID        int64  `json:"id"`
	User      string `json:"user"`
	Email     string `json:"email"`
	RoomID    int64  `json:"roomId"`
	RoomName  string `json:"roomName"`
	Room      string `json:"room,omitempty"`     // Alias for RoomName (legacy frontend compatibility)
	Location  string `json:"location,omitempty"` // Room location
	Date      string `json:"date"`               // YYYY-MM-DD
	Start     string `json:"start"`              // HH:MM 24-hour (stored in DB)
	End       string `json:"end"`                // HH:MM 24-hour (stored in DB)
	StartTime string `json:"startTime"`          // HH:MM AM/PM (for display)
	EndTime   string `json:"endTime"`            // HH:MM AM/PM (for display)
	Purpose   string `json:"purpose"`
	Status    string `json:"status"` // "Booked", "In Progress", "Completed", or "Cancelled"
}

// BookingRequest is the JSON body sent when creating or updating a booking.
// Accepts both 24-hour (start/end) and 12-hour AM/PM (startTime/endTime) formats.
type BookingRequest struct {
	User      string `json:"user"`
	Email     string `json:"email"`
	RoomID    int64  `json:"roomId"`
	Room      string `json:"room"`      // Room name — used if roomId is not provided
	Date      string `json:"date"`
	Start     string `json:"start"`     // 24-hour time
	End       string `json:"end"`       // 24-hour time
	StartTime string `json:"startTime"` // 12-hour AM/PM time
	EndTime   string `json:"endTime"`   // 12-hour AM/PM time
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
}

// CancelBookingRequest is the JSON body sent when a public user cancels their booking.
// They must provide their email to prove they own the booking.
type CancelBookingRequest struct {
	Email string `json:"email"`
}

// ErrorResponse is the standard JSON format for all error messages.
type ErrorResponse struct {
	Error string `json:"error"`
}
