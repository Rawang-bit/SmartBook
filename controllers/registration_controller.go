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

	// Only an active user can ever skip OTP — and only if this exact browser
	// previously opted in to "remember this device" and that trust hasn't
	// expired (DeviceTrustDuration is enforced server-side here, not just via
	// the cookie's own expiry).
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

// rememberDeviceIfRequested issues or revokes the trusted-device cookie for
// a user based on their explicit choice after OTP verification. When
// remember is true, a new token is generated, its hash (with a 30-day
// expiry) is stored against the user, and the cookie is set. When false, any
// previously-remembered device for this user is revoked, so an old opt-in
// doesn't linger as a silent bypass.
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
	if err := utils.VerifyTurnstile(req.CaptchaToken, clientIP(r)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
// the user with status "pending". They cannot book yet — an admin must
// approve the request first, and the user is notified by email either way.
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
		// Someone else registered this exact email between send-otp and verify-otp.
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
		// Don't fail the request over this — registration itself succeeded.
	}

	c.auditPublic(r, email, "user_self_registered", "user", email, user.ID, "")

	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name, "email": user.Email, "status": user.Status})
}

// SendAccessVerificationOTP emails a one-time code to an already-registered,
// active user accessing the public calendar from a device whose trusted
// cookie is missing or expired. Distinct from SendRegistrationOTP, which is
// only for emails that aren't registered yet.
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

// VerifyAccessOTP confirms the one-time code sent by SendAccessVerificationOTP
// for an already-active user on an unrecognized device. On success the user
// is granted access exactly as if their device had matched, and — only if
// they explicitly opt in — a trusted-device cookie is issued so this browser
// can skip OTP for up to 30 days.
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
		// Don't fail the request over this — the OTP check is what actually proves identity.
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": user.Name, "email": user.Email})
}
