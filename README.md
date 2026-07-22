# SmartBook — Online Room Booking System

## Project Purpose

SmartBook is a web-based meeting room reservation platform for organisations. It replaces manual/ad-hoc room booking with a self-service public booking calendar, an admin management portal, role-based access control, automated email notifications, and a full audit trail.

## System Architecture

Built with a Go backend (`net/http`) and a PostgreSQL database (`pgx/v5`), serving a static HTML/JavaScript + Tailwind CSS frontend that communicates exclusively through a JSON `/api/*` REST API.

```
Browser
  │
  ├── /view/*.html   Static frontend (public calendar + admin panel)
  └── /api/*         JSON REST API
        ├── controllers/   HTTP handlers
        ├── models/        Database queries + business logic
        ├── session/       Session, OTP, and login-attempt stores
        ├── utils/         Email, CAPTCHA, time helpers
        └── database/      Connection + auto-migration
```

The database schema is created and migrated automatically on server startup. The app is hosted on Render (web service + managed PostgreSQL).

## Key Functionalities

- **Public booking calendar** - passwordless email + OTP access, view/create/edit/cancel bookings, add post-meeting minutes.
- **Role-based access control** - Normal Users, General Admins, and Super Admins, each with distinct permissions.
- **Admin panel** - manage rooms, users, bookings, and (for Super Admins) other admin accounts.
- **Audit trail** - append-only log of all sensitive actions (logins, bookings, approvals, admin changes).
- **Automated email notifications** - OTPs, booking confirmations/cancellations, approvals, temporary passwords.
- **Security** - bcrypt password hashing, DB-backed sessions, login lockout after failed attempts, Cloudflare Turnstile CAPTCHA, secure cookies, and standard security headers.

## Live Application

https://smartbook-2ub8.onrender.com/
