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
