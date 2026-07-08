package models

import (
	"database/sql"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"bookroom-management-system/utils"
)

// AdminModel handles all database operations for admin accounts.
type AdminModel struct {
	DB *sql.DB
}

// normalizeAdminRole coerces unknown roles to "general_admin" — safe default over empty/invalid.
func normalizeAdminRole(role string) string {
	if role == "super_admin" {
		return role
	}
	return "general_admin"
}

// nullableString returns nil for empty string so UNIQUE constraints on optional columns
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// List returns all admin accounts ordered by creation date.
func (m *AdminModel) List() ([]AdminDetail, error) {
	rows, err := m.DB.Query(`
		SELECT id, username, name, role, COALESCE(email, ''), status, TO_CHAR(created_at, 'YYYY-MM-DD')
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
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &a.Role, &a.Email, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// ListLocked returns all admin accounts that are currently locked out. Accessible to both admin roles.
func (m *AdminModel) ListLocked() ([]AdminDetail, error) {
	rows, err := m.DB.Query(`
		SELECT id, username, name, role, COALESCE(email, ''), status, TO_CHAR(created_at, 'YYYY-MM-DD')
		FROM admins
		WHERE status = 'locked'
		ORDER BY username ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	admins := []AdminDetail{}
	for rows.Next() {
		var a AdminDetail
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &a.Role, &a.Email, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// GetByUsername returns the admin row, bcrypt hash, and status; ErrNotFound if missing.
func (m *AdminModel) GetByUsername(username string) (Admin, string, string, error) {
	var admin  Admin
	var hash   string
	var status string
	err := m.DB.QueryRow(`
		SELECT id, username, name, role, status, password, must_reset_password
		FROM admins WHERE username = $1
	`, username).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role, &status, &hash, &admin.MustResetPassword)
	if err == sql.ErrNoRows {
		return Admin{}, "", "", ErrNotFound
	}
	return admin, hash, status, err
}

// GetByID fetches an admin by primary key; returns ErrNotFound if missing.
func (m *AdminModel) GetByID(id int64) (Admin, string, error) {
	var admin Admin
	var email sql.NullString
	err := m.DB.QueryRow(`
		SELECT id, username, name, role, email FROM admins WHERE id = $1
	`, id).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role, &email)
	if err == sql.ErrNoRows {
		return Admin{}, "", ErrNotFound
	}
	return admin, email.String, err
}

// GetByUsernameAndEmail requires both fields to match — prevents reset-link abuse when an attacker knows only one field.
func (m *AdminModel) GetByUsernameAndEmail(username, email string) (int64, string, string, error) {
	var (
		id          int64
		name        string
		storedEmail sql.NullString
	)
	err := m.DB.QueryRow(`
		SELECT id, name, email
		FROM admins
		WHERE LOWER(username)          = LOWER($1)
		  AND email IS NOT NULL
		  AND LOWER(TRIM(email))       = $2
		  AND status                   = 'active'
	`, username, email).Scan(&id, &name, &storedEmail)

	if err == sql.ErrNoRows {
		return 0, "", "", ErrNotFound
	}
	if err != nil {
		return 0, "", "", err
	}
	return id, name, storedEmail.String, nil
}

// GetPasswordHash returns the bcrypt hash for an admin; ErrNotFound if missing.
func (m *AdminModel) GetPasswordHash(id int64) (string, error) {
	var hash string
	err := m.DB.QueryRow(`SELECT password FROM admins WHERE id = $1`, id).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return hash, err
}

// Create adds a new admin. Sets must_reset_password=true (super admin chose the password, not the recipient).
func (m *AdminModel) Create(req AdminRequest) (Admin, error) {
	req.Name     = strings.TrimSpace(req.Name)
	req.Password = strings.TrimSpace(req.Password)
	req.Role     = strings.TrimSpace(req.Role)
	req.Email    = utils.NormalizeEmail(req.Email)

	if req.Email == "" || req.Password == "" || req.Name == "" {
		return Admin{}, fmt.Errorf("email, password, and name are required")
	}
	if !utils.IsValidEmail(req.Email) {
		return Admin{}, fmt.Errorf("invalid email address")
	}
	if err := ValidatePasswordComplexity(req.Password); err != nil {
		return Admin{}, err
	}
	req.Role = normalizeAdminRole(req.Role)

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return Admin{}, fmt.Errorf("failed to hash password")
	}

	username := req.Email

	var admin Admin
	err = m.DB.QueryRow(`
		INSERT INTO admins(username, password, name, role, email, must_reset_password)
		VALUES($1, $2, $3, $4, $5, TRUE)
		RETURNING id, username, name, role, must_reset_password
	`, username, string(hash), req.Name, req.Role, req.Email).Scan(
		&admin.ID, &admin.Username, &admin.Name, &admin.Role, &admin.MustResetPassword,
	)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return Admin{}, ErrDuplicate
		}
		return Admin{}, err
	}
	return admin, nil
}

// CreateWithGeneratedPassword creates an admin with a random temp password; returns the plaintext for the caller to email.
func (m *AdminModel) CreateWithGeneratedPassword(username, name, role, email string) (Admin, string, error) {
	username = strings.TrimSpace(username)
	name     = strings.TrimSpace(name)
	email    = utils.NormalizeEmail(email)
	role     = normalizeAdminRole(role)

	password, err := utils.GenerateRandomPassword(16)
	if err != nil {
		return Admin{}, "", fmt.Errorf("failed to generate temporary password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Admin{}, "", fmt.Errorf("failed to hash password")
	}

	emailParam := nullableString(email)

	var admin Admin
	err = m.DB.QueryRow(`
		INSERT INTO admins(username, password, name, role, email, must_reset_password)
		VALUES($1, $2, $3, $4, $5, TRUE)
		RETURNING id, username, name, role, must_reset_password
	`, username, string(hash), name, role, emailParam).Scan(
		&admin.ID, &admin.Username, &admin.Name, &admin.Role, &admin.MustResetPassword,
	)
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return Admin{}, "", ErrDuplicate
		}
		return Admin{}, "", err
	}
	return admin, password, nil
}

// Update changes name, role, and/or email. Prevents self-demotion. ErrNotFound or ErrDuplicate on conflict.
func (m *AdminModel) Update(id int64, req AdminRequest, currentAdminID int64, currentRole string) (Admin, error) {
	req.Name  = strings.TrimSpace(req.Name)
	req.Role  = strings.TrimSpace(req.Role)
	req.Email = utils.NormalizeEmail(req.Email)

	if req.Name == "" {
		return Admin{}, fmt.Errorf("name is required")
	}
	req.Role = normalizeAdminRole(req.Role)
	if currentAdminID == id {
		req.Role = currentRole // prevent self-demotion
	}
	if req.Email != "" && !utils.IsValidEmail(req.Email) {
		return Admin{}, fmt.Errorf("invalid email address")
	}

	emailParam := nullableString(req.Email)

	var admin Admin
	err := m.DB.QueryRow(`
		UPDATE admins SET name = $1, role = $2, email = $3 WHERE id = $4
		RETURNING id, username, name, role
	`, req.Name, req.Role, emailParam, id).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role)
	if err == sql.ErrNoRows {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return Admin{}, ErrDuplicate
		}
		return Admin{}, err
	}
	return admin, nil
}

// ResetPassword sets a password chosen by another admin — sets must_reset_password=true (force change on login).
func (m *AdminModel) ResetPassword(id int64, newPassword string) error {
	if err := ValidatePasswordComplexity(newPassword); err != nil {
		return err
	}
	oldHash, err := m.GetPasswordHash(id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(newPassword)) == nil {
		return fmt.Errorf("new password must be different from the current password")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password")
	}
	result, err := m.DB.Exec(`UPDATE admins SET password = $1, must_reset_password = TRUE WHERE id = $2`, string(hash), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyPasswordReset stores a self-chosen password from the reset link; clears must_reset_password (ownership proved by token).
func (m *AdminModel) ApplyPasswordReset(id int64, newPassword string) error {
	if err := ValidatePasswordComplexity(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password")
	}
	result, err := m.DB.Exec(
		`UPDATE admins SET password = $1, must_reset_password = FALSE WHERE id = $2`,
		string(hash), id,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChangeOwnPassword verifies the current password, updates to the new one, and clears must_reset_password.
func (m *AdminModel) ChangeOwnPassword(id int64, currentPw, newPw string) error {
	if err := ValidatePasswordComplexity(newPw); err != nil {
		return err
	}
	hash, err := m.GetPasswordHash(id)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPw)); err != nil {
		return ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(newPw)) == nil {
		return fmt.Errorf("new password must be different from your current password")
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password")
	}
	_, err = m.DB.Exec(`UPDATE admins SET password = $1, must_reset_password = FALSE WHERE id = $2`, string(newHash), id)
	return err
}

// SetStatus sets an admin to "active" or "revoked"; returns ErrNotFound if missing.
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

// Delete permanently removes an admin; returns ErrNotFound if missing.
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
func (m *AdminModel) VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
