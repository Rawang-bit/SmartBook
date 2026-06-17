package models

import (
	"database/sql"
	"fmt"
	"strings"

	"bookroom-management-system/utils"
)

// UserModel manages all database operations and validation for pre-registered users.
// It is the single source of truth for who is authorized to make room bookings.
type UserModel struct {
	DB *sql.DB
}

// List returns all users ordered alphabetically by name.
func (m *UserModel) List() ([]User, error) {
	rows, err := m.DB.Query(`SELECT id, name, email FROM users ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// GetByEmail looks up a user by email address (case-insensitive).
// Returns ErrNotFound if no user has that email.
func (m *UserModel) GetByEmail(email string) (User, error) {
	var u User
	err := m.DB.QueryRow(`
		SELECT id, name, email FROM users WHERE LOWER(TRIM(email)) = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	return u, err
}

// Create pre-registers a new user after validating name and email format.
// Returns ErrDuplicate if the email address is already registered.
func (m *UserModel) Create(req UserRequest) (User, error) {
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
		INSERT INTO users(name, email) VALUES($1, $2) RETURNING id, name, email
	`, req.Name, req.Email).Scan(&u.ID, &u.Name, &u.Email)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	return u, nil
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
		UPDATE users SET name = $1, email = $2 WHERE id = $3 RETURNING id, name, email
	`, req.Name, req.Email, id).Scan(&u.ID, &u.Name, &u.Email)
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

// Delete removes a pre-registered user.
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
