package controllers

import (
	"net/http"
	"os"
	"strings"
	"time"
)

// isProduction reports whether the application is running in production mode.
// Set APP_ENV=production in the environment to activate all production security controls.
func isProduction() bool {
	return os.Getenv("APP_ENV") == "production"
}

// sessionCookieName returns the session cookie name for the current environment.
//
// In production the __Host- prefix is used. The browser enforces three invariants
// on any cookie carrying this prefix:
//   - Secure flag must be present (HTTPS-only delivery)
//   - No Domain attribute (cookie is bound to the exact host, not subdomains)
//   - Path must be "/"
//
// Together these prevent subdomain-injection and cookie-hijacking attacks.
// In development the plain name is used so the app works over plain HTTP.
func sessionCookieName() string {
	if isProduction() {
		return "__Host-smartbook_session"
	}
	return "smartbook_session"
}

// deviceCookieName returns the trusted-device cookie name for the current
// environment, mirroring sessionCookieName's __Host- hardening in production.
// This cookie is public-facing (set from the access gate, not an admin
// session) but warrants the same protections, since it lets a recognized
// browser skip OTP verification.
func deviceCookieName() string {
	if isProduction() {
		return "__Host-smartbook_device"
	}
	return "smartbook_device"
}

// DeviceTrustDuration is how long a "remembered" device skips OTP
// verification on the public access gate before needing to re-verify.
const DeviceTrustDuration = 30 * 24 * time.Hour

// setCookie writes a cookie with the security attributes shared by both the
// admin session cookie and the public device-trust cookie: HttpOnly (no
// client-JS access), Secure in production, and SameSite=Strict.
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

// setSessionCookie writes the admin session cookie, sharing the same
// attributes between login (value = session ID, maxAge = session lifetime)
// and logout (value = "", maxAge = -1, which tells the browser to delete it
// immediately).
func setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	setCookie(w, sessionCookieName(), value, maxAge)
}

// setDeviceCookie writes the public device-trust cookie. value = "" with
// maxAge = -1 clears it (used when a user declines to be remembered after
// previously opting in).
func setDeviceCookie(w http.ResponseWriter, value string, maxAge int) {
	setCookie(w, deviceCookieName(), value, maxAge)
}

// SecureHeaders wraps every HTTP response with a hardened set of security headers.
// It must be applied as the outermost middleware so headers appear on every response,
// including error pages and redirects.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent MIME-type sniffing — forces the browser to honour the declared Content-Type.
		h.Set("X-Content-Type-Options", "nosniff")

		// Block framing from any origin — prevents clickjacking.
		// Duplicated by the CSP frame-ancestors directive for maximum compatibility.
		h.Set("X-Frame-Options", "DENY")

		// Enable the browser's built-in XSS filter for legacy browsers (IE, old Edge).
		h.Set("X-XSS-Protection", "1; mode=block")

		// Send the full URL as the referrer only to same-origin requests;
		// send only the origin (no path) to cross-origin HTTPS destinations.
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Disable browser features this application never needs.
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// Content Security Policy
		// script-src  — self + inline scripts (Tailwind needs unsafe-inline) + CDN hosts +
		//               Cloudflare Turnstile script host
		// frame-src   — Cloudflare Turnstile renders its challenge as a sandboxed iframe;
		//               without this the widget is blank in every browser
		// connect-src — Turnstile's JS calls Cloudflare to verify the solved challenge
		// style-src   — self + inline styles (Tailwind generates them at runtime)
		// font-src    — self only; every page uses the system Helvetica/Arial stack
		// img-src     — self + data URIs (base64 favicons / avatars)
		// frame-ancestors — block all framing of THIS page (aligns with X-Frame-Options: DENY)
		// base-uri    — prevent <base> tag hijacking
		// form-action — restrict form POST targets to the same origin
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://challenge.cloudflare.com https://challenges.cloudflare.com; "+
				"frame-src https://challenges.cloudflare.com; "+
				"connect-src 'self' https://challenges.cloudflare.com; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self';",
		)

		// HTTP Strict Transport Security — instruct browsers to only connect over HTTPS
		// for the next year, and apply the same rule to all subdomains.
		// Only set in production: the header is meaningless over plain HTTP and browsers
		// ignore it there, but it avoids confusing local-dev tooling.
		if isProduction() {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

// HTTPSRedirect issues a permanent redirect from HTTP to HTTPS for every incoming
// request when the application is running in production.
//
// It honours the X-Forwarded-Proto header injected by reverse proxies (nginx,
// AWS ALB, Cloudflare, etc.) so that the redirect logic works correctly even when
// the Go server itself receives plain TCP connections from the proxy.
func HTTPSRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isProduction() {
			next.ServeHTTP(w, r)
			return
		}

		// Detect the transport used by the original client.
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			if r.TLS != nil {
				proto = "https"
			} else {
				proto = "http"
			}
		}

		if strings.ToLower(proto) != "https" {
			// 308 Permanent Redirect preserves the HTTP method (important for POST/PUT).
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}
