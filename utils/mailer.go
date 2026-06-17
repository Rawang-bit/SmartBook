package utils

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
)

// SendPasswordResetEmail sends a password-reset link to an admin's email address.
//
// SMTP is configured via environment variables:
//
//	SMTP_HOST     — mail server hostname (e.g. smtp.gmail.com)
//	SMTP_PORT     — port, defaults to 587 (STARTTLS); use 465 for implicit TLS
//	SMTP_USERNAME — authentication username (usually the from-address)
//	SMTP_PASSWORD — authentication password or app-specific password
//	SMTP_FROM     — envelope From address; falls back to SMTP_USERNAME if omitted
//	APP_URL       — base URL used to build the reset link (e.g. https://smartbook.company.bt)
//
// If SMTP_HOST is not set, the function logs the reset URL to the server console
// instead of sending an email. This lets the feature work during local development
// without any mail server configuration.
func SendPasswordResetEmail(toEmail, toName, resetURL string) error {
	host     := os.Getenv("SMTP_HOST")
	port     := os.Getenv("SMTP_PORT")
	username := os.Getenv("SMTP_USERNAME")
	password := os.Getenv("SMTP_PASSWORD")
	from     := os.Getenv("SMTP_FROM")

	if port == "" {
		port = "587"
	}
	if from == "" {
		from = username
	}

	if host == "" {
		// Development fallback: print the link so the developer can test the flow
		// without a live mail server.
		log.Printf(
			"[PASSWORD RESET] No SMTP configured. Reset link for %s (%s):\n  %s",
			toName, toEmail, resetURL,
		)
		return nil
	}

	subject := "SmartBook — Password Reset Request"
	body := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"You (or someone claiming to be you) requested a password reset for your SmartBook admin account.\r\n\r\n"+
			"Click the link below to set a new password. The link is valid for 15 minutes and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you did not request this reset, you can safely ignore this email — your password has not changed.\r\n\r\n"+
			"— SmartBook",
		toName, resetURL,
	)

	msg := []byte(
		"From: SmartBook <" + from + ">\r\n" +
			"To: " + toEmail + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body,
	)

	auth := smtp.PlainAuth("", username, password, host)
	if err := smtp.SendMail(host+":"+port, auth, from, []string{toEmail}, msg); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return nil
}
