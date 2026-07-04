// GSBS WebUI behavior. Everything lives here (and admin.js for admin pages)
// so the Content-Security-Policy can drop 'unsafe-inline' entirely: templates
// carry data-* attributes instead of inline handlers, and dynamic styling
// goes through the CSSOM (data-width-pct) instead of style="" attributes.
(function () {
  'use strict';

  /* ---- Delegated actions (replacements for inline on*= handlers) ---- */

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-open-dialog],[data-close-dialog],[data-set-input],[data-view-value],[data-action="toggle-theme"]');
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

  function initDynamic(root) {
    bindStopPropagation(root);
    applyDataWidths(root);
  }

  /* ---- HTMX wiring (moved from layout.html) ---- */

  function onReady(fn) {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
    else fn();
  }

  onReady(function () {
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

    initActivityTabs();
    initBulkForm();
    initOnboardingTour();
  });

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
    function done() {
      try { localStorage.setItem(KEY, '1'); } catch (err) { /* ignore */ }
      overlay.remove();
      document.removeEventListener('keydown', onKey);
    }
    function onKey(e) { if (e.key === 'Escape') done(); }
    overlay.addEventListener('click', function (e) {
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
  }
})();
