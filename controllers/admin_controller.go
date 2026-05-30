package controllers

import (
	"database/sql"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"bookroom-management-system/models"
)

// ListAdmins returns all admin accounts. Super admin only.
func (c *Controller) ListAdmins(w http.ResponseWriter, r *http.Request) {
	rows, err := c.DB.Query(`
		SELECT id, username, name, role, status, TO_CHAR(created_at, 'YYYY-MM-DD')
		FROM admins
		ORDER BY created_at ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch admins")
		return
	}
	defer rows.Close()

	var admins []models.AdminDetail
	for rows.Next() {
		var a models.AdminDetail
		if err := rows.Scan(&a.ID, &a.Username, &a.Name, &a.Role, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		admins = append(admins, a)
	}
	if admins == nil {
		admins = []models.AdminDetail{}
	}
	writeJSON(w, http.StatusOK, admins)
}

// CreateAdmin adds a new admin account. Super admin only.
func (c *Controller) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req models.AdminRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Name = strings.TrimSpace(req.Name)
	req.Password = strings.TrimSpace(req.Password)
	req.Role = strings.TrimSpace(req.Role)

	if req.Username == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "username, password, and name are required")
		return
	}
	if len(req.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	if req.Role != "super_admin" && req.Role != "general_admin" {
		req.Role = "general_admin"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var admin models.Admin
	err = c.DB.QueryRow(`
		INSERT INTO admins(username, password, name, role)
		VALUES($1, $2, $3, $4)
		RETURNING id, username, name, role
	`, req.Username, string(hash), req.Name, req.Role).Scan(
		&admin.ID, &admin.Username, &admin.Name, &admin.Role,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create admin")
		return
	}

	writeJSON(w, http.StatusCreated, admin)
}

// UpdateAdmin changes an admin's name and/or role. Super admin only.
// An admin cannot change their own role (prevents accidental self-demotion).
func (c *Controller) UpdateAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	var req models.AdminRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Role = strings.TrimSpace(req.Role)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Role != "super_admin" && req.Role != "general_admin" {
		req.Role = "general_admin"
	}

	// Prevent an admin from changing their own role
	sess, _ := c.getSession(r)
	if sess.AdminID == id {
		req.Role = sess.Role
	}

	var admin models.Admin
	err := c.DB.QueryRow(`
		UPDATE admins SET name = $1, role = $2 WHERE id = $3
		RETURNING id, username, name, role
	`, req.Name, req.Role, id).Scan(&admin.ID, &admin.Username, &admin.Name, &admin.Role)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update admin")
		return
	}

	writeJSON(w, http.StatusOK, admin)
}

// ResetAdminPassword sets a new password for any admin account. Super admin only.
func (c *Controller) ResetAdminPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	var req struct {
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	result, err := c.DB.Exec(`UPDATE admins SET password = $1 WHERE id = $2`, string(hash), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset password")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password reset"})
}

// ChangeOwnPassword lets any logged-in admin change their own password.
// Requires the current password as verification.
func (c *Controller) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	sess, ok := c.getSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "session expired")
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(req.NewPassword) < 6 {
		writeError(w, http.StatusBadRequest, "new password must be at least 6 characters")
		return
	}

	var hashInDB string
	err := c.DB.QueryRow(`SELECT password FROM admins WHERE id = $1`, sess.AdminID).Scan(&hashInDB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch admin")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashInDB), []byte(req.CurrentPassword)); err != nil {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	_, err = c.DB.Exec(`UPDATE admins SET password = $1 WHERE id = $2`, string(newHash), sess.AdminID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password changed"})
}

// ToggleAdminStatus revokes or restores an admin's login access. Super admin only.
// Path must end in /revoke or /restore — e.g. POST /api/admins/5/revoke.
// A super admin cannot revoke their own access.
func (c *Controller) ToggleAdminStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admins/")

	var newStatus string
	switch {
	case strings.HasSuffix(path, "/revoke"):
		newStatus = "revoked"
	case strings.HasSuffix(path, "/restore"):
		newStatus = "active"
	default:
		writeError(w, http.StatusBadRequest, "use /revoke or /restore")
		return
	}

	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	sess, _ := c.getSession(r)
	if sess.AdminID == id && newStatus == "revoked" {
		writeError(w, http.StatusBadRequest, "you cannot revoke your own access")
		return
	}

	result, err := c.DB.Exec(`UPDATE admins SET status = $1 WHERE id = $2`, newStatus, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update admin status")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

// DeleteAdmin removes an admin account. Super admin only.
// An admin cannot delete their own account.
func (c *Controller) DeleteAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	sess, _ := c.getSession(r)
	if sess.AdminID == id {
		writeError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}

	result, err := c.DB.Exec(`DELETE FROM admins WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete admin")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
