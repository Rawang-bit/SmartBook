# SmartBook — Online Room Booking System

SmartBook is a web-based meeting room reservation platform built for organisations. It provides a public self-service booking calendar, an admin management portal, role-based access control, automated email notifications, and a full audit trail.

---

## Table of Contents

1. [Technology Stack](#technology-stack)
2. [Architecture Overview](#architecture-overview)
3. [Role-Based Access Control (RBAC)](#role-based-access-control-rbac)
4. [User Types and Capabilities](#user-types-and-capabilities)
5. [System Flows](#system-flows)
   - [Public User Access Flow](#public-user-access-flow)
   - [Self-Registration Flow](#self-registration-flow)
   - [Admin-Added User Flow](#admin-added-user-flow)
   - [Admin Login Flow](#admin-login-flow)
   - [Booking Lifecycle](#booking-lifecycle)
   - [Account Lockout Flow](#account-lockout-flow)
6. [Feature Reference](#feature-reference)
   - [Public Calendar](#public-calendar)
   - [Bookings](#bookings)
   - [Rooms](#rooms)
   - [User Management](#user-management)
   - [Admin Management](#admin-management)
   - [Audit Trail](#audit-trail)
   - [History](#history)
   - [Dashboard](#dashboard)
   - [Email Notifications](#email-notifications)
   - [Security](#security)
7. [Database Schema](#database-schema)
8. [Environment Variables](#environment-variables)
9. [Running Locally](#running-locally)
10. [Deployment (Render)](#deployment-render)
11. [Default Credentials](#default-credentials)
12. [Project Structure](#project-structure)

---

## Technology Stack

| Layer | Technology |
|---|---|
| Backend | Go (standard library `net/http`, Go 1.22+ routing) |
| Database | PostgreSQL (via `pgx/v5` driver) |
| Frontend | Vanilla HTML + JavaScript + Tailwind CSS (CDN) |
| Icons | Lucide Icons (public calendar), Heroicons SVG (admin panel) |
| Email | Resend API (HTML + plain-text emails) |
| CAPTCHA | Cloudflare Turnstile (optional; skipped when keys are absent) |
| Hosting | Render (web service + managed PostgreSQL) |
| Timezone | Asia/Thimphu (Bhutan Standard Time, UTC+6) — pinned at startup |

---

## Architecture Overview

```
Browser
  │
  ├── /view/*.html          Static frontend (served by Go's FileServer)
  │     ├── index.html      Public access gate
  │     ├── calendar.html   Public booking calendar  (user-app.js)
  │     ├── login.html      Admin login
  │     └── dashboard.html  Admin panel  (app.js + auth-guard.js)
  │
  └── /api/*                JSON REST API
        │
        ├── controllers/    HTTP handlers
        ├── models/         Database queries + business logic
        ├── session/        In-memory session, OTP, and attempt stores
        ├── utils/          Helpers, email, Turnstile, time utilities
        └── database/       Connection + auto-migration
```

All API responses are JSON. The frontend communicates with the backend exclusively through `fetch` calls to `/api/*`. There is no template rendering — HTML is static, data is fetched at runtime.

### Auto-Migration

On every server startup `database/connection.go` runs a list of idempotent SQL statements (`ALTER TABLE … ADD COLUMN IF NOT EXISTS`, `CREATE TABLE IF NOT EXISTS`, etc.). There is no separate migration tool. Adding a new column means appending one statement to the `stmts` slice in `migrate()`.

---

## Role-Based Access Control (RBAC)

SmartBook has three distinct identity types and five statuses that govern what each person can do.

### Identity Types

| Type | How they exist | Where they log in |
|---|---|---|
| **Normal User** | Row in `users` table | Public access gate (`index.html`) |
| **General Admin** | Row in `admins` table (role = `general_admin`) | Admin login (`login.html`) |
| **Super Admin** | Row in `admins` table (role = `super_admin`) | Admin login (`login.html`) |

A person can hold **both** a Normal User account and a General Admin account simultaneously (same email, separate rows in different tables). Super Admins are explicitly excluded from having a Normal User row.

### Admin Role Matrix

| Action | Super Admin | General Admin |
|---|---|---|
| View dashboard | ✓ | ✓ |
| View booking history | ✗ | ✓ |
| List / approve / reject users | ✓ | ✓ |
| Add / edit / delete users | ✓ | ✓ |
| Create rooms | ✗ | ✓ |
| Edit / delete rooms | ✗ | ✓ |
| Create bookings (via public calendar) | ✗ (blocked) | ✓ (as Normal User) |
| Edit / delete any booking | ✗ | ✓ |
| View all bookings | ✓ | ✓ |
| List / create / edit / delete admins | ✓ | ✗ |
| Revoke / restore admin access | ✓ | ✗ |
| Unlock locked admin accounts | ✓ | ✓ |
| View audit trail | ✓ | ✗ |
| Change own password | ✓ | ✓ |
| Reset any admin's password | ✓ | ✗ |

### Middleware Guards

| Middleware | Who passes |
|---|---|
| `RequireSuperAdmin` | `role = super_admin` only |
| `RequireGeneralAdmin` | `role = general_admin` only |
| `RequireAdmin` | Either admin role |
| `BlockSuperAdmin` | Anyone **except** super_admin |

All admin routes require a valid session cookie. Sessions are stored in PostgreSQL (survive restarts), expire after **30 minutes**, and are invalidated immediately on logout, revoke, or password reset.

---

## User Types and Capabilities

### Normal User

- Accesses the system through the **public gate** at `index.html`.
- Must be in the `users` table with `status = active`.
- Can **view all room bookings** on the calendar.
- Can **create** a booking for any active room.
- Can **cancel** their own upcoming bookings (before meeting starts).
- Can **edit** their own upcoming bookings (purpose, agenda, participants, end time — before meeting starts).
- Can **add Minutes of Meeting** for their own completed bookings within 24 hours of the meeting ending.
- Cannot cancel or edit anyone else's bookings.
- Cannot see the Cancel or Edit button on someone else's booking detail.

### General Admin

Everything a Normal User can do (if they also hold a Normal User account), plus:

- Full access to the **admin panel** after logging in with admin credentials.
- Manage **Rooms**: create, edit, activate/deactivate, delete.
- Manage **Users**: add, edit, approve/reject pending self-registrations, revoke/restore access, delete.
- Manage **Bookings**: view all, edit any booking, cancel any booking (hard or soft delete).
- View **Booking History** (all completed/cancelled bookings).
- View and manage **locked admin accounts** (unlock accounts locked after failed login attempts).
- Access **Dashboard** with stats: rooms, bookings, pending users, locked admins.

### Super Admin

Everything a General Admin can do, plus:

- **Only** role that can create, edit, delete, revoke, or restore **admin accounts**.
- **Only** role that can view the **Audit Trail**.
- Deliberately **blocked** from creating bookings (to preserve separation of duties — operational tasks belong to general admins and normal users).
- Has no Normal User row; cannot use the public calendar to book rooms.

---

## System Flows

### Public User Access Flow

```
index.html
    │
    ├─ Enter email
    │       │
    │       ├─ Email not found  →  Show self-registration form
    │       │
    │       └─ Email found (active)
    │               │
    │               ├─ Recognised device cookie  →  Skip OTP  →  calendar.html
    │               │
    │               └─ Unrecognised device  →  Send OTP  →  Enter code
    │                                                │
    │                                                └─ Verified  →  (opt: remember device 30 days)
    │                                                                       →  calendar.html
    │
    └─ Email found (pending / rejected / revoked)  →  Show status message, cannot proceed
```

**Device Trust:** When a user opts in to "Remember this device", a cryptographically random token is stored server-side (hashed) and as a `HttpOnly` cookie. On the next visit from that device the OTP step is skipped for 30 days.

### Self-Registration Flow

```
index.html  →  "New here?" link
    │
    ├─ Enter name + email  →  Turnstile CAPTCHA
    │
    ├─ Send OTP to email (valid 10 minutes)
    │
    ├─ Enter 6-digit code
    │
    └─ User row created with status = 'pending'
            │
            └─ Admin reviews pending users  →  Approve / Reject
                    │
                    ├─ Approved (Normal User role)  →  status = 'active'  →  can book
                    │
                    ├─ Approved (General Admin role)  →  admin account created with temp password
                    │                                     email sent with credentials
                    │
                    └─ Rejected  →  status = 'rejected'  →  rejection reason emailed
```

### Admin-Added User Flow

```
Admin panel  →  Users  →  Add User
    │
    ├─ Admin enters name + email + intended role
    │
    ├─ Confirmation email sent to user with a one-time token link
    │
    └─ User clicks link  →  POST /api/users/confirm
            │
            ├─ Normal User intended  →  status = 'active'  →  can use public calendar
            │
            └─ Admin role intended  →  admin account created  →  temp password emailed
```

### Admin Login Flow

```
login.html
    │
    ├─ Enter username + password  (+  optional Turnstile CAPTCHA)
    │
    ├─ In-memory lock check  (fast path — no DB hit if already locked in this session)
    │
    ├─ DB lookup: username → admin row
    │       │
    │       └─ Not found  →  count failure (prevent username enumeration)
    │
    ├─ DB status check: status = 'locked'  →  reject (account locked by too many failures)
    │
    ├─ bcrypt password comparison
    │       │
    │       └─ Wrong password  →  count failure
    │               │
    │               └─ 3rd failure  →  status = 'locked'  persisted to DB  →  alert
    │
    ├─ Status check: status = 'revoked'  →  reject (after timing-safe password check)
    │
    └─ Success  →  session created (64-char random hex, stored in PostgreSQL, 30 min TTL)
            │
            └─ MustResetPassword = true  →  redirect to force-password-change.html
```

**Lockout:** After 3 consecutive failed attempts the account status is set to `locked` in the database. The lockout persists across server restarts. Either admin role can unlock the account; there is no auto-unlock timer.

### Booking Lifecycle

```
Status transitions (computed live from booking_date + start_time + end_time):

  'Booked'  →  (meeting start time reached)  →  'In Progress'
                                                         │
                                              (meeting end time reached)
                                                         │
                                                   'Completed'

  Any status  →  (admin or owner cancels)  →  'Cancelled'
```

**Owner actions (public calendar):**
- **Edit** — available only when status is `Booked` (before meeting starts). Can change: end time, purpose, agenda, participants.
- **Cancel** — available only when status is `Booked` (before meeting starts). Hidden when In Progress.

**Admin actions (admin panel):**
- Edit or cancel any booking regardless of status.
- Hard-delete a booking permanently.

**Booking retention:** A background job runs daily and permanently deletes bookings older than **365 days**.

### Account Lockout Flow

```
3 failed admin login attempts
    │
    └─ status = 'locked'  (DB persisted)
            │
            ├─ Dashboard shows "Locked Admin Accounts" panel  (both admin roles)
            │
            ├─ Admins page shows orange "Locked" badge + Unlock button
            │
            └─ Any admin clicks Unlock
                    │
                    └─ status = 'active'  +  in-memory counter reset
```

---

## Feature Reference

### Public Calendar

**File:** `view/calendar.html` + `view/user-app.js`

- Weekly and daily views of all room bookings.
- Room switcher dropdown — switch between active rooms.
- Live clock with date (Asia/Thimphu).
- Booking cards show organiser name, purpose, time, and status badge ("In Progress" or countdown to start).
- Click any booking card to view details (purpose, organiser, location, agenda, duration, room).
- **Cancel Meeting** button: owner only, before meeting starts.
- **Edit Booking** button: owner only, before meeting starts (change end time, purpose, agenda, participants).
- Empty slot hover → "+" button to create a booking.
- Calendar polls every 10 seconds; automatically re-checks that the logged-in user's access is still active.
- Gate session verified every 10 seconds — access revoked mid-session redirects to the gate immediately.

**Meeting Minutes button (clipboard icon in header):**
- Shows the logged-in user's own meetings eligible for minutes (completed, within 24 hours of ending, no minutes yet).
- Owner can add minutes; saved and emailed to all participants.

### Bookings

**Admin files:** `view/bookings.html`, `view/book-room.html`

- **List all bookings** with filters (room, status, date range).
- **Edit any booking** — change any field, room, date, or time (conflict-checked).
- **Cancel** (soft delete → status = Cancelled) or **hard delete** any booking.
- **Create booking** from the admin panel on behalf of any registered user.

### Rooms

**Admin file:** `view/rooms.html`

- List all rooms with name, capacity, location, and status.
- **Create** a new room.
- **Edit** name, capacity, location, and Active/Inactive status.
- **Delete** a room (blocked if bookings exist).
- Only **General Admins** can manage rooms; Super Admins have read-only access.

### User Management

**Admin file:** `view/users.html`

- List all users: name, email, phone, status, intended role, registration date.
- Status values: `pending`, `active`, `rejected`, `revoked`.
- **Add user** (admin-initiated flow with email confirmation link).
- **Edit** name, email, and phone.
- **Approve** a pending self-registration; choose Normal User or General Admin role.
- **Reject** a pending registration (rejection reason emailed to the user).
- **Revoke / Restore** active user access.
- **Delete** a user permanently.
- Pending users who registered themselves but haven't been approved show an "Awaiting Confirmation" indicator.

### Admin Management

**Admin file:** `view/admins.html` (Super Admin only)

- List all admin accounts with name, email, role, status, and join date.
- Status badges: **Active** (green), **Revoked** (red), **Locked** (orange).
- **Create admin** — generates a temporary password, emails credentials.
- **Edit** name, email, and role.
- **Reset password** — sets a new temporary password; forces change on next login.
- **Revoke** access (immediately invalidates all sessions).
- **Restore** access.
- **Unlock** — clears the `locked` status for accounts locked by failed login attempts (both admin roles can do this).
- **Delete** admin account permanently.
- Admins cannot revoke, demote, or delete their own account.

**Locked admins panel:** Available to both admin roles on the Dashboard — shows all currently locked accounts with a one-click Unlock button.

### Audit Trail

**Admin file:** `view/audit-logs.html` (Super Admin only)

- Append-only log of every significant action: logins, logouts, failed attempts, bookings, user approvals, admin changes, password resets, etc.
- Records: actor type (admin or system), actor label, action, target type + label, details, IP address, user agent, timestamp.
- Filterable by actor/target label, action type, and date range.
- Exportable (full unfiltered export when page = 0).
- Read-only by design — no endpoint exists to edit or delete entries.

### History

**Admin file:** `view/history.html` (General Admin only)

- Read-only view of all historical bookings (completed and cancelled).
- Filterable by room and date range.
- Shows Minutes of Meeting when available.

### Dashboard

**Admin file:** `view/dashboard.html` (both admin roles)

- Stats cards: total rooms, today's bookings, pending user registrations, locked admin accounts.
- Pending users panel — approve or reject without leaving the dashboard.
- Locked admin accounts panel (orange, hidden when empty) — unlock directly from dashboard.
- Today's booking timeline.
- Refreshes every 30 seconds.

### Email Notifications

All emails are sent asynchronously (goroutines) so delivery failures never block HTTP responses. Emails are HTML with inline CSS for mail client compatibility and also include a plain-text fallback.

| Trigger | Recipient(s) |
|---|---|
| Admin-added user invitation | User (confirmation link) |
| Self-registration OTP | User (6-digit code, 10-minute expiry) |
| User approval (Normal User) | User |
| User approval (General Admin role) | User (temporary password) |
| User rejection | User (with reason) |
| Booking confirmed | Owner + all participants |
| Booking cancelled | Owner + all participants |
| Minutes of Meeting saved | Owner + all participants |
| Password reset requested | Admin (reset link) |
| Temporary admin password | New admin (credentials) |

Email provider: **Resend API** (`RESEND_API_KEY`). If the key is absent in development, the password-reset link is printed to the server log instead.

### Security

| Feature | Detail |
|---|---|
| Password hashing | bcrypt (default cost) |
| Session tokens | 64-char hex (32 random bytes), stored in PostgreSQL |
| Session expiry | 30 minutes; purged from DB every 10 minutes |
| Session invalidation | Immediate on logout, revoke, or password reset |
| Login lockout | 3 failures → status = `locked`; DB-persisted, no auto-unlock |
| Lockout layer 1 | In-memory `AttemptStore` (fast path, cleared on restart) |
| Lockout layer 2 | DB `status = locked` (persists across restarts) |
| CAPTCHA | Cloudflare Turnstile on login, OTP send, and forgot-password |
| Admin cookies | `HttpOnly`, `SameSite=Strict`, `Secure` (production) |
| Cookie prefix | `__Host-` in production (prevents subdomain injection) |
| Device trust | 30-day hashed token cookie for public gate OTP skip |
| HTTPS redirect | 308 in production, honours `X-Forwarded-Proto` |
| HSTS | `max-age=31536000; includeSubDomains` (production only) |
| CSP | Restricts scripts, frames, connections to known CDNs only |
| Security headers | `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, etc. |
| Password complexity | Minimum 12 characters, must include upper, lower, digit, symbol |
| Timing-safe revoke check | Revoked status checked **after** bcrypt comparison to prevent timing attacks |
| Enumeration prevention | Invalid username returns the same error as wrong password |
| Audit trail | Every sensitive action logged with IP and user agent |

---

## Database Schema

Tables created and migrated automatically on startup.

### `admins`
| Column | Type | Notes |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `username` | TEXT UNIQUE | Email address |
| `password` | TEXT | bcrypt hash |
| `name` | TEXT | Display name |
| `role` | TEXT | `super_admin` or `general_admin` |
| `status` | TEXT | `active`, `revoked`, or `locked` |
| `email` | TEXT UNIQUE | Same as username for standard accounts |
| `must_reset_password` | BOOLEAN | True for temporary passwords |
| `created_at` | TIMESTAMPTZ | |

### `users`
| Column | Type | Notes |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `email` | TEXT UNIQUE | |
| `name` | TEXT | |
| `phone` | TEXT | Optional contact number |
| `status` | TEXT | `pending`, `active`, `rejected`, `revoked` |
| `intended_role` | TEXT | `normal_user`, `general_admin`, `super_admin` |
| `confirm_token` | TEXT UNIQUE | One-time activation token (admin-added users) |
| `confirm_token_expires_at` | TIMESTAMPTZ | |
| `device_token_hash` | TEXT | Hashed trusted-device token |
| `device_token_expires_at` | TIMESTAMPTZ | |
| `rejection_reason` | TEXT | Set when admin rejects |
| `created_at` | TIMESTAMPTZ | |

### `rooms`
| Column | Type | Notes |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `name` | TEXT UNIQUE | |
| `capacity` | INTEGER | |
| `location` | TEXT | |
| `status` | TEXT | `Active` or `Inactive` |
| `created_at` | TIMESTAMPTZ | |

### `bookings`
| Column | Type | Notes |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `user_name` | TEXT | Snapshot of user's name at booking time |
| `email` | TEXT | Booking owner's email |
| `room_id` | BIGINT FK → rooms | |
| `booking_date` | DATE | |
| `start_time` | TEXT | 24-hour `HH:MM` |
| `end_time` | TEXT | 24-hour `HH:MM` |
| `purpose` | TEXT | Meeting title |
| `agenda` | TEXT | Optional |
| `participants` | TEXT | Comma-separated emails |
| `minutes_of_meeting` | TEXT | Added by owner post-meeting (24-hour window) |
| `status` | TEXT | `Booked`, `In Progress`, `Completed`, `Cancelled` (computed live) |
| `created_at` | TIMESTAMPTZ | |
| `updated_at` | TIMESTAMPTZ | |

### `sessions`
| Column | Type | Notes |
|---|---|---|
| `id` | TEXT PK | 64-char hex random |
| `admin_id` | BIGINT FK → admins | |
| `username` | TEXT | Snapshot |
| `name` | TEXT | Snapshot |
| `role` | TEXT | Snapshot (role changes require re-login) |
| `must_reset_password` | BOOLEAN | |
| `expires_at` | TIMESTAMPTZ | 30 minutes from creation |
| `created_at` | TIMESTAMPTZ | |

### `audit_logs`
| Column | Type | Notes |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `actor_type` | TEXT | `admin` or `system` |
| `actor_id` | BIGINT | 0 for system events |
| `actor_label` | TEXT | Username or email snapshot |
| `action` | TEXT | e.g. `login_success`, `booking_created` |
| `target_type` | TEXT | `user`, `admin`, `room`, `booking` |
| `target_id` | BIGINT | |
| `target_label` | TEXT | Snapshot of target name/email |
| `details` | TEXT | Free-text context |
| `ip_address` | TEXT | |
| `user_agent` | TEXT | |
| `created_at` | TIMESTAMPTZ | |

---

## Environment Variables

Copy `.env` and fill in your values. Variables are read at startup via `os.Getenv`.

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection string (`postgresql://user:pass@host/db`) |
| `PORT` | No | HTTP listen port (default: `8080`) |
| `APP_ENV` | No | Set to `production` to enable HTTPS redirect, HSTS, `__Host-` cookie prefix, and Secure cookies |
| `APP_URL` | No | Public base URL used to build password-reset links (e.g. `https://smartbook.company.bt`) |
| `RESEND_API_KEY` | No | Resend API key for sending emails. If absent, reset links are printed to the log (dev only) |
| `EMAIL_FROM` | No | `From` address in sent emails (e.g. `SmartBook <no-reply@yourdomain.com>`) |
| `TURNSTILE_SITE_KEY` | No | Cloudflare Turnstile site key (public, embedded in pages). Skip CAPTCHA if absent |
| `TURNSTILE_SECRET_KEY` | No | Cloudflare Turnstile secret key (server-side verification). Skip CAPTCHA if absent |

---

## Running Locally

**Prerequisites:** Go 1.22+, PostgreSQL 14+

```bash
# 1. Clone and enter the repo
git clone <repo-url> && cd smartbook

# 2. Copy and configure environment
cp .env.example .env
# Edit .env: set DATABASE_URL to your local PostgreSQL

# 3. Run (auto-migrates the database on startup)
go run .

# The server starts at http://localhost:8080
# Admin panel:  http://localhost:8080/login.html
# Public gate:  http://localhost:8080/index.html
```

**Generate a bcrypt password hash** (for seeding or scripts):
```bash
go run scripts/hashpw.go YourPassword123!
```

---

## Deployment (Render)

SmartBook is deployed on [Render](https://render.com).

1. Create a **PostgreSQL** database in Render; copy the external connection string.
2. Create a **Web Service** pointed at this repo.
   - Build command: `go build -o smartbook .`
   - Start command: `./smartbook`
3. Set all environment variables in Render's dashboard (Settings → Environment).
4. Set `APP_ENV=production` and `APP_URL=https://your-app.onrender.com`.
5. Deploy. The schema is created automatically on first boot.

---

## Default Credentials

The database seed inserts one Super Admin account. **Change the password immediately after first login.**

| Field | Value |
|---|---|
| Username | `ratuwangchuk@dhi.bt` |
| Password | `Rawang@3013` |

---

## Project Structure

```
smartbook/
├── main.go                      Entry point: DB connect, routing, background jobs
├── routes/
│   └── routes.go                All API route registrations
├── controllers/
│   ├── admin_controller.go      Admin CRUD, revoke/restore/unlock
│   ├── auth_controller.go       Login, logout, forgot/reset password, me
│   ├── booking_controller.go    Booking CRUD + public cancel/edit/minutes
│   ├── audit_controller.go      Audit log listing
│   ├── registration_controller.go  OTP, email check, device trust
│   ├── room_controller.go       Room CRUD
│   ├── user_controller.go       User CRUD, approve/reject
│   ├── helpers.go               Shared response writers, session helpers
│   └── security.go              Middleware: HTTPS redirect, secure headers, cookies
├── models/
│   ├── models.go                All struct definitions and shared validators
│   ├── admin_model.go           Admin DB queries
│   ├── booking_model.go         Booking DB queries + business rules
│   ├── room_model.go            Room DB queries
│   ├── user_model.go            User DB queries + device token logic
│   ├── audit_model.go           Audit log DB queries
│   ├── helpers.go               FillBookingDisplayFields, NormalizeBookingInput
│   └── errors.go                Sentinel errors: ErrNotFound, ErrDuplicate, etc.
├── session/
│   ├── store.go                 DB-backed admin session store (30-min TTL)
│   ├── attempt_store.go         In-memory failed-login counter (3 attempts → lock)
│   ├── otp_store.go             In-memory OTP store (10-min expiry, single-use)
│   └── reset_store.go           In-memory password-reset token store
├── database/
│   └── connection.go            Connect + auto-migrate (idempotent DDL statements)
├── utils/
│   ├── helpers.go               Time conversion, email validation, status computation
│   ├── mailer.go                HTML email builder + Resend API client
│   ├── provisioning.go          Random password + secure token generation
│   └── turnstile.go             Cloudflare Turnstile token verification
├── scripts/
│   └── hashpw.go                CLI utility to bcrypt-hash a password
└── view/
    ├── index.html               Public access gate
    ├── calendar.html            Public booking calendar
    ├── user-app.js              Public calendar JavaScript
    ├── login.html               Admin login
    ├── dashboard.html           Admin dashboard
    ├── admins.html              Admin management (super admin)
    ├── users.html               User management
    ├── bookings.html            Booking management
    ├── book-room.html           Create booking (admin)
    ├── rooms.html               Room management
    ├── history.html             Booking history (general admin)
    ├── audit-logs.html          Audit trail (super admin)
    ├── confirm-registration.html  Email confirmation landing page
    ├── force-password-change.html  Forced password reset page
    ├── app.js                   Shared admin panel utilities + icon helpers
    ├── auth-guard.js            Admin role guard for frontend pages
    ├── sidebar-extras.js        Role-based sidebar visibility
    └── turnstile-helper.js      Cloudflare Turnstile widget loader
```
