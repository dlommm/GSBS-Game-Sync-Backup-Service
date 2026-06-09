// Lightweight toast notification system for GSBS WebUI.
(function () {
  window.gsbs = window.gsbs || {};
  window.gsbs.toast = function (msg, type) {
    var c = document.getElementById('toast-container');
    if (!c) return;
    var t = document.createElement('div');
    t.setAttribute('role', 'status');
    var bg = type === 'error' ? 'var(--error-muted)' : type === 'warn' ? 'var(--warning-muted)' : 'var(--success-muted)';
    var col = type === 'error' ? 'var(--error)' : type === 'warn' ? 'var(--warning)' : 'var(--success)';
    t.style.cssText = 'background:' + bg + ';color:' + col + ';border:1px solid ' + col + ';border-radius:var(--radius-sm);padding:0.75rem 1rem;font-size:0.875rem;box-shadow:var(--shadow);opacity:0;transition:opacity 200ms;max-width:100%;word-break:break-word;';
    t.textContent = msg;
    c.appendChild(t);
    requestAnimationFrame(function () { t.style.opacity = '1'; });
    setTimeout(function () {
      t.style.opacity = '0';
      setTimeout(function () { if (t.parentNode) t.parentNode.removeChild(t); }, 220);
    }, 4000);
  };
})();
