// Cloudflare Turnstile integration for the self-registration and admin
// forgot-password flows. The token collected here is sent to the Go backend
// which re-verifies it server-side — the frontend can't bypass the check.
//
// Call initTurnstile(containerId) when the container first becomes visible.
// It is idempotent: subsequent calls for the same container are no-ops.
//
// If TURNSTILE_SITE_KEY is not set on the server, /api/config returns an empty
// key, the containers stay empty, and getTurnstileToken returns ''. The backend
// VerifyTurnstile also fails open when TURNSTILE_SECRET_KEY is unset, so both
// sides degrade gracefully together in environments without Turnstile configured.

// Cached promise so /api/config is only fetched once per page load.
let turnstileSiteKeyPromise = null;

// Maps containerId → widgetId once rendered, or null while the render is pending.
const turnstileWidgets = {};

// Maximum number of 100 ms polls to wait for the Turnstile script to load
// before giving up. Prevents an infinite loop if the CDN script fails to load.
const TURNSTILE_MAX_RETRIES = 50; // 5 seconds total

function getTurnstileSiteKey() {
  if (!turnstileSiteKeyPromise) {
    turnstileSiteKeyPromise = fetch('/api/config')
      .then(function (res) { return res.json(); })
      .then(function (config) { return config.turnstileSiteKey || ''; })
      .catch(function () { return ''; });
  }
  return turnstileSiteKeyPromise;
}

async function initTurnstile(containerId) {
  if (containerId in turnstileWidgets) return;
  turnstileWidgets[containerId] = null; // mark in-progress to block duplicate calls

  const siteKey = await getTurnstileSiteKey();
  if (!siteKey) return;

  var retries = 0;
  (function renderWhenReady() {
    if (typeof turnstile === 'undefined') {
      if (++retries > TURNSTILE_MAX_RETRIES) return; // script failed to load
      setTimeout(renderWhenReady, 100);
      return;
    }
    turnstileWidgets[containerId] = turnstile.render('#' + containerId, { sitekey: siteKey });
  })();
}

// Returns the solved CAPTCHA token, or '' if the widget hasn't loaded yet.
function getTurnstileToken(containerId) {
  const widgetId = turnstileWidgets[containerId];
  if (!widgetId || typeof turnstile === 'undefined') return '';
  return turnstile.getResponse(widgetId) || '';
}

// Resets the widget after a submission so the next attempt requires a fresh solve.
// Turnstile tokens are single-use; skipping this would cause the backend to
// reject a reused token on the very next attempt.
function resetTurnstile(containerId) {
  const widgetId = turnstileWidgets[containerId];
  if (widgetId && typeof turnstile !== 'undefined') {
    turnstile.reset(widgetId);
  }
}
