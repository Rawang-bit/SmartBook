package controllers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
)

// ListBookings returns all bookings; optional ?room= filters by room name or ID.
func (c *Controller) ListBookings(w http.ResponseWriter, r *http.Request) {
	roomFilter := strings.TrimSpace(r.URL.Query().Get("room"))
	bookings, err := c.Bookings.List(roomFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bookings)
}

// CreateBooking books a room for a registered user (email must be active in the users table).
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

	c.sendBookingConfirmations(booking)

	c.auditPublic(r, booking.Email, "booking_created", "booking", booking.Purpose, booking.ID, "")

	writeJSON(w, http.StatusCreated, booking)
}

// bookingRecipients returns the owner email first, then all unique participant emails.
func bookingRecipients(b models.Booking) []string {
	seen := map[string]bool{strings.ToLower(b.Email): true}
	out := []string{b.Email}
	if b.Participants != "" {
		for _, p := range strings.Split(b.Participants, ",") {
			p = strings.TrimSpace(p)
			if p != "" && !seen[strings.ToLower(p)] {
				seen[strings.ToLower(p)] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// ownerName returns the booking owner's name for emails sent to the owner, blank for participants.
func ownerName(email string, b models.Booking) string {
	if strings.EqualFold(email, b.Email) {
		return b.User
	}
	return ""
}

// sendBookingConfirmations emails the owner and all participants; failures are logged, never block the response.
func (c *Controller) sendBookingConfirmations(b models.Booking) {
	for _, email := range bookingRecipients(b) {
		if err := utils.SendBookingConfirmationEmail(email, ownerName(email, b), b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose, b.Agenda); err != nil {
			log.Printf("[BOOKING CONFIRMATION] failed to notify %s: %v", email, err)
		}
	}
}

// sendCancellationNotifications emails the owner and all participants that the booking was cancelled.
func (c *Controller) sendCancellationNotifications(b models.Booking) {
	for _, email := range bookingRecipients(b) {
		if err := utils.SendBookingCancellationEmail(email, ownerName(email, b), b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose); err != nil {
			log.Printf("[BOOKING CANCELLATION] failed to notify %s: %v", email, err)
		}
	}
}

// sendMinutesNotifications emails the owner and all participants the finalised meeting minutes.
func (c *Controller) sendMinutesNotifications(b models.Booking) {
	for _, email := range bookingRecipients(b) {
		if err := utils.SendMinutesOfMeetingEmail(email, ownerName(email, b), b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose, b.MinutesOfMeeting); err != nil {
			log.Printf("[MINUTES OF MEETING] failed to notify %s: %v", email, err)
		}
	}
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

	c.audit(r, "booking_updated", "booking", booking.Purpose, booking.ID, "")

	writeJSON(w, http.StatusOK, booking)
}

// CancelBooking marks a booking as Cancelled. Admin version — no email ownership check.
func (c *Controller) CancelBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	// Fetch full details before cancelling — needed for audit log and cancellation emails.
	b, _ := c.Bookings.GetFullByID(id)

	err := c.Bookings.Cancel(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	c.audit(r, "booking_cancelled", "booking", b.Purpose, id, "booked by: "+b.Email)
	go c.sendCancellationNotifications(b)

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// DeleteBooking hard-deletes when ?hard=1 is set; otherwise cancels (soft delete).
func (c *Controller) DeleteBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	if r.URL.Query().Get("hard") != "1" {
		c.CancelBooking(w, r)
		return
	}

	// Fetch before deleting — the row will be gone after Delete() so we capture
	// the purpose and owner now for the audit log.
	b, _ := c.Bookings.GetByID(id)

	err := c.Bookings.Delete(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	c.audit(r, "booking_deleted", "booking", b.Purpose, id, "booked by: "+b.Email)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PublicBookingAction dispatches /cancel and /minutes by trailing path segment; ownership proved by email.
func (c *Controller) PublicBookingAction(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/cancel"):
		c.PublicCancelBooking(w, r)
	case strings.HasSuffix(path, "/minutes"):
		c.UpdateMinutesOfMeeting(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// PublicCancelBooking cancels a booking only if the request body's email matches the booking owner.
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

	// Fetch full details before cancelling — needed for audit log and cancellation emails.
	b, _ := c.Bookings.GetFullByID(id)

	err := c.Bookings.PublicCancel(id, email)
	if errors.Is(err, models.ErrOwnerMismatch) {
		writeError(w, http.StatusForbidden, "only the original booking owner can cancel this meeting")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	c.auditPublic(r, email, "booking_cancelled_by_owner", "booking", b.Purpose, id, "")
	go c.sendCancellationNotifications(b)

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// UpdateMinutesOfMeeting saves meeting notes for a completed booking; email proves ownership, subject to the edit window.
func (c *Controller) UpdateMinutesOfMeeting(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	var req models.MinutesOfMeetingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	b, err := c.Bookings.SetMinutesOfMeeting(id, email, req.Minutes)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if errors.Is(err, models.ErrOwnerMismatch) {
		writeError(w, http.StatusForbidden, "only the original booking owner can add meeting minutes")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	c.auditPublic(r, email, "booking_minutes_updated", "booking", b.Purpose, b.ID, "")

	// Fetch full booking (with room name) for the minutes notification email.
	if full, fetchErr := c.Bookings.GetFullByID(b.ID); fetchErr == nil {
		go c.sendMinutesNotifications(full)
	}

	writeJSON(w, http.StatusOK, b)
}
