package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// VerifyTurnstile checks a Cloudflare Turnstile token server-side using the secret key.
// Call this before taking any action the token is meant to gate (e.g. sending OTP or
// a password-reset email).
// Fails open when either TURNSTILE_SECRET_KEY or TURNSTILE_SITE_KEY is unset — both
// must be configured for enforcement. If only one is set the deployment is misconfigured
// (the widget can't render without the site key), so blocking the user serves no purpose.
func VerifyTurnstile(token, remoteIP string) error {
	secret := os.Getenv("TURNSTILE_SECRET_KEY")
	siteKey := os.Getenv("TURNSTILE_SITE_KEY")

	if secret == "" || siteKey == "" {
		if secret != siteKey { // one is set, the other isn't
			log.Printf("[TURNSTILE] warning: only one of TURNSTILE_SECRET_KEY / TURNSTILE_SITE_KEY is configured — skipping verification until both are set")
		}
		return nil
	}
	if token == "" {
		return fmt.Errorf("captcha verification is required")
	}

	form := url.Values{
		"secret":   {secret},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	resp, err := http.PostForm(turnstileVerifyURL, form)
	if err != nil {
		return fmt.Errorf("turnstile: send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("turnstile: read response: %w", err)
	}

	var result turnstileVerifyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("turnstile: parse response: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("captcha verification failed, please try again")
	}
	return nil
}
