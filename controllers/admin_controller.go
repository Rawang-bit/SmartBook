package controllers

import (
	"errors"
	"net/http"
	"strings"

	"bookroom-management-system/models"
)

// ListAdmins returns all admin accounts. Super admin only.
func (c *Controller) ListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := c.Admins.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch admins")
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

// CreateAdmin adds a new admin account. Super admin only.
func (c *Controller) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req models.AdminRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	admin, err := c.Admins.Create(req)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusConflict, "an admin with this email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, admin)
}

// UpdateAdmin changes an admin's name and/or role. Super admin only.
func (c *Controller) UpdateAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	var req models.AdminRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	sess, _ := c.getSession(r)
	admin, err := c.Admins.Update(id, req, sess.AdminID, sess.Role)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	err := c.Admins.ResetPassword(id, req.NewPassword)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	err := c.Admins.ChangeOwnPassword(sess.AdminID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, models.ErrUnauthorized) {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Lift the forced-reset block on the live session immediately so the admin
	// doesn't have to log out and back in after replacing a temporary password.
	if cookie, err := r.Cookie(sessionCookieName()); err == nil {
		c.Sessions.ClearMustResetPassword(cookie.Value)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password changed"})
}

// ToggleAdminStatus revokes or restores an admin's login access. Super admin only.
// Path must end in /revoke or /restore — e.g. POST /api/admins/5/revoke.
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

	err := c.Admins.SetStatus(id, newStatus)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update admin status")
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

	err := c.Admins.Delete(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "admin not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete admin")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
