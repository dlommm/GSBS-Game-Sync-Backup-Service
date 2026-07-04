// Applies the theme before first paint (loaded synchronously in <head> as an
// external file — CSP-safe, no flash of the wrong theme). Explicit choice via
// the topbar toggle wins; otherwise the OS preference decides; dark is the
// final fallback.
(function () {
  try {
    var stored = localStorage.getItem('gsbs.theme');
    var theme = stored === 'light' || stored === 'dark'
      ? stored
      : (window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
    if (theme === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
    }
  } catch (e) { /* private mode: keep dark */ }
})();
