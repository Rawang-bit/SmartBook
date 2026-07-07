// Cloudflare Turnstile helpers for registration and forgot-password flows.
// Tokens are verified server-side; if TURNSTILE_SITE_KEY is unset, both sides
// fail open so the app works without Turnstile configured.
// initTurnstile(containerId) is idempotent — safe to call multiple times.

// Cached promise so /api/config is only fetched once per page load.
let turnstileSiteKeyPromise = null;

// Maps containerId → widgetId once rendered, or null while the render is pending.
const turnstileWidgets = {};

// Max 100 ms polls before giving up waiting for the Turnstile CDN script.
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
    turnstileWidgets[containerId] = turnstile.render('#' + containerId, {
      sitekey: siteKey,
      theme: 'light', // always visible on the white card background
    });
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
