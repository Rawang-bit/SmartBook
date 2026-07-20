package models

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"bookroom-management-system/utils"
)

// BookingModel manages all database operations and business rules for room bookings.
type BookingModel struct {
	DB *sql.DB
}

// List returns all bookings joined with room info, optionally filtered by room name or ID.
func (m *BookingModel) List(roomFilter string) ([]Booking, error) {
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
			b.agenda,
			b.participants,
			b.minutes_of_meeting,
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

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bookings := []Booking{}
	for rows.Next() {
		var b Booking
		if err := rows.Scan(
			&b.ID, &b.User, &b.Email, &b.RoomID,
			&b.RoomName, &b.Location, &b.Date,
			&b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants, &b.MinutesOfMeeting, &b.Status,
		); err != nil {
			return nil, err
		}
		FillBookingDisplayFields(&b)
		bookings = append(bookings, b)
	}
	return bookings, rows.Err()
}

// withRoomDateLock runs fn inside a transaction holding a Postgres advisory lock scoped to
// (roomID, date). Two requests for the same room/day serialize on this lock, so the
// conflict check and the following write can't race with another request booking the
// same slot concurrently — without it, both could pass the check before either commits.
// The lock is released automatically when the transaction commits or rolls back.
func (m *BookingModel) withRoomDateLock(roomID int64, date string, fn func(tx *sql.Tx) error) error {
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	lockKey := fmt.Sprintf("booking-slot:%d:%s", roomID, date)
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, lockKey); err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Save validates all booking business rules then inserts (id==0) or updates (id>0).
func (m *BookingModel) Save(id int64, req BookingRequest) (Booking, error) {

	// ── Step 1: Clean up input ────────────────────────────────────────────────
	NormalizeBookingInput(&req)

	participants, err := NormalizeParticipants(req.Participants)
	if err != nil {
		return Booking{}, err
	}
	req.Participants = participants

	// ── Step 2: Look up room ID if only a room name was given ─────────────────
	if req.RoomID == 0 && req.Room != "" {
		err := m.DB.QueryRow(`SELECT id FROM rooms WHERE name = $1`, req.Room).Scan(&req.RoomID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Booking{}, fmt.Errorf("could not look up room")
		}
	}

	// ── Step 3: Validate required fields ──────────────────────────────────────
	if !utils.IsValidEmail(req.Email) {
		return Booking{}, fmt.Errorf("valid email address is required")
	}
	if req.RoomID == 0 {
		return Booking{}, fmt.Errorf("room is required")
	}
	if req.Date == "" || req.Start == "" || req.End == "" {
		return Booking{}, fmt.Errorf("date, start time, and end time are required")
	}
	if len(req.Purpose) < 3 {
		return Booking{}, fmt.Errorf("purpose must be at least 3 characters")
	}

	// ── Step 4: Parse the booking date ────────────────────────────────────────
	bookingDate, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
	if err != nil {
		return Booking{}, fmt.Errorf("invalid booking date — use YYYY-MM-DD format")
	}

	// ── Step 5: Parse and compare start and end times ─────────────────────────
	startMinutes, err := utils.MinutesFromTime(req.Start)
	if err != nil {
		return Booking{}, fmt.Errorf("invalid start time")
	}
	endMinutes, err := utils.MinutesFromTime(req.End)
	if err != nil {
		return Booking{}, fmt.Errorf("invalid end time")
	}
	if endMinutes <= startMinutes {
		return Booking{}, fmt.Errorf("end time must be later than start time")
	}

	// ── Step 6: Block past bookings ───────────────────────────────────────────
	bookingStart := bookingDate.Add(time.Duration(startMinutes) * time.Minute)
	if !bookingStart.After(time.Now()) {
		return Booking{}, fmt.Errorf("past dates and past time slots cannot be booked")
	}

	// ── Step 7: Confirm the email is registered and active ────────────────────
	// "pending" users cannot book — this enforces that even if a request bypasses
	// the frontend gate and calls the API directly.
	var registeredName, userStatus string
	err = m.DB.QueryRow(`
		SELECT name, status FROM users WHERE LOWER(TRIM(email)) = $1
	`, req.Email).Scan(&registeredName, &userStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("this email is not registered — ask admin to add the user first")
	}
	if err != nil {
		return Booking{}, err
	}
	if userStatus != "active" {
		return Booking{}, fmt.Errorf("your account is not yet approved for booking — please wait for admin approval")
	}
	req.User = registeredName // use the canonical name from the database

	// ── Step 8: Confirm the room exists and is active ─────────────────────────
	var roomName, roomLocation, roomStatus string
	err = m.DB.QueryRow(`
		SELECT name, location, status FROM rooms WHERE id = $1
	`, req.RoomID).Scan(&roomName, &roomLocation, &roomStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, fmt.Errorf("room not found")
	}
	if err != nil {
		return Booking{}, err
	}
	if roomStatus != "Active" {
		return Booking{}, fmt.Errorf("inactive rooms cannot be booked")
	}

	// ── Step 10: Default status ───────────────────────────────────────────────
	if req.Status == "" {
		req.Status = "Booked"
	}

	// ── Step 9 & 11: Check for time conflicts and persist, holding a per-room/date lock
	// so two concurrent requests for the same slot can't both pass the conflict check
	// before either one commits (see withRoomDateLock).
	// Overlap: new.start < existing.end AND new.end > existing.start
	var b Booking
	lockErr := m.withRoomDateLock(req.RoomID, req.Date, func(tx *sql.Tx) error {
		var hasConflict bool
		if err := tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM bookings
				WHERE room_id      = $1
				  AND booking_date = $2
				  AND status      <> 'Cancelled'
				  AND id          <> $5
				  AND $3 < end_time
				  AND $4 > start_time
			)
		`, req.RoomID, req.Date, req.Start, req.End, id).Scan(&hasConflict); err != nil {
			return err
		}
		if hasConflict {
			return fmt.Errorf("this room is already booked for the selected time slot")
		}

		var err error
		if id == 0 {
			err = tx.QueryRow(`
				INSERT INTO bookings(user_name, email, room_id, booking_date, start_time, end_time, purpose, agenda, participants, status)
				VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, agenda, participants, minutes_of_meeting, status
			`, req.User, req.Email, req.RoomID, req.Date, req.Start, req.End, req.Purpose, req.Agenda, req.Participants, req.Status).Scan(
				&b.ID, &b.User, &b.Email, &b.RoomID,
				&b.Date, &b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants, &b.MinutesOfMeeting, &b.Status,
			)
		} else {
			err = tx.QueryRow(`
				UPDATE bookings
				SET user_name    = $1,
				    email        = $2,
				    room_id      = $3,
				    booking_date = $4,
				    start_time   = $5,
				    end_time     = $6,
				    purpose      = $7,
				    agenda       = $8,
				    participants = $9,
				    status       = $10,
				    updated_at   = NOW()
				WHERE id = $11
				RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, agenda, participants, minutes_of_meeting, status
			`, req.User, req.Email, req.RoomID, req.Date, req.Start, req.End, req.Purpose, req.Agenda, req.Participants, req.Status, id).Scan(
				&b.ID, &b.User, &b.Email, &b.RoomID,
				&b.Date, &b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants, &b.MinutesOfMeeting, &b.Status,
			)
		}
		return err
	})

	if errors.Is(lockErr, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if lockErr != nil {
		return b, lockErr
	}

	b.RoomName = roomName
	b.Location = roomLocation
	FillBookingDisplayFields(&b)
	return b, nil
}

// GetByID fetches the minimal booking fields needed for audit log labels.
func (m *BookingModel) GetByID(id int64) (Booking, error) {
	var b Booking
	err := m.DB.QueryRow(`
		SELECT id, user_name, email, purpose, status FROM bookings WHERE id = $1
	`, id).Scan(&b.ID, &b.User, &b.Email, &b.Purpose, &b.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	return b, err
}

// GetFullByID fetches a complete booking joined with room info; used when full details are needed for emails.
func (m *BookingModel) GetFullByID(id int64) (Booking, error) {
	var b Booking
	err := m.DB.QueryRow(`
		SELECT b.id, b.user_name, b.email, b.room_id, r.name, r.location,
		       TO_CHAR(b.booking_date, 'YYYY-MM-DD'), b.start_time, b.end_time,
		       b.purpose, b.agenda, b.participants, b.minutes_of_meeting, b.status
		FROM bookings b
		JOIN rooms r ON r.id = b.room_id
		WHERE b.id = $1
	`, id).Scan(
		&b.ID, &b.User, &b.Email, &b.RoomID, &b.RoomName, &b.Location,
		&b.Date, &b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants,
		&b.MinutesOfMeeting, &b.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	if err != nil {
		return Booking{}, err
	}
	FillBookingDisplayFields(&b)
	return b, nil
}

// Cancel marks a booking as Cancelled; returns ErrNotFound if missing.
func (m *BookingModel) Cancel(id int64) error {
	result, err := m.DB.Exec(`
		UPDATE bookings SET status = 'Cancelled', updated_at = NOW() WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// PublicCancel cancels a booking only if the provided email matches the booking owner.
func (m *BookingModel) PublicCancel(id int64, email string) error {
	result, err := m.DB.Exec(`
		UPDATE bookings
		SET status = 'Cancelled', updated_at = NOW()
		WHERE id = $1 AND LOWER(TRIM(email)) = $2
	`, id, email)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrOwnerMismatch
	}
	return nil
}

// MinutesEditWindow is how long after a meeting ends its owner may add or edit Minutes of Meeting.
const MinutesEditWindow = 24 * time.Hour

// SetMinutesOfMeeting saves meeting notes; only the email-proven owner may edit, within MinutesEditWindow after end time.
func (m *BookingModel) SetMinutesOfMeeting(id int64, email, minutes string) (Booking, error) {
	var b Booking
	var startTimeStr, endTimeStr string
	err := m.DB.QueryRow(`
		SELECT email, status, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time
		FROM bookings WHERE id = $1
	`, id).Scan(&b.Email, &b.Status, &b.Date, &startTimeStr, &endTimeStr)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	if err != nil {
		return Booking{}, err
	}

	if utils.NormalizeEmail(b.Email) != utils.NormalizeEmail(email) {
		return Booking{}, ErrOwnerMismatch
	}

	// Use the same status computation as FillBookingDisplayFields so a save can
	// never be rejected for a booking the "eligible for minutes" list just showed.
	computedStatus := utils.ComputeBookingStatus(b.Date, startTimeStr, endTimeStr, b.Status)
	switch computedStatus {
	case "Cancelled":
		return Booking{}, fmt.Errorf("cancelled bookings cannot have meeting minutes")
	case "Booked", "In Progress":
		return Booking{}, fmt.Errorf("minutes of meeting can only be added after the meeting has ended")
	}
	if !isWithinMinutesEditWindow(b.Date, endTimeStr, computedStatus) {
		return Booking{}, fmt.Errorf("the 24-hour window to add meeting minutes has passed")
	}

	err = m.DB.QueryRow(`
		UPDATE bookings SET minutes_of_meeting = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, agenda, participants, minutes_of_meeting, status
	`, minutes, id).Scan(
		&b.ID, &b.User, &b.Email, &b.RoomID,
		&b.Date, &b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants, &b.MinutesOfMeeting, &b.Status,
	)
	if err != nil {
		return Booking{}, err
	}
	FillBookingDisplayFields(&b)
	return b, nil
}

// PublicUpdate lets a booking owner edit editable fields before the meeting starts.
// Ownership is proved by email match; the booking must still be in 'Booked' status.
func (m *BookingModel) PublicUpdate(id int64, email, purpose, agenda, participants, end string) (Booking, error) {
	purpose = strings.TrimSpace(purpose)
	agenda  = strings.TrimSpace(agenda)
	if len(purpose) < 3 {
		return Booking{}, fmt.Errorf("purpose must be at least 3 characters")
	}

	var b Booking
	var startRaw string
	err := m.DB.QueryRow(`
		SELECT email, status, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, room_id
		FROM bookings WHERE id = $1
	`, id).Scan(&b.Email, &b.Status, &b.Date, &startRaw, &b.End, &b.RoomID)
	if errors.Is(err, sql.ErrNoRows) {
		return Booking{}, ErrNotFound
	}
	if err != nil {
		return Booking{}, err
	}
	if utils.NormalizeEmail(b.Email) != utils.NormalizeEmail(email) {
		return Booking{}, ErrOwnerMismatch
	}
	if utils.ComputeBookingStatus(b.Date, startRaw, b.End, b.Status) != "Booked" {
		return Booking{}, fmt.Errorf("only upcoming bookings can be edited")
	}

	end24 := utils.To24HourTime(strings.TrimSpace(end))
	startMins, _ := utils.MinutesFromTime(startRaw)
	endMins, err := utils.MinutesFromTime(end24)
	if err != nil || endMins <= startMins {
		return Booking{}, fmt.Errorf("end time must be after start time")
	}

	cleanParticipants, err := NormalizeParticipants(participants)
	if err != nil {
		return Booking{}, err
	}

	// Conflict check and update run inside a per-room/date lock so a concurrent request
	// for the same slot can't slip in between the check and the write (see withRoomDateLock).
	lockErr := m.withRoomDateLock(b.RoomID, b.Date, func(tx *sql.Tx) error {
		var hasConflict bool
		if err := tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM bookings
				WHERE room_id      = $1
				  AND booking_date = $2
				  AND status      <> 'Cancelled'
				  AND id          <> $5
				  AND $3 < end_time
				  AND $4 > start_time
			)
		`, b.RoomID, b.Date, startRaw, end24, id).Scan(&hasConflict); err != nil {
			return err
		}
		if hasConflict {
			return fmt.Errorf("the new end time conflicts with another booking in this room")
		}

		return tx.QueryRow(`
			UPDATE bookings
			SET purpose = $1, agenda = $2, participants = $3, end_time = $4, updated_at = NOW()
			WHERE id = $5
			RETURNING id, user_name, email, room_id, TO_CHAR(booking_date,'YYYY-MM-DD'), start_time, end_time, purpose, agenda, participants, minutes_of_meeting, status
		`, purpose, agenda, cleanParticipants, end24, id).Scan(
			&b.ID, &b.User, &b.Email, &b.RoomID,
			&b.Date, &b.Start, &b.End, &b.Purpose, &b.Agenda, &b.Participants, &b.MinutesOfMeeting, &b.Status,
		)
	})
	if lockErr != nil {
		return Booking{}, lockErr
	}
	FillBookingDisplayFields(&b)
	return b, nil
}

// Delete permanently removes a booking; returns ErrNotFound if missing.
func (m *BookingModel) Delete(id int64) error {
	result, err := m.DB.Exec(`DELETE FROM bookings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// BookingRetentionDays is how long a booking record is kept before it is purged.
const BookingRetentionDays = 365

// PurgeOldBookings deletes bookings older than BookingRetentionDays; returns the row count.
func (m *BookingModel) PurgeOldBookings() (int64, error) {
	result, err := m.DB.Exec(`
		DELETE FROM bookings WHERE booking_date < CURRENT_DATE - $1::int
	`, BookingRetentionDays)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
