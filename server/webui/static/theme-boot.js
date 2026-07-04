// Applies the saved theme before first paint (loaded synchronously in <head>
// as an external file — CSP-safe, no flash of the wrong theme). Dark is the
// default; "light" is opt-in via the topbar toggle.
(function () {
  try {
    if (localStorage.getItem('gsbs.theme') === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
    }
  } catch (e) { /* private mode: keep dark */ }
})();
