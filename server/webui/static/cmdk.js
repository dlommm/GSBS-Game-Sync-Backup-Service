// cmdk.js — global command palette (Cmd/Ctrl+K). Vanilla JS, no dependencies
// beyond HTMX (which fetches the result list into #cmdk-results). Adds a
// client-side "Recent" section (localStorage) and action commands (theme
// toggle) on top of the server-rendered results.
(function () {
  var dialog = document.getElementById('cmdk-dialog');
  var input = document.getElementById('cmdk-input');
  if (!dialog || !input) return;
  var results = document.getElementById('cmdk-results');
  var sel = -1;
  var RECENTS_KEY = 'gsbs.cmdk.recents';

  function readRecents() {
    try { return JSON.parse(localStorage.getItem(RECENTS_KEY) || '[]'); } catch (e) { return []; }
  }
  function pushRecent(href, label) {
    if (!href || href.charAt(0) !== '/') return;
    try {
      var list = readRecents().filter(function (r) { return r.href !== href; });
      list.unshift({ href: href, label: label });
      localStorage.setItem(RECENTS_KEY, JSON.stringify(list.slice(0, 5)));
    } catch (e) { /* private mode */ }
  }

  function items() {
    return Array.prototype.slice.call(results.querySelectorAll('.cmdk-item'));
  }
  function highlight() {
    var its = items();
    its.forEach(function (el, i) { el.classList.toggle('selected', i === sel); });
    if (sel >= 0 && its[sel]) its[sel].scrollIntoView({ block: 'nearest' });
  }

  // Prepend Recent pages + built-in actions when the palette opens empty.
  function renderLocalSections() {
    if (input.value.trim() !== '') return;
    var frag = document.createDocumentFragment();
    var recents = readRecents();
    if (recents.length) {
      var h = document.createElement('div');
      h.className = 'cmdk-group-label';
      h.textContent = 'Recent';
      frag.appendChild(h);
      recents.forEach(function (r) {
        var a = document.createElement('a');
        a.className = 'cmdk-item';
        a.href = r.href;
        a.textContent = r.label || r.href;
        frag.appendChild(a);
      });
    }
    var ah = document.createElement('div');
    ah.className = 'cmdk-group-label';
    ah.textContent = 'Actions';
    frag.appendChild(ah);
    var toggle = document.createElement('a');
    toggle.className = 'cmdk-item';
    toggle.href = '#';
    toggle.setAttribute('data-cmdk-action', 'toggle-theme');
    toggle.textContent = 'Toggle light/dark theme';
    frag.appendChild(toggle);
    results.insertBefore(frag, results.firstChild);
  }

  function runItem(target) {
    if (!target) return;
    var action = target.getAttribute('data-cmdk-action');
    if (action === 'toggle-theme') {
      close();
      var btn = document.querySelector('[data-action="toggle-theme"]');
      if (btn) btn.click();
      return;
    }
    var href = target.getAttribute('href');
    pushRecent(href, target.textContent.trim());
    window.location = href;
  }

  function open() {
    if (dialog.open) return;
    dialog.showModal();
    input.value = '';
    sel = -1;
    if (window.htmx) window.htmx.trigger(input, 'cmdk-open');
    setTimeout(function () { input.focus(); }, 0);
  }
  function close() { if (dialog.open) dialog.close(); }

  document.addEventListener('keydown', function (e) {
    if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      dialog.open ? close() : open();
    }
  });

  input.addEventListener('keydown', function (e) {
    var its = items();
    if (e.key === 'ArrowDown') {
      e.preventDefault(); sel = Math.min(sel + 1, its.length - 1); highlight();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault(); sel = Math.max(sel - 1, 0); highlight();
    } else if (e.key === 'Enter') {
      var target = sel >= 0 ? its[sel] : its[0];
      if (target) { e.preventDefault(); runItem(target); }
    }
  });

  // Clicked results also run through runItem (records recents, handles actions).
  results.addEventListener('click', function (e) {
    var item = e.target.closest('.cmdk-item');
    if (!item) return;
    e.preventDefault();
    runItem(item);
  });

  // Reset selection + inject local sections whenever the list is swapped in.
  results.addEventListener('htmx:afterSwap', function () {
    sel = -1;
    renderLocalSections();
  });

  // Click on the dialog backdrop (the dialog element itself) closes it.
  dialog.addEventListener('click', function (e) { if (e.target === dialog) close(); });

  // Any element with [data-cmdk-open] (e.g. the topbar Search button) opens it.
  document.querySelectorAll('[data-cmdk-open]').forEach(function (btn) {
    btn.addEventListener('click', function (e) { e.preventDefault(); open(); });
  });
})();
