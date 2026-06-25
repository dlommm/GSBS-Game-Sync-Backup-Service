// cmdk.js — global command palette (Cmd/Ctrl+K). Vanilla JS, no dependencies
// beyond HTMX (which fetches the result list into #cmdk-results).
(function () {
  var dialog = document.getElementById('cmdk-dialog');
  var input = document.getElementById('cmdk-input');
  if (!dialog || !input) return;
  var results = document.getElementById('cmdk-results');
  var sel = -1;

  function items() {
    return Array.prototype.slice.call(results.querySelectorAll('.cmdk-item'));
  }
  function highlight() {
    var its = items();
    its.forEach(function (el, i) { el.classList.toggle('selected', i === sel); });
    if (sel >= 0 && its[sel]) its[sel].scrollIntoView({ block: 'nearest' });
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
      if (target) { e.preventDefault(); window.location = target.getAttribute('href'); }
    }
  });

  // Reset selection whenever the result list is swapped in.
  results.addEventListener('htmx:afterSwap', function () { sel = -1; });

  // Click on the dialog backdrop (the dialog element itself) closes it.
  dialog.addEventListener('click', function (e) { if (e.target === dialog) close(); });

  // Any element with [data-cmdk-open] (e.g. the topbar Search button) opens it.
  document.querySelectorAll('[data-cmdk-open]').forEach(function (btn) {
    btn.addEventListener('click', function (e) { e.preventDefault(); open(); });
  });
})();
