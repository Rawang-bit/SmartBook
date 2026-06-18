-- Migration: add agenda and participants fields to bookings.
--
-- agenda       — free-text meeting agenda, optional.
-- participants — comma-separated participant email addresses, optional.
--                Notified by email alongside the booking owner once a
--                booking is created.

ALTER TABLE bookings ADD COLUMN IF NOT EXISTS agenda TEXT NOT NULL DEFAULT '';
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS participants TEXT NOT NULL DEFAULT '';
