package controllers

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"bookroom-management-system/models"
	"bookroom-management-system/utils"
)

// CheckEmail looks up a user by email and checks the device cookie so recognized devices can skip OTP.
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

	// Only an active user on a device they previously opted into "remember" can skip OTP.
	// DeviceTrustDuration is enforced server-side here, not just via the cookie's own expiry.
	deviceVerified := false
	if user.Status == "active" {
		if cookie, cookieErr := r.Cookie(deviceCookieName()); cookieErr == nil {
			deviceVerified, _ = c.Users.DeviceTokenMatches(email, cookie.Value)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"exists":         true,
		"name":           user.Name,
		"email":          user.Email,
		"status":         user.Status,
		"deviceVerified": deviceVerified,
	})
}

// rememberDeviceIfRequested issues or clears the trusted-device cookie based on the user's opt-in choice.
func (c *Controller) rememberDeviceIfRequested(w http.ResponseWriter, userID int64, remember bool) error {
	if !remember {
		return c.Users.ClearDeviceToken(userID)
	}

	token, err := utils.GenerateSecureToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(DeviceTrustDuration)
	if err := c.Users.SetDeviceToken(userID, token, expiresAt); err != nil {
		return err
	}

	setDeviceCookie(w, token, int(DeviceTrustDuration.Seconds()))
	return nil
}

// SendRegistrationOTP sends an OTP to verify a new user's email before self-registration.
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
	if err := utils.VerifyTurnstile(req.CaptchaToken, clientIP(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Reject already-registered emails — the caller should use the access page instead.
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

// VerifyRegistrationOTP validates the code and creates a "pending" user row; an admin must approve before they can book.
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

	user, err := c.Users.Register(models.UserRequest{Name: name, Email: email, Phone: strings.TrimSpace(req.Phone)})
	if errors.Is(err, models.ErrDuplicate) {
		// Someone registered this email between send-otp and verify-otp — return the existing row.
		existing, getErr := c.Users.GetByEmail(email)
		if getErr == nil {
			writeJSON(w, http.StatusOK, map[string]any{"name": existing.Name, "email": existing.Email, "status": existing.Status})
			return
		}
		writeError(w, http.StatusBadRequest, "this email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := c.rememberDeviceIfRequested(w, user.ID, req.RememberDevice); err != nil {
		log.Printf("[REGISTRATION] failed to update device trust for %s: %v", email, err)
		// Don't fail the request — registration itself succeeded.
	}

	c.auditPublic(r, email, "user_self_registered", "user", email, user.ID, "")

	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name, "email": user.Email, "status": user.Status})
}

// SendAccessVerificationOTP sends an OTP to an active user on an unrecognized device.
func (c *Controller) SendAccessVerificationOTP(w http.ResponseWriter, r *http.Request) {
	var req models.CheckEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	if !utils.IsValidEmail(email) {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	if err := utils.VerifyTurnstile(req.CaptchaToken, clientIP(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := c.Users.GetByEmail(email)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "this email is not a registered user")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if user.Status != "active" {
		writeError(w, http.StatusBadRequest, "this account is not active — see the access page for details")
		return
	}

	code := c.OTPs.Create(email)

	if err := utils.SendOTPEmail(email, user.Name, code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send verification code, please try again")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "A 6-digit verification code has been sent to your email."})
}

// VerifyAccessOTP confirms OTP for an active user on an unrecognized device and optionally issues a trusted-device cookie.
func (c *Controller) VerifyAccessOTP(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyOTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := utils.NormalizeEmail(req.Email)
	code := strings.TrimSpace(req.OTP)

	if email == "" || code == "" {
		writeError(w, http.StatusBadRequest, "email and verification code are required")
		return
	}

	if !c.OTPs.Verify(email, code) {
		writeError(w, http.StatusBadRequest, "invalid or expired verification code")
		return
	}

	user, err := c.Users.GetByEmail(email)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "this email is not a registered user")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if user.Status != "active" {
		writeError(w, http.StatusBadRequest, "this account is not active — see the access page for details")
		return
	}

	if err := c.rememberDeviceIfRequested(w, user.ID, req.RememberDevice); err != nil {
		log.Printf("[DEVICE VERIFY] failed to update device trust for %s: %v", email, err)
		// Don't fail — the OTP check is what actually proves identity.
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name, "email": user.Email})
}
