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

// SendPasswordResetEmail delivers a password-reset link via the Resend email API.
//
// Required environment variables:
//
//	RESEND_API_KEY  — API key from https://resend.com (free tier: 3,000 emails/month)
//	EMAIL_FROM      — Verified "From" address, e.g. "SmartBook <no-reply@yourdomain.com>"
//	                  During testing you may use "SmartBook <onboarding@resend.dev>"
//	APP_URL         — Base URL for reset links, e.g. https://smartbook.onrender.com
//
// If RESEND_API_KEY is not set, the reset URL is logged to the server console
// (development fallback — no email is sent).
func SendPasswordResetEmail(toEmail, toName, resetURL string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	from   := os.Getenv("EMAIL_FROM")

	if apiKey == "" {
		// Development fallback: print the link so the developer can test without
		// a live email API key.
		log.Printf(
			"[PASSWORD RESET] No RESEND_API_KEY set. Reset link for %s (%s):\n  %s",
			toName, toEmail, resetURL,
		)
		return nil
	}

	if from == "" {
		from = "SmartBook <onboarding@resend.dev>"
	}

	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"You (or someone claiming to be you) requested a password reset for your SmartBook admin account.\r\n\r\n"+
			"Click the link below to set a new password. The link is valid for 15 minutes and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you did not request this reset, you can safely ignore this email — your password has not changed.\r\n\r\n"+
			"— SmartBook",
		toName, resetURL,
	)

	payload, err := json.Marshal(resendPayload{
		From:    from,
		To:      []string{toEmail},
		Subject: "SmartBook — Password Reset Request",
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

// sendSimpleEmail is a shared helper for the short notification emails
// (approval / rejection) that just need a subject and a plain-text body.
// Falls back to logging when RESEND_API_KEY is not set.
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

// SendApprovalEmail notifies a self-registered user that an admin approved
// their access request. They can now return to the access page and book rooms.
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

// SendRejectionEmail notifies a self-registered user that an admin rejected
// their access request.
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

// SendBookingConfirmationEmail notifies a recipient that a room has been
// booked. toName may be empty (e.g. for participants, whose names are never
// collected) — the greeting falls back to a generic "Hello,".
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

// SendTemporaryAdminPasswordEmail notifies a newly promoted admin of their
// login credentials. The recipient must change this password immediately —
// the server enforces that regardless of whether they read this email.
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

// baseAppURL returns the application's configured public base URL,
// defaulting to localhost for development.
func baseAppURL() string {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}
	return appURL
}

// accessURL returns the base URL of the public access page, used in
// approval emails so the user has a direct link back to the app.
func accessURL() string {
	return baseAppURL() + "/index.html"
}

// loginURL returns the base URL of the admin login page, used in admin
// promotion emails so the recipient has a direct link to sign in.
func loginURL() string {
	return baseAppURL() + "/login.html"
}

// SendOTPEmail delivers a one-time verification code used during public
// self-registration on the booking access page.
// Falls back to logging the code to the server console when RESEND_API_KEY
// is not set, identical in spirit to SendPasswordResetEmail.
func SendOTPEmail(toEmail, toName, code string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	from   := os.Getenv("EMAIL_FROM")

	if apiKey == "" {
		log.Printf(
			"[REGISTRATION OTP] No RESEND_API_KEY set. Code for %s (%s): %s",
			toName, toEmail, code,
		)
		return nil
	}

	if from == "" {
		from = "SmartBook <onboarding@resend.dev>"
	}

	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook verification code is:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"Enter this code on the registration page to verify your email. It expires in 10 minutes and can only be used once.\r\n\r\n"+
			"If you did not request this, you can safely ignore this email.\r\n\r\n"+
			"— SmartBook",
		toName, code,
	)

	payload, err := json.Marshal(resendPayload{
		From:    from,
		To:      []string{toEmail},
		Subject: "SmartBook — Your Verification Code",
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
