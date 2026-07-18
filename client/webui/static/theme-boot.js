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

  // Design variant (token layer): ?design= wins and persists, then the
  // stored choice, then the server default (<meta name="gsbs-design">,
  // set via GSBS_DESIGN). Unknown names fall back to the default look.
  try {
    var DESIGNS = ['default', 'hud', 'crt', 'hearth', 'synth', 'slate'];
    var design = null;
    var m = /[?&]design=([a-z]+)/.exec(window.location.search);
    if (m && DESIGNS.indexOf(m[1]) >= 0) {
      design = m[1];
      localStorage.setItem('gsbs.design', design);
    }
    if (!design) {
      var saved = localStorage.getItem('gsbs.design');
      if (saved && DESIGNS.indexOf(saved) >= 0) design = saved;
    }
    if (!design) {
      var meta = document.querySelector('meta[name="gsbs-design"]');
      if (meta && DESIGNS.indexOf(meta.content) >= 0) design = meta.content;
    }
    if (design && design !== 'default') {
      document.documentElement.setAttribute('data-design', design);
    }
  } catch (e) { /* keep default look */ }
})();
