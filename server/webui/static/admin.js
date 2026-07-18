// Admin-page behavior (loaded by the admin shell in addition to app.js).
(function () {
  'use strict';

  function onReady(fn) {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
    else fn();
  }

  /* ---- Sidebar nav groups (collapse state persisted) ---- */

  function initNavGroups() {
    var KEY = 'gsbs.admin.nav.';
    document.querySelectorAll('.admin-nav-group').forEach(function (group) {
      var name = group.getAttribute('data-group');
      var btn = group.querySelector('.admin-group-toggle');
      if (!btn) return;
      try {
        if (localStorage.getItem(KEY + name) === '0') {
          group.classList.add('collapsed');
          btn.setAttribute('aria-expanded', 'false');
        }
      } catch (e) { /* private mode */ }
      btn.addEventListener('click', function () {
        var collapsed = group.classList.toggle('collapsed');
        btn.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
        try { localStorage.setItem(KEY + name, collapsed ? '0' : '1'); } catch (e) { /* ignore */ }
      });
    });
  }

  /* ---- Users page: quota dialog ---- */

  function initQuotaDialog() {
    var GB = 1024 * 1024 * 1024;
    document.addEventListener('click', function (e) {
      var open = e.target.closest('[data-quota-open]');
      if (open) {
        var id = document.getElementById('quota-user-id');
        var name = document.getElementById('quota-dialog-user');
        var gb = document.getElementById('quota_gb');
        if (id) id.value = open.getAttribute('data-user-id') || '';
        if (name) name.textContent = open.getAttribute('data-username') || '';
        if (gb) {
          var bytes = parseInt(open.getAttribute('data-quota') || '0', 10) || 0;
          gb.value = bytes === 0 ? '0' : String(Math.round((bytes / GB) * 2) / 2);
        }
        var dlg = document.getElementById('quota-dialog');
        if (dlg && dlg.showModal) dlg.showModal();
        return;
      }
      var preset = e.target.closest('[data-quota-preset]');
      if (preset) {
        var input = document.getElementById('quota_gb');
        if (input) input.value = preset.getAttribute('data-quota-preset');
      }
    });
    // The handler still receives bytes: convert the GB field on submit.
    document.addEventListener('submit', function (e) {
      var form = e.target;
      if (!form.querySelector || !form.querySelector('#quota_gb')) return;
      var gb = parseFloat(form.querySelector('#quota_gb').value || '0') || 0;
      var bytesField = form.querySelector('#quota_bytes');
      if (bytesField) bytesField.value = String(Math.round(gb * GB));
    });
  }

  /* ---- Users page: action-menu dropdown positioning ---- */

  function resetActionMenuPanel(panel) {
    panel.classList.remove('is-fixed');
    panel.style.top = '';
    panel.style.left = '';
    panel.style.right = '';
    panel.style.bottom = '';
  }

  function positionActionMenu(menu) {
    var panel = menu.querySelector('.action-menu-panel');
    var summary = menu.querySelector('summary');
    if (!panel || !summary) return;
    resetActionMenuPanel(panel);
    if (!menu.open) return;
    panel.classList.add('is-fixed');
    var rect = summary.getBoundingClientRect();
    var gap = 4;
    var left = rect.right - panel.offsetWidth;
    if (left < 8) left = 8;
    if (left + panel.offsetWidth > window.innerWidth - 8) {
      left = window.innerWidth - panel.offsetWidth - 8;
    }
    var top = rect.bottom + gap;
    if (top + panel.offsetHeight > window.innerHeight - 8) {
      top = rect.top - panel.offsetHeight - gap;
    }
    if (top < 8) top = 8;
    panel.style.left = left + 'px';
    panel.style.top = top + 'px';
  }

  function initActionMenus() {
    document.querySelectorAll('details.action-menu').forEach(function (menu) {
      if (menu.dataset.gsbsBound) return;
      menu.dataset.gsbsBound = '1';
      menu.addEventListener('toggle', function () {
        var cell = menu.closest('.cell-action');
        if (menu.open) {
          document.querySelectorAll('details.action-menu').forEach(function (other) {
            if (other !== menu && other.open) other.open = false;
          });
          // The actions column is position:sticky with z-index:1, so each
          // row's cell is its own stacking context. Raise the open row's
          // cell so its (fixed) dropdown isn't painted under the next row.
          if (cell) cell.style.zIndex = '40';
          positionActionMenu(menu);
        } else {
          if (cell) cell.style.zIndex = '';
          var panel = menu.querySelector('.action-menu-panel');
          if (panel) resetActionMenuPanel(panel);
        }
      });
    });
  }

  window.addEventListener('resize', function () {
    document.querySelectorAll('details.action-menu[open]').forEach(positionActionMenu);
  });
  document.addEventListener('click', function (evt) {
    if (evt.target.closest('details.action-menu')) return;
    document.querySelectorAll('details.action-menu[open]').forEach(function (menu) {
      menu.open = false;
    });
  });

  /* ---- Long settings forms: show the sticky save bar once dirty ---- */

  function initDirtyTracking() {
    document.querySelectorAll('form[data-dirty-track]').forEach(function (form) {
      if (form.dataset.gsbsBound) return;
      form.dataset.gsbsBound = '1';
      var bar = form.querySelector('[data-save-bar]');
      if (!bar) return;
      function markDirty() { bar.classList.add('visible'); }
      form.addEventListener('input', markDirty);
      form.addEventListener('change', markDirty);
    });
  }

  /* ---- Logs page: filter + auto-refresh ---- */

  function initLogsPage() {
    var form = document.getElementById('logs-filters');
    var table = document.getElementById('admin-logs-table');
    var refreshBtn = document.getElementById('logs-refresh-btn');
    if (!form || !table) return;

    function buildQuery() {
      var params = new URLSearchParams(new FormData(form));
      return '/admin/partial/logs?' + params.toString();
    }
    function refreshLogs() {
      if (!window.htmx) return;
      var req = window.htmx.ajax('GET', buildQuery(), { target: '#admin-logs-table', swap: 'innerHTML' });
      if (req && typeof req.then === 'function') {
        req.then(function () {
          table.classList.remove('refresh-pulse');
          void table.offsetWidth; // restart the animation
          table.classList.add('refresh-pulse');
        });
      }
    }
    var csvBtn = document.getElementById('logs-csv-btn');
    function updateCsvLink() {
      if (!csvBtn) return;
      var params = new URLSearchParams(new FormData(form));
      csvBtn.href = '/admin/logs/export.csv?' + params.toString();
    }
    form.addEventListener('submit', function (evt) {
      evt.preventDefault();
      refreshLogs();
      updateCsvLink();
    });
    if (refreshBtn) refreshBtn.addEventListener('click', refreshLogs);

    // Newer/Older paging: buttons live inside the swapped table region, so
    // delegate. The hidden offset input keeps the position across refreshes.
    var offsetInput = document.getElementById('logs-offset');
    function currentLimit() {
      var sel = form.querySelector('select[name="limit"]');
      return sel ? (parseInt(sel.value, 10) || 200) : 200;
    }
    document.addEventListener('click', function (evt) {
      var btn = evt.target.closest('[data-logs-page]');
      if (!btn || !offsetInput) return;
      var cur = parseInt(offsetInput.value || '0', 10) || 0;
      var next = btn.getAttribute('data-logs-page') === 'older'
        ? cur + currentLimit()
        : Math.max(0, cur - currentLimit());
      offsetInput.value = String(next);
      refreshLogs();
    });

    var timer = null;
    function syncAutoRefresh() {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
      var enabled = form.querySelector('input[name="auto"]');
      var seconds = form.querySelector('select[name="refresh"]');
      var interval = seconds ? Math.max(2000, parseInt(seconds.value || '5', 10) * 1000) : 5000;
      if (enabled && enabled.checked) timer = setInterval(refreshLogs, interval);
    }
    form.addEventListener('change', function (evt) {
      syncAutoRefresh();
      updateCsvLink();
      if (evt.target.name !== 'auto' && evt.target.name !== 'refresh') {
        if (offsetInput) offsetInput.value = '0'; // filters changed: back to newest
        refreshLogs();
      }
    });
    syncAutoRefresh();
    updateCsvLink();
  }

  /* ---- Filter forms that feed a CSV export link (data-filter-csv) ---- */

  function initFilterCsvLinks() {
    document.querySelectorAll('form[data-filter-csv]').forEach(function (form) {
      if (form.dataset.gsbsBound) return;
      form.dataset.gsbsBound = '1';
      var link = document.querySelector(form.getAttribute('data-filter-csv'));
      var base = form.getAttribute('data-csv-base');
      if (!link || !base) return;
      function update() {
        var params = new URLSearchParams(new FormData(form));
        link.href = base + '?' + params.toString();
      }
      form.addEventListener('change', update);
      form.addEventListener('input', update);
      update();
    });
  }

  onReady(function () {
    initNavGroups();
    initQuotaDialog();
    initActionMenus();
    initLogsPage();
    initFilterCsvLinks();
    initDirtyTracking();
    // Rebind per-element inits after HTMX swaps (idempotent via data flags),
    // so action menus / dirty bars / CSV links inside swapped partials work.
    document.body.addEventListener('htmx:afterSwap', function () {
      initActionMenus();
      initFilterCsvLinks();
      initDirtyTracking();
    });
  });
})();
