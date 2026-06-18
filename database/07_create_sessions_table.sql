-- Migration: persist admin sessions in PostgreSQL instead of server memory.
--
-- In-memory sessions were wiped on every process restart — including
-- free-tier idle spin-down and every redeploy — logging admins out long
-- before their actual 30-minute session window expired. Storing sessions
-- here means a restart no longer invalidates active logins.
--
-- Each row is a snapshot of the admin's identity at login time (matches the
-- previous in-memory behaviour: a role change doesn't apply to an already
-- logged-in session until the next login).

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

CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
