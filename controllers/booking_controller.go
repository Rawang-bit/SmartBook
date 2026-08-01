package controllers

import (
	"errors"
	"fmt"
	"io"
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

// recipientName returns the display name for a booking email recipient: the owner's stored
// name for the owner, or the participant's registered name (looked up by email) when they're
// a known user; blank if the participant isn't a registered user.
func (c *Controller) recipientName(email string, b models.Booking) string {
	if strings.EqualFold(email, b.Email) {
		return b.User
	}
	if u, err := c.Users.GetByEmail(email); err == nil {
		return u.Name
	}
	return ""
}

// notifyRecipients emails every booking recipient via sendFn, logging (never blocking) on failure.
// isOwner tells sendFn whether the recipient is the booking owner or a participant.
func (c *Controller) notifyRecipients(b models.Booking, logPrefix string, sendFn func(email, name string, isOwner bool) error) {
	for _, email := range bookingRecipients(b) {
		isOwner := strings.EqualFold(email, b.Email)
		if err := sendFn(email, c.recipientName(email, b), isOwner); err != nil {
			log.Printf("[%s] failed to notify %s: %v", logPrefix, email, err)
		}
	}
}

// sendBookingConfirmations emails the owner and all participants; failures are logged, never block the response.
func (c *Controller) sendBookingConfirmations(b models.Booking) {
	c.notifyRecipients(b, "BOOKING CONFIRMATION", func(email, name string, isOwner bool) error {
		return utils.SendBookingConfirmationEmail(email, name, isOwner, b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose, b.Agenda)
	})
}

// sendCancellationNotifications emails the owner and all participants that the booking was cancelled.
func (c *Controller) sendCancellationNotifications(b models.Booking) {
	c.notifyRecipients(b, "BOOKING CANCELLATION", func(email, name string, isOwner bool) error {
		return utils.SendBookingCancellationEmail(email, name, b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose)
	})
}

// sendMinutesNotifications emails the owner and all participants the finalised meeting minutes document.
func (c *Controller) sendMinutesNotifications(b models.Booking, fileName string, fileData []byte) {
	c.notifyRecipients(b, "MINUTES OF MEETING", func(email, name string, isOwner bool) error {
		return utils.SendMinutesOfMeetingEmail(email, name, b.RoomName, b.Date, b.StartTime, b.EndTime, b.Purpose, fileName, fileData)
	})
}

// UpdateBooking allows a general_admin to edit any existing booking.
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

// PublicBookingAction dispatches /cancel, /minutes, and /update by trailing path segment; ownership proved by email.
func (c *Controller) PublicBookingAction(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/cancel"):
		c.PublicCancelBooking(w, r)
	case strings.HasSuffix(path, "/minutes"):
		c.UpdateMinutesOfMeeting(w, r)
	case strings.HasSuffix(path, "/update"):
		c.PublicUpdateBooking(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// PublicUpdateBooking lets the booking owner edit their booking before the meeting starts.
func (c *Controller) PublicUpdateBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	var req struct {
		Email        string `json:"email"`
		Purpose      string `json:"purpose"`
		Agenda       string `json:"agenda"`
		Participants string `json:"participants"`
		End          string `json:"end"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	b, err := c.Bookings.PublicUpdate(id, email, req.Purpose, req.Agenda, req.Participants, req.End)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}
	if errors.Is(err, models.ErrOwnerMismatch) {
		writeError(w, http.StatusForbidden, "only the original booking owner can edit this booking")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	c.auditPublic(r, email, "booking_updated_by_owner", "booking", b.Purpose, b.ID, "")
	writeJSON(w, http.StatusOK, b)
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

// UpdateMinutesOfMeeting saves an uploaded minutes document (PDF/Word) for a completed booking;
// email proves ownership, subject to the edit window.
func (c *Controller) UpdateMinutesOfMeeting(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	// A little headroom above the file size cap for the rest of the multipart form (email field, boundaries).
	r.Body = http.MaxBytesReader(w, r.Body, utils.MaxMinutesFileSize+64<<10)
	if err := r.ParseMultipartForm(utils.MaxMinutesFileSize); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large (max 5MB) or the request is malformed")
		return
	}

	email := utils.NormalizeEmail(r.FormValue("email"))
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "a meeting minutes file is required")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !utils.IsAllowedMinutesFile(header.Filename, contentType) {
		writeError(w, http.StatusBadRequest, "only PDF or Word documents (.pdf, .doc, .docx) are allowed")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the uploaded file")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "the uploaded file is empty")
		return
	}

	b, err := c.Bookings.SetMinutesOfMeetingFile(id, email, header.Filename, contentType, data)
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
		go c.sendMinutesNotifications(full, header.Filename, data)
	}

	writeJSON(w, http.StatusOK, b)
}

// DownloadMinutesFile serves a booking's uploaded minutes document, if one has been added.
// Unauthenticated by design: GET /api/bookings already exposes the same booking's purpose,
// agenda, and participants to anyone, so gating just the file would not reduce exposure.
func (c *Controller) DownloadMinutesFile(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	if !strings.HasSuffix(path, "/minutes/file") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	fileName, mime, data, err := c.Bookings.GetMinutesFile(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no meeting minutes file for this booking")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fileName))
	w.Write(data)
}
