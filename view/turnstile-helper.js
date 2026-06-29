// Cloudflare Turnstile integration, shared by every public page that needs
// a CAPTCHA before triggering an email send (self-registration OTP, admin
// password reset). The token collected here proves nothing by itself — the
// Go backend re-verifies it server-side (utils.VerifyTurnstile) before
// acting on the request; this file only handles getting that token onto
// the page and into the request body.
//
// Rendering is lazy and idempotent — call initTurnstile(containerId) right
// before its container becomes visible (e.g. inside showStep()); it's a
// no-op on every call after the first for that container.
//
// If TURNSTILE_SITE_KEY isn't configured on the server, /api/config returns
// an empty key and the container is left empty — getTurnstileToken then
// always returns '', and the backend fails open in that same case, so the
// feature is transparently disabled end to end until it's configured.

let turnstileSiteKeyPromise = null;
const turnstileWidgets = {}; // containerId -> widgetId, or null while pending

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
  if (containerId in turnstileWidgets) return; // already rendered or in progress
  turnstileWidgets[containerId] = null;

  const siteKey = await getTurnstileSiteKey();
  if (!siteKey) return;

  (function renderWhenReady() {
    if (typeof turnstile === 'undefined') {
      setTimeout(renderWhenReady, 100);
      return;
    }
    turnstileWidgets[containerId] = turnstile.render('#' + containerId, { sitekey: siteKey });
  })();
}

function getTurnstileToken(containerId) {
  const widgetId = turnstileWidgets[containerId];
  if (!widgetId || typeof turnstile === 'undefined') return '';
  return turnstile.getResponse(widgetId) || '';
}

function resetTurnstile(containerId) {
  const widgetId = turnstileWidgets[containerId];
  if (widgetId && typeof turnstile !== 'undefined') {
    turnstile.reset(widgetId);
  }
}
