package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// resendPayload is the JSON body sent to the Resend API.
type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

// SendPasswordResetEmail delivers a password-reset link via the Resend API.
func SendPasswordResetEmail(toEmail, toName, resetURL string) error {
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"You (or someone claiming to be you) requested a password reset for your SmartBook admin account.\r\n\r\n"+
			"Click the link below to set a new password. The link is valid for 15 minutes and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you did not request this reset, you can safely ignore this email — your password has not changed.\r\n\r\n"+
			"— SmartBook",
		toName, resetURL,
	)
	return sendSimpleEmail(toEmail, "SmartBook — Password Reset Request", body, "PASSWORD RESET")
}

// sendSimpleEmail sends a notification via Resend; logs instead when RESEND_API_KEY is unset.
func sendSimpleEmail(toEmail, subject, body, logTag string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	from   := os.Getenv("EMAIL_FROM")

	if apiKey == "" {
		log.Printf("[%s] No RESEND_API_KEY set. Would have sent to %s:\n%s", logTag, toEmail, body)
		return nil
	}

	if from == "" {
		from = "SmartBook <onboarding@resend.dev>"
	}

	payload, err := json.Marshal(resendPayload{
		From:    from,
		To:      []string{toEmail},
		Subject: subject,
		Text:    body,
	})
	if err != nil {
		return fmt.Errorf("resend: marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendRegistrationConfirmationEmail emails an admin-added user a confirmation link to prove email ownership.
func SendRegistrationConfirmationEmail(toEmail, toName, token string) error {
	confirmURL := baseAppURL() + "/confirm-registration.html?token=" + token

	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"An administrator has added you to SmartBook.\r\n\r\n"+
			"Click the link below to confirm your email and activate your account. This link is valid for 7 days and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you weren't expecting this, you can safely ignore this email.\r\n\r\n"+
			"— SmartBook",
		toName, confirmURL,
	)

	return sendSimpleEmail(toEmail, "SmartBook — Confirm Your Account", body, "USER INVITE")
}

// SendApprovalEmail notifies a self-registered user that an admin approved their request.
func SendApprovalEmail(toEmail, toName string) error {
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Good news — your SmartBook access request has been approved.\r\n\r\n"+
			"You can now return to the booking page and enter your email to access the calendar:\r\n"+
			"  %s\r\n\r\n"+
			"— SmartBook",
		toName, accessURL(),
	)
	return sendSimpleEmail(toEmail, "SmartBook — Access Approved", body, "USER APPROVED")
}

// SendRejectionEmail notifies a self-registered user that their request was rejected.
func SendRejectionEmail(toEmail, toName string) error {
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook access request was not approved.\r\n\r\n"+
			"If you believe this is a mistake, please contact your administrator.\r\n\r\n"+
			"— SmartBook",
		toName,
	)
	return sendSimpleEmail(toEmail, "SmartBook — Access Request Declined", body, "USER REJECTED")
}

// SendBookingConfirmationEmail notifies a recipient of a room booking; toName may be empty for participants.
func SendBookingConfirmationEmail(toEmail, toName, roomName, date, startTime, endTime, purpose, agenda string) error {
	greeting := "Hello,"
	if toName != "" {
		greeting = fmt.Sprintf("Hi %s,", toName)
	}

	agendaBlock := ""
	if agenda != "" {
		agendaBlock = fmt.Sprintf("\r\nAgenda:\r\n  %s\r\n", agenda)
	}

	body := fmt.Sprintf(
		"%s\r\n\r\n"+
			"This room %s has been booked for \"%s\" on %s at %s - %s.\r\n"+
			"%s\r\n"+
			"— SmartBook",
		greeting, roomName, purpose, date, startTime, endTime, agendaBlock,
	)

	return sendSimpleEmail(toEmail, fmt.Sprintf("SmartBook — Room Booked: %s", roomName), body, "BOOKING CONFIRMATION")
}

// SendTemporaryAdminPasswordEmail sends login credentials to a newly promoted admin (server enforces password change on first login).
func SendTemporaryAdminPasswordEmail(toEmail, toName, username, tempPassword string) error {
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook access request has been approved and you have been granted admin access.\r\n\r\n"+
			"Your login credentials:\r\n"+
			"  Username: %s\r\n"+
			"  Temporary Password: %s\r\n\r\n"+
			"Log in here:\r\n"+
			"  %s\r\n\r\n"+
			"You will be required to set your own password immediately after logging in.\r\n\r\n"+
			"— SmartBook",
		toName, username, tempPassword, loginURL(),
	)
	return sendSimpleEmail(toEmail, "SmartBook — Your Admin Account Is Ready", body, "ADMIN TEMP PASSWORD")
}

// baseAppURL returns the configured public base URL, defaulting to localhost.
func baseAppURL() string {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}
	return appURL
}

// accessURL returns the public access page URL, used in approval emails.
func accessURL() string {
	return baseAppURL() + "/index.html"
}

// loginURL returns the admin login page URL, used in admin promotion emails.
func loginURL() string {
	return baseAppURL() + "/login.html"
}

// SendOTPEmail delivers a one-time verification code used during self-registration.
func SendOTPEmail(toEmail, toName, code string) error {
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook verification code is:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"Enter this code on the registration page to verify your email. It expires in 10 minutes and can only be used once.\r\n\r\n"+
			"If you did not request this, you can safely ignore this email.\r\n\r\n"+
			"— SmartBook",
		toName, code,
	)
	return sendSimpleEmail(toEmail, "SmartBook — Your Verification Code", body, "REGISTRATION OTP")
}
