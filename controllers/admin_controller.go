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

	syncNormalUserAccess(c, admin.Username, admin.Name, admin.Role)
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
		syncNormalUserAccess(c, admin.Username, admin.Name, admin.Role)

		// Sessions are a snapshot of role taken at login (see session/store.go),
		// so without this their current browser would keep working under the
		// old role for up to SessionDuration — same reasoning as resetting a
		// password or revoking access. AdminModel.Update blocks a self-role-
		// change, so this can only ever sign out someone other than the
		// caller.
		c.Sessions.DeleteByAdminID(id)

		c.audit(r, "admin_role_changed", "admin", admin.Username, admin.ID, "role: "+models.RoleLabel(before.Role)+" → "+models.RoleLabel(admin.Role))
	} else {
		c.audit(r, "admin_updated", "admin", admin.Username, admin.ID, "name/email updated")
	}

	writeJSON(w, http.StatusOK, admin)
}

// syncNormalUserAccess keeps an admin's linked Normal User row consistent
// with the multi-role rule: General Admin retains Normal User booking
// capability (the one allowed combination), Super Admin is exclusive and
// retains none. username doubles as email for every admin created through
// this app; a legacy non-email username (e.g. the original seed account) is
// left alone rather than risking a bad row in the users table.
func syncNormalUserAccess(c *Controller, username, name, role string) {
	if !utils.IsValidEmail(username) {
		return
	}
	var err error
	if role == "super_admin" {
		err = c.Users.RemoveNormalUserAccess(username)
	} else {
		err = c.Users.EnsureActiveForEmail(name, username)
	}
	if err != nil {
		log.Printf("[ADMIN SYNC] failed to sync Normal User access for %s (role=%s): %v", username, role, err)
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

	// Force re-login with the new password immediately, rather than letting
	// any session opened under the old password keep working until it
	// naturally expires.
	c.Sessions.DeleteByAdminID(id)

	c.audit(r, "admin_password_reset", "admin", target.Username, id, "")

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

	c.audit(r, "admin_password_self_changed", "admin", sess.Username, sess.AdminID, "")

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

	target, _, getErr := c.Admins.GetByID(id)
	if getErr != nil {
		log.Printf("[ADMIN] GetByID(%d) before status change failed — audit label will be empty: %v", id, getErr)
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

	// Cut off access immediately — without this, a revoked admin's existing
	// browser session keeps working until it naturally expires (up to
	// SessionDuration later), since status is otherwise only checked at login.
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

	// The admin account is gone entirely now, not just revoked — clean up
	// any Normal User row that existed only as a side effect of them being a
	// general_admin (see syncNormalUserAccess), so it doesn't linger as an
	// orphaned, no-longer-meaningful booking-access grant. A no-op if there
	// was never one (e.g. they were already a super_admin).
	if err := c.Users.RemoveNormalUserAccess(target.Username); err != nil {
		log.Printf("[ADMIN DELETED] failed to clean up Normal User access for %s: %v", target.Username, err)
	}

	c.audit(r, "admin_deleted", "admin", target.Username, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
