// Shared popup notification, used in place of the old top-of-page/top-of-form
// inline message banners. Auto-dismisses after 5s; the user can also close it early.
(function () {
  function ensureContainer() {
    let container = document.getElementById('toastContainer');
    if (!container) {
      container = document.createElement('div');
      container.id = 'toastContainer';
      container.className = 'fixed top-4 right-4 z-[9999] flex w-full max-w-sm flex-col gap-3';
      document.body.appendChild(container);
    }
    return container;
  }

  window.showToast = function (text, type) {
    const container = ensureContainer();

    const toast = document.createElement('div');
    toast.className =
      'flex items-start gap-3 rounded-xl border px-4 py-3 text-sm font-bold shadow-lg ' +
      (type === 'success'
        ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
        : 'bg-red-50 text-red-700 border-red-200');

    const message = document.createElement('span');
    message.className = 'flex-1';
    message.textContent = text;
    toast.appendChild(message);

    const closeBtn = document.createElement('button');
    closeBtn.type = 'button';
    closeBtn.setAttribute('aria-label', 'Close');
    closeBtn.className = 'shrink-0 text-lg leading-none opacity-60 transition hover:opacity-100';
    closeBtn.textContent = '×';
    toast.appendChild(closeBtn);

    const timer = setTimeout(remove, 5000);

    function remove() {
      clearTimeout(timer);
      toast.remove();
    }

    closeBtn.addEventListener('click', remove);

    container.appendChild(toast);
    return remove;
  };
})();
