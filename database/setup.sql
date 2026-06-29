-- SmartBook — one-time database setup.
--
-- Consolidates what used to be 8 separate, sequentially-numbered files
-- (01_create_database.sql .. 08_add_user_confirmation.sql) into the final
-- schema they collectively produced — every later ALTER TABLE folded
-- straight into its table's CREATE TABLE, since this script defines those
-- tables for the first time rather than evolving existing ones.
--
-- Run this once against a fresh PostgreSQL server. The running app's own
-- migrate() (see database/connection.go) is idempotent and re-applies
-- harmlessly on every boot, so this script does not need to be re-run for
-- schema changes going forward — it exists purely as a readable, from-
-- scratch reference and a way to provision a database without first
-- running the app.

-- Created only if it doesn't already exist, so re-running this script
-- against a server where it partially ran before doesn't error out.
SELECT 'CREATE DATABASE bookroom_db'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'bookroom_db')\gexec

\c bookroom_db

-- ── Admins table ─────────────────────────────────────────────────────────────
-- Stores admin login credentials. Passwords are bcrypt hashes, never plaintext.
-- role:                'super_admin' (security, users, roles, audit — no
--                       day-to-day room/booking operations) or 'general_admin'
--                       (manages rooms, bookings, and normal-user approvals).
-- status:               'active' (can log in) or 'revoked' (suspended by a
--                       super admin).
-- email:                optional; used for password-reset lookups. Doubles as
--                       the login username for every admin created through
--                       the app itself — the original seed admin below is the
--                       one exception, created directly with a plain username.
-- must_reset_password:  true for accounts created with a generated temporary
--                       password (direct creation or promotion); cleared the
--                       moment that password is replaced.
CREATE TABLE IF NOT EXISTS admins (
    id                  BIGSERIAL PRIMARY KEY,
    username            TEXT UNIQUE NOT NULL,
    password            TEXT NOT NULL,
    name                TEXT NOT NULL,
    role                TEXT NOT NULL DEFAULT 'general_admin',
    status              TEXT NOT NULL DEFAULT 'active',
    email               TEXT UNIQUE,
    must_reset_password BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Case-insensitive index used by the password-reset lookup.
CREATE INDEX IF NOT EXISTS idx_admins_email_lower
    ON admins (LOWER(TRIM(email)))
    WHERE email IS NOT NULL;

-- ── Users table ──────────────────────────────────────────────────────────────
-- Registered people allowed to book rooms. A row here also doubles as a
-- General Admin's Normal User capability — "Normal User + General Admin" is
-- the one allowed multi-role combination; Super Admin is exclusive and has
-- no row here at all.
-- status:                   'pending', 'active', 'rejected', or 'revoked'.
-- intended_role:            what to grant once an admin-added row's
--                           confirmation link is clicked — 'normal_user',
--                           'general_admin', or 'super_admin'.
-- rejection_reason:         optional note recorded when a registration is rejected.
-- confirm_token (+expiry):  single-use link for admin-added users to prove
--                           ownership of their email; NULL once consumed or
--                           never needed (self-registration already proves
--                           it via OTP).
-- device_token_hash/expiry: trusted-device cookie support for the public
--                           access gate — a hashed token with an expiry, set
--                           only when a user opts in to "remember this
--                           device" after OTP verification.
CREATE TABLE IF NOT EXISTS users (
    id                       BIGSERIAL PRIMARY KEY,
    email                    TEXT UNIQUE NOT NULL,
    name                     TEXT NOT NULL,
    phone                    TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'active',
    intended_role            TEXT NOT NULL DEFAULT 'normal_user',
    rejection_reason         TEXT NOT NULL DEFAULT '',
    confirm_token            TEXT UNIQUE,
    confirm_token_expires_at TIMESTAMPTZ,
    device_token_hash        TEXT,
    device_token_expires_at  TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email_lower  ON users (LOWER(email));
CREATE INDEX IF NOT EXISTS idx_users_confirm_token ON users (confirm_token) WHERE confirm_token IS NOT NULL;

-- ── Rooms table ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS rooms (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    capacity   INTEGER NOT NULL DEFAULT 1,
    location   TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'Active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Bookings table ───────────────────────────────────────────────────────────
-- room_id references rooms(id). ON DELETE RESTRICT prevents deleting a room
-- that still has bookings linked to it.
-- agenda/participants: optional meeting details; participants is a
--                       comma-separated list of emails, notified alongside
--                       the booking owner.
-- minutes_of_meeting:   set by the booking's owner after the meeting ends,
--                       within a 24-hour edit window (see BookingModel.MinutesEditWindow).
CREATE TABLE IF NOT EXISTS bookings (
    id                 BIGSERIAL PRIMARY KEY,
    user_name          TEXT NOT NULL,
    email              TEXT NOT NULL,
    room_id            BIGINT NOT NULL REFERENCES rooms(id) ON DELETE RESTRICT,
    booking_date       DATE NOT NULL,
    start_time         TEXT NOT NULL,
    end_time           TEXT NOT NULL,
    purpose            TEXT NOT NULL,
    agenda             TEXT NOT NULL DEFAULT '',
    participants       TEXT NOT NULL DEFAULT '',
    minutes_of_meeting TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'Booked',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bookings_room_date ON bookings (room_id, booking_date);

-- ── Sessions table ───────────────────────────────────────────────────────────
-- Admin sessions, persisted here rather than in server memory so a restart
-- (deploy, crash, free-tier idle spin-down) doesn't log everyone out before
-- their actual session window expires. Each row is a snapshot of the admin's
-- identity at login time — a role change doesn't apply to an already-active
-- session until the next login.
CREATE TABLE IF NOT EXISTS sessions (
    id                  TEXT PRIMARY KEY,
    admin_id            BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    username            TEXT NOT NULL,
    name                TEXT NOT NULL,
    role                TEXT NOT NULL,
    must_reset_password BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);

-- ── Audit logs table ─────────────────────────────────────────────────────────
-- Every admin-initiated action (login/logout, user/admin/room/booking
-- management) plus self-service public actions (self-registration, booking
-- create/cancel, Minutes of Meeting). Append-only — no UPDATE or DELETE
-- endpoint is ever exposed for this table; visible only to Super Admin.
-- actor_label/target_label are denormalized snapshots (username/email/name
-- at the time of the action) so an entry stays meaningful even after the
-- underlying account is later renamed or deleted.
CREATE TABLE IF NOT EXISTS audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    actor_type   TEXT NOT NULL,
    actor_id     BIGINT,
    actor_label  TEXT NOT NULL,
    action       TEXT NOT NULL,
    target_type  TEXT NOT NULL DEFAULT '',
    target_id    BIGINT,
    target_label TEXT NOT NULL DEFAULT '',
    details      TEXT NOT NULL DEFAULT '',
    ip_address   TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action     ON audit_logs (action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id    ON audit_logs (actor_id);

-- ── Default admin account ────────────────────────────────────────────────────
-- Password below is a bcrypt hash of: Rawang@3013
-- IMPORTANT: Change this password immediately after first login.
-- To generate a new hash, run:  go run scripts/hashpw.go <newpassword>
INSERT INTO admins(username, password, name, role) VALUES
(
  'Rawang',
  '$2a$10$.ZliKLUQLYpvfPVmE1lVhe3AZePpopcWdxn4WaLh765vSiPsDLzO2',
  'System Admin',
  'super_admin'
)
ON CONFLICT(username) DO NOTHING;

-- ── Sample rooms ─────────────────────────────────────────────────────────────
INSERT INTO rooms(name, capacity, location, status) VALUES
('Executive Boardroom', 18, 'Level 4', 'Active'),
('Meeting Room 01',     12, 'Level 3', 'Active'),
('Meeting Room 02',      8, 'Level 2', 'Active'),
('Conference Suite',    20, 'Level 1', 'Active')
ON CONFLICT(name) DO NOTHING;
