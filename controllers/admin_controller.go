package controllers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
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

	syncNormalUserAccess(c, admin.Username, admin.Role)
	c.audit(r, "admin_created", "admin", admin.Username, admin.ID, "role: "+models.RoleLabel(admin.Role))

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

	before, _, err := c.Admins.GetByID(id)
	if err != nil {
		log.Printf("[ADMIN] GetByID(%d) before update failed — audit will show empty before-role: %v", id, err)
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

	if before.Role != admin.Role {
		syncNormalUserAccess(c, admin.Username, admin.Role)

		// Sessions are a snapshot of role taken at login — invalidate now so the old
		// role doesn't stay effective for up to SessionDuration. AdminModel.Update
		// prevents self-role-change, so this only ever signs out someone else.
		c.Sessions.DeleteByAdminID(id)

		c.audit(r, "admin_role_changed", "admin", admin.Username, admin.ID, "role: "+models.RoleLabel(before.Role)+" → "+models.RoleLabel(admin.Role))
	} else {
		c.audit(r, "admin_updated", "admin", admin.Username, admin.ID, "name/email updated")
	}

	writeJSON(w, http.StatusOK, admin)
}

// syncNormalUserAccess enforces Super Admin exclusivity: removes the Normal User row when a super_admin
// is created or promoted. Does NOT auto-grant Normal User access for general_admin — that must be done explicitly.
func syncNormalUserAccess(c *Controller, username, role string) {
	if role != "super_admin" || !utils.IsValidEmail(username) {
		return
	}
	if err := c.Users.RemoveNormalUserAccess(username); err != nil {
		log.Printf("[ADMIN SYNC] failed to remove Normal User access for super_admin %s: %v", username, err)
	}
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

	target, _, getErr := c.Admins.GetByID(id)
	if getErr != nil {
		log.Printf("[ADMIN] GetByID(%d) before password reset failed — audit label will be empty: %v", id, getErr)
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

	// Invalidate existing sessions so the old password can't keep working until expiry.
	c.Sessions.DeleteByAdminID(id)

	c.audit(r, "admin_password_reset", "admin", target.Username, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "password reset"})
}

// ChangeOwnPassword lets a logged-in admin change their own password; requires current password as proof.
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

	// Clear the forced-reset flag on the live session immediately so the admin
	// doesn't need to re-login after replacing a temporary password.
	if cookie, err := r.Cookie(sessionCookieName()); err == nil {
		c.Sessions.ClearMustResetPassword(cookie.Value)
	}

	c.audit(r, "admin_password_self_changed", "admin", sess.Username, sess.AdminID, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "password changed"})
}

// ToggleAdminStatus handles /revoke and /restore (super_admin only) and /unlock (any admin role).
func (c *Controller) ToggleAdminStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admins/")

	isUnlock := strings.HasSuffix(path, "/unlock")
	var newStatus string
	switch {
	case strings.HasSuffix(path, "/revoke"):
		newStatus = "revoked"
	case strings.HasSuffix(path, "/restore"):
		newStatus = "active"
	case isUnlock:
		// handled below
	default:
		writeError(w, http.StatusBadRequest, "use /revoke, /restore, or /unlock")
		return
	}

	// revoke and restore are super_admin-only; unlock is open to any authenticated admin.
	if !isUnlock {
		sess, _ := c.getSession(r)
		if sess.Role != "super_admin" {
			writeError(w, http.StatusForbidden, forbiddenModuleMsg)
			return
		}
	}

	id, ok := idFromPath(w, r, "/api/admins/")
	if !ok {
		return
	}

	target, _, getErr := c.Admins.GetByID(id)
	if getErr != nil {
		log.Printf("[ADMIN] GetByID(%d) before status change failed — audit label will be empty: %v", id, getErr)
	}

	if isUnlock {
		if err := c.Admins.SetStatus(id, "active"); err != nil {
			if errors.Is(err, models.ErrNotFound) {
				writeError(w, http.StatusNotFound, "admin not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to unlock admin")
			return
		}
		c.LoginAttempts.Reset(target.Username)
		c.audit(r, "admin_unlocked", "admin", target.Username, id, "login lock cleared by admin")
		writeJSON(w, http.StatusOK, map[string]string{"status": "unlocked"})
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

	// Cut off access immediately — status is only checked at login otherwise,
	// so a revoked admin's session would keep working until it naturally expires.
	if newStatus == "revoked" {
		c.Sessions.DeleteByAdminID(id)
	}

	action := map[string]string{
		"active":  "admin_activated",
		"revoked": "admin_revoked",
	}[newStatus]
	c.audit(r, action, "admin", target.Username, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

// ListLockedAdmins returns all currently locked admin accounts. Accessible to both admin roles.
func (c *Controller) ListLockedAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := c.Admins.ListLocked()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch locked admins")
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

// DeleteAdmin removes an admin account (super_admin only); an admin cannot delete their own account.
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

	target, _, getErr := c.Admins.GetByID(id)
	if getErr != nil {
		log.Printf("[ADMIN] GetByID(%d) before delete failed — audit label will be empty: %v", id, getErr)
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

	// Remove any linked Normal User row so it doesn't linger as an orphaned grant
	// after the admin account is gone (see syncNormalUserAccess). No-op for super_admin.
	if err := c.Users.RemoveNormalUserAccess(target.Username); err != nil {
		log.Printf("[ADMIN DELETED] failed to clean up Normal User access for %s: %v", target.Username, err)
	}

	c.audit(r, "admin_deleted", "admin", target.Username, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
