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

// sessionCookieName returns the session cookie name, with the __Host- prefix in
// production (enforces Secure flag, no Domain attribute, path="/", preventing
// subdomain injection). Plain name in development for plain-HTTP compatibility.
func sessionCookieName() string {
	if isProduction() {
		return "__Host-smartbook_session"
	}
	return "smartbook_session"
}

// deviceCookieName mirrors sessionCookieName's __Host- hardening for the trusted-device
// cookie, which lets a recognized browser skip OTP verification.
func deviceCookieName() string {
	if isProduction() {
		return "__Host-smartbook_device"
	}
	return "smartbook_device"
}

// DeviceTrustDuration is how long a "remembered" device skips OTP on the public access gate.
const DeviceTrustDuration = 30 * 24 * time.Hour

// setCookie writes a cookie with shared security attributes: HttpOnly, Secure in
// production, and SameSite=Strict.
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

// SecureHeaders wraps every HTTP response with a hardened set of security headers.
// Must be the outermost middleware so headers appear on every response including errors.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// CSP notes:
		// script-src: unsafe-inline required by Tailwind; CDN hosts for Tailwind + Turnstile.
		// frame-src/connect-src: Turnstile renders its challenge as a sandboxed iframe and
		//   calls Cloudflare to verify the solve — both challenge.cloudflare.com and
		//   challenges.cloudflare.com must appear or the widget silently fails.
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

// HTTPSRedirect permanently redirects HTTP to HTTPS in production.
// Honours X-Forwarded-Proto from reverse proxies so the redirect works even when
// the Go server itself receives plain TCP connections from the proxy.
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
