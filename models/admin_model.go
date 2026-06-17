package models

import (
	"database/sql"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"bookroom-management-system/utils"
)

// AdminModel manages all database operations and authentication logic for admin accounts.
// It is the single source of truth for admin state and business rules.
type AdminModel struct {
	DB *sql.DB
}

// List returns all admin accounts ordered by creation date.
func (m *AdminModel) List() ([]AdminDetail, error) {
	rows, err := m.DB.Query(`
		SELECT id, username, name, role, status, TO_CHAR(created_at, 'YYYY-MM-DD')
		FROM admins
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	admins := []AdminDetail{}
	for rows.Next() {
		var a AdminDetail
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &a.Role, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// GetByUsername fetches an admin by username along with its bcrypt hash and account status.
// Returns ErrNotFound if the username does not exist.
func (m *AdminModel) GetByUsername(username string) (Admin, string, string, error) {
	var admin  Admin
	var hash   string
	var status string
	err := m.DB.QueryRow(`
		SELECT id, username, name, role, status, password
		FROM admins WHERE username = $1
	`, username).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role, &status, &hash)
	if err == sql.ErrNoRows {
		return Admin{}, "", "", ErrNotFound
	}
	return admin, hash, status, err
}

// GetPasswordHash returns only the bcrypt hash for a given admin ID.
// Returns ErrNotFound if the admin does not exist.
func (m *AdminModel) GetPasswordHash(id int64) (string, error) {
	var hash string
	err := m.DB.QueryRow(`SELECT password FROM admins WHERE id = $1`, id).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return hash, err
}

// Create inserts a new admin account after validating and hashing the password.
// Returns ErrDuplicate if the username is already taken.
func (m *AdminModel) Create(req AdminRequest) (Admin, error) {
	req.Username = strings.TrimSpace(req.Username)
	req.Name     = strings.TrimSpace(req.Name)
	req.Password = strings.TrimSpace(req.Password)
	req.Role     = strings.TrimSpace(req.Role)

	if req.Username == "" || req.Password == "" || req.Name == "" {
		return Admin{}, fmt.Errorf("username, password, and name are required")
	}
	if len(req.Password) < 6 {
		return Admin{}, fmt.Errorf("password must be at least 6 characters")
	}
	if req.Role != "super_admin" && req.Role != "general_admin" {
		req.Role = "general_admin"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return Admin{}, fmt.Errorf("failed to hash password")
	}

	var admin Admin
	err = m.DB.QueryRow(`
		INSERT INTO admins(username, password, name, role)
		VALUES($1, $2, $3, $4)
		RETURNING id, username, name, role
	`, req.Username, string(hash), req.Name, req.Role).Scan(
		&admin.ID, &admin.Username, &admin.Name, &admin.Role,
	)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return Admin{}, ErrDuplicate
		}
		return Admin{}, err
	}
	return admin, nil
}

// Update changes an admin's name and/or role.
// currentAdminID and currentRole prevent an admin from demoting their own account.
// Returns ErrNotFound if the target admin does not exist.
func (m *AdminModel) Update(id int64, req AdminRequest, currentAdminID int64, currentRole string) (Admin, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Role = strings.TrimSpace(req.Role)

	if req.Name == "" {
		return Admin{}, fmt.Errorf("name is required")
	}
	if req.Role != "super_admin" && req.Role != "general_admin" {
		req.Role = "general_admin"
	}
	// An admin cannot change their own role
	if currentAdminID == id {
		req.Role = currentRole
	}

	var admin Admin
	err := m.DB.QueryRow(`
		UPDATE admins SET name = $1, role = $2 WHERE id = $3
		RETURNING id, username, name, role
	`, req.Name, req.Role, id).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role)
	if err == sql.ErrNoRows {
		return Admin{}, ErrNotFound
	}
	return admin, err
}

// ResetPassword sets a new bcrypt-hashed password for any admin.
// Validates minimum length before hashing.
// Returns ErrNotFound if the admin does not exist.
func (m *AdminModel) ResetPassword(id int64, newPassword string) error {
	if len(newPassword) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password")
	}
	result, err := m.DB.Exec(`UPDATE admins SET password = $1 WHERE id = $2`, string(hash), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChangeOwnPassword verifies the admin's current password then stores the new one.
// Returns ErrUnauthorized if the current password is wrong.
func (m *AdminModel) ChangeOwnPassword(id int64, currentPw, newPw string) error {
	if len(newPw) < 6 {
		return fmt.Errorf("new password must be at least 6 characters")
	}

	hash, err := m.GetPasswordHash(id)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPw)); err != nil {
		return ErrUnauthorized
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password")
	}

	_, err = m.DB.Exec(`UPDATE admins SET password = $1 WHERE id = $2`, string(newHash), id)
	return err
}

// SetStatus updates an admin's account status to "active" or "revoked".
// Returns ErrNotFound if the admin does not exist.
func (m *AdminModel) SetStatus(id int64, status string) error {
	result, err := m.DB.Exec(`UPDATE admins SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete permanently removes an admin account.
// Returns ErrNotFound if the admin does not exist.
func (m *AdminModel) Delete(id int64) error {
	result, err := m.DB.Exec(`DELETE FROM admins WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// VerifyPassword compares a plaintext password against a stored bcrypt hash.
// Returns nil if they match, non-nil otherwise.
func (m *AdminModel) VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
