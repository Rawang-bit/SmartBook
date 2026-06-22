/*
  SmartBook Public Calendar Frontend
  Loaded only by calendar.html. Requires a gate session (set by index.html
  after email check / OTP registration) — see getGateUser() / window.onload.
  Booking uses the gate-verified identity directly; only cancellation asks
  for an email, to confirm ownership of that specific meeting.
  Shows only upcoming and in-progress bookings.
*/

let localUsers = [];
let rooms = [];

let state = {
  view: 'weekly',
  room: '',
  roomId: null,
  location: '',
  bookings: [],
  pending: null,
  activeUser: null,
  currentBookingId: null,
  cancelVerified: false,
  baseDate: new Date(),
  currentWeekDates: [],
  minutesEligible: [],
  currentMinutesBookingId: null
};

const hours = [
  '09:00 AM', '09:30 AM',
  '10:00 AM', '10:30 AM',
  '11:00 AM', '11:30 AM',
  '12:00 PM', '12:30 PM',
  '01:00 PM', '01:30 PM',
  '02:00 PM', '02:30 PM',
  '03:00 PM', '03:30 PM',
  '04:00 PM', '04:30 PM',
  '05:00 PM', '05:30 PM',
  '06:00 PM', '06:30 PM',
  '07:00 PM'
];

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
    ...options
  });

  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || 'Request failed');

  return data;
}

// The public access gate (index.html) verifies the user's email once and stores
// {name, email} here. The calendar trusts this for booking — no further email
// entry is required to create a booking. Cancellation still asks for the email
// to confirm ownership of that specific meeting.
function getGateUser() {
  try {
    return JSON.parse(localStorage.getItem('sbUser') || 'null');
  } catch (_) {
    return null;
  }
}

// Leaves the calendar and returns to the access gate, which always shows
// the email form again — logging out is a real logout, not an instant
// bounce back in. This only clears the local "viewing as X" state; it
// deliberately leaves the trusted-device cookie alone, so someone who
// opted in to "remember this device for 30 days" still skips the OTP step
// (but does re-enter their email) the next time they come back, until that
// 30-day window actually expires.
function exitAccess() {
  localStorage.removeItem('sbUser');
  window.location.href = 'index.html';
}

function renderGateUserBadge(user) {
  const el = document.getElementById('gateUserName');
  if (el) el.innerText = user.name;
}

// Fades out and removes the full-screen loading spinner shown while the
// calendar's initial data (rooms, users, bookings) is being fetched.
function hideLoadingOverlay() {
  const el = document.getElementById('loadingOverlay');
  if (!el) return;
  el.style.transition = 'opacity 300ms ease';
  el.style.opacity = '0';
  setTimeout(() => el.remove(), 300);
}

function updateHeaderClock() {
  const now = new Date();

  document.getElementById('clock').innerText =
    now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });

  document.getElementById('date-label').innerText =
    now.toDateString().toUpperCase();
}

async function loadRooms() {
  const data = await api('/api/rooms');

  rooms = data
    .filter(room => room.status === 'Active')
    .map(room => ({
      id: room.id,
      name: room.name,
      location: room.location,
      capacity: room.capacity
    }));

  if (!rooms.length) {
    showMessageModal(
      'No Rooms Available',
      'No active meeting rooms are available. Please ask admin to add rooms.',
      'circle-alert'
    );
    return;
  }

  if (!state.roomId) {
    state.roomId = rooms[0].id;
    state.room = rooms[0].name;
    state.location = rooms[0].location;
  }
}

// localUsers is kept for future use; the public site does not fetch the user list.
async function loadUsers() {}

function normalizeTimeDisplay(value) {
  if (!value) return '';
  if (value.includes('AM') || value.includes('PM')) return value;

  const [hour, minute] = value.split(':').map(Number);
  const date = new Date();
  date.setHours(hour, minute, 0, 0);

  return date.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: true
  });
}

function getMinutesFromTime(timeStr) {
  const [time, modifier] = timeStr.split(' ');
  let [hour, minute] = time.split(':').map(Number);

  if (hour === 12) hour = 0;
  if (modifier === 'PM') hour += 12;

  return hour * 60 + minute;
}

function getMeetingEndDate(dateStr, endTimeStr) {
  const meetingDate = new Date(dateStr);
  const endMinutes = getMinutesFromTime(endTimeStr);

  meetingDate.setHours(
    Math.floor(endMinutes / 60),
    endMinutes % 60,
    0,
    0
  );

  return meetingDate;
}

function isMeetingCompleted(dateStr, endTimeStr) {
  return new Date() >= getMeetingEndDate(dateStr, endTimeStr);
}

// Public site should not show booking history.
// It keeps only future bookings and currently running meetings.
function shouldShowOnPublicCalendar(booking) {
  if ((booking.status || '').toLowerCase() === 'cancelled') return false;
  return !isMeetingCompleted(booking.date, booking.endTime);
}

async function loadBookings() {
  const data = await api('/api/bookings');

  state.bookings = data
    .map(booking => ({
      id: booking.id,
      roomId: booking.roomId,
      room: booking.roomName || booking.room,
      location: booking.location || '',
      date: new Date(booking.date + 'T00:00:00').toDateString(),
      startTime: normalizeTimeDisplay(booking.startTime || booking.start),
      endTime: normalizeTimeDisplay(booking.endTime || booking.end),
      user: booking.user,
      email: booking.email,
      purpose: booking.purpose,
      agenda: booking.agenda || '',
      status: booking.status
    }))
    .filter(shouldShowOnPublicCalendar);
}

function toISODate(dateStr) {
  const date = new Date(dateStr);

  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

function populateRoomSwitcher() {
  const roomSwitcher = document.getElementById('roomSwitcher');

  roomSwitcher.innerHTML = rooms.map(room => `
    <option value="${room.id}" ${Number(room.id) === Number(state.roomId) ? 'selected' : ''}>
      ${escapeHtml(room.name)} - ${escapeHtml(room.location)}
    </option>
  `).join('');
}

function getMonday(date) {
  const current = new Date(date);
  const dayOfWeek = current.getDay();
  const diff = current.getDate() - dayOfWeek + (dayOfWeek === 0 ? -6 : 1);

  return new Date(new Date(current).setDate(diff));
}

function calculateDates() {
  const current = new Date(state.baseDate);
  const monday = getMonday(current);

  state.currentWeekDates = [];

  for (let i = 0; i < 5; i++) {
    const date = new Date(monday);
    date.setDate(monday.getDate() + i);

    state.currentWeekDates.push({
      short: date.toLocaleDateString('en-US', { weekday: 'short' }),
      day: date.getDate(),
      full: date.toDateString(),
      isToday: date.toDateString() === new Date().toDateString()
    });
  }

  document.getElementById('calendarHeaderDate').innerText =
    current.toLocaleDateString('en-US', { month: 'long', year: 'numeric' });
}

function navigate(direction) {
  if (direction === 0) {
    state.baseDate = new Date();
  } else {
    state.baseDate = new Date(state.baseDate);
    state.baseDate.setDate(
      state.baseDate.getDate() + direction * (state.view === 'weekly' ? 7 : 1)
    );
  }

  calculateDates();
  renderCalendar();
}

function getTimeIndex(timeStr) {
  return hours.indexOf(timeStr);
}

function formatDuration(startTime, endTime) {
  const minutes = getMinutesFromTime(endTime) - getMinutesFromTime(startTime);
  const hrs = Math.floor(minutes / 60);
  const mins = minutes % 60;

  if (hrs > 0 && mins > 0) return `${hrs}h ${mins}m`;
  if (hrs > 0) return `${hrs}h`;

  return `${mins}m`;
}

function getTimeUntilMeetingStart(dateStr, startTimeStr) {
  const now = new Date();
  const meetingDate = new Date(dateStr);
  const startMinutes = getMinutesFromTime(startTimeStr);

  meetingDate.setHours(
    Math.floor(startMinutes / 60),
    startMinutes % 60,
    0,
    0
  );

  const diffMinutes = Math.floor((meetingDate - now) / 60000);

  if (diffMinutes <= 0) return null;
  if (diffMinutes < 60) return `${diffMinutes}m left`;

  const hoursLeft = Math.floor(diffMinutes / 60);
  const minutesLeft = diffMinutes % 60;

  if (minutesLeft === 0) return `${hoursLeft}h left`;

  return `${hoursLeft}h ${minutesLeft}m left`;
}

function isMeetingInProgress(dateStr, startTimeStr, endTimeStr) {
  const now = new Date();
  const meetingDate = new Date(dateStr);

  const startMinutes = getMinutesFromTime(startTimeStr);
  const endMinutes = getMinutesFromTime(endTimeStr);

  const startDate = new Date(meetingDate);
  startDate.setHours(Math.floor(startMinutes / 60), startMinutes % 60, 0, 0);

  const endDate = new Date(meetingDate);
  endDate.setHours(Math.floor(endMinutes / 60), endMinutes % 60, 0, 0);

  return now >= startDate && now < endDate;
}

function isPastSlot(dateStr, timeStr) {
  const now = new Date();
  const slotDate = new Date(dateStr);

  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const slotDay = new Date(slotDate.getFullYear(), slotDate.getMonth(), slotDate.getDate());

  if (slotDay < today) return true;
  if (slotDay > today) return false;

  return getMinutesFromTime(timeStr) <= now.getHours() * 60 + now.getMinutes();
}

function getRoomBookings() {
  return state.bookings.filter(booking => Number(booking.roomId) === Number(state.roomId));
}

function hasBookingConflict(roomId, date, startTime, endTime, excludeId = null) {
  const newStart = getMinutesFromTime(startTime);
  const newEnd = getMinutesFromTime(endTime);

  return state.bookings.some(booking => {
    if (Number(booking.roomId) !== Number(roomId)) return false;
    if (booking.date !== date) return false;
    if (excludeId && booking.id === excludeId) return false;

    return newStart < getMinutesFromTime(booking.endTime) &&
           newEnd > getMinutesFromTime(booking.startTime);
  });
}

function getAvailableEndTimes(roomId, date, startTime) {
  const endTimes = [];
  const startIndex = getTimeIndex(startTime);

  for (let i = startIndex + 1; i < hours.length; i++) {
    const candidateEndTime = hours[i];

    if (hasBookingConflict(roomId, date, startTime, candidateEndTime)) break;

    endTimes.push(candidateEndTime);
  }

  return endTimes;
}

function clearBookingValidation() {
  const el = document.getElementById('bookingValidationMessage');
  el.innerText = '';
  el.classList.add('hidden');
}

function showBookingValidation(message) {
  const el = document.getElementById('bookingValidationMessage');
  el.innerText = message;
  el.classList.remove('hidden');
}

function openModal() {
  const overlay = document.getElementById('modalOverlay');
  overlay.classList.remove('hidden');
  overlay.classList.add('flex');
}

function closeModal() {
  const overlay = document.getElementById('modalOverlay');
  overlay.classList.add('hidden');
  overlay.classList.remove('flex');

  ['bookStep', 'detailsStep', 'cancelConfirmStep', 'messageStep', 'successStep', 'minutesListStep', 'minutesEditStep']
    .forEach(id => {
      const el = document.getElementById(id);
      if (el) el.classList.add('hidden');
    });

  resetBookingFormFields();
  resetTransientState();
}

function showStep(step) {
  openModal();

  const steps = {
    book: 'bookStep',
    details: 'detailsStep',
    cancelConfirm: 'cancelConfirmStep',
    message: 'messageStep',
    success: 'successStep',
    minutesList: 'minutesListStep',
    minutesEdit: 'minutesEditStep'
  };

  Object.values(steps).forEach(id => {
    const el = document.getElementById(id);
    if (el) el.classList.add('hidden');
  });

  const active = document.getElementById(steps[step]);
  if (active) active.classList.remove('hidden');

  if (window.lucide) {
    lucide.createIcons();
  }
}

function showMessageModal(title, text, icon = 'info') {
  document.getElementById('messageTitle').innerText = title;
  document.getElementById('messageText').innerText = text;

  const wrap = document.getElementById('messageIconWrap');
  const iconEl = document.getElementById('messageIcon');

  wrap.className =
    'mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl';

  if (icon === 'circle-alert') {
    wrap.classList.add('bg-amber-50', 'text-amber-600');
  } else if (icon === 'shield-alert') {
    wrap.classList.add('bg-red-50', 'text-red-600');
  } else {
    wrap.classList.add('bg-indigo-50', 'text-indigo-600');
  }

  iconEl.setAttribute('data-lucide', icon);

  showStep('message');
}

function resetBookingFormFields() {
  document.getElementById('meetingPurpose').value = '';
  document.getElementById('meetingAgenda').value = '';
  document.getElementById('meetingParticipants').value = '';
  document.getElementById('endTimeSelect').innerHTML = '';

  clearBookingValidation();
}

function resetTransientState() {
  state.pending = null;
  // state.activeUser is the gate-verified user for this browser session — it
  // persists across bookings and must not be cleared here.
  state.currentBookingId = null;
  state.cancelVerified = false;
}

function updateRoomHeader() {
  document.getElementById('calendarSubtext').innerText = state.room;
  document.getElementById('activeLocationDisplay').innerText = state.location;
  document.getElementById('roomSwitcher').value = state.roomId;
}

async function switchRoom(roomId) {
  const selectedRoom = rooms.find(room => Number(room.id) === Number(roomId));
  if (!selectedRoom) return;

  state.roomId = selectedRoom.id;
  state.room = selectedRoom.name;
  state.location = selectedRoom.location;

  updateRoomHeader();
  await loadBookings();
  renderCalendar();
}

function renderEmptyStateHint(container) {
  if (getRoomBookings().length > 0) return;

  container.insertAdjacentHTML(
    'beforeend',
    `
      <div class="absolute bottom-6 right-6 z-20 rounded-2xl border border-slate-200 bg-white/90 px-4 py-3 shadow-lg backdrop-blur">
        <p class="text-[10px] font-black uppercase tracking-widest text-slate-400">Tip</p>
        <p class="text-sm font-bold text-slate-600">
          Hover a free slot to create a booking in ${escapeHtml(state.room)}
        </p>
      </div>
    `
  );
}

function renderCalendar() {
  const container = document.getElementById('calendarContainer');
  const roomBookings = getRoomBookings();

  const displayDays = state.view === 'weekly'
    ? state.currentWeekDates
    : [{
        short: state.baseDate.toLocaleDateString('en-US', { weekday: 'short' }),
        day: state.baseDate.getDate(),
        full: state.baseDate.toDateString(),
        isToday: state.baseDate.toDateString() === new Date().toDateString()
      }];

  let html = `
    <div id="calendarGridWrapper" class="relative min-w-[1000px]">
      <table id="calendarTable" class="relative w-full border-collapse">
        <thead>
          <tr>
            <th class="sticky left-0 top-0 z-50 w-24 border-r border-slate-200 bg-white p-6"></th>
  `;

  displayDays.forEach((dateObj, dayIndex) => {
    html += `
      <th
        data-day-index="${dayIndex}"
        class="sticky top-0 z-40 border-r border-slate-100 p-6 text-center ${dateObj.isToday ? 'border-b-[3px] border-b-indigo-600 bg-indigo-50' : 'bg-white'}"
      >
        <span class="mb-1 block text-[10px] font-black uppercase tracking-widest ${dateObj.isToday ? 'text-indigo-600' : 'text-slate-400'}">
          ${dateObj.short}
        </span>

        <span class="${dateObj.isToday ? 'mx-auto mt-1 flex h-10 w-10 items-center justify-center rounded-full bg-indigo-600 font-black text-white shadow-lg' : 'text-2xl font-black text-slate-700'}">
          ${dateObj.day}
        </span>
      </th>
    `;
  });

  html += `
          </tr>
        </thead>
        <tbody class="relative divide-y divide-slate-100">
          <tr class="h-4">
            <td class="sticky left-0 z-30 border-r border-slate-200 bg-[#f8fafc]"></td>
            ${displayDays.map(() => '<td class="border-r border-slate-100"></td>').join('')}
          </tr>
  `;

  hours.forEach((time, index) => {
    html += `
      <tr class="h-28" data-hour-index="${index}">
        <td class="sticky left-0 z-30 border-r border-slate-200 bg-[#f8fafc] relative">
          <span class="absolute right-4 top-0 z-10 -translate-y-1/2 bg-[#f8fafc] px-1 text-[10px] font-bold uppercase tabular-nums text-slate-400">
            ${time}
          </span>
        </td>
    `;

    displayDays.forEach(dateObj => {
      const isBooked = roomBookings.some(booking =>
        booking.date === dateObj.full &&
        getTimeIndex(booking.startTime) <= index &&
        getTimeIndex(booking.endTime) > index
      );

      const isPast = isPastSlot(dateObj.full, time);

      html += `
        <td class="group relative border-r border-slate-100 p-2">
          ${
            index < hours.length - 1 && !isBooked && !isPast
              ? `
                <button
                  onclick="openBookingFlow('${dateObj.full}', '${time}')"
                  class="flex h-full w-full items-center justify-center rounded-2xl border-2 border-dashed border-emerald-200 text-emerald-600 opacity-0 transition-all group-hover:opacity-100"
                >
                  <i data-lucide="plus"></i>
                </button>
              `
              : isPast && dateObj.full === new Date().toDateString()
                ? '<div class="h-full w-full rounded-2xl bg-slate-50/70"></div>'
                : ''
          }
        </td>
      `;
    });

    html += '</tr>';
  });

  html += `
        </tbody>
      </table>

      <div id="bookingOverlay" class="pointer-events-none absolute inset-0"></div>
    </div>
  `;

  container.innerHTML = html;

  const wrapper = document.getElementById('calendarGridWrapper');
  const overlay = document.getElementById('bookingOverlay');
  const wrapperRect = wrapper.getBoundingClientRect();

  roomBookings.forEach(booking => {
    const dayIndex = displayDays.findIndex(day => day.full === booking.date);
    if (dayIndex === -1) return;

    const startIndex = getTimeIndex(booking.startTime);
    const endIndex = getTimeIndex(booking.endTime);
    if (startIndex === -1 || endIndex === -1) return;

    const header = wrapper.querySelector(`[data-day-index="${dayIndex}"]`);
    const startRow = wrapper.querySelector(`[data-hour-index="${startIndex}"]`);
    if (!header || !startRow) return;

    const headerRect = header.getBoundingClientRect();
    const rowRect = startRow.getBoundingClientRect();

    const left = headerRect.left - wrapperRect.left + 8;
    const top = rowRect.top - wrapperRect.top + 8;
    const width = headerRect.width - 16;
    const height = ((endIndex - startIndex) * rowRect.height) - 16;

    const isActive = isMeetingInProgress(booking.date, booking.startTime, booking.endTime);
    const remaining = getTimeUntilMeetingStart(booking.date, booking.startTime);

    const baseCardClass =
      'absolute pointer-events-auto z-10 overflow-hidden rounded-2xl border-l-[6px] p-4 shadow-md transition-all duration-200 hover:-translate-y-0.5 hover:z-20 hover:shadow-xl';

    const cardClass = isActive
      ? `${baseCardClass} border-emerald-600 bg-emerald-50`
      : `${baseCardClass} border-indigo-600 bg-indigo-50`;

    const timeClass = isActive ? 'text-emerald-500' : 'text-indigo-400';
    const titleClass = isActive ? 'text-emerald-900' : 'text-indigo-900';
    const userClass = isActive ? 'text-emerald-700' : 'text-indigo-600';

    const badgeHtml = isActive
      ? '<span class="inline-flex animate-pulse items-center whitespace-nowrap rounded-md bg-emerald-600 px-1.5 py-0.5 text-[8px] font-black uppercase tracking-tighter text-white"><span class="mr-1 inline-block h-[7px] w-[7px] rounded-full bg-current"></span>In Progress</span>'
      : remaining
        ? `<span class="whitespace-nowrap rounded-md bg-indigo-600 px-1.5 py-0.5 text-[8px] font-black uppercase tracking-tighter text-white">${remaining}</span>`
        : '';

    const card = document.createElement('div');

    card.className = cardClass;
    card.style.top = `${top}px`;
    card.style.left = `${left}px`;
    card.style.width = `${Math.max(width, 120)}px`;
    card.style.height = `${Math.max(height, 70)}px`;
    card.onclick = () => showDetails(booking.id);

    card.innerHTML = `
      <div class="mb-1 flex items-start justify-between gap-2">
        <p class="text-[10px] font-black uppercase ${timeClass}">
          ${escapeHtml(booking.startTime)}
        </p>
        ${badgeHtml}
      </div>

      <p class="truncate text-base font-extrabold leading-tight ${titleClass}">
        ${escapeHtml(booking.purpose)}
      </p>

      <p class="mt-2 truncate text-[11px] font-bold ${userClass}">
        ${escapeHtml(booking.user)}
      </p>
    `;

    overlay.appendChild(card);
  });

  renderEmptyStateHint(container);

  if (window.lucide) {
    lucide.createIcons();
  }
}

function switchView(view) {
  state.view = view;

  ['daily', 'weekly'].forEach(id => {
    const btn = document.getElementById('view-' + id);

    if (btn) {
      btn.className =
        `rounded-xl px-5 py-2 text-sm font-bold transition-all ${view === id ? 'bg-indigo-600 text-white shadow-md' : 'text-slate-500'}`;
    }
  });

  calculateDates();
  renderCalendar();
}

// Booking no longer asks the user to (re-)verify their email — that already
// happened once on the public access gate (index.html). We go straight to
// the booking form using the gate-verified identity in state.activeUser.
function openBookingFlow(date, time) {
  if (isPastSlot(date, time)) {
    showMessageModal('Invalid Time Slot', 'Past time slots cannot be booked.', 'circle-alert');
    return;
  }

  if (!state.activeUser || !state.activeUser.email) {
    showMessageModal('Session Expired', 'Please verify your email again to continue.', 'shield-alert');
    exitAccess();
    return;
  }

  state.pending = {
    date,
    time,
    room: state.room,
    roomId: state.roomId
  };

  clearBookingValidation();
  prepareBookStep();
}

function prepareBookStep() {
  const user = state.activeUser;

  document.getElementById('userNameDisplay').innerText = user.name;
  document.getElementById('userEmailDisplay').innerText = user.email;
  document.getElementById('userInitial').innerText = user.name.charAt(0).toUpperCase();
  document.getElementById('startTimeStatic').value = state.pending.time;
  document.getElementById('selectedRoomDisplay').value = state.pending.room;

  const endSelect = document.getElementById('endTimeSelect');
  endSelect.innerHTML = '';

  const availableEndTimes = getAvailableEndTimes(
    state.pending.roomId,
    state.pending.date,
    state.pending.time
  );

  availableEndTimes.forEach(time => {
    endSelect.add(new Option(time, time));
  });

  if (!availableEndTimes.length) {
    showMessageModal('No End Time Available', 'No valid end times are available for this selected slot.', 'circle-alert');
    return;
  }

  showStep('book');
}

async function submitBooking() {
  clearBookingValidation();

  const meetingPurpose = document.getElementById('meetingPurpose').value.trim();
  const meetingAgenda = document.getElementById('meetingAgenda').value.trim();
  const meetingParticipants = document.getElementById('meetingParticipants').value.trim();
  const endTime = document.getElementById('endTimeSelect').value;

  if (!state.pending || !state.activeUser) {
    showBookingValidation('Booking session expired. Please try again.');
    return;
  }

  if (!meetingPurpose || meetingPurpose.length < 3) {
    showBookingValidation('Meeting purpose must be at least 3 characters.');
    return;
  }

  if (!endTime) {
    showBookingValidation('Please select an end time.');
    return;
  }

  if (isPastSlot(state.pending.date, state.pending.time)) {
    showBookingValidation('Past time slots cannot be booked.');
    return;
  }

  if (hasBookingConflict(state.pending.roomId, state.pending.date, state.pending.time, endTime)) {
    showBookingValidation('This slot overlaps with an existing booking.');
    return;
  }

  try {
    await api('/api/bookings', {
      method: 'POST',
      body: JSON.stringify({
        roomId: state.pending.roomId,
        room: state.pending.room,
        date: toISODate(state.pending.date),
        start: state.pending.time,
        end: endTime,
        startTime: state.pending.time,
        endTime,
        user: state.activeUser.name,
        email: state.activeUser.email,
        purpose: meetingPurpose,
        agenda: meetingAgenda,
        participants: meetingParticipants,
        status: 'Booked'
      })
    });

    await loadBookings();

    document.getElementById('successTitle').innerText = 'Success!';
    document.getElementById('successMessage').innerText = 'Your booking has been created successfully. A confirmation email has been sent to you and any participants.';

    resetBookingFormFields();
    resetTransientState();

    showStep('success');
    renderCalendar();
  } catch (err) {
    showBookingValidation(err.message);
  }
}

function showDetails(id) {
  const booking = state.bookings.find(item => item.id === id);
  if (!booking) return;

  state.currentBookingId = id;
  state.cancelVerified = false;

  document.getElementById('viewMeetingTitle').innerText = booking.purpose;
  document.getElementById('viewMeetingTime').innerText =
    `${booking.date} | ${booking.startTime} - ${booking.endTime}`;
  document.getElementById('viewMeetingUser').innerText = booking.user;
  document.getElementById('viewMeetingLocation').innerText = booking.location || '';
  document.getElementById('viewMeetingDuration').innerText =
    formatDuration(booking.startTime, booking.endTime);
  document.getElementById('viewMeetingRoom').innerText = booking.room;

  const agendaRow = document.getElementById('viewMeetingAgendaRow');
  if (booking.agenda) {
    document.getElementById('viewMeetingAgenda').innerText = booking.agenda;
    agendaRow.classList.remove('hidden');
  } else {
    agendaRow.classList.add('hidden');
  }

  showStep('details');
}

function requestCancelVerify() {
  const booking = state.bookings.find(item => item.id === state.currentBookingId);
  if (!booking) return;

  // The gate session already proves who's signed in, so there's nothing
  // left to "verify" — either this is their own meeting, or it isn't.
  const isOwner = state.activeUser && state.activeUser.email &&
    booking.email.toLowerCase() === state.activeUser.email.toLowerCase();

  if (!isOwner) {
    showMessageModal('Access Denied', "You can't cancel a meeting booked by someone else.", 'shield-alert');
    return;
  }

  proceedToCancelConfirm(booking);
}

function proceedToCancelConfirm(booking) {
  state.cancelVerified = true;

  document.getElementById('cancelConfirmText').innerText =
    `Are you sure you want to cancel "${booking.purpose}" in ${booking.room}?`;

  showStep('cancelConfirm');
}

async function confirmCancelBooking() {
  if (!state.cancelVerified || !state.currentBookingId) return;

  const booking = state.bookings.find(item => item.id === state.currentBookingId);

  try {
    await api(`/api/bookings/${state.currentBookingId}/cancel`, {
      method: 'POST',
      body: JSON.stringify({
        email: booking.email
      })
    });

    await loadBookings();

    document.getElementById('successTitle').innerText = 'Cancelled';
    document.getElementById('successMessage').innerText =
      'The meeting booking has been cancelled successfully.';

    resetBookingFormFields();
    resetTransientState();

    showStep('success');
    renderCalendar();
  } catch (err) {
    showMessageModal('Cancel Failed', err.message, 'circle-alert');
  }
}

// Meeting Minutes lives outside the normal calendar flow because completed
// meetings are filtered out of state.bookings entirely (shouldShowOnPublicCalendar
// keeps the grid to upcoming/in-progress only) — so this fetches bookings
// fresh and finds the signed-in user's own meetings still awaiting minutes.
// Eligibility (ended, not cancelled, still within the 24-hour window) comes
// straight from the server's minutesEditable flag rather than being
// recomputed here, so this list can never disagree with what a save will
// actually accept.
async function openMinutesOfMeeting() {
  if (!state.activeUser || !state.activeUser.email) return;

  let data;
  try {
    data = await api('/api/bookings');
  } catch (err) {
    showMessageModal('Loading Failed', err.message, 'circle-alert');
    return;
  }

  const myEmail = state.activeUser.email.toLowerCase();

  state.minutesEligible = data
    .filter(b => (b.email || '').toLowerCase() === myEmail)
    // Trust the server's own eligibility check rather than recomputing it
    // here — it's the same check SetMinutesOfMeeting enforces when saving,
    // so the list can never show something the server would then reject.
    .filter(b => b.minutesEditable)
    // Only meetings still needing minutes — once added, this list is no
    // longer how you'd revisit them.
    .filter(b => !b.minutesOfMeeting)
    .map(b => ({
      id: b.id,
      purpose: b.purpose,
      room: b.roomName || b.room,
      startTime: normalizeTimeDisplay(b.startTime || b.start),
      endTime: normalizeTimeDisplay(b.endTime || b.end)
    }));

  renderMinutesList();
  showStep('minutesList');
}

function renderMinutesList() {
  const container = document.getElementById('minutesListItems');
  const empty = document.getElementById('minutesListEmpty');

  if (!state.minutesEligible.length) {
    container.innerHTML = '';
    empty.classList.remove('hidden');
    return;
  }
  empty.classList.add('hidden');

  container.innerHTML = state.minutesEligible.map(b => `
    <button onclick="openMinutesEditor(${b.id})" class="w-full rounded-2xl border border-slate-100 bg-slate-50 p-4 text-left transition hover:bg-slate-100">
      <p class="text-sm font-bold text-slate-800">${escapeHtml(b.purpose)}</p>
      <p class="mt-1 text-xs font-bold text-slate-400">${escapeHtml(b.room)} &middot; ${b.startTime} - ${b.endTime}</p>
      <p class="mt-2 text-[10px] font-black uppercase tracking-widest text-amber-500">Add minutes</p>
    </button>
  `).join('');

  if (window.lucide) lucide.createIcons();
}

function openMinutesEditor(id) {
  const booking = state.minutesEligible.find(b => b.id === id);
  if (!booking) return;

  state.currentMinutesBookingId = id;
  document.getElementById('minutesEditTitle').innerText = booking.purpose;
  document.getElementById('minutesEditMeta').innerText = `${booking.room} · ${booking.startTime} - ${booking.endTime}`;
  document.getElementById('minutesText').value = '';

  showStep('minutesEdit');
}

async function saveMinutesOfMeeting() {
  if (!state.currentMinutesBookingId || !state.activeUser) return;

  const minutes = document.getElementById('minutesText').value.trim();
  const btn = document.getElementById('saveMinutesBtn');
  btn.disabled = true;
  btn.textContent = 'Saving…';

  try {
    await api(`/api/bookings/${state.currentMinutesBookingId}/minutes`, {
      method: 'POST',
      body: JSON.stringify({ email: state.activeUser.email, minutes })
    });

    document.getElementById('successTitle').innerText = 'Saved';
    document.getElementById('successMessage').innerText = 'Meeting minutes have been saved successfully.';
    showStep('success');
  } catch (err) {
    showMessageModal('Save Failed', err.message, 'circle-alert');
  } finally {
    btn.disabled = false;
    btn.textContent = 'Save Minutes';
  }
}

// Confirms the gate-verified email is still a registered, active user.
// Checked both at page load — in case a deleted/revoked/rejected user has
// calendar.html bookmarked or cached from before — and every 10 seconds
// while browsing, in case an admin removes their access mid-session.
// Returns true if still valid; otherwise shows a notice and logs them out.
async function verifyGateUserStillActive() {
  const gateUser = getGateUser();
  if (!gateUser || !gateUser.email) return false;

  try {
    const res = await api('/api/access/check-email', {
      method: 'POST',
      body: JSON.stringify({ email: gateUser.email })
    });

    if (res.exists && res.status === 'active') return true;
  } catch (_) {
    // Network hiccup — don't log the user out over a transient failure.
    return true;
  }

  // The loading overlay sits above the message modal (z-[200] vs z-[100]) —
  // hide it first so this notice is actually visible if it fires during the
  // initial page-load check, not just during the periodic re-check.
  hideLoadingOverlay();
  showMessageModal('Access Revoked', "Your access has been removed by an administrator. You're being redirected.", 'shield-alert');
  setTimeout(exitAccess, 2500);
  return false;
}

window.onload = async function() {
  // The calendar is only for users who already verified their email on the
  // public access gate (index.html). Bounce anyone without a gate session.
  const gateUser = getGateUser();
  if (!gateUser || !gateUser.email) {
    window.location.href = 'index.html';
    return;
  }

  if (!(await verifyGateUserStillActive())) return;

  state.activeUser = gateUser;
  renderGateUserBadge(gateUser);

  try {
    await loadRooms();
    await loadUsers();
    await loadBookings();

    populateRoomSwitcher();
    updateRoomHeader();
    calculateDates();
    renderCalendar();
    updateHeaderClock();
  } catch (err) {
    showMessageModal('Loading Failed', err.message, 'circle-alert');
  } finally {
    hideLoadingOverlay();
  }

  setInterval(async function() {
    updateHeaderClock();

    if (!(await verifyGateUserStillActive())) return;

    try {
      await loadBookings();
      renderCalendar();
    } catch (_) {}
  }, 10000);

  setInterval(function() {
    document.getElementById('clock').innerText =
      new Date().toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
      });
  }, 1000);
};