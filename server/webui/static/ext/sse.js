/* GSBS SSE bridge (htmx 2 compatible).
   Opens one EventSource to the URL in [data-sse] and re-dispatches every
   server event as a bubbling DOM CustomEvent on <body>, which is the contract
   the markup relies on (hx-trigger="save-updated from:body", ...) and the
   app.js listeners read (evt.detail.data). */
(function () {
  'use strict';

  var EVENTS = [
    'save-updated',
    'client-activity',
    'inbox-updated',
    'conflict-recorded',
    'conflict-resolved',
    'manifest-updated',
    'job-progress',
    'job-finished',
    'audit-updated'
  ];

  function connect(url, retryMs) {
    var source = new EventSource(url);
    EVENTS.forEach(function (name) {
      source.addEventListener(name, function (ev) {
        retryMs = 5000;
        document.body.dispatchEvent(new CustomEvent(name, {
          bubbles: true,
          detail: { data: ev.data }
        }));
      });
    });
    // EventSource retries transient drops on its own; only a fatal close
    // (non-200 response, e.g. an expired session redirecting to /login)
    // lands here in the CLOSED state.
    source.onerror = function () {
      if (source.readyState === EventSource.CLOSED) {
        setTimeout(function () {
          connect(url, Math.min(retryMs * 2, 60000));
        }, retryMs);
      }
    };
  }

  function init() {
    var el = document.querySelector('[data-sse]');
    if (el && window.EventSource) connect(el.getAttribute('data-sse'), 5000);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
