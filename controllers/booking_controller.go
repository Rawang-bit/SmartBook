package controllers

import (
	"errors"
	"net/http"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
)

// ListBookings returns all bookings with room name and location.
// Optional ?room= query param filters by room name or ID. Public endpoint.
func (c *Controller) ListBookings(w http.ResponseWriter, r *http.Request) {
	roomFilter := strings.TrimSpace(r.URL.Query().Get("room"))
	bookings, err := c.Bookings.List(roomFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bookings)
}

// CreateBooking allows a public user to book a room.
// The user's email must already be registered by an admin.
func (c *Controller) CreateBooking(w http.ResponseWriter, r *http.Request) {
	var req models.BookingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	booking, err := c.Bookings.Save(0, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, booking)
}

// UpdateBooking allows an admin to edit any existing booking.
func (c *Controller) UpdateBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	var req models.BookingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	booking, err := c.Bookings.Save(id, req)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, booking)
}

// CancelBooking marks a booking as Cancelled. Admin version — no email ownership check.
func (c *Controller) CancelBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	err := c.Bookings.Cancel(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// DeleteBooking permanently removes a booking when ?hard=1 is in the URL.
// Without ?hard=1 it cancels instead of deleting (soft delete).
func (c *Controller) DeleteBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	if r.URL.Query().Get("hard") != "1" {
		c.CancelBooking(w, r)
		return
	}

	err := c.Bookings.Delete(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PublicCancelBooking lets a user cancel their own booking by proving their email.
// URL must end with "/cancel" and the body must contain {"email": "..."}.
func (c *Controller) PublicCancelBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	if !strings.HasSuffix(strings.Trim(r.URL.Path, "/"), "/cancel") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var req models.CancelBookingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	err := c.Bookings.PublicCancel(id, email)
	if errors.Is(err, models.ErrOwnerMismatch) {
		writeError(w, http.StatusForbidden, "only the original booking owner can cancel this meeting")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
