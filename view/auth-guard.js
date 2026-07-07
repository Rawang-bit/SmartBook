// auth-guard.js — runs immediately on every admin page to redirect unauthenticated users.
// Real security lives in the HttpOnly session cookie; localStorage is a quick UI hint only.
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

  // Temporary-password accounts must change before doing anything else; the server
  // also enforces this on every API call — this redirect just lands them faster.
  if (localStorage.getItem('adminMustResetPassword') === 'true' && !isForceChangePage) {
    window.location.replace('force-password-change.html');
  }
})();
