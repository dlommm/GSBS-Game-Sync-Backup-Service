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

  // Appearance (color scheme + layout token layers). Signed-in pages carry
  // server-rendered data-design/data-layout from the account's prefs — that
  // is authoritative; only an explicit ?design=/?layout= preview overrides
  // it (without persisting). On anonymous pages (login, docker previews)
  // the param persists, then the stored choice, then the <meta> default.
  try {
    var applyAxis = function (attr, param, storageKey, metaName, names, defaultName) {
      var root = document.documentElement;
      var serverSet = root.hasAttribute(attr);
      var m = new RegExp('[?&]' + param + '=([a-z]+)').exec(window.location.search);
      var value = null;
      if (m && names.indexOf(m[1]) >= 0) {
        value = m[1];
        if (!serverSet) {
          try { localStorage.setItem(storageKey, value); } catch (e2) { /* private mode */ }
        }
      } else if (!serverSet) {
        var saved = null;
        try { saved = localStorage.getItem(storageKey); } catch (e2) { saved = null; }
        if (saved && names.indexOf(saved) >= 0) {
          value = saved;
        } else {
          var meta = document.querySelector('meta[name="' + metaName + '"]');
          if (meta && names.indexOf(meta.content) >= 0) value = meta.content;
        }
      }
      if (value === null) return;
      if (value === defaultName) {
        root.removeAttribute(attr);
      } else {
        root.setAttribute(attr, value);
      }
    };
    applyAxis('data-design', 'design', 'gsbs.design', 'gsbs-design',
      ['default', 'hud', 'crt', 'hearth', 'synth', 'slate'], 'default');
    applyAxis('data-layout', 'layout', 'gsbs.layout', 'gsbs-layout',
      ['sidebar', 'topnav', 'dense', 'library'], 'sidebar');
  } catch (e) { /* keep default look */ }
})();
