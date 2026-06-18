package models

import (
	"fmt"
	"strings"

	"bookroom-management-system/utils"
)

// NormalizeBookingInput cleans all fields in a booking request:
// trims whitespace, normalises the email, and converts times to 24-hour format.
// Kept in the models package because it references the BookingRequest struct.
func NormalizeBookingInput(req *BookingRequest) {
	req.User    = strings.TrimSpace(req.User)
	req.Email   = utils.NormalizeEmail(req.Email)
	req.Room    = strings.TrimSpace(req.Room)
	req.Date    = strings.TrimSpace(req.Date)
	req.Purpose = strings.TrimSpace(req.Purpose)
	req.Agenda  = strings.TrimSpace(req.Agenda)
	req.Status  = strings.TrimSpace(req.Status)

	// Accept startTime/endTime (12-hour AM/PM) as fallbacks for start/end
	if req.Start == "" {
		req.Start = req.StartTime
	}
	if req.End == "" {
		req.End = req.EndTime
	}

	// Always store times in 24-hour HH:MM format
	req.Start = utils.To24HourTime(req.Start)
	req.End   = utils.To24HourTime(req.End)
}

// NormalizeParticipants cleans a comma-separated list of participant emails:
// trims whitespace, drops empty entries, and lower-cases each address.
// The list is optional — an empty input returns an empty result with no error.
// Returns an error naming the first invalid address found, if any.
func NormalizeParticipants(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parts := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		email := utils.NormalizeEmail(p)
		if email == "" {
			continue
		}
		if !utils.IsValidEmail(email) {
			return "", fmt.Errorf("invalid participant email: %s", strings.TrimSpace(p))
		}
		cleaned = append(cleaned, email)
	}
	return strings.Join(cleaned, ", "), nil
}

// FillBookingDisplayFields populates the derived display fields on a Booking:
//   - Room      = copy of RoomName (legacy alias expected by the frontend)
//   - StartTime / EndTime converted to 12-hour AM/PM format
//   - Status recomputed from the current wall clock
//
// Kept in the models package because it references the Booking struct.
func FillBookingDisplayFields(b *Booking) {
	b.Room      = b.RoomName
	b.StartTime = utils.ToDisplayTime(b.Start)
	b.EndTime   = utils.ToDisplayTime(b.End)
	b.Status    = utils.ComputeBookingStatus(b.Date, b.Start, b.End, b.Status)
}
