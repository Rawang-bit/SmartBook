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
  const isLoginPage  = currentPage === 'login.html';
  const isPublicPage = currentPage === '' || currentPage === 'index.html';

  // Login and public pages do not need an auth check
  if (isLoginPage || isPublicPage) return;

  // If the localStorage flag is missing, send the user to login
  if (localStorage.getItem('adminLoggedIn') !== 'true') {
    window.location.replace('login.html');
  }
})();
