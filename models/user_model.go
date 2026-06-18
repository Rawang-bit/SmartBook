package models

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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

// List returns all users ordered alphabetically by name.
func (m *UserModel) List() ([]User, error) {
	rows, err := m.DB.Query(`SELECT id, name, email, status, intended_role FROM users ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole); err != nil {
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
		SELECT id, name, email, status, intended_role FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
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
		SELECT id, name, email, status, intended_role FROM users WHERE LOWER(TRIM(email)) = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Create registers a new user added directly by an admin. Unlike self-
// registration (which already proves email ownership via OTP), an admin
// typing in someone else's email address hasn't proven anything — so the
// new entry starts as "pending" and stays inactive until the recipient
// clicks the confirmation link emailed to them.
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
	req.Name  = strings.TrimSpace(req.Name)
	req.Email = utils.NormalizeEmail(req.Email)
	role := strings.TrimSpace(req.Role)
	if role != "general_admin" && role != "super_admin" {
		role = "normal_user"
	}

	if req.Name == "" {
		return User{}, "", fmt.Errorf("user name is required")
	}
	if !utils.IsValidEmail(req.Email) {
		return User{}, "", fmt.Errorf("valid email address is required")
	}

	token, err := generateConfirmToken()
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

// generateConfirmToken returns a cryptographically random 64-char hex token.
func generateConfirmToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Register self-registers a new user through the public access gate.
// The user is created with status "pending" and must be approved by an
// admin before they can book rooms.
// Returns ErrDuplicate if the email address is already registered.
func (m *UserModel) Register(req UserRequest) (User, error) {
	req.Name  = strings.TrimSpace(req.Name)
	req.Email = utils.NormalizeEmail(req.Email)

	if req.Name == "" {
		return User{}, fmt.Errorf("user name is required")
	}
	if !utils.IsValidEmail(req.Email) {
		return User{}, fmt.Errorf("valid email address is required")
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
	return u, nil
}

// SetStatus updates a user's approval status ("approved" or "rejected").
// Returns the updated user so the caller can notify them by email.
// Returns ErrNotFound if the user does not exist.
func (m *UserModel) SetStatus(id int64, status string) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET status = $1 WHERE id = $2
		RETURNING id, name, email, status, intended_role
	`, status, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Update changes a user's name or email.
// Returns ErrNotFound if the user does not exist, ErrDuplicate if the new email is taken.
func (m *UserModel) Update(id int64, req UserRequest) (User, error) {
	req.Name  = strings.TrimSpace(req.Name)
	req.Email = utils.NormalizeEmail(req.Email)

	if req.Name == "" {
		return User{}, fmt.Errorf("user name is required")
	}
	if !utils.IsValidEmail(req.Email) {
		return User{}, fmt.Errorf("valid email address is required")
	}

	var u User
	err := m.DB.QueryRow(`
		UPDATE users SET name = $1, email = $2 WHERE id = $3
		RETURNING id, name, email, status, intended_role
	`, req.Name, req.Email, id).Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.IntendedRole)
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
