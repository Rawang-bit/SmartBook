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

// normalizeAndValidateUserInput trims/normalizes Name and Email; returns error if invalid.
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

// List returns all users alphabetically, excluding rows whose email matches a super_admin (exclusive role).
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

// GetByID fetches a single user by primary key; returns ErrNotFound if missing.
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

// GetByEmail looks up a user by email (case-insensitive); returns ErrNotFound if missing.
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

// Create adds an admin-invited user as "pending" with a 7-day confirm token; normal_user activates, admin roles promote.
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

// ConsumeConfirmToken validates and burns a registration token (cleared on first use, cannot replay).
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

// Register self-registers a user via the public gate as "pending"; OTP already proved email ownership.
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

// SetStatus updates a user's status; returns the user (for email notifications) or ErrNotFound.
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

// Reject marks a user as rejected and stores the reason; returns ErrNotFound if missing.
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

// Update changes a user's name, email, or phone; returns ErrNotFound or ErrDuplicate on conflict.
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

// Delete removes a user; returns ErrNotFound if missing.
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

// EnsureActiveForEmail upserts an active user row for email — required for the Normal User + General Admin multi-role combo.
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

// RemoveNormalUserAccess deletes the users row for email (Super Admin is exclusive — no booking row allowed).
func (m *UserModel) RemoveNormalUserAccess(email string) error {
	email = utils.NormalizeEmail(email)
	_, err := m.DB.Exec(`DELETE FROM users WHERE LOWER(TRIM(email)) = $1`, email)
	return err
}

// DeviceTokenMatches checks rawToken against the stored trusted-device hash for email; false if expired or unset.
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

// SetDeviceToken stores a new trusted-device token hash (overwrites any previous — one device at a time).
func (m *UserModel) SetDeviceToken(id int64, rawToken string, expiresAt time.Time) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = $1, device_token_expires_at = $2 WHERE id = $3
	`, utils.HashToken(rawToken), expiresAt, id)
	return err
}

// ClearDeviceToken removes any trusted-device token so the user must re-verify on next access.
func (m *UserModel) ClearDeviceToken(id int64) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = NULL, device_token_expires_at = NULL WHERE id = $1
	`, id)
	return err
}
