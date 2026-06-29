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
	mux.HandleFunc("GET /api/config", c.PublicConfig)

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

	// Re-verification for an already-registered, active user on a device
	// whose trusted-device cookie is missing, mismatched, or expired.
	mux.HandleFunc("POST /api/access/send-otp",   c.SendAccessVerificationOTP)
	mux.HandleFunc("POST /api/access/verify-otp", c.VerifyAccessOTP)

	// ── Users ─────────────────────────────────────────────────────────────────
	// Public: an admin-added user clicks this link from their email to confirm
	// ownership and activate the account. The token itself is the credential.
	mux.HandleFunc("POST /api/users/confirm", c.ConfirmRegistration)

	// Admin: list, create, edit, and delete — general_admin manages Users
	// end to end; assigning an admin role within these still requires the
	// matching privilege (see canAssignRole).
	mux.HandleFunc("GET /api/users",     c.RequireAdmin(c.ListUsers))
	mux.HandleFunc("POST /api/users",    c.RequireAdmin(c.CreateUser))
	mux.HandleFunc("PUT /api/users/",    c.RequireAdmin(c.UpdateUser))
	mux.HandleFunc("DELETE /api/users/", c.RequireAdmin(c.DeleteUser))

	// Approve or reject a pending self-registration — POST /api/users/{id}/approve|reject
	mux.HandleFunc("POST /api/users/", c.RequireAdmin(c.ToggleUserStatus))

	// ── Rooms ─────────────────────────────────────────────────────────────────
	// Public: read room list — needed by the public calendar, by Dashboard's
	// stats (both roles), and by History (super_admin). Reading room data
	// isn't "managing" it, so this stays open to everyone; only the writes
	// below are role-gated.
	mux.HandleFunc("GET /api/rooms", c.ListRooms)

	// General admin only: create, update, delete rooms — operational, so
	// super_admin is deliberately excluded (see RequireGeneralAdmin).
	mux.HandleFunc("POST /api/rooms",    c.RequireGeneralAdmin(c.CreateRoom))
	mux.HandleFunc("PUT /api/rooms/",    c.RequireGeneralAdmin(c.UpdateRoom))
	mux.HandleFunc("DELETE /api/rooms/", c.RequireGeneralAdmin(c.DeleteRoom))

	// ── Bookings ──────────────────────────────────────────────────────────────
	// Public: view all bookings — same reasoning as Rooms above, needed by
	// Dashboard and History too. Creating a booking is a write, though, so
	// that one still excludes an authenticated super_admin.
	mux.HandleFunc("GET /api/bookings",  c.ListBookings)
	mux.HandleFunc("POST /api/bookings", c.BlockSuperAdmin(c.CreateBooking))

	// Public: cancel own booking (POST /api/bookings/{id}/cancel) or add
	// Minutes of Meeting after it ends (POST /api/bookings/{id}/minutes)
	mux.HandleFunc("POST /api/bookings/", c.PublicBookingAction)

	// General admin only: edit or delete any booking — operational, so
	// super_admin is deliberately excluded (see RequireGeneralAdmin).
	mux.HandleFunc("PUT /api/bookings/",    c.RequireGeneralAdmin(c.UpdateBooking))
	mux.HandleFunc("DELETE /api/bookings/", c.RequireGeneralAdmin(c.DeleteBooking))

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

	// ── Audit trail (super_admin only) ────────────────────────────────────────
	// Read-only by design — no endpoint exists to edit or delete an entry.
	mux.HandleFunc("GET /api/audit-logs", c.RequireSuperAdmin(c.ListAuditLogs))
}
