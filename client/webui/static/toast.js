// Lightweight toast notification system for GSBS WebUI.
// Styling lives in app.css (.toast, .toast-error, .toast-warn, .toast-info).
(function () {
  window.gsbs = window.gsbs || {};
  window.gsbs.toast = function (msg, type, durationMs) {
    var c = document.getElementById('toast-container');
    if (!c) return;
    var t = document.createElement('div');
    // Errors/warnings interrupt (assertive); the rest announce politely. The
    // per-toast role is the only live region — the container must NOT also be
    // aria-live or screen readers announce every toast twice.
    t.setAttribute('role', type === 'error' || type === 'warn' ? 'alert' : 'status');
    t.className = 'toast' +
      (type === 'error' ? ' toast-error' : type === 'warn' ? ' toast-warn' : type === 'info' ? ' toast-info' : ' toast-success');
    t.textContent = msg;
    c.appendChild(t);
    requestAnimationFrame(function () { t.classList.add('toast-visible'); });
    var ttl = typeof durationMs === 'number' && durationMs > 0 ? durationMs : 4000;
    setTimeout(function () {
      t.classList.remove('toast-visible');
      setTimeout(function () { if (t.parentNode) t.parentNode.removeChild(t); }, 220);
    }, ttl);
  };
})();
