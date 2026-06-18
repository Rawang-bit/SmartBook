package routes

import (
	"net/http"

	"bookroom-management-system/controllers"
)

// RegisterRoutes wires every API URL to its handler.
//
// Public routes  — callable by anyone, no login needed.
// Admin routes   — wrapped with RequireAdmin; rejected with 401 if no valid session cookie.
func RegisterRoutes(mux *http.ServeMux, c *controllers.Controller) {

	// ── Health ────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/health", c.HealthCheck)

	// ── Auth ──────────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/auth/login",           c.Login)
	mux.HandleFunc("POST /api/auth/logout",          c.Logout)
	mux.HandleFunc("GET  /api/auth/me",              c.Me)
	mux.HandleFunc("POST /api/auth/forgot-password", c.ForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password",  c.ResetPassword)

	// ── Public access gate (self-registration with OTP) ─────────────────────────
	// Email check happens once, up front, on the public access page.
	// Booking itself no longer asks for email verification; only cancellation does.
	mux.HandleFunc("POST /api/access/check-email",  c.CheckEmail)
	mux.HandleFunc("POST /api/register/send-otp",   c.SendRegistrationOTP)
	mux.HandleFunc("POST /api/register/verify-otp", c.VerifyRegistrationOTP)

	// ── Users ─────────────────────────────────────────────────────────────────
	// Public: an admin-added user clicks this link from their email to confirm
	// ownership and activate the account. The token itself is the credential.
	mux.HandleFunc("POST /api/users/confirm", c.ConfirmRegistration)

	// Admin: list and create — general_admin can add users
	mux.HandleFunc("GET /api/users",  c.RequireAdmin(c.ListUsers))
	mux.HandleFunc("POST /api/users", c.RequireAdmin(c.CreateUser))

	// Super admin only: edit and delete users
	mux.HandleFunc("PUT /api/users/",    c.RequireSuperAdmin(c.UpdateUser))
	mux.HandleFunc("DELETE /api/users/", c.RequireSuperAdmin(c.DeleteUser))

	// Approve or reject a pending self-registration — POST /api/users/{id}/approve|reject
	mux.HandleFunc("POST /api/users/", c.RequireAdmin(c.ToggleUserStatus))

	// ── Rooms ─────────────────────────────────────────────────────────────────
	// Public: read room list (needed by the public calendar)
	mux.HandleFunc("GET /api/rooms", c.ListRooms)

	// Super admin only: create, update, delete rooms
	mux.HandleFunc("POST /api/rooms",    c.RequireSuperAdmin(c.CreateRoom))
	mux.HandleFunc("PUT /api/rooms/",    c.RequireSuperAdmin(c.UpdateRoom))
	mux.HandleFunc("DELETE /api/rooms/", c.RequireSuperAdmin(c.DeleteRoom))

	// ── Bookings ──────────────────────────────────────────────────────────────
	// Public: view all bookings, create a new booking
	mux.HandleFunc("GET /api/bookings",  c.ListBookings)
	mux.HandleFunc("POST /api/bookings", c.CreateBooking)

	// Public: cancel own booking — POST /api/bookings/{id}/cancel
	mux.HandleFunc("POST /api/bookings/", c.PublicCancelBooking)

	// Super admin only: edit or delete any booking
	mux.HandleFunc("PUT /api/bookings/",    c.RequireSuperAdmin(c.UpdateBooking))
	mux.HandleFunc("DELETE /api/bookings/", c.RequireSuperAdmin(c.DeleteBooking))

	// ── Admin management (super_admin only) ───────────────────────────────────
	mux.HandleFunc("GET /api/admins",     c.RequireSuperAdmin(c.ListAdmins))
	mux.HandleFunc("POST /api/admins",    c.RequireSuperAdmin(c.CreateAdmin))
	mux.HandleFunc("PUT /api/admins/",    c.RequireSuperAdmin(c.UpdateAdmin))
	mux.HandleFunc("PATCH /api/admins/",  c.RequireSuperAdmin(c.ResetAdminPassword))
	mux.HandleFunc("DELETE /api/admins/", c.RequireSuperAdmin(c.DeleteAdmin))

	// Revoke or restore an admin's access — POST /api/admins/{id}/revoke|restore
	mux.HandleFunc("POST /api/admins/", c.RequireSuperAdmin(c.ToggleAdminStatus))

	// Any logged-in admin can change their own password
	mux.HandleFunc("POST /api/admin/change-password", c.RequireAdmin(c.ChangeOwnPassword))
}
