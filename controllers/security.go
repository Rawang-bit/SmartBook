package controllers

import (
	"net/http"
	"os"
	"strings"
	"time"
)

// isProduction reports whether APP_ENV=production is set.
func isProduction() bool {
	return os.Getenv("APP_ENV") == "production"
}

// sessionCookieName returns the cookie name; __Host- prefix in production prevents subdomain injection.
func sessionCookieName() string {
	if isProduction() {
		return "__Host-smartbook_session"
	}
	return "smartbook_session"
}

// deviceCookieName returns the device-trust cookie name with the same __Host- hardening as sessionCookieName.
func deviceCookieName() string {
	if isProduction() {
		return "__Host-smartbook_device"
	}
	return "smartbook_device"
}

// DeviceTrustDuration is how long a "remembered" device skips OTP on the public access gate.
const DeviceTrustDuration = 30 * 24 * time.Hour

// setCookie writes a cookie with HttpOnly, SameSite=Strict, and Secure (production only).
func setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isProduction(),
	})
}

// setSessionCookie writes the admin session cookie (value="" + maxAge=-1 to delete).
func setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	setCookie(w, sessionCookieName(), value, maxAge)
}

// setDeviceCookie writes the public device-trust cookie (value="" + maxAge=-1 to clear).
func setDeviceCookie(w http.ResponseWriter, value string, maxAge int) {
	setCookie(w, deviceCookieName(), value, maxAge)
}

// SecureHeaders wraps every response with hardened security headers; must be outermost middleware.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// CSP: unsafe-inline needed by Tailwind; both challenge/challenges.cloudflare.com required — widget fails silently without both.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://challenges.cloudflare.com https://challenge.cloudflare.com; "+
				"frame-src https://challenges.cloudflare.com https://challenge.cloudflare.com; "+
				"connect-src 'self' https://challenges.cloudflare.com https://challenge.cloudflare.com; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"img-src 'self' data: https://challenges.cloudflare.com; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self';",
		)

		// HSTS only in production — meaningless over plain HTTP and ignored by browsers there.
		if isProduction() {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

// HTTPSRedirect permanently redirects HTTP to HTTPS in production, honouring X-Forwarded-Proto from reverse proxies.
func HTTPSRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isProduction() {
			next.ServeHTTP(w, r)
			return
		}

		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			if r.TLS != nil {
				proto = "https"
			} else {
				proto = "http"
			}
		}

		if strings.ToLower(proto) != "https" {
			// 308 preserves the HTTP method (important for POST/PUT).
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}
