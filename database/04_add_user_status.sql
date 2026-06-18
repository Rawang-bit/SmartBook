-- Migration: add status column to users table for the self-registration
-- approval workflow.
--
-- Existing rows, and any future insert that doesn't specify status, default
-- to 'approved' so users added directly by an admin are unaffected.
-- Self-registration (via the public access gate + OTP) explicitly inserts
-- with status = 'pending' until an admin approves or rejects the request.

ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'approved';
