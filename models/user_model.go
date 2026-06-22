package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"bookroom-management-system/utils"
)

// UserModel manages all database operations and validation for registered users.
// It is the single source of truth for who is authorized to make room bookings.
type UserModel struct {
	DB *sql.DB
}

// normalizeAndValidateUserInput trims/normalizes Name and Email in place and
// returns an error if either is invalid. Shared by Create, Register, and Update.
func normalizeAndValidateUserInput(req *UserRequest) error {
	req.Name  = strings.TrimSpace(req.Name)
	req.Email = utils.NormalizeEmail(req.Email)

	if req.Name == "" {
		return fmt.Errorf("user name is required")
	}
	if !utils.IsValidEmail(req.Email) {
		return fmt.Errorf("valid email address is required")
	}
	return nil
}

// List returns all users ordered alphabetically by name.
func (m *UserModel) List() ([]User, error) {
	rows, err := m.DB.Query(`
		SELECT id, name, email, status, intended_role, confirm_token IS NOT NULL
		FROM users ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation); err != nil {
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
		SELECT id, name, email, status, intended_role, confirm_token IS NOT NULL
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
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

// Create registers a new user added directly by an admin. Unlike self-
// registration (which already proves email ownership via OTP), an admin
// typing in someone else's email address hasn't proven anything — so the
// new entry starts as "pending" and stays inactive until the recipient
// clicks the confirmation link emailed to them. It never appears in the
// Users page as something an admin reviews/approves — AwaitingConfirmation
// tells the frontend to hide the Approve/Reject actions for this row and
// show a "Pending Approval" status instead.
//
// req.Role records what should happen once they confirm: "normal_user"
// simply activates the booking-user row; "general_admin"/"super_admin"
// promotes the confirmation into a brand-new admin account (the caller
// handles that branch — see ConsumeConfirmToken).
//
// Returns the created user and the plaintext confirmation token — the
// caller is responsible for emailing it; it is never stored in plaintext
// logs and is cleared the moment it's used.
// Returns ErrDuplicate if the email address is already registered.
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
		INSERT INTO users(name, email, status, intended_role, confirm_token, confirm_token_expires_at)
		VALUES($1, $2, 'pending', $3, $4, NOW() + INTERVAL '7 days')
		RETURNING id, name, email, status, intended_role
	`, req.Name, req.Email, role, token).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return User{}, "", ErrDuplicate
		}
		return User{}, "", err
	}
	u.AwaitingConfirmation = true
	return u, token, nil
}

// ConsumeConfirmToken validates and single-uses a registration confirmation
// token sent to an admin-added user. The token is cleared immediately
// regardless of outcome, so it can never be replayed.
// Returns ErrNotFound if the token doesn't exist (already used or never valid).
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

	// Clear the token now so it can never be used a second time, whether or
	// not it turns out to have expired.
	_, _ = m.DB.Exec(`UPDATE users SET confirm_token = NULL WHERE id = $1`, u.ID)

	if !expiresAt.Valid || time.Now().After(expiresAt.Time) {
		return User{}, fmt.Errorf("this confirmation link has expired — please ask an admin to add you again")
	}
	return u, nil
}

// Register self-registers a new user through the public access gate.
// The user is created with status "pending" and must be approved by an
// admin before they can book rooms. Unlike admin-added users, no email
// confirmation step is needed — the OTP already proved they own the address —
// so this row always shows up in the Users page for an admin to review.
// Returns ErrDuplicate if the email address is already registered.
func (m *UserModel) Register(req UserRequest) (User, error) {
	if err := normalizeAndValidateUserInput(&req); err != nil {
		return User{}, err
	}

	var u User
	err := m.DB.QueryRow(`
		INSERT INTO users(name, email, status) VALUES($1, $2, 'pending')
		RETURNING id, name, email, status, intended_role
	`, req.Name, req.Email).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
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

// Update changes a user's name or email.
// Returns ErrNotFound if the user does not exist, ErrDuplicate if the new email is taken.
func (m *UserModel) Update(id int64, req UserRequest) (User, error) {
	if err := normalizeAndValidateUserInput(&req); err != nil {
		return User{}, err
	}

	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET name = $1, email = $2 WHERE id = $3
		RETURNING id, name, email, status, intended_role, confirm_token IS NOT NULL
	`, req.Name, req.Email, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole, &u.AwaitingConfirmation)
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

// DeviceTokenMatches reports whether rawToken matches the stored
// trusted-device token for email, and that it has not expired. Used by the
// public access gate to decide whether a returning visitor can skip OTP
// verification. A user who never opted in to "remember this device" simply
// has no stored hash, so this always returns false for them.
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

// SetDeviceToken stores the hash of a newly-issued device-trust token for a
// user along with its expiry, overwriting any previously remembered device —
// only one device is trusted at a time. Called only when the user explicitly
// opts in to "remember this device" after a successful OTP verification.
func (m *UserModel) SetDeviceToken(id int64, rawToken string, expiresAt time.Time) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = $1, device_token_expires_at = $2 WHERE id = $3
	`, utils.HashToken(rawToken), expiresAt, id)
	return err
}

// ClearDeviceToken revokes any previously remembered device for a user.
// Called when a user verifies via OTP but declines to be remembered this
// time, so an old opt-in doesn't linger as a silent bypass.
func (m *UserModel) ClearDeviceToken(id int64) error {
	_, err := m.DB.Exec(`
		UPDATE users SET device_token_hash = NULL, device_token_expires_at = NULL WHERE id = $1
	`, id)
	return err
}
