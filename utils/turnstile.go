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

// VerifyTurnstile checks a Turnstile token server-side; fails open when either env key is unset (both required for enforcement).
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
