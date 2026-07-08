package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// resendPayload is the JSON body sent to the Resend API.
type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	Html    string   `json:"html,omitempty"`
}

// ── HTML layout helpers ───────────────────────────────────────────────────────

func esc(s string) string { return html.EscapeString(s) }

// wrapEmailHTML wraps email body content in the standard SmartBook email chrome.
func wrapEmailHTML(content string) string {
	return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1.0">` +
		`<title>SmartBook</title></head>` +
		`<body style="margin:0;padding:0;background-color:#f1f5f9;` +
		`font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;">` +
		`<table width="100%" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#f1f5f9;">` +
		`<tr><td align="center" style="padding:40px 16px;">` +
		`<table width="600" cellpadding="0" cellspacing="0" role="presentation" style="max-width:600px;width:100%;">` +
		`<tr><td style="background-color:#0f172a;border-radius:12px 12px 0 0;padding:28px 40px;text-align:center;">` +
		`<span style="font-size:20px;font-weight:700;color:#ffffff;letter-spacing:-0.3px;">SmartBook</span><br>` +
		`<span style="font-size:11px;color:#64748b;letter-spacing:1px;text-transform:uppercase;">Online Room Booking System</span>` +
		`</td></tr>` +
		`<tr><td style="background-color:#ffffff;padding:40px;">` +
		content +
		`</td></tr>` +
		`<tr><td style="background-color:#f8fafc;border-top:1px solid #e2e8f0;border-radius:0 0 12px 12px;` +
		`padding:20px 40px;text-align:center;">` +
		`<p style="margin:0;color:#94a3b8;font-size:12px;">This is an automated message — please do not reply.</p>` +
		`<p style="margin:6px 0 0;color:#cbd5e1;font-size:11px;">&copy; SmartBook &middot; Online Room Booking System</p>` +
		`</td></tr>` +
		`</table></td></tr></table></body></html>`
}

func p(text string) string {
	return `<p style="margin:0 0 16px;color:#334155;font-size:15px;line-height:1.65;">` + text + `</p>`
}

func pLast(text string) string {
	return `<p style="margin:0;color:#334155;font-size:15px;line-height:1.65;">` + text + `</p>`
}

func pMuted(text string) string {
	return `<p style="margin:16px 0 0;color:#94a3b8;font-size:13px;line-height:1.6;">` + text + `</p>`
}

func ctaButton(label, url string) string {
	return `<table cellpadding="0" cellspacing="0" role="presentation" style="margin:28px 0;">` +
		`<tr><td style="border-radius:8px;background-color:#2563eb;">` +
		`<a href="` + esc(url) + `" style="display:inline-block;padding:13px 32px;color:#ffffff;` +
		`font-size:15px;font-weight:600;text-decoration:none;border-radius:8px;letter-spacing:0.2px;">` +
		esc(label) + `</a></td></tr></table>`
}

func urlFallback(url string) string {
	return `<p style="margin:8px 0 0;color:#94a3b8;font-size:12px;line-height:1.5;">` +
		`If the button doesn&rsquo;t work, copy and paste this link:<br>` +
		`<a href="` + esc(url) + `" style="color:#2563eb;word-break:break-all;font-size:12px;">` +
		esc(url) + `</a></p>`
}

func divider() string {
	return `<hr style="border:none;border-top:1px solid #e2e8f0;margin:24px 0;">`
}

func otpDisplay(code string) string {
	return `<div style="margin:28px 0;text-align:center;">` +
		`<div style="display:inline-block;background-color:#f1f5f9;border:2px dashed #cbd5e1;` +
		`border-radius:10px;padding:18px 40px;">` +
		`<span style="font-size:40px;font-weight:800;letter-spacing:12px;color:#0f172a;` +
		`font-family:'Courier New',Courier,monospace;">` + esc(code) + `</span>` +
		`</div></div>`
}

func detailsTable(rows [][2]string) string {
	var b strings.Builder
	b.WriteString(`<table cellpadding="0" cellspacing="0" role="presentation" ` +
		`style="width:100%;margin:20px 0;border-radius:8px;overflow:hidden;border:1px solid #e2e8f0;">`)
	for i, row := range rows {
		bg := "#ffffff"
		if i%2 == 1 {
			bg = "#f8fafc"
		}
		b.WriteString(`<tr style="background-color:` + bg + `;">`)
		b.WriteString(`<td style="padding:10px 16px;color:#64748b;font-size:12px;font-weight:600;` +
			`text-transform:uppercase;letter-spacing:0.5px;width:35%;border-right:1px solid #e2e8f0;">` +
			esc(row[0]) + `</td>`)
		b.WriteString(`<td style="padding:10px 16px;color:#0f172a;font-size:14px;">` + esc(row[1]) + `</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	return b.String()
}

func credentialsBox(username, password string) string {
	return `<div style="margin:24px 0;background-color:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;padding:20px 24px;">` +
		`<p style="margin:0 0 12px;color:#64748b;font-size:11px;font-weight:700;` +
		`text-transform:uppercase;letter-spacing:0.8px;">Your Login Credentials</p>` +
		`<p style="margin:0 0 8px;color:#334155;font-size:14px;">` +
		`<span style="color:#64748b;">Username:</span> <strong style="color:#0f172a;">` + esc(username) + `</strong></p>` +
		`<p style="margin:0;color:#334155;font-size:14px;">` +
		`<span style="color:#64748b;">Temporary Password:</span> ` +
		`<code style="background-color:#e2e8f0;padding:2px 8px;border-radius:4px;` +
		`font-family:'Courier New',Courier,monospace;font-size:13px;color:#0f172a;">` + esc(password) + `</code></p>` +
		`</div>`
}

// ── Core send function ────────────────────────────────────────────────────────

// sendEmail posts to the Resend API with both plain-text and HTML bodies; logs when no API key is set.
func sendEmail(toEmail, subject, textBody, htmlBody, logTag string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("EMAIL_FROM")

	if apiKey == "" {
		log.Printf("[%s] No RESEND_API_KEY set. Would have sent to %s:\n%s", logTag, toEmail, textBody)
		return nil
	}

	if from == "" {
		from = "SmartBook <onboarding@resend.dev>"
	}

	payload, err := json.Marshal(resendPayload{
		From:    from,
		To:      []string{toEmail},
		Subject: subject,
		Text:    textBody,
		Html:    htmlBody,
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

// ── Email functions ───────────────────────────────────────────────────────────

// SendPasswordResetEmail delivers a password-reset link via the Resend API.
func SendPasswordResetEmail(toEmail, toName, resetURL string) error {
	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"You (or someone claiming to be you) requested a password reset for your SmartBook admin account.\r\n\r\n"+
			"Click the link below to set a new password. The link is valid for 15 minutes and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you did not request this reset, you can safely ignore this email — your password has not changed.\r\n\r\n"+
			"— SmartBook",
		toName, resetURL,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("You (or someone claiming to be you) requested a password reset for your SmartBook admin account.") +
			p("Click the button below to set a new password. The link is valid for <strong>15 minutes</strong> and can only be used once.") +
			ctaButton("Reset Password", resetURL) +
			urlFallback(resetURL) +
			divider() +
			pMuted("If you did not request this reset, you can safely ignore this email — your password has not changed."),
	)
	return sendEmail(toEmail, "SmartBook — Password Reset Request", textBody, htmlBody, "PASSWORD RESET")
}

// SendRegistrationConfirmationEmail emails an admin-added user a confirmation link to prove email ownership.
func SendRegistrationConfirmationEmail(toEmail, toName, token string) error {
	confirmURL := baseAppURL() + "/confirm-registration.html?token=" + token

	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"An administrator has added you to SmartBook.\r\n\r\n"+
			"Click the link below to confirm your email and activate your account. This link is valid for 7 days and can only be used once:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"If you weren't expecting this, you can safely ignore this email.\r\n\r\n"+
			"— SmartBook",
		toName, confirmURL,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("An administrator has added you to SmartBook.") +
			p("Click the button below to confirm your email and activate your account. This link is valid for <strong>7 days</strong> and can only be used once.") +
			ctaButton("Confirm My Account", confirmURL) +
			urlFallback(confirmURL) +
			divider() +
			pMuted("If you weren&rsquo;t expecting this invitation, you can safely ignore this email."),
	)
	return sendEmail(toEmail, "SmartBook — Confirm Your Account", textBody, htmlBody, "USER INVITE")
}

// SendApprovalEmail notifies a self-registered user that an admin approved their request.
func SendApprovalEmail(toEmail, toName string) error {
	url := accessURL()
	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Good news — your SmartBook access request has been approved.\r\n\r\n"+
			"You can now return to the booking page and enter your email to access the calendar:\r\n"+
			"  %s\r\n\r\n"+
			"— SmartBook",
		toName, url,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("Good news &mdash; your SmartBook access request has been <strong>approved</strong>.") +
			p("You can now access the booking calendar and reserve rooms for your meetings.") +
			ctaButton("Access Booking Calendar", url) +
			urlFallback(url),
	)
	return sendEmail(toEmail, "SmartBook — Access Approved", textBody, htmlBody, "USER APPROVED")
}

// SendRejectionEmail notifies a self-registered user that their request was rejected.
func SendRejectionEmail(toEmail, toName string) error {
	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook access request was not approved.\r\n\r\n"+
			"If you believe this is a mistake, please contact your administrator.\r\n\r\n"+
			"— SmartBook",
		toName,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("Your SmartBook access request was not approved.") +
			pLast("If you believe this is a mistake, please contact your administrator."),
	)
	return sendEmail(toEmail, "SmartBook — Access Request Declined", textBody, htmlBody, "USER REJECTED")
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

	textBody := fmt.Sprintf(
		"%s\r\n\r\n"+
			"This room, %s has been booked for \"%s\" on %s at %s - %s.\r\n"+
			"%s\r\n"+
			"— SmartBook",
		greeting, roomName, purpose, date, startTime, endTime, agendaBlock,
	)

	greetingHTML := "Hello,"
	if toName != "" {
		greetingHTML = "Hi <strong>" + esc(toName) + "</strong>,"
	}

	rows := [][2]string{
		{"Room", roomName},
		{"Purpose", purpose},
		{"Date", date},
		{"Time", startTime + " – " + endTime},
	}
	if agenda != "" {
		rows = append(rows, [2]string{"Agenda", agenda})
	}

	htmlBody := wrapEmailHTML(
		p(greetingHTML) +
			p("A room has been booked. Here are the details:") +
			detailsTable(rows) +
			pMuted("If you have any questions, please contact the booking administrator."),
	)

	return sendEmail(toEmail, fmt.Sprintf("SmartBook — Room Booked: %s", roomName), textBody, htmlBody, "BOOKING CONFIRMATION")
}

// SendTemporaryAdminPasswordEmail sends login credentials to a newly promoted admin (server enforces password change on first login).
func SendTemporaryAdminPasswordEmail(toEmail, toName, username, tempPassword string) error {
	url := loginURL()
	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook access request has been approved and you have been granted admin access.\r\n\r\n"+
			"Your login credentials:\r\n"+
			"  Username: %s\r\n"+
			"  Temporary Password: %s\r\n\r\n"+
			"Log in here:\r\n"+
			"  %s\r\n\r\n"+
			"You will be required to set your own password immediately after logging in.\r\n\r\n"+
			"— SmartBook",
		toName, username, tempPassword, url,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("Your SmartBook access request has been approved and you have been granted <strong>admin access</strong>.") +
			credentialsBox(username, tempPassword) +
			p("Use these credentials to log in. You will be required to set your own password immediately after logging in.") +
			ctaButton("Log In to SmartBook", url) +
			urlFallback(url),
	)
	return sendEmail(toEmail, "SmartBook — Your Admin Account Is Ready", textBody, htmlBody, "ADMIN TEMP PASSWORD")
}

// SendOTPEmail delivers a one-time verification code used during self-registration.
func SendOTPEmail(toEmail, toName, code string) error {
	textBody := fmt.Sprintf(
		"Hi %s,\r\n\r\n"+
			"Your SmartBook verification code is:\r\n\r\n"+
			"  %s\r\n\r\n"+
			"Enter this code on the registration page to verify your email. It expires in 10 minutes and can only be used once.\r\n\r\n"+
			"If you did not request this, you can safely ignore this email.\r\n\r\n"+
			"— SmartBook",
		toName, code,
	)
	htmlBody := wrapEmailHTML(
		p("Hi <strong>"+esc(toName)+"</strong>,") +
			p("Your SmartBook verification code is:") +
			otpDisplay(code) +
			p("Enter this code on the registration page to verify your email. It expires in <strong>10 minutes</strong> and can only be used once.") +
			divider() +
			pMuted("If you did not request this, you can safely ignore this email."),
	)
	return sendEmail(toEmail, "SmartBook — Your Verification Code", textBody, htmlBody, "REGISTRATION OTP")
}

// ── URL helpers ───────────────────────────────────────────────────────────────

func baseAppURL() string {
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:8080"
	}
	return appURL
}

func accessURL() string {
	return baseAppURL() + "/index.html"
}

func loginURL() string {
	return baseAppURL() + "/login.html"
}
