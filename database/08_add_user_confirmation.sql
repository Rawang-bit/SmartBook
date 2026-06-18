-- Migration: admin-added users must confirm their own email before being
-- activated, instead of being approved instantly.
--
-- intended_role            — what role to grant once the link is confirmed
--                             ("normal_user", "general_admin", "super_admin").
--                             Chosen by the admin at creation time.
-- confirm_token             — single-use token emailed to the new user; NULL
--                             once consumed or for rows that never needed one
--                             (e.g. self-registered users, who already proved
--                             their email via OTP).
-- confirm_token_expires_at  — link expires 7 days after creation.

ALTER TABLE users ADD COLUMN IF NOT EXISTS intended_role TEXT NOT NULL DEFAULT 'normal_user';
ALTER TABLE users ADD COLUMN IF NOT EXISTS confirm_token TEXT UNIQUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS confirm_token_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_confirm_token ON users(confirm_token) WHERE confirm_token IS NOT NULL;
