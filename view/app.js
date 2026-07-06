/*
  SmartBook Admin Frontend Helpers
  ------------------------------------------------------------------
  Shared utilities used across every admin page:
  - api(): fetch wrapper that sends the session cookie and handles 401 redirects
  - auth helpers (isSuperAdmin, requireAuth, logout)
  - time / date formatting utilities
  - API call wrappers for bookings, rooms, users, admins, audit

  Keep page-specific rendering logic inside each HTML page's own <script>.
*/

const API_BASE = '/api';

// api() is the single fetch wrapper for all admin API calls. The session cookie
// is sent automatically (same-origin) — no manual token is needed.
// A 401 on any endpoint other than /auth/login means the session expired on the
// server (it's re-validated on every request), so we redirect to login rather
// than leaving the admin staring at a broken page.
async function api(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };

  const res = await fetch(API_BASE + path, {
    headers,
    credentials: 'same-origin',
    ...options,
  });

  let data = null;
  try { data = await res.json(); } catch (_) {}

  if (res.status === 401 && path !== '/auth/login') {
    forceLogout();
  }

  if (!res.ok) throw new Error((data && data.error) || 'Request failed');
  return data;
}

// Returns true if the admin appears to be logged in.
// Note: this localStorage flag is only for quick UI checks (e.g. redirects).
// The real security happens server-side — the session cookie is validated by the server.
function adminLoggedIn() {
  return localStorage.getItem('adminLoggedIn') === 'true';
}

// Returns the current admin's role from localStorage ('super_admin' or 'general_admin').
function adminRole() {
  return localStorage.getItem('adminRole') || 'general_admin';
}

// Returns true if the current admin is a super admin.
function isSuperAdmin() {
  return adminRole() === 'super_admin';
}

// Returns true if the current admin is a general admin. Room and booking
// management are general-admin-only operations — super_admin is deliberately
// excluded, since its responsibilities are security, users, roles, and
// audit monitoring, not day-to-day operations (see RequireGeneralAdmin).
function isGeneralAdmin() {
  return adminRole() === 'general_admin';
}

// Redirects to login.html if the admin is not logged in.
function requireAuth() {
  const page = window.location.pathname.split('/').pop() || 'index.html';
  const adminPages = ['dashboard.html', 'rooms.html', 'users.html', 'bookings.html', 'book-room.html', 'history.html', 'admins.html', 'audit-logs.html', 'force-password-change.html'];

  if (adminPages.includes(page) && !adminLoggedIn()) {
    window.location.replace('login.html');
  }
}

// Returns true if the current admin must replace a temporary password
// before they can use anything else in the admin panel.
function mustResetPassword() {
  return localStorage.getItem('adminMustResetPassword') === 'true';
}

// Clears the local admin session markers and sends the browser to the login
// page. Used both for manual "Sign Out" and when the server reports a missing
// or expired session (401) on any admin API call.
function forceLogout() {
  localStorage.removeItem('adminLoggedIn');
  localStorage.removeItem('adminId');
  localStorage.removeItem('adminName');
  localStorage.removeItem('adminRole');
  localStorage.removeItem('adminMustResetPassword');

  if (window.location.pathname.split('/').pop() !== 'login.html') {
    window.location.href = 'login.html';
  }
}

// Logs out the admin by:
// 1. Calling the server to delete the session
// 2. Clearing the localStorage UI flag
// 3. Redirecting to the login page
async function logout() {
  try {
    await api('/auth/logout', { method: 'POST' });
  } catch (_) {
    // Even if the server call fails, we still clear local state and redirect
  }
  forceLogout();
}

// Sends login credentials to the server.
// On success, the server sets an HttpOnly session cookie automatically.
// We save the admin's name and role in localStorage only for display purposes.
async function loginAdmin(username, password, captchaToken) {
  const result = await api('/auth/login', { method: 'POST', body: JSON.stringify({ username, password, captchaToken }) });
  localStorage.setItem('adminLoggedIn', 'true');
  localStorage.setItem('adminId',   String(result.admin.id));
  localStorage.setItem('adminName', result.admin.name);
  localStorage.setItem('adminRole', result.admin.role || 'general_admin');
  localStorage.setItem('adminMustResetPassword', result.admin.mustResetPassword ? 'true' : 'false');
  return result;
}

async function getRooms() { return api('/rooms'); }
async function createRoom(room) { return api('/rooms', { method: 'POST', body: JSON.stringify(room) }); }
async function updateRoom(id, room) { return api('/rooms/' + id, { method: 'PUT', body: JSON.stringify(room) }); }
async function deleteRoomApi(id) { return api('/rooms/' + id, { method: 'DELETE' }); }

// Normalize every booking returned from the backend so all admin pages use
// one reliable shape. status is left exactly as the server computed it
// (Booked/In Progress/Completed/Cancelled) — the server's clock is pinned to
// Bhutan time (see main.go), so recomputing "has this ended" again here from
// the viewer's own device clock could only ever introduce a disagreement,
// never improve on it.
async function getBookings() {
  const rows = await api('/bookings');
  return (rows || []).map(normalizeBookingRecord);
}

async function createBooking(booking) { return api('/bookings', { method: 'POST', body: JSON.stringify(booking) }); }
async function cancelBookingApi(id) { return api('/bookings/' + id, { method: 'DELETE' }); }
async function deleteBookingApi(id) { return api('/bookings/' + id + '?hard=1', { method: 'DELETE' }); }

async function getUsers() { return api('/users'); }
async function createUser(user) { return api('/users', { method: 'POST', body: JSON.stringify(user) }); }
async function updateUser(id, user) { return api('/users/' + id, { method: 'PUT', body: JSON.stringify(user) }); }
async function deleteUserApi(id) { return api('/users/' + id, { method: 'DELETE' }); }
async function approveUserApi(id, role) {
  return api('/users/' + id + '/approve', { method: 'POST', body: JSON.stringify({ role: role || 'normal_user' }) });
}
async function rejectUserApi(id, reason) { return api('/users/' + id + '/reject', { method: 'POST', body: JSON.stringify({ reason: reason || '' }) }); }
async function revokeUserApi(id) { return api('/users/' + id + '/revoke', { method: 'POST' }); }
async function restoreUserApi(id) { return api('/users/' + id + '/restore', { method: 'POST' }); }

function normalizeBookingRecord(b) {
  const startRaw = b.start || b.startTime || '';
  const endRaw = b.end || b.endTime || '';
  return {
    ...b,
    id: Number(b.id),
    roomId: Number(b.roomId),
    roomName: b.roomName || b.room || '',
    user: b.user || b.userName || '',
    email: b.email || b.userEmail || '',
    date: String(b.date || '').split('T')[0],
    start: toTime24(startRaw),
    end: toTime24(endRaw),
    startTime: formatTime12(startRaw),
    endTime: formatTime12(endRaw),
    purpose: b.purpose || '',
    status: b.status || 'Booked'
  };
}

function formatStatus(status) {
  const classes = {
    Booked: 'bg-blue-100 text-blue-700',
    Active: 'bg-emerald-100 text-emerald-700',
    'In Progress': 'bg-emerald-100 text-emerald-700 animate-pulse',
    Completed: 'bg-slate-200 text-slate-700',
    Cancelled: 'bg-red-100 text-red-700',
    Inactive: 'bg-red-100 text-red-700'
  };
  return `<span class="inline-flex items-center rounded-full px-3 py-1 text-xs font-extrabold whitespace-nowrap ${classes[status] || 'bg-slate-200 text-slate-700'}">${escapeHtml(status)}</span>`;
}

function showMessage(id, text, type='success') {
  const el = document.getElementById(id);
  if (!el) return;
  el.className = `rounded-xl px-4 py-3 mb-4 font-bold ${type === 'success' ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-red-50 text-red-700 border border-red-200'}`;
  el.textContent = text;
  el.style.display = 'block';
  setTimeout(() => { el.style.display = 'none'; }, 3000);
}

// Generic show/hide for any modal built with the "hidden flex" pattern used
// throughout the admin panel (admins.html, users.html, sidebar-extras.js).
function showModal(id) {
  const el = document.getElementById(id);
  el.classList.remove('hidden');
  el.classList.add('flex');
}

function hideModal(id) {
  const el = document.getElementById(id);
  el.classList.add('hidden');
  el.classList.remove('flex');
}

// Shows a persistent message inside a modal (no auto-dismiss timeout, unlike
// showMessage) — used for in-form validation/error feedback that should stay
// visible until the admin fixes the input or closes the modal.
function showModalMsg(id, text, type = 'error') {
  const el = document.getElementById(id);
  el.className = `mb-4 rounded-xl px-4 py-3 font-bold text-sm ${type === 'success'
    ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
    : 'bg-red-50 text-red-700 border border-red-200'}`;
  el.textContent = text;
  el.classList.remove('hidden');
}

function clearModalMsg(id) {
  const el = document.getElementById(id);
  el.textContent = '';
  el.classList.add('hidden');
}

// Shared 18px outline icon glyphs for the compact action buttons used in
// admin tables and cards (rooms, users, bookings, admins).
const ICON_PATHS = {
  edit: '<path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />',
  trash: '<path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />',
  check: '<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />',
  x: '<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />',
  ban: '<path stroke-linecap="round" stroke-linejoin="round" d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />',
  refresh: '<path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />',
  lock: '<path stroke-linecap="round" stroke-linejoin="round" d="M8 11V7a4 4 0 118 0v4M5 11h14v9H5v-9z" />',
  eye: '<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />'
};

const ICON_BUTTON_TONES = {
  indigo: 'text-indigo-600 hover:bg-indigo-100 hover:text-indigo-700',
  red: 'text-red-500 hover:bg-red-100 hover:text-red-700',
  emerald: 'text-emerald-600 hover:bg-emerald-100 hover:text-emerald-700',
  amber: 'text-amber-600 hover:bg-amber-100 hover:text-amber-700',
  orange: 'text-orange-600 hover:bg-orange-100 hover:text-orange-700',
  slate: 'text-slate-500 hover:bg-slate-100 hover:text-slate-700'
};

// Renders a compact 36x36 icon-only action button — the same visual language
// as the room-card action rail, reused for table-row actions across the
// admin panel. onclickJs is inserted verbatim as the onclick attribute value.
function iconButton(iconKey, onclickJs, title, tone = 'slate') {
  const toneClass = ICON_BUTTON_TONES[tone] || ICON_BUTTON_TONES.slate;
  return `<button type="button" onclick="${onclickJs}" title="${title}" aria-label="${title}"
      class="flex h-9 w-9 items-center justify-center rounded-xl transition ${toneClass}">
      <svg class="h-[18px] w-[18px]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">${ICON_PATHS[iconKey] || ''}</svg>
    </button>`;
}

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function todayISO() { return new Date().toISOString().slice(0, 10); }

function formatTime12(value) {
  if (!value) return '';
  value = String(value).trim();
  if (value.includes('AM') || value.includes('PM')) {
    const parts = value.split(' ');
    const hm = parts[0].split(':');
    return String(Number(hm[0])).padStart(2, '0') + ':' + String(hm[1] || '00').padStart(2, '0') + ' ' + parts[1].toUpperCase();
  }
  const parts = value.split(':');
  let hour = Number(parts[0]);
  const minute = parts[1] || '00';
  const suffix = hour >= 12 ? 'PM' : 'AM';
  hour = hour % 12 || 12;
  return String(hour).padStart(2, '0') + ':' + String(minute).padStart(2, '0') + ' ' + suffix;
}

function toTime24(value) {
  if (!value) return '';
  value = String(value).trim();
  if (!value.includes('AM') && !value.includes('PM')) {
    const parts = value.split(':');
    return String(parts[0]).padStart(2, '0') + ':' + String(parts[1] || '00').padStart(2, '0');
  }
  const parts = value.split(' ');
  const hm = parts[0].split(':').map(Number);
  let hour = hm[0];
  const minute = hm[1] || 0;
  const modifier = parts[1].toUpperCase();
  if (modifier === 'PM' && hour !== 12) hour += 12;
  if (modifier === 'AM' && hour === 12) hour = 0;
  return String(hour).padStart(2, '0') + ':' + String(minute).padStart(2, '0');
}

function formatDateDisplay(value) {
  if (!value) return '';
  const d = new Date(String(value).split('T')[0] + 'T00:00:00');
  if (Number.isNaN(d.getTime())) return value;
  return d.toLocaleDateString('en-US', { weekday: 'short', year: 'numeric', month: 'short', day: '2-digit' });
}

// Returns a booking's start time as minutes since midnight for sorting.
// Guards against a missing start field by returning 0 rather than NaN.
function bookingStartMinutes(booking) {
  const time = toTime24(booking.start || booking.startTime || '');
  if (!time) return 0;
  const parts = time.split(':').map(Number);
  return (parts[0] || 0) * 60 + (parts[1] || 0);
}

function hasConflictInList(bookings, roomId, date, start, end, excludeId = null) {
  const newStart = toTime24(start);
  const newEnd = toTime24(end);
  return bookings.some(raw => {
    const b = normalizeBookingRecord(raw);
    return Number(b.roomId) === Number(roomId) &&
      b.date === date &&
      b.status !== 'Cancelled' &&
      Number(b.id) !== Number(excludeId) &&
      newStart < b.end && newEnd > b.start;
  });
}

// Admin management API helpers (super_admin only)
async function getAdmins() { return api('/admins'); }
async function createAdminApi(data) { return api('/admins', { method: 'POST', body: JSON.stringify(data) }); }
async function updateAdminApi(id, data) { return api('/admins/' + id, { method: 'PUT', body: JSON.stringify(data) }); }
async function resetAdminPasswordApi(id, newPassword) { return api('/admins/' + id, { method: 'PATCH', body: JSON.stringify({ newPassword }) }); }
async function revokeAdminApi(id) { return api('/admins/' + id + '/revoke', { method: 'POST' }); }
async function restoreAdminApi(id) { return api('/admins/' + id + '/restore', { method: 'POST' }); }
async function deleteAdminApi(id) { return api('/admins/' + id, { method: 'DELETE' }); }

// Audit trail (super_admin only). filter is a plain object — any key with a
// non-empty value becomes a query param (actor, action, from, to).
async function getAuditLogs(filter) {
  const params = new URLSearchParams();
  Object.keys(filter || {}).forEach(function (key) {
    if (filter[key]) params.set(key, filter[key]);
  });
  const qs = params.toString();
  return api('/audit-logs' + (qs ? '?' + qs : ''));
}
async function changeOwnPasswordApi(currentPassword, newPassword) {
  const result = await api('/admin/change-password', { method: 'POST', body: JSON.stringify({ currentPassword, newPassword }) });
  localStorage.setItem('adminMustResetPassword', 'false');
  return result;
}

// Toggles a password field between hidden and visible.
// Call with the toggle button element; it finds the input via the nearest .pw-wrap parent.
function togglePw(btn) {
  var wrap   = btn.closest('.pw-wrap');
  var input  = wrap.querySelector('input');
  var isHidden = input.type === 'password';
  input.type = isHidden ? 'text' : 'password';
  wrap.querySelector('.eye-open').classList.toggle('hidden', !isHidden);
  wrap.querySelector('.eye-closed').classList.toggle('hidden', isHidden);
}

// Password-reset helpers — public endpoints, no session cookie required.
async function forgotPasswordApi(username, email, captchaToken) {
  return api('/auth/forgot-password', { method: 'POST', body: JSON.stringify({ username, email, captchaToken }) });
}
async function resetPasswordApi(token, password) {
  return api('/auth/reset-password', { method: 'POST', body: JSON.stringify({ token, password }) });
}

// Shared admin booking slots. Matches the public calendar: 30-minute gaps.
const BOOKING_TIME_OPTIONS = [
  '09:00 AM', '09:30 AM', '10:00 AM', '10:30 AM', '11:00 AM', '11:30 AM',
  '12:00 PM', '12:30 PM', '01:00 PM', '01:30 PM', '02:00 PM', '02:30 PM',
  '03:00 PM', '03:30 PM', '04:00 PM', '04:30 PM', '05:00 PM', '05:30 PM',
  '06:00 PM', '06:30 PM', '07:00 PM'
];
