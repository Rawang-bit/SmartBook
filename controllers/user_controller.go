package controllers

import (
	"errors"
	"net/http"

	"bookroom-management-system/models"
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
