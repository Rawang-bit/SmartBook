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
//
// The new entry is never approved instantly — the admin typing in someone
// else's email hasn't proven they actually own it, so it starts as
// "pending" and a confirmation link is emailed to that address. Only once
// the recipient clicks it does the account (or admin promotion, if role
// is not "normal_user") actually activate. Choosing a role other than
// normal_user is a privilege grant, so it requires a super_admin.
func (c *Controller) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req models.UserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "normal_user"
	}
	if role != "normal_user" {
		sess, ok := c.getSession(r)
		if !ok || sess.Role != "super_admin" {
			writeError(w, http.StatusForbidden, "only a super admin can add a user with this role")
			return
		}
	}
	req.Role = role

	u, token, err := c.Users.Create(req)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusBadRequest, "a user with this email already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if sendErr := utils.SendRegistrationConfirmationEmail(u.Email, u.Name, token); sendErr != nil {
		log.Printf("[USER INVITE] failed to email confirmation link to %s: %v", u.Email, sendErr)
	}

	writeJSON(w, http.StatusCreated, u)
}

// ConfirmRegistration activates an admin-added user once they click the
// confirmation link emailed to them. Public endpoint — the token itself,
// not a session cookie, is the proof that the request comes from the
// person who actually owns that email address.
//
// If the role chosen when the entry was created is "normal_user", this just
// marks the booking-user row approved. Any other role instead promotes the
// confirmation into a brand-new admin account — the same path used when a
// super_admin approves a self-registration with an admin role.
func (c *Controller) ConfirmRegistration(w http.ResponseWriter, r *http.Request) {
	var req models.ConfirmRegistrationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeError(w, http.StatusBadRequest, "confirmation token is required")
		return
	}

	user, err := c.Users.ConsumeConfirmToken(token)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "this confirmation link is invalid or has already been used")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if user.IntendedRole == "" || user.IntendedRole == "normal_user" {
		if _, err := c.Users.SetStatus(user.ID, "active"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to activate your account, please contact an admin")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "Your account is now active. You can book rooms from the access page.",
			"role":    "normal_user",
		})
		return
	}

	admin, tempPassword, err := c.createAdminFromApproval(user.Name, user.Email, user.IntendedRole)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if sendErr := utils.SendTemporaryAdminPasswordEmail(user.Email, user.Name, admin.Username, tempPassword); sendErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to email temp password to %s: %v", user.Email, sendErr)
	}

	if delErr := c.Users.Delete(user.ID); delErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to remove confirmed user row %d: %v", user.ID, delErr)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Your admin account is ready. Check your email for login credentials.",
		"role":    user.IntendedRole,
	})
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

// ToggleUserStatus approves, rejects, revokes, or restores a registered
// user. Path must end in /approve, /reject, /revoke, or /restore — e.g.
// POST /api/users/5/approve. Admin only (approving with an admin role
// additionally requires super_admin — see approveUser).
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
	case strings.HasSuffix(path, "/revoke"):
		c.revokeUser(w, r, id)
	case strings.HasSuffix(path, "/restore"):
		c.restoreUser(w, r, id)
	default:
		writeError(w, http.StatusBadRequest, "use /approve, /reject, /revoke, or /restore")
	}
}

// approveUser handles POST /api/users/{id}/approve.
//
// Approving with role "normal_user" (the default when the role is omitted)
// keeps the original behaviour: the pending row is marked "active" and the
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
		user, err := c.Users.SetStatus(id, "active")
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

		writeJSON(w, http.StatusOK, map[string]string{"status": "active", "role": "normal_user"})
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

	writeJSON(w, http.StatusOK, map[string]string{"status": "active", "role": role, "username": admin.Username})
}

// createAdminFromApproval creates the admin account using the applicant's
// email address as their login username, so admins promoted this way always
// log in with the same email they registered and were approved with.
func (c *Controller) createAdminFromApproval(name, email, role string) (models.Admin, string, error) {
	admin, password, err := c.Admins.CreateWithGeneratedPassword(email, name, role, email)
	if err != nil {
		if errors.Is(err, models.ErrDuplicate) {
			return models.Admin{}, "", fmt.Errorf("an admin with this email already exists")
		}
		return models.Admin{}, "", err
	}
	return admin, password, nil
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

// revokeUser handles POST /api/users/{id}/revoke. Pulls booking access from
// an already-active user without deleting their record — BookingModel.Save
// only allows status "active", so a revoked user is blocked from booking
// the moment this is set, with no other code changes needed. Any admin may
// revoke, mirroring the reject gate above.
func (c *Controller) revokeUser(w http.ResponseWriter, r *http.Request, id int64) {
	_, err := c.Users.SetStatus(id, "revoked")
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke access")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// restoreUser handles POST /api/users/{id}/restore. Reactivates a
// previously revoked user, restoring their booking access.
func (c *Controller) restoreUser(w http.ResponseWriter, r *http.Request, id int64) {
	_, err := c.Users.SetStatus(id, "active")
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore access")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
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
