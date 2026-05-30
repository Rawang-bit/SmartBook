# SmartBook — Meeting Room Booking System

SmartBook is a lightweight meeting-room reservation system built with **Go** (backend) and plain **HTML / Tailwind CSS / JavaScript** (frontend). It uses **PostgreSQL** as the database. No heavyweight framework is used — everything is written with Go's built-in `net/http` package.

---

## Table of Contents

1. [Features](#features)
2. [Project Structure](#project-structure)
3. [How It Works — Architecture](#how-it-works--architecture)
4. [Setup & Running Locally](#setup--running-locally)
5. [Environment Variables](#environment-variables)
6. [Database](#database)
7. [API Endpoints](#api-endpoints)
8. [Frontend Pages](#frontend-pages)
9. [Admin Roles & Permissions](#admin-roles--permissions)
10. [Authentication & Sessions](#authentication--sessions)
11. [Security Notes](#security-notes)
12. [Changing the Admin Password](#changing-the-admin-password)

---

## Features

| Area | What it does |
|---|---|
| **Public calendar** | Anyone can view room availability and create a booking |
| **Email validation** | Users must be pre-registered by admin before they can book |
| **Conflict detection** | The backend blocks double-booking the same room at the same time |
| **Role-based admin** | Two tiers: `super_admin` (full access) and `general_admin` (limited access) |
| **Admin management** | Super admin can create, edit, reset passwords, revoke, and delete other admins |
| **Revoke access** | Super admin can suspend any admin's login without deleting the account |
| **Cookie sessions** | Admin login uses secure server-side sessions (no token in browser storage) |
| **Bcrypt passwords** | Admin passwords are stored as bcrypt hashes, never plaintext |
| **Auto status** | Booking status updates live: Booked → In Progress → Completed |

---

## Project Structure

```
smartbook/
│
├── main.go                           # Entry point: loads config, connects DB, starts server
│
├── .env                              # Environment variables (do not commit to git)
├── go.mod / go.sum                   # Go module and dependency lock files
│
├── scripts/
│   └── hashpw.go                     # Helper: generate a bcrypt hash for a password
│
├── database/
│   ├── connection.go                 # Opens the PostgreSQL connection (reads DATABASE_URL)
│   ├── 01_create_database.sql        # Creates the bookroom_db database
│   └── 02_create_tables_and_seed.sql # Creates all tables, indexes, and seeds default data
│
├── session/
│   └── store.go                      # In-memory session store with automatic hourly cleanup
│
├── models/
│   └── models.go                     # All Go structs (Admin, User, Room, Booking, etc.)
│
├── routes/
│   └── routes.go                     # Registers all API URL routes with role-based middleware
│
├── controllers/
│   ├── helpers.go                    # Shared utilities: JSON helpers, middleware, RequireSuperAdmin
│   ├── auth_controller.go            # Login, Logout, Me
│   ├── admin_controller.go           # Admin CRUD + revoke/restore + change own password
│   ├── room_controller.go            # CRUD for rooms
│   ├── user_controller.go            # CRUD for registered users
│   └── booking_controller.go        # CRUD for bookings + public cancel
│
└── web/                              # Static frontend files served by Go
    ├── index.html                    # Public booking calendar
    ├── login.html                    # Admin login page
    ├── dashboard.html                # Admin dashboard (stats + live room status)
    ├── rooms.html                    # Admin: manage rooms (super admin only for writes)
    ├── users.html                    # Admin: manage registered users
    ├── bookings.html                 # Admin: view and manage bookings
    ├── book-room.html                # Admin: create a booking manually
    ├── history.html                  # Admin: completed and cancelled bookings
    ├── admins.html                   # Super admin: manage admin accounts
    ├── app.js                        # Shared admin JavaScript (API calls, formatting helpers)
    ├── auth-guard.js                 # Redirects unauthenticated visitors to login.html
    ├── sidebar-extras.js             # Shared sidebar logic: admin nav visibility + change password modal
    └── user-app.js                   # Public calendar JavaScript
```

---

## How It Works — Architecture

```
Browser
  │
  │  HTTP requests
  ▼
main.go  ──►  routes.go  ──►  controllers/
                                  │
                                  ├── RequireAdmin middleware     (checks session cookie)
                                  ├── RequireSuperAdmin middleware (role check on top of session)
                                  │
                                  ├── auth_controller.go          (login / logout / me)
                                  ├── admin_controller.go         (admin CRUD / revoke / change pw)
                                  ├── room_controller.go          (list / create / update / delete)
                                  ├── user_controller.go          (list / validate / create / update / delete)
                                  └── booking_controller.go       (list / create / update / cancel / delete)
                                  │
                                  ▼
                            PostgreSQL database
```

**Request flow — public user creates a booking:**

1. User opens `index.html` and clicks a free time slot.
2. They enter their email. Browser calls `GET /api/public/users/validate?email=...`.
3. Server checks the `users` table. If registered, it returns the user's name.
4. User fills in the meeting title and clicks Confirm.
5. Browser calls `POST /api/bookings` with the booking details.
6. Server validates date, time, room availability, and checks for conflicts.
7. On success a row is inserted into `bookings` and the saved booking is returned.

**Request flow — admin logs in:**

1. Admin opens `login.html` and submits username + password.
2. Browser calls `POST /api/auth/login`.
3. Server looks up the admin, checks `status` is `active`, then verifies password with **bcrypt**.
4. Server creates a session, stores it in memory, and sets an `HttpOnly` cookie.
5. Browser stores the cookie automatically; all future admin API calls include it.
6. On each admin request, `RequireAdmin` reads the cookie and validates the session.
7. Routes that require `super_admin` additionally go through `RequireSuperAdmin`.

---

## Setup & Running Locally

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [PostgreSQL 14+](https://www.postgresql.org/download/)

### Steps

**1. Clone the repository**

```bash
git clone <repo-url>
cd smartbook
```

**2. Create the database**

Open a PostgreSQL shell (`psql`) and run:

```sql
\i database/01_create_database.sql
\c bookroom_db
\i database/02_create_tables_and_seed.sql
```

**3. Set environment variables**

The app reads configuration directly from environment variables — there is no `.env` loading in the Go code itself.

```bash
export DATABASE_URL=postgres://postgres:yourpassword@localhost:5432/bookroom_db?sslmode=disable
export PORT=8080                   # optional, defaults to 8080
export APP_ENV=production          # optional, set when running behind HTTPS
```

> **Dev container / Docker users:** the `.devcontainer/docker-compose.yml` picks up a `.env` file at the project root automatically. Copy the values above into `.env` instead of exporting them.

`APP_ENV=production` enables the `Secure` flag on the session cookie (HTTPS-only). Leave it unset for local HTTP development.

**4. Install Go dependencies**

```bash
go mod tidy
```

**5. Start the server**

```bash
go run main.go
```

**6. Open the app**

| URL | Page |
|---|---|
| `http://localhost:8080` | Public booking calendar |
| `http://localhost:8080/login.html` | Admin login |

**Default admin credentials** (change immediately after first login via Admins page):

| Username | Password | Role |
|---|---|---|
| `Rawang` | `Rawang@3013` | `super_admin` |

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | **Yes** | — | PostgreSQL connection string |
| `PORT` | No | `8080` | Port the HTTP server listens on |
| `APP_ENV` | No | `""` | Set to `production` to add `Secure` flag to session cookie (HTTPS only) |

---

## Database

### Tables

#### `admins`

| Column | Type | Notes |
|---|---|---|
| id | BIGSERIAL | Primary key |
| username | TEXT | Unique login name |
| password | TEXT | **bcrypt hash** — never plaintext |
| name | TEXT | Display name |
| role | TEXT | `super_admin` or `general_admin` |
| status | TEXT | `active` or `revoked` |
| created_at | TIMESTAMPTZ | Set automatically |

#### `users`
People pre-registered by admin who are allowed to make bookings.

| Column | Type | Notes |
|---|---|---|
| id | BIGSERIAL | Primary key |
| email | TEXT | Unique; compared in lowercase |
| name | TEXT | Display name |
| created_at | TIMESTAMPTZ | Set automatically |

#### `rooms`

| Column | Type | Notes |
|---|---|---|
| id | BIGSERIAL | Primary key |
| name | TEXT | Unique |
| capacity | INTEGER | Maximum number of people |
| location | TEXT | Floor or building |
| status | TEXT | `Active` or `Inactive` |
| created_at | TIMESTAMPTZ | Set automatically |

#### `bookings`

| Column | Type | Notes |
|---|---|---|
| id | BIGSERIAL | Primary key |
| user_name | TEXT | Name captured at time of booking |
| email | TEXT | Booker's email |
| room_id | BIGINT | Foreign key → `rooms(id)` ON DELETE RESTRICT |
| booking_date | DATE | Format: YYYY-MM-DD |
| start_time | TEXT | Format: HH:MM (24-hour) |
| end_time | TEXT | Format: HH:MM (24-hour) |
| purpose | TEXT | Meeting title / reason |
| status | TEXT | `Booked`, `In Progress`, `Completed`, `Cancelled` |
| created_at | TIMESTAMPTZ | Set automatically |
| updated_at | TIMESTAMPTZ | Updated on every change |

> **Existing database migration:** if you created the database before `role` and `status` were added to `admins`, run:
> ```sql
> ALTER TABLE admins ADD COLUMN IF NOT EXISTS role   TEXT NOT NULL DEFAULT 'general_admin';
> ALTER TABLE admins ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
> UPDATE admins SET role = 'super_admin' WHERE username = 'Rawang';
> ```

---

## API Endpoints

### Auth

| Method | URL | Auth | Description |
|---|---|---|---|
| `POST` | `/api/auth/login` | Public | Login. Sets `HttpOnly` session cookie on success. |
| `POST` | `/api/auth/logout` | Public | Deletes session and clears cookie. |
| `GET` | `/api/auth/me` | Public | Returns current admin info if session is valid. |

### Admin Management

| Method | URL | Auth | Description |
|---|---|---|---|
| `GET` | `/api/admins` | Super Admin | List all admins with status. |
| `POST` | `/api/admins` | Super Admin | Create a new admin account. |
| `PUT` | `/api/admins/{id}` | Super Admin | Update admin name or role. |
| `PATCH` | `/api/admins/{id}` | Super Admin | Reset another admin's password. |
| `POST` | `/api/admins/{id}/revoke` | Super Admin | Revoke an admin's login access. |
| `POST` | `/api/admins/{id}/restore` | Super Admin | Restore a revoked admin's access. |
| `DELETE` | `/api/admins/{id}` | Super Admin | Permanently delete an admin. |
| `POST` | `/api/admin/change-password` | Any Admin | Change own password (requires current password). |

### Users

| Method | URL | Auth | Description |
|---|---|---|---|
| `GET` | `/api/public/users/validate?email=` | Public | Check if an email is registered. |
| `GET` | `/api/public/users` | Public | List all registered users. |
| `GET` | `/api/users` | Admin | List all users. |
| `POST` | `/api/users` | Admin | Add a new registered user. |
| `PUT` | `/api/users/{id}` | Super Admin | Update user name or email. |
| `DELETE` | `/api/users/{id}` | Super Admin | Remove a user. |

### Rooms

| Method | URL | Auth | Description |
|---|---|---|---|
| `GET` | `/api/rooms` | Public | List all rooms. |
| `POST` | `/api/rooms` | Super Admin | Create a room. |
| `PUT` | `/api/rooms/{id}` | Super Admin | Update a room. |
| `DELETE` | `/api/rooms/{id}` | Super Admin | Delete a room (blocked if it has bookings). |

### Bookings

| Method | URL | Auth | Description |
|---|---|---|---|
| `GET` | `/api/bookings` | Public | List bookings. Optional `?room=` filter. |
| `POST` | `/api/bookings` | Public | Create a booking (email must be registered). |
| `POST` | `/api/bookings/{id}/cancel` | Public | Cancel own booking by providing email in body. |
| `PUT` | `/api/bookings/{id}` | Super Admin | Edit any booking. |
| `DELETE` | `/api/bookings/{id}` | Super Admin | Cancel a booking (soft). |
| `DELETE` | `/api/bookings/{id}?hard=1` | Super Admin | Permanently delete a booking. |

### Example requests

**Login**
```json
POST /api/auth/login
{ "username": "Rawang", "password": "Rawang@3013" }

200 OK
{ "admin": { "id": 1, "username": "Rawang", "name": "System Admin", "role": "super_admin" } }
```

**Create booking**
```json
POST /api/bookings
{
  "email":   "alice@company.com",
  "roomId":  1,
  "date":    "2026-06-01",
  "start":   "10:00",
  "end":     "11:00",
  "purpose": "Team standup"
}
```

**Error format** — all errors use this shape:
```json
{ "error": "this room is already booked for the selected time slot" }
```

---

## Frontend Pages

### Public

| File | URL | Description |
|---|---|---|
| `index.html` | `/` | Weekly/daily calendar grid. Click a free slot to book. |

### Admin (login required)

| File | URL | Role | Description |
|---|---|---|---|
| `login.html` | `/login.html` | — | Admin login form |
| `dashboard.html` | `/dashboard.html` | Any | Stats cards, live room occupancy, upcoming meetings |
| `rooms.html` | `/rooms.html` | Any (writes: Super) | Add, edit, or deactivate rooms |
| `users.html` | `/users.html` | Any (writes: Super) | Pre-register users who are allowed to book |
| `book-room.html` | `/book-room.html` | Any | Manually create a booking for any registered user |
| `bookings.html` | `/bookings.html` | Any (actions: Super) | Filter, cancel, or permanently delete bookings |
| `history.html` | `/history.html` | Any | Completed and cancelled bookings archive |
| `admins.html` | `/admins.html` | **Super Admin only** | Create, edit, reset passwords, revoke, and delete admins |

All admin pages include a **sidebar** with navigation links and a Logout button pinned to the bottom.

---

## Admin Roles & Permissions

| Action | General Admin | Super Admin |
|---|---|---|
| View dashboard, rooms, bookings, history | ✅ | ✅ |
| Create bookings | ✅ | ✅ |
| Add registered users | ✅ | ✅ |
| Edit / delete users | ❌ | ✅ |
| Create / edit / delete rooms | ❌ | ✅ |
| Cancel / delete bookings | ❌ | ✅ |
| Access Admins page | ❌ | ✅ |
| Create / edit / delete other admins | ❌ | ✅ |
| Revoke / restore admin access | ❌ | ✅ |
| Change own password | ✅ (via Admins page) | ✅ (via Admins page) |

---

## Authentication & Sessions

SmartBook uses **cookie-based sessions** with **server-side session storage**.

| Step | What happens |
|---|---|
| Login | Admin POSTs credentials → server checks `status = active` → verifies password with bcrypt → session created in memory → `HttpOnly` cookie set in browser |
| Each request | Browser sends cookie automatically → `RequireAdmin` middleware validates session |
| Super admin routes | Additionally validated by `RequireSuperAdmin` middleware |
| Logout | Session deleted from server → browser cookie cleared |
| Expiry | Sessions last 8 hours. Expired sessions are cleaned up automatically every hour. |

**Why `HttpOnly`?**
JavaScript cannot read an `HttpOnly` cookie. This protects the session ID from XSS attacks.

**Why server-side storage?**
The browser only holds a random opaque ID. The actual admin info never leaves the server.

---

## Security Notes

| Topic | Implementation |
|---|---|
| **Passwords** | bcrypt hash with cost 10. Never stored or logged as plaintext. |
| **Session cookies** | `HttpOnly` + `SameSite=Strict`. Add `APP_ENV=production` to also set `Secure` (HTTPS-only). |
| **SQL injection** | All queries use parameterised placeholders (`$1`, `$2`…). No string concatenation in SQL. |
| **Email validation** | Go's `net/mail.ParseAddress` (RFC 5322). Rejects malformed addresses. |
| **Input sanitisation** | All text inputs are trimmed and normalised before use or storage. |
| **Conflict checking** | Server-side overlap detection prevents double-booking even under concurrent requests. |
| **Foreign keys** | `bookings.room_id ON DELETE RESTRICT` — rooms with bookings cannot be accidentally deleted. |
| **Revoked accounts** | Status is checked after bcrypt verification to avoid leaking whether a username exists. |
| **Role enforcement** | Role checks happen server-side via middleware — client-side role flags are UI-only. |

---

## Changing the Admin Password

**Via the UI (recommended):**
Log in as any admin, go to **Admins**, find your own row, and click **Change PW**. You will be prompted for your current password and a new one.

**Via the command line** (useful for initial setup or recovery):

```bash
go run scripts/hashpw.go "MyNewSecurePassword!"
# Prints: $2a$10$...
```

Then update the database:

```sql
UPDATE admins
SET password = '$2a$10$...'   -- paste the output from hashpw
WHERE username = 'Rawang';
```
