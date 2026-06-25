// sidebar-extras.js
// Loaded by every admin page. Responsibilities:
//   1. Show/hide the Admins nav link based on role (super_admin only)
//   2. Populate the header admin name badge
//   3. Populate the sidebar admin info panel (name, initial, role)
//   4. Inject and wire up the shared Change Password modal
//      (used only by admins.html own-row "Change PW" action)
//   5. Inject and wire up the shared Confirm modal — a generic replacement
//      for native confirm() used by every admin page's destructive actions
//      (delete room, cancel/delete booking, delete user, revoke/restore admin)
//   6. Collapsible sidebar — persisted via localStorage (see bottom of file)

(function () {

  // ── UI Population ────────────────────────────────────────────────────────
  document.addEventListener('DOMContentLoaded', function () {
    const role = localStorage.getItem('adminRole') || 'general_admin';
    const name = localStorage.getItem('adminName') || 'Admin';

    // Admins and Audit Logs nav links are hidden by default in HTML; show
    // them only for super_admin — both are exclusively that role's
    // responsibility. Anchor elements default to inline when hidden is
    // removed, so an explicit display utility is required — flex, to match
    // every other nav link's icon + label layout.
    if (role === 'super_admin') {
      ['adminsNavLink', 'auditLogsNavLink'].forEach(function (id) {
        const link = document.getElementById(id);
        if (link) {
          link.classList.remove('hidden');
          link.classList.add('flex');
        }
      });

      // Rooms, Book Room, and Bookings are general-admin-only modules — hide
      // them from super_admin in the opposite direction. These links have no
      // id (their styling already varies per page for the "active" one), so
      // they're targeted by href instead of needing an HTML change.
      ['rooms.html', 'book-room.html', 'bookings.html'].forEach(function (href) {
        const link = document.querySelector('a.sidebar-link[href="' + href + '"]');
        if (link) link.classList.add('hidden');
      });
    }

    // Header admin name badge (present on every admin page)
    const headerNameEl = document.getElementById('adminName');
    if (headerNameEl) headerNameEl.textContent = name;

    // Sidebar admin panel: avatar initial, full name, and human-readable role
    const sidebarNameEl = document.getElementById('sidebarAdminName');
    if (sidebarNameEl) sidebarNameEl.textContent = name;

    const sidebarInitialEl = document.getElementById('sidebarAdminInitial');
    if (sidebarInitialEl) sidebarInitialEl.textContent = name.charAt(0).toUpperCase();

    const sidebarRoleEl = document.getElementById('sidebarAdminRole');
    if (sidebarRoleEl) {
      sidebarRoleEl.textContent = role === 'super_admin' ? 'Super Admin' : 'General Admin';
    }
  });

  // ── Change Password Modal ────────────────────────────────────────────────
  // Injected once into the body so every page gets it without duplicating markup.
  document.addEventListener('DOMContentLoaded', function () {
    const modal = document.createElement('div');
    modal.id = 'sharedChangePasswordModal';
    modal.className = 'fixed inset-0 z-50 hidden items-center justify-center bg-slate-900/30 backdrop-blur-md p-4';
    modal.innerHTML = `
      <div class="w-full max-w-md rounded-3xl border border-white/60 bg-white/75 p-6 shadow-2xl backdrop-blur-xl">
        <h2 class="mb-5 text-xl font-black text-slate-900">Change My Password</h2>
        <div id="sharedCpMsg" class="mb-4 hidden rounded-xl px-4 py-3 font-bold text-sm"></div>
        <form id="sharedCpForm" class="space-y-4">
          <div>
            <label class="mb-1 block text-xs font-extrabold uppercase tracking-wider text-slate-500">Current Password</label>
            <div class="relative pw-wrap">
              <input id="sharedCpCurrent" type="password" required
                class="w-full rounded-xl border border-slate-200 px-4 py-3 pr-10 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                placeholder="Your current password" />
              <button type="button" tabindex="-1" onclick="togglePw(this)" aria-label="Toggle password visibility"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-700 transition">
                <svg class="eye-open h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>
                <svg class="eye-closed hidden h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 4.411m0 0L21 21"/></svg>
              </button>
            </div>
          </div>
          <div>
            <label class="mb-1 block text-xs font-extrabold uppercase tracking-wider text-slate-500">New Password</label>
            <div class="relative pw-wrap">
              <input id="sharedCpNew" type="password" required minlength="12"
                class="w-full rounded-xl border border-slate-200 px-4 py-3 pr-10 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                placeholder="At least 12 characters" />
              <button type="button" tabindex="-1" onclick="togglePw(this)" aria-label="Toggle password visibility"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-700 transition">
                <svg class="eye-open h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>
                <svg class="eye-closed hidden h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 4.411m0 0L21 21"/></svg>
              </button>
            </div>
          </div>
          <div>
            <label class="mb-1 block text-xs font-extrabold uppercase tracking-wider text-slate-500">Confirm New Password</label>
            <div class="relative pw-wrap">
              <input id="sharedCpConfirm" type="password" required minlength="12"
                class="w-full rounded-xl border border-slate-200 px-4 py-3 pr-10 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                placeholder="Re-enter new password" />
              <button type="button" tabindex="-1" onclick="togglePw(this)" aria-label="Toggle password visibility"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-700 transition">
                <svg class="eye-open h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg>
                <svg class="eye-closed hidden h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 4.411m0 0L21 21"/></svg>
              </button>
            </div>
          </div>
          <div class="flex gap-3 pt-2">
            <button type="button" onclick="closeSharedCpModal()"
              class="flex-1 rounded-xl border border-slate-200 px-4 py-3 text-sm font-bold text-slate-600 transition hover:bg-slate-50">
              Cancel
            </button>
            <button type="submit" id="sharedCpSubmit"
              class="flex-1 rounded-xl bg-indigo-600 px-4 py-3 text-sm font-extrabold text-white transition hover:bg-indigo-700">
              Update Password
            </button>
          </div>
        </form>
      </div>`;
    document.body.appendChild(modal);

    // Close when clicking the backdrop (outside the card)
    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeSharedCpModal();
    });

    document.getElementById('sharedCpForm').addEventListener('submit', async function (e) {
      e.preventDefault();

      const current = document.getElementById('sharedCpCurrent').value;
      const newPw   = document.getElementById('sharedCpNew').value;
      const confirm = document.getElementById('sharedCpConfirm').value;
      const btn     = document.getElementById('sharedCpSubmit');

      if (newPw !== confirm) {
        showSharedCpMsg('New passwords do not match', 'error');
        return;
      }

      btn.disabled    = true;
      btn.textContent = 'Updating…';

      try {
        await changeOwnPasswordApi(current, newPw);
        showSharedCpMsg('Password changed! Logging you out…', 'success');
        setTimeout(() => logout(), 2000);
      } catch (err) {
        showSharedCpMsg(err.message, 'error');
        btn.disabled    = false;
        btn.textContent = 'Update Password';
      }
    });
  });

  // ── Shared Confirm Modal ─────────────────────────────────────────────────
  // Generic yes/no confirmation used everywhere a page previously called the
  // native confirm(). Injected once into the body so no page duplicates the
  // markup. See confirmAction() below for the call-site API.
  document.addEventListener('DOMContentLoaded', function () {
    const modal = document.createElement('div');
    modal.id = 'sharedConfirmModal';
    modal.className = 'fixed inset-0 z-50 hidden items-center justify-center bg-slate-900/30 backdrop-blur-md p-4';
    modal.innerHTML = `
      <div class="w-full max-w-sm rounded-3xl border border-white/60 bg-white/75 p-6 shadow-2xl backdrop-blur-xl text-center">
        <div id="sharedConfirmIconWrap" class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-red-100">
          <svg id="sharedConfirmIcon" class="h-8 w-8 text-red-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
          </svg>
        </div>
        <h3 id="sharedConfirmTitle" class="mb-2 text-lg font-black text-slate-900">Are you sure?</h3>
        <p id="sharedConfirmText" class="mb-6 text-sm font-semibold text-slate-500"></p>
        <div class="flex gap-3">
          <button type="button" onclick="closeSharedConfirm()"
            class="flex-1 rounded-xl border border-slate-200 px-4 py-3 text-sm font-bold text-slate-600 transition hover:bg-slate-50">
            Cancel
          </button>
          <button type="button" id="sharedConfirmBtn"
            class="flex-1 rounded-xl bg-red-600 px-4 py-3 text-sm font-extrabold text-white transition hover:bg-red-700">
            Confirm
          </button>
        </div>
      </div>`;
    document.body.appendChild(modal);

    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeSharedConfirm();
    });

    document.getElementById('sharedConfirmBtn').addEventListener('click', function () {
      const callback = _sharedConfirmCallback;
      closeSharedConfirm();
      if (callback) callback();
    });
  });

})();

// ── Confirm Modal API ─────────────────────────────────────────────────────

let _sharedConfirmCallback = null;

// Shows the shared confirmation modal and calls onConfirm() if the admin
// clicks the confirm button. Replaces native confirm() across the admin panel.
//
// options (all optional):
//   title          — heading text, defaults to "Are you sure?"
//   confirmLabel   — confirm button text, defaults to "Confirm"
//   tone           — "danger" (red, default) or "positive" (emerald) —
//                     controls the icon and confirm button color
function confirmAction(message, onConfirm, options = {}) {
  const tone = options.tone === 'positive'
    ? { wrap: 'bg-emerald-100', icon: 'text-emerald-600', btn: 'bg-emerald-600 hover:bg-emerald-700' }
    : { wrap: 'bg-red-100',     icon: 'text-red-600',     btn: 'bg-red-600 hover:bg-red-700' };

  document.getElementById('sharedConfirmTitle').textContent = options.title || 'Are you sure?';
  document.getElementById('sharedConfirmText').textContent = message;

  document.getElementById('sharedConfirmIconWrap').className =
    `mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full ${tone.wrap}`;
  document.getElementById('sharedConfirmIcon').setAttribute('class', `h-8 w-8 ${tone.icon}`);

  const btn = document.getElementById('sharedConfirmBtn');
  btn.textContent = options.confirmLabel || 'Confirm';
  btn.className = `flex-1 rounded-xl px-4 py-3 text-sm font-extrabold text-white transition ${tone.btn}`;

  _sharedConfirmCallback = onConfirm;
  showModal('sharedConfirmModal');
}

function closeSharedConfirm() {
  _sharedConfirmCallback = null;
  hideModal('sharedConfirmModal');
}

// ── Modal API ──────────────────────────────────────────────────────────────

function openSharedCpModal() {
  document.getElementById('sharedCpForm').reset();
  const msg = document.getElementById('sharedCpMsg');
  msg.textContent = '';
  msg.classList.add('hidden');
  const modal = document.getElementById('sharedChangePasswordModal');
  modal.classList.remove('hidden');
  modal.classList.add('flex');
}

function closeSharedCpModal() {
  const modal = document.getElementById('sharedChangePasswordModal');
  modal.classList.add('hidden');
  modal.classList.remove('flex');
}

function showSharedCpMsg(text, type) {
  const el = document.getElementById('sharedCpMsg');
  el.className = `mb-4 rounded-xl px-4 py-3 font-bold text-sm ${
    type === 'success'
      ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
      : 'bg-red-50 text-red-700 border border-red-200'
  }`;
  el.textContent = text;
  el.classList.remove('hidden');
}

// ── Collapsible Sidebar ──────────────────────────────────────────────────
// Persisted in localStorage so the collapsed/expanded state survives
// navigating between admin pages — each is a separate full page load, not
// an SPA, so there's no in-memory state to carry over otherwise.
//
// Applied as a plain top-level call below (not inside DOMContentLoaded):
// this script tag sits at the end of <body>, after the <aside> markup has
// already been parsed, so the saved state can be applied immediately with
// no flash of the opposite state on load.

// Toggles every part of the sidebar that needs to shrink, hide its label, or
// re-center when the sidebar is collapsed to an icon-only rail.
function applySidebarState(collapsed) {
  const sidebar = document.getElementById('adminSidebar');
  if (!sidebar) return;

  sidebar.classList.toggle('w-60', !collapsed);
  sidebar.classList.toggle('w-20', collapsed);
  sidebar.classList.toggle('p-5', !collapsed);
  sidebar.classList.toggle('p-3', collapsed);

  document.querySelectorAll('.sidebar-label').forEach(function (el) {
    el.classList.toggle('hidden', collapsed);
  });

  document.querySelectorAll('.sidebar-link').forEach(function (el) {
    el.classList.toggle('justify-center', collapsed);
    el.classList.toggle('px-4', !collapsed);
    el.classList.toggle('px-2', collapsed);
  });

  const logoCard = document.getElementById('sidebarLogoCard');
  if (logoCard) {
    logoCard.classList.toggle('justify-center', collapsed);
    logoCard.classList.toggle('border', !collapsed);
    logoCard.classList.toggle('border-white/10', !collapsed);
    logoCard.classList.toggle('bg-white/10', !collapsed);
    logoCard.classList.toggle('p-3', !collapsed);
  }

  const adminRow = document.getElementById('sidebarAdminRow');
  if (adminRow) {
    adminRow.classList.toggle('justify-center', collapsed);
    adminRow.classList.toggle('px-3', !collapsed);
    adminRow.classList.toggle('px-1', collapsed);
  }

  const toggleIcon = document.getElementById('sidebarToggleIcon');
  if (toggleIcon) toggleIcon.classList.toggle('rotate-180', collapsed);

  const toggleBtn = document.getElementById('sidebarToggleBtn');
  if (toggleBtn) {
    const label = collapsed ? 'Expand sidebar' : 'Collapse sidebar';
    toggleBtn.title = label;
    toggleBtn.setAttribute('aria-label', label);
  }
}

function toggleSidebar() {
  const sidebar = document.getElementById('adminSidebar');
  if (!sidebar) return;
  const collapsed = !sidebar.classList.contains('w-20');
  applySidebarState(collapsed);
  localStorage.setItem('sidebarCollapsed', collapsed ? 'true' : 'false');
}

applySidebarState(localStorage.getItem('sidebarCollapsed') === 'true');
