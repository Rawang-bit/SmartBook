package controllers

import (
	"errors"
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
// request and emails them the outcome. Path must end in /approve or /reject —
// e.g. POST /api/users/5/approve. Admin only.
func (c *Controller) ToggleUserStatus(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/users/")

	var newStatus string
	switch {
	case strings.HasSuffix(path, "/approve"):
		newStatus = "approved"
	case strings.HasSuffix(path, "/reject"):
		newStatus = "rejected"
	default:
		writeError(w, http.StatusBadRequest, "use /approve or /reject")
		return
	}

	id, ok := idFromPath(w, r, "/api/users/")
	if !ok {
		return
	}

	user, err := c.Users.SetStatus(id, newStatus)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user status")
		return
	}

	var sendErr error
	if newStatus == "approved" {
		sendErr = utils.SendApprovalEmail(user.Email, user.Name)
	} else {
		sendErr = utils.SendRejectionEmail(user.Email, user.Name)
	}
	if sendErr != nil {
		log.Printf("[USER %s] failed to notify %s: %v", strings.ToUpper(newStatus), user.Email, sendErr)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
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
