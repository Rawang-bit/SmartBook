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

// canAssignRole reports whether an admin with sessionRole may assign target.
// Both roles may grant normal_user or general_admin; only super_admin may grant super_admin.
func canAssignRole(sessionRole, target string) bool {
	switch target {
	case "", "normal_user":
		return true
	case "general_admin":
		return sessionRole == "general_admin" || sessionRole == "super_admin"
	default: // super_admin
		return sessionRole == "super_admin"
	}
}

// CreateUser pre-registers a new user. Starts as "pending" — a confirmation link is
// emailed to the address to prove ownership before the account activates.
// Role records the intended promotion if not "normal_user", gated by canAssignRole.
func (c *Controller) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req models.UserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "normal_user"
	}
	sess, _ := c.getSession(r)
	if !canAssignRole(sess.Role, role) {
		writeError(w, http.StatusForbidden, "you are not allowed to assign this role")
		return
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

	c.audit(r, "user_created", "user", u.Email, u.ID, "intended role: "+models.RoleLabel(role))

	writeJSON(w, http.StatusCreated, u)
}

// ConfirmRegistration activates an admin-added user once they click the emailed link.
// The token proves email ownership. For "normal_user" it marks the row active; for
// admin roles it promotes the confirmation into a new admin account instead.
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
		c.auditPublic(r, user.Email, "user_confirmed_registration", "user", user.Email, user.ID, "")
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

	// Normal User + General Admin is the one allowed multi-role combination.
	// Super Admin is exclusive, so its Normal User row is deleted instead.
	if user.IntendedRole == "super_admin" {
		if delErr := c.Users.Delete(user.ID); delErr != nil {
			log.Printf("[ADMIN PROMOTED] failed to remove confirmed user row %d: %v", user.ID, delErr)
		}
	} else if _, statusErr := c.Users.SetStatus(user.ID, "active"); statusErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to activate Normal User access for row %d: %v", user.ID, statusErr)
	}

	c.auditPublic(r, user.Email, "user_confirmed_admin_promotion", "user", user.Email, user.ID, "role: "+models.RoleLabel(user.IntendedRole))

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

	requestedRole := strings.TrimSpace(req.Role)

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

	// Editing can also promote to admin — skip if they already hold that role
	// to avoid trying to create a duplicate admin account for the same email.
	if requestedRole == "general_admin" || requestedRole == "super_admin" {
		if _, _, _, existsErr := c.Admins.GetByUsername(u.Email); existsErr != nil {
			sess, ok := c.getSession(r)
			if !ok || !canAssignRole(sess.Role, requestedRole) {
				writeError(w, http.StatusForbidden, "you are not allowed to assign this role")
				return
			}
			admin, promoted := c.promoteUserToAdmin(w, id, u.Name, u.Email, requestedRole)
			if !promoted {
				return
			}
			c.audit(r, "user_promoted_to_admin", "user", u.Email, id, "role: Normal User → "+models.RoleLabel(requestedRole))
			writeJSON(w, http.StatusOK, map[string]string{"status": "active", "role": requestedRole, "username": admin.Username})
			return
		}
	}

	c.audit(r, "user_updated", "user", u.Email, u.ID, "")

	writeJSON(w, http.StatusOK, u)
}

// ToggleUserStatus dispatches /approve, /reject, /revoke, or /restore actions.
// Path must end with one of those suffixes, e.g. POST /api/users/5/approve.
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
// "normal_user" marks the row active; "general_admin" or "super_admin" promotes the
// applicant into a new admin account with a generated temporary password.
// Which roles the caller may assign is gated by canAssignRole.
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

		c.audit(r, "user_approved", "user", user.Email, user.ID, "approved with role: Normal User")

		writeJSON(w, http.StatusOK, map[string]string{"status": "active", "role": "normal_user"})
		return
	}

	if role != "general_admin" && role != "super_admin" {
		writeError(w, http.StatusBadRequest, "role must be normal_user, general_admin, or super_admin")
		return
	}

	sess, ok := c.getSession(r)
	if !ok || !canAssignRole(sess.Role, role) {
		writeError(w, http.StatusForbidden, "you are not allowed to approve this role")
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

	admin, promoted := c.promoteUserToAdmin(w, id, pendingUser.Name, pendingUser.Email, role)
	if !promoted {
		return
	}

	c.audit(r, "user_approved", "user", pendingUser.Email, id, "approved with role: "+models.RoleLabel(role))

	writeJSON(w, http.StatusOK, map[string]string{"status": "active", "role": role, "username": admin.Username})
}

// createAdminFromApproval creates an admin account using the applicant's email as their
// login username, so promoted admins always log in with the same email they registered with.
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

// promoteUserToAdmin creates a new admin account, emails the temp password, and applies
// multi-role sync: general_admin keeps their Normal User row; super_admin loses it.
// Writes its own error response and returns ok=false on failure.
func (c *Controller) promoteUserToAdmin(w http.ResponseWriter, userID int64, name, email, role string) (admin models.Admin, ok bool) {
	admin, tempPassword, err := c.createAdminFromApproval(name, email, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return models.Admin{}, false
	}

	if sendErr := utils.SendTemporaryAdminPasswordEmail(email, name, admin.Username, tempPassword); sendErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to email temp password to %s: %v", email, sendErr)
	}

	if role == "super_admin" {
		if delErr := c.Users.Delete(userID); delErr != nil {
			log.Printf("[ADMIN PROMOTED] failed to remove promoted user row %d: %v", userID, delErr)
		}
	} else if _, statusErr := c.Users.SetStatus(userID, "active"); statusErr != nil {
		log.Printf("[ADMIN PROMOTED] failed to activate Normal User access for row %d: %v", userID, statusErr)
	}

	return admin, true
}

// rejectUser handles POST /api/users/{id}/reject.
// Any admin may reject — it never grants privileges. Optional {"reason": "..."} body.
func (c *Controller) rejectUser(w http.ResponseWriter, r *http.Request, id int64) {
	var req models.RejectUserRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty body is fine — reason is optional
	}

	user, err := c.Users.Reject(id, req.Reason)
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

	c.audit(r, "user_rejected", "user", user.Email, user.ID, req.Reason)

	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// revokeUser handles POST /api/users/{id}/revoke. Sets status to "revoked" which
// blocks booking immediately — BookingModel.Save only allows status "active".
func (c *Controller) revokeUser(w http.ResponseWriter, r *http.Request, id int64) {
	user, err := c.Users.SetStatus(id, "revoked")
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke access")
		return
	}

	c.audit(r, "user_revoked", "user", user.Email, user.ID, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// restoreUser handles POST /api/users/{id}/restore. Reactivates a previously revoked user.
func (c *Controller) restoreUser(w http.ResponseWriter, r *http.Request, id int64) {
	user, err := c.Users.SetStatus(id, "active")
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to restore access")
		return
	}

	c.audit(r, "user_restored", "user", user.Email, user.ID, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// DeleteUser removes a pre-registered user. Admin only.
func (c *Controller) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/users/")
	if !ok {
		return
	}

	target, getErr := c.Users.GetByID(id)
	if getErr != nil {
		log.Printf("[USER] GetByID(%d) before delete failed — audit label will be empty: %v", id, getErr)
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

	c.audit(r, "user_deleted", "user", target.Email, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
