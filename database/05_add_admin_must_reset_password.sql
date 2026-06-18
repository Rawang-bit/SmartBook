-- Migration: add must_reset_password flag to admins.
--
-- Used for admin accounts created via the user-approval promotion flow
-- (Users page → Approve with a role other than normal_user). Those accounts
-- get a randomly generated temporary password and must change it on first
-- login before they can use any other part of the admin panel.

ALTER TABLE admins ADD COLUMN IF NOT EXISTS must_reset_password BOOLEAN NOT NULL DEFAULT FALSE;
