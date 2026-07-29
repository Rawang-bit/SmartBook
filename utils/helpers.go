// Package utils provides shared helper functions for models and controllers.
package utils

import (
	"net/mail"
	"path/filepath"
	"strings"
	"time"
)

// MaxMinutesFileSize is the largest Minutes of Meeting upload accepted (5MB).
const MaxMinutesFileSize = 5 << 20

// allowedMinutesFileTypes maps accepted lowercase extensions to their canonical content-type,
// used to detect the file when a browser doesn't set (or misreports) Content-Type.
var allowedMinutesFileTypes = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// allowedMinutesContentTypes is the set of declared content-types accepted alongside a matching extension.
// application/octet-stream is included because some browsers send it for .doc/.docx uploads.
var allowedMinutesContentTypes = map[string]bool{
	"application/pdf":    true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/octet-stream": true,
}

// IsAllowedMinutesFile reports whether filename/contentType look like a PDF or Word document.
// The extension is the primary gate; the declared content-type must also be plausible for it.
func IsAllowedMinutesFile(filename, contentType string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if _, ok := allowedMinutesFileTypes[ext]; !ok {
		return false
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return allowedMinutesContentTypes[contentType]
}

// IsValidEmail returns true if the address is a properly formatted email (RFC 5322).
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// NormalizeEmail lower-cases an email and trims surrounding whitespace.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeRoomStatus returns "Active" if status is empty, otherwise returns it as-is.
func NormalizeRoomStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "Active"
	}
	return status
}

// To24HourTime converts "14:30", "14:30:00", or "02:30 PM" to "HH:MM" 24-hour format.
func To24HourTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if t, err := time.Parse("15:04", value); err == nil {
		return t.Format("15:04")
	}
	if t, err := time.Parse("15:04:05", value); err == nil {
		return t.Format("15:04")
	}
	if t, err := time.Parse("03:04 PM", strings.ToUpper(value)); err == nil {
		return t.Format("15:04")
	}

	return value
}

// ToDisplayTime converts a 24-hour time string to 12-hour AM/PM — e.g. "14:30" → "02:30 PM".
func ToDisplayTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if t, err := time.Parse("15:04", value); err == nil {
		return t.Format("03:04 PM")
	}
	if t, err := time.Parse("15:04:05", value); err == nil {
		return t.Format("03:04 PM")
	}
	if t, err := time.Parse("03:04 PM", strings.ToUpper(value)); err == nil {
		return t.Format("03:04 PM")
	}

	return value
}

// MinutesFromTime converts a time string to total minutes since midnight — e.g. "09:30" → 570.
func MinutesFromTime(value string) (int, error) {
	value = To24HourTime(value)
	t, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// ComputeBookingStatus derives the live status: Cancelled stays as-is; otherwise Booked → In Progress → Completed.
func ComputeBookingStatus(dateStr, startStr, endStr, savedStatus string) string {
	if strings.EqualFold(strings.TrimSpace(savedStatus), "Cancelled") {
		return "Cancelled"
	}

	bookingDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), time.Local)
	if err != nil {
		return "Booked"
	}

	startMinutes, err := MinutesFromTime(startStr)
	if err != nil {
		return "Booked"
	}
	endMinutes, err := MinutesFromTime(endStr)
	if err != nil {
		return "Booked"
	}

	startAt := bookingDate.Add(time.Duration(startMinutes) * time.Minute)
	endAt   := bookingDate.Add(time.Duration(endMinutes)   * time.Minute)
	now     := time.Now()

	switch {
	case now.Before(startAt):
		return "Booked"
	case now.Before(endAt):
		return "In Progress"
	default:
		return "Completed"
	}
}

// IsUniqueViolation returns true when a database error is a unique-constraint failure.
func IsUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique")
}

