-- SmartBook — database setup for pgAdmin 4
--
-- HOW TO USE IN pgAdmin 4
-- ────────────────────────
-- Step 1: Create the database
--   • In the left panel, right-click "Databases" → Create → Database
--   • Name: bookroom_db   Owner: (your postgres user)   → Save
--
-- Step 2: Open the Query Tool connected to bookroom_db
--   • Click bookroom_db in the left panel to select it
--   • Menu: Tools → Query Tool  (or press Alt+Shift+Q)
--   • Paste this entire file into the editor
--   • Press F5 (or the ▶ Execute button) to run it
--
-- Every statement uses IF NOT EXISTS / ON CONFLICT DO NOTHING, so
-- re-running this script on an already-populated database is safe.


-- ── Admins table ─────────────────────────────────────────────────────────────
-- Stores admin login credentials. Passwords are bcrypt hashes, never plaintext.
--
-- username:            the email address used to log in. Named 'username' in the
--                      schema for historical reasons but always stores an email.
-- role:                'super_admin' — security, user management, audit only
--                        (no room or booking operations); or
--                      'general_admin' — manages rooms, bookings, and normal-user approvals.
-- status:              'active' (can log in) or 'revoked' (suspended by a super admin).
-- email:               must match username; used by the password-reset lookup so
--                      the admin can recover access without knowing a separate identifier.
-- must_reset_password: true for accounts given a generated temporary password;
--                      cleared the moment the password is replaced.
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

-- Case-insensitive index used by the password-reset email lookup.
CREATE INDEX IF NOT EXISTS idx_admins_email_lower
    ON admins (LOWER(TRIM(email)))
    WHERE email IS NOT NULL;


-- ── Users table ──────────────────────────────────────────────────────────────
-- People registered to book rooms. A row here also enables the Normal User
-- capability for a General Admin — "Normal User + General Admin" is the one
-- allowed multi-role combination. Super Admins have no row here at all.
--
-- status:                  'pending', 'active', 'rejected', or 'revoked'.
-- intended_role:           role to assign once the confirmation link is clicked
--                          ('normal_user', 'general_admin', or 'super_admin').
-- rejection_reason:        optional note recorded when a registration is rejected.
-- confirm_token (+expiry): single-use link for admin-added users to confirm
--                          ownership of their email address; NULL once consumed,
--                          or never set for self-registrations (OTP already proves ownership).
-- device_token_hash/expiry: trusted-device cookie support — stored as a hashed
--                           token so a database leak cannot be replayed.
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

CREATE INDEX IF NOT EXISTS idx_users_email_lower   ON users (LOWER(email));
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
-- ON DELETE RESTRICT on room_id prevents deleting a room that still has
-- bookings linked to it — the bookings must be cancelled first.
--
-- agenda/participants:  optional meeting details; participants is a
--                       comma-separated list of emails notified at booking time.
-- minutes_of_meeting:  filled in by the booking owner after the meeting,
--                       within a 24-hour edit window.
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
-- Admin sessions are stored in the database (not server memory) so that
-- a restart — deploy, crash, or free-tier spin-down — does not log everyone
-- out before their session window actually expires.
--
-- Each row is a snapshot of the admin's identity at login time.
-- A role change only takes effect after the admin logs in again.
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
-- Append-only record of every admin-initiated action (login/logout, user/admin/
-- room/booking management) and self-service public actions (self-registration,
-- booking create/cancel, minutes of meeting).
-- Visible only to Super Admin. No UPDATE or DELETE endpoint is ever exposed.
--
-- actor_label / target_label are denormalized name snapshots so a log entry
-- stays readable even after the underlying account is renamed or deleted.
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
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id   ON audit_logs (actor_id);


-- ── Default super admin account ───────────────────────────────────────────────
-- Password: Rawang@3013  (bcrypt hash stored below)
-- IMPORTANT: Change this password immediately after first login.
-- To generate a replacement hash: go run scripts/hashpw.go <newpassword>
INSERT INTO admins (username, password, name, role, email) VALUES
(
    'ratuwangchuk@dhi.bt',
    '$2a$10$.ZliKLUQLYpvfPVmE1lVhe3AZePpopcWdxn4WaLh765vSiPsDLzO2',
    'System Admin',
    'super_admin',
    'ratuwangchuk@dhi.bt'
)
ON CONFLICT (username) DO NOTHING;


-- ── Sample rooms ──────────────────────────────────────────────────────────────
INSERT INTO rooms (name, capacity, location, status) VALUES
    ('Executive Boardroom', 18, 'Level 4', 'Active'),
    ('Meeting Room 01',     12, 'Level 3', 'Active'),
    ('Meeting Room 02',      8, 'Level 2', 'Active'),
    ('Conference Suite',    20, 'Level 1', 'Active')
ON CONFLICT (name) DO NOTHING;
