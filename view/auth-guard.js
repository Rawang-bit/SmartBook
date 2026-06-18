// auth-guard.js — runs immediately on every admin page.
// If the admin does not appear to be logged in, redirect to login.html right away.
//
// How this works with cookie-based sessions:
//   - When the admin logs in, the SERVER sets an HttpOnly session cookie.
//   - HttpOnly means JavaScript cannot read the cookie directly.
//   - So we keep a small "adminLoggedIn" flag in localStorage just for this quick check.
//   - Even if someone sets that flag manually, they still cannot call any protected
//     API — the server will reject requests that do not have a valid session cookie.
(function () {
  const currentPage = window.location.pathname.split('/').pop() || 'dashboard.html';
  const isLoginPage       = currentPage === 'login.html';
  const isPublicPage      = currentPage === '' || currentPage === 'index.html';
  const isForceChangePage = currentPage === 'force-password-change.html';

  // Login and public pages do not need an auth check
  if (isLoginPage || isPublicPage) return;

  // If the localStorage flag is missing, send the user to login
  if (localStorage.getItem('adminLoggedIn') !== 'true') {
    window.location.replace('login.html');
    return;
  }

  // Accounts created with a generated temporary password (admin roles granted
  // via the Users-page approval flow) must replace it before using anything
  // else in the admin panel. The server enforces this on every API call too —
  // this redirect is just so the admin lands on the right page immediately.
  if (localStorage.getItem('adminMustResetPassword') === 'true' && !isForceChangePage) {
    window.location.replace('force-password-change.html');
  }
})();
