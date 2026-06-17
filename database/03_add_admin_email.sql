-- Migration: add email column to admins table.
-- Run once against an existing database to apply the schema change.
-- The column is nullable so existing admin rows are not affected.

ALTER TABLE admins ADD COLUMN IF NOT EXISTS email TEXT UNIQUE;

-- Case-insensitive index used by the password-reset lookup.
CREATE INDEX IF NOT EXISTS idx_admins_email_lower
    ON admins (LOWER(TRIM(email)))
    WHERE email IS NOT NULL;
