package controllers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"bookroom-management-system/models"
)

// ListUsers returns all pre-registered users. Admin only.
func (c *Controller) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := c.DB.Query(`SELECT id, name, email FROM users ORDER BY name ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	users := []models.User{}
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, users)
}

// ValidateUser checks whether an email belongs to a registered user.
// The public booking form calls this before allowing someone to book.
func (c *Controller) ValidateUser(w http.ResponseWriter, r *http.Request) {
	email := normalizeEmail(r.URL.Query().Get("email"))

	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	var u models.User
	err := c.DB.QueryRow(`
		SELECT id, name, email
		FROM users
		WHERE LOWER(TRIM(email)) = $1
	`, email).Scan(&u.ID, &u.Name, &u.Email)

	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user is not pre-registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"id":    u.ID,
		"name":  u.Name,
		"email": u.Email,
	})
}

// CreateUser pre-registers a new user. Admin only.
func (c *Controller) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req models.UserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name  = strings.TrimSpace(req.Name)
	req.Email = normalizeEmail(req.Email)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "user name is required")
		return
	}
	if !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	var u models.User
	err := c.DB.QueryRow(`
		INSERT INTO users(name, email)
		VALUES($1, $2)
		RETURNING id, name, email
	`, req.Name, req.Email).Scan(&u.ID, &u.Name, &u.Email)

	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusBadRequest, "a user with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
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

	req.Name  = strings.TrimSpace(req.Name)
	req.Email = normalizeEmail(req.Email)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "user name is required")
		return
	}
	if !isValidEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	var u models.User
	err := c.DB.QueryRow(`
		UPDATE users
		SET name = $1, email = $2
		WHERE id = $3
		RETURNING id, name, email
	`, req.Name, req.Email, id).Scan(&u.ID, &u.Name, &u.Email)

	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusBadRequest, "a user with this email already exists")
			return
		}
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

	result, err := c.DB.Exec(`DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
