package controllers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"bookroom-management-system/models"
)

// ListBookings returns all bookings with room name and location.
// Optional ?room= filter by room name or ID. Public endpoint.
func (c *Controller) ListBookings(w http.ResponseWriter, r *http.Request) {
	roomFilter := strings.TrimSpace(r.URL.Query().Get("room"))

	query := `
		SELECT
			b.id,
			b.user_name,
			b.email,
			b.room_id,
			r.name        AS room_name,
			r.location,
			TO_CHAR(b.booking_date, 'YYYY-MM-DD') AS booking_date,
			b.start_time,
			b.end_time,
			b.purpose,
			b.status
		FROM bookings b
		JOIN rooms r ON r.id = b.room_id
	`

	args := []any{}
	if roomFilter != "" {
		query += ` WHERE r.name = $1 OR b.room_id::text = $1 `
		args = append(args, roomFilter)
	}
	query += ` ORDER BY b.booking_date ASC, b.start_time ASC, b.id ASC `

	rows, err := c.DB.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	bookings := []models.Booking{}
	for rows.Next() {
		var b models.Booking
		if err := rows.Scan(
			&b.ID, &b.User, &b.Email, &b.RoomID,
			&b.RoomName, &b.Location, &b.Date,
			&b.Start, &b.End, &b.Purpose, &b.Status,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		fillBookingDisplayFields(&b)
		bookings = append(bookings, b)
	}

	if err := rows.Err(); err != nil {
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

	booking, err := c.saveBooking(0, req)
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

	booking, err := c.saveBooking(id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, booking)
}

// saveBooking validates all booking fields then inserts (id==0) or updates (id>0).
func (c *Controller) saveBooking(id int64, req models.BookingRequest) (models.Booking, error) {

	// ── Step 1: Clean up input ────────────────────────────────────────────────
	normalizeBookingInput(&req)

	// ── Step 2: Look up room ID if only a room name was given ─────────────────
	if req.RoomID == 0 && req.Room != "" {
		err := c.DB.QueryRow(`SELECT id FROM rooms WHERE name = $1`, req.Room).Scan(&req.RoomID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return models.Booking{}, errors.New("could not look up room")
		}
	}

	// ── Step 3: Validate required fields ──────────────────────────────────────
	if !isValidEmail(req.Email) {
		return models.Booking{}, errors.New("valid email address is required")
	}
	if req.RoomID == 0 {
		return models.Booking{}, errors.New("room is required")
	}
	if req.Date == "" || req.Start == "" || req.End == "" {
		return models.Booking{}, errors.New("date, start time, and end time are required")
	}
	if len(req.Purpose) < 3 {
		return models.Booking{}, errors.New("purpose must be at least 3 characters")
	}

	// ── Step 4: Parse the booking date ────────────────────────────────────────
	bookingDate, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
	if err != nil {
		return models.Booking{}, errors.New("invalid booking date — use YYYY-MM-DD format")
	}

	// ── Step 5: Parse and compare start and end times ─────────────────────────
	startMinutes, err := minutesFromTime(req.Start)
	if err != nil {
		return models.Booking{}, errors.New("invalid start time")
	}
	endMinutes, err := minutesFromTime(req.End)
	if err != nil {
		return models.Booking{}, errors.New("invalid end time")
	}
	if endMinutes <= startMinutes {
		return models.Booking{}, errors.New("end time must be later than start time")
	}

	// ── Step 6: Block past bookings ───────────────────────────────────────────
	bookingStart := bookingDate.Add(time.Duration(startMinutes) * time.Minute)
	if !bookingStart.After(time.Now()) {
		return models.Booking{}, errors.New("past dates and past time slots cannot be booked")
	}

	// ── Step 7: Confirm the email is registered ───────────────────────────────
	var registeredName string
	err = c.DB.QueryRow(`
		SELECT name FROM users WHERE LOWER(TRIM(email)) = $1
	`, req.Email).Scan(&registeredName)

	if errors.Is(err, sql.ErrNoRows) {
		return models.Booking{}, errors.New("this email is not registered — ask admin to add the user first")
	}
	if err != nil {
		return models.Booking{}, err
	}
	req.User = registeredName // use the name from the database, not the request

	// ── Step 8: Confirm the room exists and is active ─────────────────────────
	var roomName, roomLocation, roomStatus string
	err = c.DB.QueryRow(`
		SELECT name, location, status FROM rooms WHERE id = $1
	`, req.RoomID).Scan(&roomName, &roomLocation, &roomStatus)

	if errors.Is(err, sql.ErrNoRows) {
		return models.Booking{}, errors.New("room not found")
	}
	if err != nil {
		return models.Booking{}, err
	}
	if roomStatus != "Active" {
		return models.Booking{}, errors.New("inactive rooms cannot be booked")
	}

	// ── Step 9: Check for time conflicts ──────────────────────────────────────
	// Overlap condition: new.start < existing.end AND new.end > existing.start
	var hasConflict bool
	err = c.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM bookings
			WHERE room_id      = $1
			  AND booking_date = $2
			  AND status      <> 'Cancelled'
			  AND id          <> $5
			  AND $3 < end_time
			  AND $4 > start_time
		)
	`, req.RoomID, req.Date, req.Start, req.End, id).Scan(&hasConflict)

	if err != nil {
		return models.Booking{}, err
	}
	if hasConflict {
		return models.Booking{}, errors.New("this room is already booked for the selected time slot")
	}

	// ── Step 10: Default status ───────────────────────────────────────────────
	if req.Status == "" {
		req.Status = "Booked"
	}

	// ── Step 11: Save to the database ─────────────────────────────────────────
	var b models.Booking

	if id == 0 {
		err = c.DB.QueryRow(`
			INSERT INTO bookings(user_name, email, room_id, booking_date, start_time, end_time, purpose, status)
			VALUES($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, status
		`, req.User, req.Email, req.RoomID, req.Date, req.Start, req.End, req.Purpose, req.Status).Scan(
			&b.ID, &b.User, &b.Email, &b.RoomID,
			&b.Date, &b.Start, &b.End, &b.Purpose, &b.Status,
		)
	} else {
		err = c.DB.QueryRow(`
			UPDATE bookings
			SET user_name    = $1,
			    email        = $2,
			    room_id      = $3,
			    booking_date = $4,
			    start_time   = $5,
			    end_time     = $6,
			    purpose      = $7,
			    status       = $8,
			    updated_at   = NOW()
			WHERE id = $9
			RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, status
		`, req.User, req.Email, req.RoomID, req.Date, req.Start, req.End, req.Purpose, req.Status, id).Scan(
			&b.ID, &b.User, &b.Email, &b.RoomID,
			&b.Date, &b.Start, &b.End, &b.Purpose, &b.Status,
		)
	}

	if errors.Is(err, sql.ErrNoRows) {
		return b, errors.New("booking not found")
	}
	if err != nil {
		return b, err
	}

	b.RoomName = roomName
	b.Location = roomLocation
	fillBookingDisplayFields(&b)
	return b, nil
}

// CancelBooking marks a booking as Cancelled. Admin version — no email check needed.
func (c *Controller) CancelBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	result, err := c.DB.Exec(`
		UPDATE bookings SET status = 'Cancelled', updated_at = NOW() WHERE id = $1
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// DeleteBooking permanently removes a booking when ?hard=1 is in the URL.
// Without ?hard=1 it cancels instead of deleting.
func (c *Controller) DeleteBooking(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/bookings/")
	if !ok {
		return
	}

	if r.URL.Query().Get("hard") != "1" {
		c.CancelBooking(w, r)
		return
	}

	result, err := c.DB.Exec(`DELETE FROM bookings WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusNotFound, "booking not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PublicCancelBooking lets a user cancel their own booking by proving their email.
// URL must end with "/cancel" and body must contain {"email": "..."}.
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

	email := normalizeEmail(req.Email)
	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	// Only cancel if the email matches the booking's owner
	result, err := c.DB.Exec(`
		UPDATE bookings
		SET status = 'Cancelled', updated_at = NOW()
		WHERE id = $1 AND LOWER(TRIM(email)) = $2
	`, id, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusForbidden, "only the original booking owner can cancel this meeting")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
