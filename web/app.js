/*
  SmartBook Admin Frontend Helpers
  ------------------------------------------------------------------
  This file contains shared admin-side JavaScript:
  - API wrapper with admin token
  - authentication helpers
  - formatting utilities
  - shared booking/room/user fetch helpers

  Keep page-specific rendering inside each HTML page unless the same
  behavior is reused across multiple pages.
*/

const API_BASE = '/api';

// Common fetch helper for admin API calls.
// Cookies are sent automatically by the browser — no manual token needed.
async function api(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };

  const res = await fetch(API_BASE + path, {
    headers,
    credentials: 'same-origin', // Always include cookies with same-origin requests
    ...options,
  });

  let data = null;
  try { data = await res.json(); } catch (_) {}
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

// Redirects to login.html if the admin is not logged in.
function requireAuth() {
  const page = window.location.pathname.split('/').pop() || 'index.html';
  const adminPages = ['dashboard.html', 'rooms.html', 'users.html', 'bookings.html', 'book-room.html', 'history.html', 'admins.html'];

  if (adminPages.includes(page) && !adminLoggedIn()) {
    window.location.replace('login.html');
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
  localStorage.removeItem('adminLoggedIn');
  localStorage.removeItem('adminId');
  localStorage.removeItem('adminName');
  localStorage.removeItem('adminRole');
  window.location.href = 'login.html';
}

// Sends login credentials to the server.
// On success, the server sets an HttpOnly session cookie automatically.
// We save the admin's name and role in localStorage only for display purposes.
async function loginAdmin(username, password) {
  const result = await api('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) });
  localStorage.setItem('adminLoggedIn', 'true');
  localStorage.setItem('adminId',   String(result.admin.id));
  localStorage.setItem('adminName', result.admin.name);
  localStorage.setItem('adminRole', result.admin.role || 'general_admin');
  return result;
}

async function getRooms() { return api('/rooms'); }
async function createRoom(room) { return api('/rooms', { method: 'POST', body: JSON.stringify(room) }); }
async function updateRoom(id, room) { return api('/rooms/' + id, { method: 'PUT', body: JSON.stringify(room) }); }
async function deleteRoomApi(id) { return api('/rooms/' + id, { method: 'DELETE' }); }

// Normalize every booking returned from the backend so all admin pages use one reliable shape.
async function getBookings() {
  const rows = await api('/bookings');
  return (rows || []).map(normalizeBookingRecord).map(normalizeBookingWithDynamicStatus);
}

async function createBooking(booking) { return api('/bookings', { method: 'POST', body: JSON.stringify(booking) }); }
async function updateBooking(id, booking) { return api('/bookings/' + id, { method: 'PUT', body: JSON.stringify(booking) }); }
async function cancelBookingApi(id) { return api('/bookings/' + id, { method: 'DELETE' }); }
async function deleteBookingApi(id) { return api('/bookings/' + id + '?hard=1', { method: 'DELETE' }); }

async function getUsers() { return api('/users'); }
async function createUser(user) { return api('/users', { method: 'POST', body: JSON.stringify(user) }); }
async function updateUser(id, user) { return api('/users/' + id, { method: 'PUT', body: JSON.stringify(user) }); }
async function deleteUserApi(id) { return api('/users/' + id, { method: 'DELETE' }); }

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

function bookingDateTime(dateValue, timeValue) {
  const d = new Date(String(dateValue).split('T')[0] + 'T00:00:00');
  const time = toTime24(timeValue);
  const parts = time.split(':').map(Number);
  d.setHours(parts[0] || 0, parts[1] || 0, 0, 0);
  return d;
}

// Returns the live status used in admin lists and history.
function getDynamicBookingStatus(booking) {
  if ((booking.status || '').toLowerCase() === 'cancelled') return 'Cancelled';
  const now = new Date();
  const startAt = bookingDateTime(booking.date, booking.start || booking.startTime);
  const endAt = bookingDateTime(booking.date, booking.end || booking.endTime);
  if (now >= startAt && now < endAt) return 'In Progress';
  if (now >= endAt) return 'Completed';
  return 'Booked';
}

function normalizeBookingWithDynamicStatus(booking) {
  return { ...booking, status: getDynamicBookingStatus(booking) };
}

function bookingStartMinutes(booking) {
  const time = toTime24(booking.start || booking.startTime || '');
  const parts = time.split(':').map(Number);
  return parts[0] * 60 + parts[1];
}

function bookingEndMinutes(booking) {
  const time = toTime24(booking.end || booking.endTime || '');
  const parts = time.split(':').map(Number);
  return parts[0] * 60 + parts[1];
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
async function changeOwnPasswordApi(currentPassword, newPassword) {
  return api('/admin/change-password', { method: 'POST', body: JSON.stringify({ currentPassword, newPassword }) });
}

// Shared admin booking slots. Matches the public calendar: 30-minute gaps.
const BOOKING_TIME_OPTIONS = [
  '09:00 AM', '09:30 AM', '10:00 AM', '10:30 AM', '11:00 AM', '11:30 AM',
  '12:00 PM', '12:30 PM', '01:00 PM', '01:30 PM', '02:00 PM', '02:30 PM',
  '03:00 PM', '03:30 PM', '04:00 PM', '04:30 PM', '05:00 PM', '05:30 PM',
  '06:00 PM', '06:30 PM', '07:00 PM'
];
