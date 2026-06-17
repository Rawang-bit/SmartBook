package models

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")

	// ErrDuplicate is returned when a unique constraint is violated.
	ErrDuplicate = errors.New("already exists")

	// ErrForeignKey is returned when a delete is blocked by a dependent record.
	ErrForeignKey = errors.New("has associated records")

	// ErrOwnerMismatch is returned when a public cancel is attempted by a non-owner.
	ErrOwnerMismatch = errors.New("owner mismatch")

	// ErrUnauthorized is returned when a password verification fails.
	ErrUnauthorized = errors.New("unauthorized")
)
