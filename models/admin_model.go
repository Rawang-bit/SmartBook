package models

import (
	"database/sql"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"bookroom-management-system/utils"
)

// AdminModel manages all database operations and authentication logic for admin accounts.
type AdminModel struct {
	DB *sql.DB
}

// normalizeAdminRole defaults any role other than "super_admin" to "general_admin".
func normalizeAdminRole(role string) string {
	if role == "super_admin" {
		return role
	}
	return "general_admin"
}

// nullableString converts an empty string to SQL NULL so a unique constraint
// on an optional column (e.g. admins.email) doesn't treat multiple blank
// values as duplicates of each other.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// List returns all admin accounts ordered by creation date, including their email.
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
			continue
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// GetByUsername fetches an admin by username along with its bcrypt hash and account status.
// The returned Admin's MustResetPassword field tells the caller whether this
// account still needs to replace a generated temporary password.
// Returns ErrNotFound if the username does not exist.
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

// GetByID fetches an admin's username, name, role, and email by primary key.
// Returns ErrNotFound if the admin does not exist.
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

// GetByUsernameAndEmail looks up an active admin whose username AND email both match.
// Both comparisons are case-insensitive.
// Returns the admin's ID, display name, and stored email, or ErrNotFound if no row matches.
//
// This is the gating check for the forgot-password flow: an attacker who only knows
// a username (or only an email) cannot trigger a reset for someone else's account.
func (m *AdminModel) GetByUsernameAndEmail(username, email string) (int64, string, string, error) {
	var (
		id         int64
		name       string
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
// The admin's email address doubles as their login username — there is no
// separate username field — so every admin created here logs in with the
// same address used for password-reset emails, matching the username
// assigned when a self-registration is promoted to an admin role (see
// createAdminFromApproval in the users controller).
// Returns ErrDuplicate if the email is already taken.
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
	if len(req.Password) < MinPasswordLength {
		return Admin{}, fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	req.Role = normalizeAdminRole(req.Role)

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return Admin{}, fmt.Errorf("failed to hash password")
	}

	username := req.Email

	// A super admin chose this password directly, not the new admin
	// themselves — force a change on first login so it isn't a password
	// someone else picked and now knows, mirroring CreateWithGeneratedPassword.
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

// CreateWithGeneratedPassword creates a new admin account with a securely
// generated random temporary password and marks it as requiring a password
// change on first login. Used when a pending self-registration is promoted
// to an admin role instead of approved as a normal booking user.
//
// Returns the created admin and the plaintext temporary password — the
// caller is responsible for emailing it and must never log or persist it.
// Returns ErrDuplicate if the username or email is already taken.
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

// Update changes an admin's name, role, and/or email.
// currentAdminID and currentRole prevent an admin from demoting their own account.
// Setting email to an empty string removes the stored email (sets it to NULL).
// Returns ErrNotFound if the target admin does not exist, ErrDuplicate on email conflict.
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

// ResetPassword sets a new bcrypt-hashed password for any admin, chosen by
// someone other than the account's owner — so, like CreateWithGeneratedPassword,
// it forces a change on next login rather than letting a password someone
// else now knows remain in use indefinitely.
// Returns ErrNotFound if the admin does not exist.
func (m *AdminModel) ResetPassword(id int64, newPassword string) error {
	if len(newPassword) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
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

// ChangeOwnPassword verifies the admin's current password then stores the new
// one, and clears must_reset_password so a temporary password is only ever
// usable once.
// Returns ErrUnauthorized if the current password is wrong.
func (m *AdminModel) ChangeOwnPassword(id int64, currentPw, newPw string) error {
	if len(newPw) < MinPasswordLength {
		return fmt.Errorf("new password must be at least %d characters", MinPasswordLength)
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
	_, err = m.DB.Exec(`UPDATE admins SET password = $1, must_reset_password = FALSE WHERE id = $2`, string(newHash), id)
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
func (m *AdminModel) VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
