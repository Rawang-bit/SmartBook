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
	mux.HandleFunc("POST /api/auth/login",  c.Login)
	mux.HandleFunc("POST /api/auth/logout", c.Logout)
	mux.HandleFunc("GET  /api/auth/me",     c.Me)

	// ── Users ─────────────────────────────────────────────────────────────────
	// Public: validate whether an email is registered (called before booking)
	mux.HandleFunc("GET /api/public/users/validate", c.ValidateUser)
	mux.HandleFunc("GET /api/public/users",          c.ListUsers)

	// Admin: full CRUD on registered users
	mux.HandleFunc("GET /api/users",     c.RequireAdmin(c.ListUsers))
	mux.HandleFunc("POST /api/users",    c.RequireAdmin(c.CreateUser))
	mux.HandleFunc("PUT /api/users/",    c.RequireAdmin(c.UpdateUser))
	mux.HandleFunc("DELETE /api/users/", c.RequireAdmin(c.DeleteUser))

	// ── Rooms ─────────────────────────────────────────────────────────────────
	// Public: read room list (needed by the public calendar)
	mux.HandleFunc("GET /api/rooms", c.ListRooms)

	// Admin: create, update, delete rooms
	mux.HandleFunc("POST /api/rooms",    c.RequireAdmin(c.CreateRoom))
	mux.HandleFunc("PUT /api/rooms/",    c.RequireAdmin(c.UpdateRoom))
	mux.HandleFunc("DELETE /api/rooms/", c.RequireAdmin(c.DeleteRoom))

	// ── Bookings ──────────────────────────────────────────────────────────────
	// Public: view all bookings, create a new booking
	mux.HandleFunc("GET /api/bookings",  c.ListBookings)
	mux.HandleFunc("POST /api/bookings", c.CreateBooking)

	// Public: cancel own booking — POST /api/bookings/{id}/cancel
	mux.HandleFunc("POST /api/bookings/", c.PublicCancelBooking)

	// Admin: edit or delete any booking
	mux.HandleFunc("PUT /api/bookings/",    c.RequireAdmin(c.UpdateBooking))
	mux.HandleFunc("DELETE /api/bookings/", c.RequireAdmin(c.DeleteBooking))
}
