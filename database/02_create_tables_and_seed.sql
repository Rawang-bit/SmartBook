-- Run this script once after creating the bookroom_db database.
-- It creates all tables, indexes, and seeds one default admin account.

-- ── Admins table ─────────────────────────────────────────────────────────────
-- Stores admin login credentials.
-- Passwords are stored as bcrypt hashes (never plaintext).
-- role:   'super_admin' (full access + admin management) or 'general_admin'
-- status: 'active' (can log in) or 'revoked' (access suspended by super admin)
CREATE TABLE IF NOT EXISTS admins (
    id         BIGSERIAL PRIMARY KEY,
    username   TEXT UNIQUE NOT NULL,
    password   TEXT NOT NULL,           -- bcrypt hash, NOT plaintext
    name       TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'general_admin',
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Users table ──────────────────────────────────────────────────────────────
-- Pre-registered users who are allowed to book rooms.
-- Admin must add a user here before they can make a booking.
CREATE TABLE IF NOT EXISTS users (
    id         BIGSERIAL PRIMARY KEY,
    email      TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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
CREATE TABLE IF NOT EXISTS bookings (
    id           BIGSERIAL PRIMARY KEY,
    user_name    TEXT NOT NULL,
    email        TEXT NOT NULL,
    room_id      BIGINT NOT NULL REFERENCES rooms(id) ON DELETE RESTRICT,
    booking_date DATE NOT NULL,
    start_time   TEXT NOT NULL,
    end_time     TEXT NOT NULL,
    purpose      TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'Booked',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes speed up the most common queries (filter by room + date, and email lookup)
CREATE INDEX IF NOT EXISTS idx_bookings_room_date ON bookings(room_id, booking_date);
CREATE INDEX IF NOT EXISTS idx_users_email_lower  ON users(LOWER(email));

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
