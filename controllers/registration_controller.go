package controllers

import (
	"errors"
	"net/http"
	"strings"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
)

// CheckEmail looks up whether an email belongs to a registered user.
// The public access gate calls this first; if the email is registered the
// caller is sent straight to the calendar, otherwise the registration step is shown.
func (c *Controller) CheckEmail(w http.ResponseWriter, r *http.Request) {
	var req models.CheckEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	user, err := c.Users.GetByEmail(email)
	if errors.Is(err, models.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"exists": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"exists": true,
		"name":   user.Name,
		"email":  user.Email,
	})
}

// SendRegistrationOTP emails a one-time code to verify a new user's email
// address before they are added to the registered users table.
func (c *Controller) SendRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var req models.SendOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name  := strings.TrimSpace(req.Name)
	email := utils.NormalizeEmail(req.Email)

	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	// Don't issue a code for an email that is already registered — the
	// caller should use the access page directly instead.
	if _, err := c.Users.GetByEmail(email); err == nil {
		writeError(w, http.StatusBadRequest, "this email is already registered — please use the access page to continue")
		return
	}

	code := c.OTPs.Create(email)

	if err := utils.SendOTPEmail(email, name, code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send verification code, please try again")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "A 6-digit verification code has been sent to your email."})
}

// VerifyRegistrationOTP checks the submitted code and, on success, creates
// the user so they can immediately access the booking calendar.
func (c *Controller) VerifyRegistrationOTP(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name  := strings.TrimSpace(req.Name)
	email := utils.NormalizeEmail(req.Email)
	code  := strings.TrimSpace(req.OTP)

	if name == "" || email == "" || code == "" {
		writeError(w, http.StatusBadRequest, "name, email, and verification code are required")
		return
	}

	if !c.OTPs.Verify(email, code) {
		writeError(w, http.StatusBadRequest, "invalid or expired verification code")
		return
	}

	user, err := c.Users.Create(models.UserRequest{Name: name, Email: email})
	if errors.Is(err, models.ErrDuplicate) {
		// Someone else registered this exact email between send-otp and verify-otp.
		existing, getErr := c.Users.GetByEmail(email)
		if getErr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"name": existing.Name, "email": existing.Email})
			return
		}
		writeError(w, http.StatusBadRequest, "this email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name, "email": user.Email})
}
