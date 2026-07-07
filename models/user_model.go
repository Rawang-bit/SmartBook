package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"bookroom-management-system/utils"
)

// UserModel manages all database operations and validation for registered users.
type UserModel struct {
	DB *sql.DB
}

// normalizeAndValidateUserInput trims/normalizes Name and Email in place and
// returns an error if either is invalid. Shared by Create, Register, and Update.
func normalizeAndValidateUserInput(req *UserRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Email = utils.NormalizeEmail(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)

	if req.Name == "" {
		return fmt.Errorf("user name is required")
	}
	if !utils.IsValidEmail(req.Email) {
		return fmt.Errorf("valid email address is required")
	}
	return nil
}

// List returns all users ordered alphabetically by name, excluding those whose
// email matches a super_admin username — super_admin is exclusive and any such
// users row would be stale data, not an active booking grant.
func (m *UserModel) List() ([]User, error) {
	rows, err := m.DB.Query(`
		SELECT u.id, u.name, u.email, u.phone, u.status, u.intended_role, u.confirm_token IS NOT NULL,
		       u.rejection_reason, TO_CHAR(u.created_at, 'YYYY-MM-DD HH24:MI'), COALESCE(a.role, '')
		FROM users u
		LEFT JOIN admins a ON LOWER(TRIM(a.username)) = LOWER(TRIM(u.email))
		WHERE NOT EXISTS (
			SELECT 1 FROM admins a2
			WHERE LOWER(TRIM(a2.username)) = LOWER(TRIM(u.email)) AND a2.role = 'super_admin'
		)
		ORDER BY u.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation, &u.RejectionReason, &u.CreatedAt, &u.AdminRole); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetByID fetches a single user by primary key.
// Returns ErrNotFound if no user has that ID.
func (m *UserModel) GetByID(id int64) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		SELECT id, name, email, phone, status, intended_role, confirm_token IS NOT NULL,
		       rejection_reason, TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI')
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation, &u.RejectionReason, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// GetByEmail looks up a user by email address (case-insensitive).
// Returns ErrNotFound if no user has that email.
func (m *UserModel) GetByEmail(email string) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		SELECT id, name, email, status, intended_role, confirm_token IS NOT NULL
		FROM users WHERE LOWER(TRIM(email)) = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Create registers a new user added directly by an admin. Starts as "pending" with a
// 7-day confirmation token — the admin typing someone else's email hasn't proved they
// own it, so the recipient must click the emailed link to activate.
// req.Role records what happens on confirm: "normal_user" activates the booking row;
// any admin role promotes it to a new admin account (see ConsumeConfirmToken).
// Returns the created user and plaintext token (caller must email it, never log it).
// Returns ErrDuplicate if the email is already registered.
func (m *UserModel) Create(req UserRequest) (User, string, error) {
	if err := normalizeAndValidateUserInput(&req); err != nil {
		return User{}, "", err
	}
	role := strings.TrimSpace(req.Role)
	if role != "general_admin" && role != "super_admin" {
		role = "normal_user"
	}

	token, err := utils.GenerateSecureToken()
	if err != nil {
		return User{}, "", fmt.Errorf("failed to generate confirmation token")
	}

	var u User
	err = m.DB.QueryRow(`
		INSERT INTO users(name, email, phone, status, intended_role, confirm_token, confirm_token_expires_at)
		VALUES($1, $2, $3, 'pending', $4, $5, NOW() + INTERVAL '7 days')
		RETURNING id, name, email, phone, status, intended_role
	`, req.Name, req.Email, req.Phone, role, token).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &u.IntendedRole)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return User{}, "", ErrDuplicate
		}
		return User{}, "", err
	}
	u.AwaitingConfirmation = true
	return u, token, nil
}

// ConsumeConfirmToken validates and single-uses a registration confirmation token.
// The token is cleared immediately regardless of outcome — it can never be replayed.
// Returns ErrNotFound if the token doesn't exist (already used or never issued).
func (m *UserModel) ConsumeConfirmToken(token string) (User, error) {
	var u User
	var expiresAt sql.NullTime

	err := m.DB.QueryRow(`
		SELECT id, name, email, status, intended_role, confirm_token_expires_at
		FROM users WHERE confirm_token = $1
	`, token).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &expiresAt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}

	// Clear the token now so it can never be used a second time.
	_, _ = m.DB.Exec(`UPDATE users SET confirm_token = NULL WHERE id = $1`, u.ID)

	if !expiresAt.Valid || time.Now().After(expiresAt.Time) {
		return User{}, fmt.Errorf("this confirmation link has expired — please ask an admin to add you again")
	}
	return u, nil
}

// Register self-registers a new user through the public access gate. The OTP already
// proved email ownership so no confirmation step is needed — the row starts as "pending"
// and appears for admin review immediately. Returns ErrDuplicate if already registered.
func (m *UserModel) Register(req UserRequest) (User, error) {
	if err := normalizeAndValidateUserInput(&req); err != nil {
		return User{}, err
	}

	var u User
	err := m.DB.QueryRow(`
		INSERT INTO users(name, email, phone, status) VALUES($1, $2, $3, 'pending')
		RETURNING id, name, email, phone, status, intended_role
	`, req.Name, req.Email, req.Phone).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &u.IntendedRole)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	u.AwaitingConfirmation = false
	return u, nil
}

// SetStatus updates a user's approval status ("active", "rejected", or "revoked").
// Returns the updated user so the caller can notify them by email.
// Returns ErrNotFound if the user does not exist.
func (m *UserModel) SetStatus(id int64, status string) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET status = $1 WHERE id = $2
		RETURNING id, name, email, status, intended_role, confirm_token IS NOT NULL
	`, status, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Reject marks a pending registration as rejected and stores the reason.
// Returns ErrNotFound if the user does not exist.
func (m *UserModel) Reject(id int64, reason string) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET status = 'rejected', rejection_reason = $1 WHERE id = $2
		RETURNING id, name, email, status, intended_role, confirm_token IS NOT NULL
	`, strings.TrimSpace(reason), id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Update changes a user's name, email, or phone.
// Returns ErrNotFound if the user does not exist, ErrDuplicate if the new email is taken.
func (m *UserModel) Update(id int64, req UserRequest) (User, error) {
	if err := normalizeAndValidateUserInput(&req); err != nil {
		return User{}, err
	}

	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET name = $1, email = $2, phone = $3 WHERE id = $4
		RETURNING id, name, email, phone, status, intended_role, confirm_token IS NOT NULL
	`, req.Name, req.Email, req.Phone, id).Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	return u, nil
}

// Delete removes a registered user.
// Returns ErrNotFound if the user does not exist.
func (m *UserModel) Delete(id int64) error {
	result, err := m.DB.Exec(`DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureActiveForEmail upserts an active booking-capable row for email.
// Called whenever General Admin role is granted — Normal User + General Admin is the
// one allowed multi-role combination, so the Normal User row must exist and be active.
func (m *UserModel) EnsureActiveForEmail(name, email string) error {
	name = strings.TrimSpace(name)
	email = utils.NormalizeEmail(email)
	_, err := m.DB.Exec(`
		INSERT INTO users(name, email, status)
		VALUES($1, $2, 'active')
		ON CONFLICT (email) DO UPDATE SET status = 'active'
	`, name, email)
	return err
}

// RemoveNormalUserAccess deletes any users-table row for email. Super Admin is an
// exclusive role — no Normal User booking capability is allowed alongside it.
// A no-op if no such row exists.
func (m *UserModel) RemoveNormalUserAccess(email string) error {
	email = utils.NormalizeEmail(email)
	_, err := m.DB.Exec(`DELETE FROM users WHERE LOWER(TRIM(email)) = $1`, email)
	return err
}

// DeviceTokenMatches reports whether rawToken matches the stored trusted-device token
// for email, and that it has not expired. Returns false for users who never opted in.
func (m *UserModel) DeviceTokenMatches(email, rawToken string) (bool, error) {
	if rawToken == "" {
		return false, nil
	}

	var storedHash sql.NullString
	var expiresAt sql.NullTime
	err := m.DB.QueryRow(`
		SELECT device_token_hash, device_token_expires_at
		FROM users WHERE LOWER(TRIM(email)) = $1
	`, email).Scan(&storedHash, &expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !storedHash.Valid || storedHash.String == "" || !expiresAt.Valid {
		return false, nil
	}
	if time.Now().After(expiresAt.Time) {
		return false, nil
	}
	return storedHash.String == utils.HashToken(rawToken), nil
}

// SetDeviceToken stores the hash and expiry of a new trusted-device token for a user,
// overwriting any previously remembered device — only one is trusted at a time.
// Called only when the user explicitly opts in to "remember this device" after OTP.
func (m *UserModel) SetDeviceToken(id int64, rawToken string, expiresAt time.Time) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = $1, device_token_expires_at = $2 WHERE id = $3
	`, utils.HashToken(rawToken), expiresAt, id)
	return err
}

// ClearDeviceToken revokes any previously remembered device for a user so an old
// opt-in doesn't linger as a silent OTP bypass.
func (m *UserModel) ClearDeviceToken(id int64) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = NULL, device_token_expires_at = NULL WHERE id = $1
	`, id)
	return err
}
