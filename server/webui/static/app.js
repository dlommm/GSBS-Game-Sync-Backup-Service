// GSBS WebUI behavior. Everything lives here (and admin.js for admin pages)
// so the Content-Security-Policy can drop 'unsafe-inline' entirely: templates
// carry data-* attributes instead of inline handlers, and dynamic styling
// goes through the CSSOM (data-width-pct) instead of style="" attributes.
(function () {
  'use strict';

  /* ---- Delegated actions (replacements for inline on*= handlers) ---- */

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-open-dialog],[data-close-dialog],[data-set-input],[data-view-value],[data-action="toggle-theme"],[data-action="toggle-sidebar"]');
    if (!el) return;

    if (el.hasAttribute('data-open-dialog')) {
      if (el.hasAttribute('data-preview-title')) {
        var t = document.getElementById('preview-title');
        if (t) t.textContent = el.getAttribute('data-preview-title');
      }
      var dlg = document.getElementById(el.getAttribute('data-open-dialog'));
      if (dlg && dlg.showModal) dlg.showModal();
    }
    if (el.hasAttribute('data-close-dialog')) {
      var d = el.closest('dialog');
      if (d) d.close();
    }
    if (el.hasAttribute('data-set-input')) {
      var input = document.getElementById(el.getAttribute('data-set-input'));
      if (input) input.value = el.getAttribute('data-value') || '';
    }
    if (el.hasAttribute('data-view-value')) {
      var hidden = document.getElementById('games-view');
      if (hidden) hidden.value = el.getAttribute('data-view-value');
      if (el.parentNode) {
        el.parentNode.querySelectorAll('.view-btn').forEach(function (b) { b.classList.remove('active'); });
      }
      el.classList.add('active');
    }
    if (el.getAttribute('data-action') === 'toggle-theme') {
      var root = document.documentElement;
      var next = root.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('gsbs.theme', next); } catch (err) { /* private mode */ }
      syncThemeColorMeta();
    }
    if (el.getAttribute('data-action') === 'toggle-sidebar') {
      document.body.classList.toggle('sidebar-open');
    }
  });

  // Off-canvas sidebar closes on Escape (mobile drawer).
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') document.body.classList.remove('sidebar-open');
  });

  // Keep the browser-chrome color in step with the active theme.
  function syncThemeColorMeta() {
    var light = document.documentElement.getAttribute('data-theme') === 'light';
    document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
      m.setAttribute('content', light ? '#6cc7b8' : '#2fa693');
      m.removeAttribute('media');
    });
  }

  /* ---- Clipboard: <button data-copy="text"> or data-copy-target="#sel" ---- */

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-copy],[data-copy-target]');
    if (!el) return;
    var text = el.getAttribute('data-copy');
    if (!text) {
      var target = document.querySelector(el.getAttribute('data-copy-target') || '');
      if (target) text = target.value !== undefined && target.value !== '' ? target.value : target.textContent.trim();
    }
    if (!text || !navigator.clipboard) return;
    navigator.clipboard.writeText(text).then(function () {
      if (window.gsbs && window.gsbs.toast) window.gsbs.toast('Copied to clipboard', 'info', 2000);
    }, function () {
      if (window.gsbs && window.gsbs.toast) window.gsbs.toast('Copy failed — select and copy manually', 'error');
    });
  });

  /* ---- Password fields: show/hide, strength meter, confirm match ---- */

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-password-toggle]');
    if (!btn) return;
    var input = document.getElementById(btn.getAttribute('data-password-toggle'));
    if (!input) return;
    var show = input.type === 'password';
    input.type = show ? 'text' : 'password';
    btn.setAttribute('aria-pressed', show ? 'true' : 'false');
    btn.classList.toggle('showing', show);
  });

  function passwordScore(v) {
    if (!v) return 0;
    var score = 0;
    if (v.length >= 8) score++;
    if (v.length >= 12) score++;
    var variety = 0;
    if (/[a-z]/.test(v)) variety++;
    if (/[A-Z]/.test(v)) variety++;
    if (/[0-9]/.test(v)) variety++;
    if (/[^A-Za-z0-9]/.test(v)) variety++;
    if (variety >= 2) score++;
    if (variety >= 3 && v.length >= 10) score++;
    return score; // 0..4
  }

  document.addEventListener('input', function (e) {
    var el = e.target;
    // Strength meter: <div data-strength-for="input-id"> with .strength-bar inside
    document.querySelectorAll('[data-strength-for="' + el.id + '"]').forEach(function (meter) {
      var score = passwordScore(el.value);
      var labels = ['', 'Weak', 'Fair', 'Good', 'Strong'];
      meter.setAttribute('data-score', String(score));
      var label = meter.querySelector('.strength-label');
      if (label) label.textContent = el.value ? labels[score] || 'Weak' : '';
    });
    // Confirm-match: input with data-match-of="other-input-id"
    var pairs = [];
    if (el.hasAttribute('data-match-of')) pairs.push(el);
    document.querySelectorAll('[data-match-of="' + el.id + '"]').forEach(function (other) { pairs.push(other); });
    pairs.forEach(function (confirm) {
      var source = document.getElementById(confirm.getAttribute('data-match-of'));
      if (!source) return;
      if (!confirm.value) { confirm.setCustomValidity(''); confirm.classList.remove('input-mismatch'); return; }
      var ok = confirm.value === source.value;
      confirm.setCustomValidity(ok ? '' : 'Passwords do not match');
      confirm.classList.toggle('input-mismatch', !ok);
    });
  });

  /* ---- Client-side table sorting: <th data-sortable [data-sort-numeric]> ---- */

  document.addEventListener('click', function (e) {
    var th = e.target.closest('th[data-sortable]');
    if (!th) return;
    var table = th.closest('table');
    var tbody = table && table.tBodies[0];
    if (!tbody) return;
    var idx = Array.prototype.indexOf.call(th.parentNode.children, th);
    var dir = th.getAttribute('aria-sort') === 'ascending' ? -1 : 1;
    th.parentNode.querySelectorAll('th[data-sortable]').forEach(function (o) { o.removeAttribute('aria-sort'); });
    th.setAttribute('aria-sort', dir === 1 ? 'ascending' : 'descending');
    var numeric = th.hasAttribute('data-sort-numeric');
    var rows = Array.prototype.slice.call(tbody.rows);
    rows.sort(function (a, b) {
      var av = (a.cells[idx] ? a.cells[idx].textContent : '').trim();
      var bv = (b.cells[idx] ? b.cells[idx].textContent : '').trim();
      if (numeric) {
        var an = parseFloat(av.replace(/[^0-9.+-]/g, '')) || 0;
        var bn = parseFloat(bv.replace(/[^0-9.+-]/g, '')) || 0;
        return (an - bn) * dir;
      }
      return av.localeCompare(bv, undefined, { numeric: true, sensitivity: 'base' }) * dir;
    });
    rows.forEach(function (r) { tbody.appendChild(r); });
  });

  /* ---- ARIA tablists: arrow-key navigation (roving tabindex) ---- */

  document.addEventListener('keydown', function (e) {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight' && e.key !== 'Home' && e.key !== 'End') return;
    var tab = e.target.closest('[role="tab"]');
    if (!tab) return;
    var list = tab.closest('[role="tablist"]');
    if (!list) return;
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
    var i = tabs.indexOf(tab);
    var next = i;
    if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
    if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
    if (e.key === 'Home') next = 0;
    if (e.key === 'End') next = tabs.length - 1;
    if (next !== i) {
      e.preventDefault();
      tabs[next].focus();
      tabs[next].click();
    }
  });

  /* ---- Keyboard shortcuts + "?" help overlay ---- */

  var NAV_CHORDS = { d: '/dashboard', g: '/dashboard/games', i: '/dashboard/analytics', s: '/dashboard/settings', v: '/dashboard/clients', c: '/dashboard/conflicts' };
  var chordArmed = false;
  var chordTimer = null;

  function inEditable(el) {
    if (!el) return false;
    var tag = (el.tagName || '').toLowerCase();
    return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable;
  }

  function shortcutsDialog() {
    var dlg = document.getElementById('gsbs-shortcuts');
    if (dlg) return dlg;
    dlg = document.createElement('dialog');
    dlg.id = 'gsbs-shortcuts';
    dlg.className = 'shortcuts-dialog';
    dlg.setAttribute('aria-label', 'Keyboard shortcuts');
    dlg.innerHTML = '<div class="dialog-body"><div class="dialog-head"><h3 class="u-m0">Keyboard shortcuts</h3></div>' +
      '<dl class="shortcuts-list">' +
      '<div><dt><kbd>⌘K</kbd> / <kbd>Ctrl K</kbd></dt><dd>Search &amp; command palette</dd></div>' +
      '<div><dt><kbd>g</kbd> then <kbd>d</kbd></dt><dd>Dashboard</dd></div>' +
      '<div><dt><kbd>g</kbd> then <kbd>g</kbd></dt><dd>My Games</dd></div>' +
      '<div><dt><kbd>g</kbd> then <kbd>i</kbd></dt><dd>Insights</dd></div>' +
      '<div><dt><kbd>g</kbd> then <kbd>v</kbd></dt><dd>Devices</dd></div>' +
      '<div><dt><kbd>g</kbd> then <kbd>s</kbd></dt><dd>Settings</dd></div>' +
      '<div><dt><kbd>?</kbd></dt><dd>This help</dd></div>' +
      '<div><dt><kbd>Esc</kbd></dt><dd>Close dialogs</dd></div>' +
      '</dl><div class="dialog-actions"><button type="button" class="btn-ghost-sm" data-close-dialog>Close</button></div></div>';
    document.body.appendChild(dlg);
    return dlg;
  }

  document.addEventListener('keydown', function (e) {
    if (inEditable(e.target) || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === '?') {
      e.preventDefault();
      var dlg = shortcutsDialog();
      if (dlg.showModal && !dlg.open) dlg.showModal();
      return;
    }
    if (chordArmed) {
      chordArmed = false;
      clearTimeout(chordTimer);
      var dest = NAV_CHORDS[e.key.toLowerCase()];
      if (dest) {
        e.preventDefault();
        window.location.href = dest;
      }
      return;
    }
    if (e.key.toLowerCase() === 'g') {
      chordArmed = true;
      chordTimer = setTimeout(function () { chordArmed = false; }, 1200);
    }
  });

  // Form guards: submit events bubble, so one listener covers all forms
  // (including HTMX-swapped content).
  document.addEventListener('submit', function (e) {
    var f = e.target;
    if (f.hasAttribute && f.hasAttribute('data-prevent-submit')) {
      e.preventDefault();
      return;
    }
    var msg = f.getAttribute && f.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) e.preventDefault();
  });

  document.addEventListener('change', function (e) {
    var el = e.target.closest('[data-submit-on-change]');
    if (!el || !el.form) return;
    if (el.type === 'file' && !el.files.length) return;
    el.form.submit();
  });

  // Cover images fall back to the generated tile: error events don't bubble,
  // so listen in the capture phase.
  document.addEventListener('error', function (e) {
    var img = e.target;
    if (img && img.classList && img.classList.contains('cover-img')) img.remove();
  }, true);

  // Forms inside <summary> rows must not toggle the row when clicked
  // (stopPropagation has to happen ON the element, so bind directly and
  // re-bind after HTMX swaps).
  function bindStopPropagation(root) {
    (root || document).querySelectorAll('[data-stop-propagation]').forEach(function (el) {
      if (el.__gsbsStop) return;
      el.__gsbsStop = true;
      el.addEventListener('click', function (e) { e.stopPropagation(); });
    });
  }

  // Dynamic widths (progress/quota bars): CSSOM writes are CSP-legal where
  // style="" attributes are not.
  function applyDataWidths(root) {
    (root || document).querySelectorAll('[data-width-pct]').forEach(function (el) {
      var v = parseFloat(el.getAttribute('data-width-pct'));
      if (!isNaN(v)) el.style.width = Math.max(0, Math.min(100, v)) + '%';
    });
  }

  /* ---- Table customization: <table data-table-id> gets a "Table" menu ----
     Per-column show/hide + compact rows + zebra stripes, persisted per table
     in localStorage. Menus are (re)built after HTMX swaps via initDynamic. */

  function tablePrefs(id) {
    try { return JSON.parse(localStorage.getItem('gsbs.table.' + id) || '{}') || {}; } catch (e) { return {}; }
  }
  function saveTablePrefs(id, p) {
    try { localStorage.setItem('gsbs.table.' + id, JSON.stringify(p)); } catch (e) { /* private mode */ }
  }

  function applyTablePrefs(table) {
    var id = table.getAttribute('data-table-id');
    if (!id) return;
    var p = tablePrefs(id);
    var hidden = p.cols || [];
    var rows = [];
    if (table.tHead && table.tHead.rows[0]) rows.push(table.tHead.rows[0]);
    if (table.tBodies[0]) rows = rows.concat(Array.prototype.slice.call(table.tBodies[0].rows));
    rows.forEach(function (row) {
      Array.prototype.forEach.call(row.cells, function (cell, i) {
        cell.classList.toggle('col-hidden', hidden.indexOf(i) !== -1);
      });
    });
    table.classList.toggle('table-compact', !!p.compact);
    table.classList.toggle('table-zebra', !!p.zebra);
  }

  function buildTableMenu(table) {
    var id = table.getAttribute('data-table-id');
    var wrap = table.closest('.table-wrap');
    if (!id || !wrap || !table.tHead || !table.tHead.rows[0]) return;
    var parent = wrap.parentNode;
    // Remove a stale menu from a previous render of this region.
    var old = parent.querySelector('.table-tools[data-for="' + id + '"]');
    if (old) old.remove();

    var p = tablePrefs(id);
    var tools = document.createElement('div');
    tools.className = 'table-tools';
    tools.setAttribute('data-for', id);
    var menu = document.createElement('details');
    menu.className = 'action-menu table-tools-menu';
    var summary = document.createElement('summary');
    summary.className = 'btn-ghost-sm';
    summary.textContent = 'Table ⚙';
    menu.appendChild(summary);
    var panel = document.createElement('div');
    panel.className = 'action-menu-panel';

    function addCheckbox(label, checked, onChange) {
      var lab = document.createElement('label');
      lab.className = 'checkbox-label';
      var cb = document.createElement('input');
      cb.type = 'checkbox';
      cb.checked = checked;
      cb.addEventListener('change', onChange);
      lab.appendChild(cb);
      lab.appendChild(document.createTextNode(' ' + label));
      panel.appendChild(lab);
      return cb;
    }

    var title = document.createElement('div');
    title.className = 'table-tools-title';
    title.textContent = 'Columns';
    panel.appendChild(title);
    Array.prototype.forEach.call(table.tHead.rows[0].cells, function (th, i) {
      var name = (th.textContent || '').trim() || ('Column ' + (i + 1));
      addCheckbox(name, (p.cols || []).indexOf(i) === -1, function () {
        var cur = tablePrefs(id);
        var cols = cur.cols || [];
        var at = cols.indexOf(i);
        if (at === -1) cols.push(i); else cols.splice(at, 1);
        cur.cols = cols;
        saveTablePrefs(id, cur);
        applyTablePrefs(table);
      });
    });
    var sep = document.createElement('div');
    sep.className = 'table-tools-sep';
    panel.appendChild(sep);
    var title2 = document.createElement('div');
    title2.className = 'table-tools-title';
    title2.textContent = 'Display';
    panel.appendChild(title2);
    addCheckbox('Compact rows', !!p.compact, function () {
      var cur = tablePrefs(id);
      cur.compact = !cur.compact;
      saveTablePrefs(id, cur);
      applyTablePrefs(table);
    });
    addCheckbox('Zebra stripes', !!p.zebra, function () {
      var cur = tablePrefs(id);
      cur.zebra = !cur.zebra;
      saveTablePrefs(id, cur);
      applyTablePrefs(table);
    });

    menu.appendChild(panel);
    tools.appendChild(menu);
    parent.insertBefore(tools, wrap);
  }

  function initTables(root) {
    var scope = root || document;
    var tables = scope.querySelectorAll ? scope.querySelectorAll('table[data-table-id]') : [];
    Array.prototype.forEach.call(tables, function (table) {
      buildTableMenu(table);
      applyTablePrefs(table);
    });
  }

  // Close table menus when clicking elsewhere.
  document.addEventListener('click', function (e) {
    if (e.target.closest('details.table-tools-menu')) return;
    document.querySelectorAll('details.table-tools-menu[open]').forEach(function (m) { m.open = false; });
  });

  /* ---- Shared table pager: per-page select (partials/table_pager.html) ---- */

  document.addEventListener('change', function (e) {
    var sel = e.target.closest('[data-pager-per]');
    if (!sel || !window.htmx) return;
    var base = sel.getAttribute('data-pager-base') || '';
    var target = sel.getAttribute('data-pager-target');
    if (!target) return;
    window.htmx.ajax('GET', base + 'per=' + encodeURIComponent(sel.value) + '&page=1', { target: target, swap: 'innerHTML' });
  });

  function initDynamic(root) {
    bindStopPropagation(root);
    applyDataWidths(root);
    initTables(root);
  }

  /* ---- HTMX wiring (moved from layout.html) ---- */

  function onReady(fn) {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
    else fn();
  }

  onReady(function () {
    syncThemeColorMeta();
    initDynamic(document);
    document.body.addEventListener('htmx:configRequest', function (evt) {
      evt.detail.credentials = 'same-origin';
    });
    document.body.addEventListener('htmx:afterSwap', function (evt) {
      initDynamic(evt.target);
    });
    document.body.addEventListener('audit-updated', function (evt) {
      if (window.gsbs && window.gsbs.toast) {
        var msg = (evt.detail && evt.detail.data) ? evt.detail.data : 'Activity log updated';
        window.gsbs.toast(msg, 'info', 3000);
      }
    });
    document.body.addEventListener('client-activity', onClientActivity);

    initActivityTabs();
    initBulkForm();
    initOnboardingTour();
    initSetupWizard();
  });

  /* ---- Live Sync Pulse (v5.1): dashboard rail stream + per-device flash ---- */

  var PULSE_MAX = 8;
  var syncingTimers = {};

  function onClientActivity(evt) {
    var d = {};
    try { d = JSON.parse((evt.detail && evt.detail.data) || '{}'); } catch (err) { return; }
    if (!d.client_id) return;

    // Flash "syncing now" on any device row/card for this client (~10s).
    document.querySelectorAll('[data-client-id="' + d.client_id + '"]').forEach(function (el) {
      el.classList.add('row-syncing');
      if (syncingTimers[d.client_id]) clearTimeout(syncingTimers[d.client_id]);
      syncingTimers[d.client_id] = setTimeout(function () {
        document.querySelectorAll('[data-client-id="' + d.client_id + '"]').forEach(function (e2) {
          e2.classList.remove('row-syncing');
        });
      }, 10000);
    });

    // Prepend to the Live activity stream when the panel exists (dashboard).
    var list = document.getElementById('sync-pulse');
    if (!list) return;
    var empty = list.querySelector('.sync-pulse-empty');
    if (empty) empty.remove();

    var deviceEl = document.querySelector('[data-client-id="' + d.client_id + '"][data-client-name]');
    var device = deviceEl ? deviceEl.getAttribute('data-client-name') : 'A device';
    var game = d.game_title || d.game_id || 'a save';
    var when = 'just now';
    if (d.at) {
      var t = new Date(d.at);
      if (!isNaN(t)) when = t.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    }

    var li = document.createElement('li');
    li.className = 'sync-pulse-item';
    var dot = document.createElement('span');
    dot.className = 'pulse-dot';
    dot.setAttribute('aria-hidden', 'true');
    var text = document.createElement('span');
    text.className = 'sync-pulse-text';
    var strongDev = document.createElement('strong');
    strongDev.textContent = device;
    text.appendChild(strongDev);
    text.appendChild(document.createTextNode(' synced '));
    var strongGame = document.createElement('strong');
    strongGame.textContent = game;
    text.appendChild(strongGame);
    var meta = document.createElement('span');
    meta.className = 'sync-pulse-when';
    meta.textContent = when;
    li.appendChild(dot);
    li.appendChild(text);
    li.appendChild(meta);
    list.insertBefore(li, list.firstChild);
    while (list.children.length > PULSE_MAX) list.removeChild(list.lastChild);

    var indicator = document.getElementById('pulse-indicator');
    if (indicator) {
      indicator.hidden = false;
      clearTimeout(indicator._t);
      indicator._t = setTimeout(function () { indicator.hidden = true; }, 10000);
    }
  }

  /* ---- First-run setup wizard: stepped form (setup.html) ---- */

  function initSetupWizard() {
    var form = document.getElementById('setup-wizard-form');
    if (!form) return;
    var steps = Array.prototype.slice.call(form.querySelectorAll('.setup-step'));
    var labels = Array.prototype.slice.call(document.querySelectorAll('#setup-progress .wizard-step'));
    var backBtn = form.querySelector('[data-wizard-back]');
    var nextBtn = form.querySelector('[data-wizard-next]');
    var submitBtn = form.querySelector('[data-wizard-submit]');
    var cur = 0;

    function validateStep(i) {
      var inputs = steps[i].querySelectorAll('input, select, textarea');
      for (var j = 0; j < inputs.length; j++) {
        if (!inputs[j].checkValidity()) {
          inputs[j].reportValidity();
          return false;
        }
      }
      return true;
    }

    function fillReview() {
      var get = function (name) { return form.querySelector('[name="' + name + '"]'); };
      var set = function (key, val) {
        var dd = form.querySelector('[data-review="' + key + '"]');
        if (dd) dd.textContent = val;
      };
      set('username', (get('username') || {}).value || '—');
      set('allow_register', get('allow_register') && get('allow_register').checked ? 'On — anyone can register' : 'Off — admin creates users');
      var quota = (get('storage_quota_gb') || {}).value;
      set('storage_quota_gb', quota ? quota + ' GB' : 'Unlimited');
      set('enable_backups', get('enable_backups') && get('enable_backups').checked ? 'On — nightly' : 'Off');
      var hook = (get('notify_webhook_url') || {}).value;
      set('notify_webhook_url', hook || '—');
    }

    function show(i) {
      cur = i;
      steps.forEach(function (s, j) { s.classList.toggle('active', j === i); });
      labels.forEach(function (l, j) {
        l.className = 'wizard-step ' + (j < i ? 'wizard-step-done' : j === i ? 'wizard-step-active' : 'wizard-step-pending');
      });
      backBtn.hidden = i === 0;
      nextBtn.hidden = i === steps.length - 1;
      submitBtn.hidden = i !== steps.length - 1;
      if (i === steps.length - 1) fillReview();
      var focusable = steps[i].querySelector('input, select, textarea');
      if (focusable) focusable.focus();
    }

    nextBtn.addEventListener('click', function () {
      if (!validateStep(cur)) return;
      show(Math.min(cur + 1, steps.length - 1));
    });
    backBtn.addEventListener('click', function () { show(Math.max(cur - 1, 0)); });
    // Enter on a non-final step advances instead of submitting half a form.
    form.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && cur < steps.length - 1 && e.target.tagName !== 'TEXTAREA' && e.target.type !== 'submit' && e.target.type !== 'button') {
        e.preventDefault();
        nextBtn.click();
      }
    });
    show(0);
  }

  /* ---- Dashboard: activity feed tabs ---- */

  function initActivityTabs() {
    var tabs = document.getElementById('activity-tabs');
    var feed = document.getElementById('activity-feed');
    var empty = document.getElementById('activity-tab-empty');
    if (!tabs || !feed) return;
    var cur = 'all';
    function apply() {
      var items = feed.querySelectorAll('.timeline-item');
      var shown = 0;
      items.forEach(function (li) {
        var match = cur === 'all' || li.getAttribute('data-cat') === cur;
        li.style.display = match ? '' : 'none';
        if (match) shown++;
      });
      if (empty) empty.hidden = !(items.length > 0 && shown === 0);
    }
    tabs.addEventListener('click', function (e) {
      var b = e.target.closest('.tab');
      if (!b) return;
      cur = b.getAttribute('data-cat');
      tabs.querySelectorAll('.tab').forEach(function (t) {
        var on = t === b;
        t.classList.toggle('active', on);
        t.setAttribute('aria-selected', on ? 'true' : 'false');
      });
      apply();
    });
    feed.addEventListener('htmx:afterSwap', apply);
  }

  /* ---- My Games: bulk selection ---- */

  function initBulkForm() {
    var form = document.getElementById('bulk-form');
    var bar = document.getElementById('bulk-bar');
    var count = document.getElementById('bulk-count');
    var grid = document.getElementById('games-grid');
    if (!form || !bar) return;
    function checks() { return Array.prototype.slice.call(form.querySelectorAll('.bulk-check')); }
    function refresh() {
      var n = checks().filter(function (c) { return c.checked; }).length;
      count.textContent = n + ' selected';
      bar.hidden = n === 0;
    }
    form.addEventListener('change', function (e) {
      if (e.target.classList.contains('bulk-select-all')) {
        var on = e.target.checked;
        checks().forEach(function (c) { c.checked = on; });
      }
      if (e.target.classList.contains('bulk-check') || e.target.classList.contains('bulk-select-all')) refresh();
    });
    form.addEventListener('click', function (e) {
      if (e.target.matches('[data-bulk-clear]')) {
        checks().forEach(function (c) { c.checked = false; });
        var all = form.querySelector('.bulk-select-all');
        if (all) all.checked = false;
        refresh();
      }
    });
    form.addEventListener('submit', function (e) {
      var n = checks().filter(function (c) { return c.checked; }).length;
      if (n === 0) { e.preventDefault(); return; }
      if (!confirm('Delete ALL saves for ' + n + ' game' + (n === 1 ? '' : 's') + '? This cannot be undone.')) e.preventDefault();
    });
    if (grid) grid.addEventListener('htmx:afterSwap', refresh);
  }

  /* ---- First-login onboarding tour ---- */

  function initOnboardingTour() {
    var mount = document.getElementById('gsbs-tour');
    if (!mount) return; // only the dashboard offers the tour
    var KEY = 'gsbs.tour.done';
    try { if (localStorage.getItem(KEY) === '1') return; } catch (err) { return; }

    var steps = [
      { title: 'Welcome to GSBS', body: 'Your game saves are synced and versioned here. This 30-second tour shows you around — press Esc anytime to skip.' },
      { title: 'Storage at a glance', body: 'The dashboard shows connected devices, synced saves, and your storage usage including version history.' },
      { title: 'Connect your PCs', body: 'Install the GSBS client on each machine and run “gsbs-client login”. Devices appear under Devices, and saves flow automatically.' },
      { title: 'My Games', body: 'Browse every synced game with cover art, drill into files, restore old versions, and export real save archives.' },
      { title: 'Insights & Settings', body: 'Insights charts your sync activity and warns about stale devices. Settings has 2FA, end-to-end encryption, sessions, and personal notifications.' }
    ];
    var i = 0;
    var overlay = document.createElement('div');
    overlay.className = 'tour-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-label', 'Getting started tour');
    overlay.innerHTML = '<div class="tour-card">' +
      '<h3 class="tour-title"></h3><p class="tour-body"></p>' +
      '<div class="tour-nav"><span class="tour-progress"></span>' +
      '<span class="tour-buttons"><button type="button" class="btn-ghost-sm" data-tour-skip>Skip</button>' +
      '<button type="button" class="btn-primary-sm" data-tour-next>Next</button></span></div></div>';
    function render() {
      overlay.querySelector('.tour-title').textContent = steps[i].title;
      overlay.querySelector('.tour-body').textContent = steps[i].body;
      overlay.querySelector('.tour-progress').textContent = (i + 1) + ' / ' + steps.length;
      overlay.querySelector('[data-tour-next]').textContent = i === steps.length - 1 ? 'Done' : 'Next';
    }
    var restoreFocus = document.activeElement;
    function done() {
      try { localStorage.setItem(KEY, '1'); } catch (err) { /* ignore */ }
      overlay.remove();
      document.removeEventListener('keydown', onKey);
      if (restoreFocus && restoreFocus.focus) restoreFocus.focus();
    }
    function onKey(e) {
      if (e.key === 'Escape') { done(); return; }
      // Focus trap: keep Tab cycling inside the tour card.
      if (e.key === 'Tab') {
        var focusables = overlay.querySelectorAll('button');
        if (!focusables.length) return;
        var first = focusables[0];
        var last = focusables[focusables.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        } else if (!overlay.contains(document.activeElement)) {
          e.preventDefault();
          first.focus();
        }
      }
    }
    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) { done(); return; } // backdrop click dismisses
      if (e.target.hasAttribute('data-tour-skip')) done();
      if (e.target.hasAttribute('data-tour-next')) {
        if (i >= steps.length - 1) { done(); return; }
        i++;
        render();
      }
    });
    document.addEventListener('keydown', onKey);
    render();
    mount.appendChild(overlay);
    var nextBtn = overlay.querySelector('[data-tour-next]');
    if (nextBtn) nextBtn.focus();
  }
})();
