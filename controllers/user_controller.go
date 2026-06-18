package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
)

// ListUsers returns all pre-registered users. Admin only.
func (c *Controller) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := c.Users.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// CreateUser pre-registers a new user. Admin only.
func (c *Controller) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req models.UserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	u, err := c.Users.Create(req)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusBadRequest, "a user with this email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, u)
}

// UpdateUser changes the name or email of a registered user. Admin only.
func (c *Controller) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/users/")
	if !ok {
		return
	}

	var req models.UserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	u, err := c.Users.Update(id, req)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusBadRequest, "a user with this email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, u)
}

// ToggleUserStatus approves or rejects a pending user's self-registration
// request. Path must end in /approve or /reject — e.g. POST /api/users/5/approve.
// Admin only (approving with an admin role additionally requires super_admin —
// see approveUser).
func (c *Controller) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/users/")

	id, ok := idFromPath(w, r, "/api/users/")
	if !ok {
		return
	}

	switch {
	case strings.HasSuffix(path, "/approve"):
		c.approveUser(w, r, id)
	case strings.HasSuffix(path, "/reject"):
		c.rejectUser(w, r, id)
	default:
		writeError(w, http.StatusBadRequest, "use /approve or /reject")
	}
}

// approveUser handles POST /api/users/{id}/approve.
//
// Approving with role "normal_user" (the default when the role is omitted)
// keeps the original behaviour: the pending row is marked "approved" and the
// applicant can book/cancel rooms from the public calendar.
//
// Approving with "general_admin" or "super_admin" instead promotes the
// applicant straight into a new admin account: a random temporary password
// is generated and emailed to them, must_reset_password is set so they are
// forced to replace it on first login, and the pending users-table row is
// removed since they are no longer a normal booking user. Because this
// grants admin privileges, only a super_admin may choose a role other than
// normal_user.
func (c *Controller) approveUser(w http.ResponseWriter, r *http.Request, id int64) {
	var req models.ApproveUserRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty body is fine — defaults to normal_user
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "normal_user"
	}

	if role == "normal_user" {
		user, err := c.Users.SetStatus(id, "approved")
		if errors.Is(err, models.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user status")
			return
		}

		if sendErr := utils.SendApprovalEmail(user.Email, user.Name); sendErr != nil {
			log.Printf("[USER APPROVED] failed to notify %s: %v", user.Email, sendErr)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "role": "normal_user"})
		return
	}

	if role != "general_admin" && role != "super_admin" {
		writeError(w, http.StatusBadRequest, "role must be normal_user, general_admin, or super_admin")
		return
	}

	// Granting an admin role is a privilege escalation — only a super admin may do it.
	sess, ok := c.getSession(r)
	if !ok || sess.Role != "super_admin" {
		writeError(w, http.StatusForbidden, "only a super admin can approve this role")
		return
	}

	pendingUser, err := c.Users.GetByID(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	admin, tempPassword, err := c.createAdminFromApproval(pendingUser.Name, pendingUser.Email, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if sendErr := utils.SendTemporaryAdminPasswordEmail(pendingUser.Email, pendingUser.Name, admin.Username, tempPassword); sendErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to email temp password to %s: %v", pendingUser.Email, sendErr)
	}

	// The registration request is now an admin account, not a booking user.
	if delErr := c.Users.Delete(id); delErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to remove promoted user row %d: %v", id, delErr)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "role": role, "username": admin.Username})
}

// createAdminFromApproval derives a username from email and creates the admin
// account, retrying with a numeric suffix if the derived username collides
// with an existing account.
func (c *Controller) createAdminFromApproval(name, email, role string) (models.Admin, string, error) {
	base := utils.DeriveUsernameFromEmail(email)

	const maxAttempts = 6
	for attempt := 0; attempt < maxAttempts; attempt++ {
		username := base
		if attempt > 0 {
			username = fmt.Sprintf("%s%d", base, attempt)
		}

		admin, password, err := c.Admins.CreateWithGeneratedPassword(username, name, role, email)
		if err == nil {
			return admin, password, nil
		}
		if !errors.Is(err, models.ErrDuplicate) {
			return models.Admin{}, "", err
		}
	}
	return models.Admin{}, "", fmt.Errorf("could not create admin account — username or email already in use")
}

// rejectUser handles POST /api/users/{id}/reject. Any admin may reject —
// rejection never grants privileges, so it doesn't need the super_admin gate.
func (c *Controller) rejectUser(w http.ResponseWriter, r *http.Request, id int64) {
	user, err := c.Users.SetStatus(id, "rejected")
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}

	if sendErr := utils.SendRejectionEmail(user.Email, user.Name); sendErr != nil {
		log.Printf("[USER REJECTED] failed to notify %s: %v", user.Email, sendErr)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// DeleteUser removes a pre-registered user. Admin only.
func (c *Controller) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/users/")
	if !ok {
		return
	}

	err := c.Users.Delete(id)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
