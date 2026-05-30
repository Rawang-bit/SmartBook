// sidebar-extras.js — loaded by every admin page.
// Handles role-based Admins nav link visibility and the Change Password modal.

(function () {
  // Show the Admins nav link only for super_admin
  document.addEventListener('DOMContentLoaded', function () {
    const link = document.getElementById('adminsNavLink');
    if (link && localStorage.getItem('adminRole') === 'super_admin') {
      link.classList.remove('hidden');
    }
  });

  // Inject the Change Password modal once, after the page loads
  document.addEventListener('DOMContentLoaded', function () {
    const modal = document.createElement('div');
    modal.id = 'sharedChangePasswordModal';
    modal.className = 'fixed inset-0 z-50 hidden items-center justify-center bg-black/50 backdrop-blur-sm p-4';
    modal.innerHTML = `
      <div class="w-full max-w-md rounded-3xl bg-white p-6 shadow-2xl">
        <h2 class="mb-5 text-xl font-black text-slate-900">Change My Password</h2>
        <div id="sharedCpMsg" class="mb-4 hidden rounded-xl px-4 py-3 font-bold text-sm"></div>
        <form id="sharedCpForm" class="space-y-4">
          <div>
            <label class="block text-xs font-extrabold uppercase tracking-wider text-slate-500 mb-1">Current Password</label>
            <input id="sharedCpCurrent" type="password" required
              class="w-full rounded-xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
              placeholder="Your current password" />
          </div>
          <div>
            <label class="block text-xs font-extrabold uppercase tracking-wider text-slate-500 mb-1">New Password</label>
            <input id="sharedCpNew" type="password" required minlength="6"
              class="w-full rounded-xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
              placeholder="At least 6 characters" />
          </div>
          <div>
            <label class="block text-xs font-extrabold uppercase tracking-wider text-slate-500 mb-1">Confirm New Password</label>
            <input id="sharedCpConfirm" type="password" required minlength="6"
              class="w-full rounded-xl border border-slate-200 px-4 py-3 text-sm font-semibold text-slate-900 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
              placeholder="Re-enter new password" />
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

    // Close on backdrop click
    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeSharedCpModal();
    });

    document.getElementById('sharedCpForm').addEventListener('submit', async function (e) {
      e.preventDefault();
      const current = document.getElementById('sharedCpCurrent').value;
      const newPw   = document.getElementById('sharedCpNew').value;
      const confirm = document.getElementById('sharedCpConfirm').value;
      const msgEl   = document.getElementById('sharedCpMsg');
      const btn     = document.getElementById('sharedCpSubmit');

      if (newPw !== confirm) {
        showSharedCpMsg('New passwords do not match', 'error');
        return;
      }

      btn.disabled = true;
      btn.textContent = 'Updating…';

      try {
        await changeOwnPasswordApi(current, newPw);
        showSharedCpMsg('Password changed! Logging you out…', 'success');
        setTimeout(() => logout(), 2000);
      } catch (err) {
        showSharedCpMsg(err.message, 'error');
        btn.disabled = false;
        btn.textContent = 'Update Password';
      }
    });
  });
})();

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
  el.className = `mb-4 rounded-xl px-4 py-3 font-bold text-sm ${type === 'success'
    ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
    : 'bg-red-50 text-red-700 border border-red-200'}`;
  el.textContent = text;
  el.classList.remove('hidden');
}
