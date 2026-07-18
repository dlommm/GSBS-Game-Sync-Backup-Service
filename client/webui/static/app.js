// GSBS client WebUI behavior. All interactivity lives here (data-* wiring) so
// the loopback server's Content-Security-Policy can drop 'unsafe-inline'
// entirely — guarded by client/webui/template_csp_test.go.
(function () {
  'use strict';

  function onReady(fn) {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
    else fn();
  }
  function toast(msg, type, ms) {
    if (window.gsbs && window.gsbs.toast) window.gsbs.toast(msg, type, ms);
  }
  function timeAgo(iso) {
    if (!iso) return '—';
    var sec = Math.floor((Date.now() - new Date(iso)) / 1000);
    if (sec < 5) return 'just now';
    if (sec < 60) return sec + 's ago';
    if (sec < 3600) return Math.floor(sec / 60) + 'm ago';
    if (sec < 86400) return (sec / 3600).toFixed(1) + 'h ago';
    return Math.floor(sec / 86400) + 'd ago';
  }
  function formatETA(sec) {
    if (!sec || sec <= 0) return 'soon';
    if (sec < 90) return 'in ' + sec + 's';
    if (sec < 5400) return 'in ' + Math.round(sec / 60) + 'm';
    return 'in ' + (sec / 3600).toFixed(1) + 'h';
  }

  /* ---- Shared visibility gate (hidden tabs stop polling) ----
     Wraps a repeating tick: while the tab is hidden the work is skipped, and
     one catch-up tick runs as soon as the tab becomes visible again. */
  function gatedTick(fn) {
    var missed = false;
    document.addEventListener('visibilitychange', function () {
      if (!document.hidden && missed) { missed = false; fn(); }
    });
    return function () {
      if (document.hidden) { missed = true; return; }
      fn();
    };
  }

  /* ---- Theme toggle (shared gsbs.theme key with the server WebUI) ---- */

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-action="toggle-theme"]');
    if (!el) return;
    var root = document.documentElement;
    var next = root.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
    root.setAttribute('data-theme', next);
    try { localStorage.setItem('gsbs.theme', next); } catch (err) { /* private mode */ }
    syncThemeColorMeta();
  });
  function syncThemeColorMeta() {
    var light = document.documentElement.getAttribute('data-theme') === 'light';
    document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
      m.setAttribute('content', light ? '#f5f5f7' : '#09090b');
      m.removeAttribute('media');
    });
  }

  /* ---- Clipboard ---- */

  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-copy],[data-copy-target]');
    if (!el) return;
    var text = el.getAttribute('data-copy');
    if (!text) {
      var target = document.querySelector(el.getAttribute('data-copy-target') || '');
      if (target) text = (target.value !== undefined && target.value !== '') ? target.value : target.textContent.trim();
    }
    if (!text || !navigator.clipboard) return;
    navigator.clipboard.writeText(text).then(function () { toast('Copied to clipboard', 'info', 2000); },
      function () { toast('Copy failed — select and copy manually', 'error'); });
  });

  /* ---- Dynamic bar widths (CSP-safe CSSOM writes) ---- */

  function applyDataWidths(root) {
    (root || document).querySelectorAll('[data-width-pct]').forEach(function (el) {
      var v = parseFloat(el.getAttribute('data-width-pct'));
      if (!isNaN(v)) el.style.width = Math.max(0, Math.min(100, v)) + '%';
    });
  }

  /* ---- Sync now (real completion: wait for last_sync_at to advance) ---- */

  var lastKnownSyncAt = null;

  function bindSyncButtons() {
    document.addEventListener('click', function (e) {
      var btn = e.target.closest('[data-sync-now]');
      if (!btn) return;
      var label = btn.querySelector('[data-sync-now-label]') || btn;
      var original = label.textContent;
      btn.disabled = true;
      label.textContent = 'Syncing…';
      var before = lastKnownSyncAt;
      var deadline = Date.now() + 60000;
      fetch('/api/sync-now', { method: 'POST' }).then(function (r) { return r.json(); }).then(function (d) {
        if (!d || d.ok === false) {
          // Honest failure: the sync loop isn't running (not logged in /
          // setup unfinished) — nothing was queued, so say so.
          btn.disabled = false;
          label.textContent = original;
          toast(d && d.reason === 'not_running'
            ? 'Sync isn’t running on this PC — log in (or finish setup) first'
            : 'Could not start a sync — see the client log', 'error', 6000);
          return;
        }
        (function poll() {
          if (Date.now() > deadline) {
            btn.disabled = false;
            label.textContent = original;
            toast('Sync requested — still running in the background', 'info');
            return;
          }
          setTimeout(function () {
            fetch('/status').then(function (r) { return r.json(); }).then(function (s) {
              if (s.last_sync_at && s.last_sync_at !== before) {
                btn.disabled = false;
                label.textContent = original;
                if (s.last_sync_ok) toast('Sync complete', 'success');
                else toast('Sync finished with an error: ' + (s.last_sync_err || 'see logs'), 'error', 6000);
                if (typeof refreshStatus === 'function') refreshStatus();
              } else {
                poll();
              }
            }).catch(poll);
          }, 1500);
        })();
      }).catch(function () {
        btn.disabled = false;
        label.textContent = original;
        toast('Could not reach the local client', 'error');
      });
    });
  }

  /* ---- Setup: test connection before logging in ---- */

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-test-connection]');
    if (!btn) return;
    var urlInput = document.getElementById('server_url');
    var result = document.getElementById('testConnResult');
    if (!urlInput || !result) return;
    btn.disabled = true;
    result.textContent = 'Testing…';
    var body = new URLSearchParams();
    body.set('server_url', urlInput.value);
    fetch('/setup/test-connection', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: body.toString() })
      .then(function (r) { return r.json(); })
      .then(function (out) {
        btn.disabled = false;
        result.textContent = out.detail || (out.ok ? 'Server reachable ✓' : 'Unreachable');
        result.className = out.ok ? 'badge-success' : 'badge-failed';
      })
      .catch(function () {
        btn.disabled = false;
        result.textContent = 'Could not reach the local client';
        result.className = 'badge-failed';
      });
  });

  /* ---- Check for updates ---- */

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-check-update]');
    if (!btn) return;
    var label = btn.querySelector('[data-check-update-label]') || btn;
    var original = label.textContent;
    btn.disabled = true;
    label.textContent = 'Checking…';
    fetch('/api/check-update', { method: 'POST' }).then(function () {
      setTimeout(function () {
        fetch('/status').then(function (r) { return r.json(); }).then(function (s) {
          btn.disabled = false;
          label.textContent = original;
          if (s.update_status === 'available') toast('Update available: ' + (s.update_available_tag || '') + ' — Install now on the Status page', 'warn', 6000);
          else if (s.update_status === 'checking') toast('Still checking — result will appear on the Status page', 'info');
          else toast('You are up to date', 'success');
        }).catch(function () { btn.disabled = false; label.textContent = original; });
      }, 2500);
    }).catch(function () {
      btn.disabled = false;
      label.textContent = original;
      toast('Could not reach the local client', 'error');
    });
  });

  /* ---- Install update from the WebUI (v5.2): same flow as the tray ---- */

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-apply-update]');
    if (!btn) return;
    btn.disabled = true;
    btn.textContent = 'Installing…';
    fetch('/api/apply-update', { method: 'POST' }).then(function (r) { return r.json(); }).then(function (d) {
      if (d.manual_url) {
        toast('Automatic install unavailable here — opening the releases page', 'info', 6000);
        window.open(d.manual_url, '_blank', 'noopener');
        btn.disabled = false;
        btn.textContent = 'Install now';
      } else if (d.status === 'installing') {
        var hint = document.getElementById('updateCardHint');
        if (hint) hint.textContent = 'Installing ' + (d.tag || 'the update') + ' — GSBS restarts itself when done.';
        toast('Installing update — GSBS will restart shortly', 'info', 8000);
      } else {
        toast(d.error || 'Could not start the update', 'warn', 6000);
        btn.disabled = false;
        btn.textContent = 'Install now';
      }
    }).catch(function () {
      btn.disabled = false;
      btn.textContent = 'Install now';
      toast('Could not reach the local client', 'error');
    });
  });

  /* ---- Reveal game folder (insights) ---- */

  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-open-folder]');
    if (!btn) return;
    fetch('/open-folder?what=game&game_id=' + encodeURIComponent(btn.getAttribute('data-open-folder')), { method: 'POST' })
      .then(function (r) {
        if (!r.ok) { toast('Folder not found on disk', 'error'); return null; }
        return r.json();
      })
      .then(function (d) {
        if (!d) return;
        if (d.ok) toast('Opening folder…', 'info', 2000);
        else toast('Could not open the folder — see the client log', 'error');
      })
      .catch(function () { toast('Could not reach the local client', 'error'); });
  });

  /* ---- Dashboard status polling (visibility-gated, backoff on failure) ---- */

  var refreshStatus = null;

  function initDashboard() {
    var hero = document.getElementById('status-hero');
    if (!hero) return;
    var failures = 0;
    var timer = null;
    var lastGameListSig = null;

    function el(id) { return document.getElementById(id); }
    function setText(id, txt) {
      // Only touch the DOM when the text changed: #refreshLabel is aria-live,
      // and rewriting identical text every tick makes screen readers chatter.
      var n = el(id);
      if (n && n.textContent !== txt) n.textContent = txt;
    }
    function setDot(id, ok) {
      var dot = el(id);
      if (dot) dot.className = 'status-dot ' + (ok === true ? 'dot-ok' : ok === false ? 'dot-error' : 'dot-muted');
    }
    function setHero(state, title, sub) {
      hero.className = 'status-hero status-hero-' + state;
      el('hero-title').textContent = title;
      el('hero-sub').textContent = sub;
    }

    function apply(s) {
      lastKnownSyncAt = s.last_sync_at || null;
      setText('refreshLabel', 'Updated just now');

      // Hero state.
      var relogin = el('reloginBtn');
      var syncBtn = el('syncNowBtn');
      if (!s.logged_in || s.auth_failed) {
        setHero('error', s.auth_failed ? 'Re-login required' : 'Not connected',
          s.auth_failed ? 'This device’s access was revoked or expired. Log in again to resume syncing.'
                        : 'Connect this PC to your GSBS server to start syncing.');
        relogin.hidden = false;
        syncBtn.hidden = true;
      } else if (s.paused) {
        // Previously invisible here: a paused client rendered "All synced"
        // and Sync now silently did nothing for 60s before a misleading toast.
        setHero('warn', 'Sync paused', 'Resume from the tray menu to continue protecting your saves.');
        relogin.hidden = true;
        syncBtn.hidden = true;
      } else if (s.metered) {
        setHero('warn', 'Metered connection — sync skipped',
          'Syncing resumes automatically on an unmetered network (or turn off the metered setting).');
        relogin.hidden = true;
        syncBtn.hidden = true;
      } else if (s.conflict_count > 0 || (s.last_sync_at && !s.last_sync_ok)) {
        setHero('warn', 'Attention needed',
          s.conflict_count > 0 ? s.conflict_count + ' conflict' + (s.conflict_count === 1 ? '' : 's') + ' recorded — details in Insights.'
                               : 'Last sync failed: ' + (s.last_sync_err || 'see logs.'));
        relogin.hidden = true;
        syncBtn.hidden = false;
      } else if (s.games_running > 0) {
        setHero('sync', 'In game — sync deferred', 'Saves upload as soon as your game exits.');
        relogin.hidden = true;
        syncBtn.hidden = false;
      } else {
        setHero('ok', 'All synced', (s.matched_games || 0) + ' game' + (s.matched_games === 1 ? '' : 's') + ' protected' +
          (s.last_sync_at ? ' · last sync ' + timeAgo(s.last_sync_at) : '') + '.');
        relogin.hidden = true;
        syncBtn.hidden = false;
      }

      // Server dashboard links (topbar quick action too).
      var dashURL = s.server_url ? s.server_url.replace(/\/$/, '') + '/dashboard' : '';
      var dashLink = el('serverDashLink');
      if (dashLink) { dashLink.hidden = !dashURL; if (dashURL) dashLink.href = dashURL; }

      setDot('conn-dot', s.logged_in ? (s.auth_failed ? false : true) : false);
      el('conn-label').textContent = s.auth_failed ? 'Re-login required' : (s.logged_in ? 'Logged in' : 'Not connected');
      el('serverURLLabel').textContent = s.server_url || '';

      setDot('sync-dot', s.last_sync_at ? s.last_sync_ok : null);
      el('sync-label').textContent = s.last_sync_at ? timeAgo(s.last_sync_at) : 'Never';
      el('lastSyncErr').textContent = s.last_sync_err || '';

      el('nextSyncLabel').textContent = s.logged_in ? formatETA(s.next_sync_eta_sec) : '—';

      setDot('watch-dot', s.watcher_healthy);
      el('watch-label').textContent = s.watcher_healthy ? 'Healthy' : 'Recovering';
      el('watchedPaths').textContent = s.watched_paths != null ? s.watched_paths : '—';

      el('discoveredCount').textContent = s.matched_games;

      el('pendingCount').textContent = s.pending_uploads;
      el('pendingCard').hidden = !s.pending_uploads;
      el('conflictCount').textContent = s.conflict_count;
      el('conflictCard').hidden = !s.conflict_count;

      // Access panel: sandbox-blocked folders + unsafe-root skips + the
      // Flatpak game-detection limitation. Built from nodes, no HTML concat.
      var accessPanel = el('accessPanel');
      if (accessPanel) {
        var blocked = s.blocked_dirs || [];
        var unsafe = s.unsafe_skips || [];
        var showPanel = blocked.length > 0 || unsafe.length > 0 || s.game_aware_limited;
        accessPanel.hidden = !showPanel;
        if (showPanel) {
          var intro = el('accessIntro');
          if (blocked.length > 0) {
            intro.textContent = s.flatpak
              ? 'These save folders exist but the Flatpak sandbox can’t read them. Grant access with the command below (or Flatseal), then restart GSBS.'
              : 'These save folders exist but GSBS can’t read them — check folder permissions.';
          } else {
            intro.textContent = '';
          }
          var list = el('blockedDirList');
          list.textContent = '';
          blocked.forEach(function (dir) {
            var li = document.createElement('li');
            li.className = 'client-game-item';
            var name = document.createElement('code');
            name.className = 'code-inline';
            name.textContent = dir;
            li.appendChild(name);
            if (s.flatpak) {
              var fix = document.createElement('code');
              fix.className = 'code-inline';
              fix.textContent = 'flatpak override --user --filesystem="' + dir + '" io.github.dlommm.GSBS';
              li.appendChild(document.createElement('br'));
              li.appendChild(fix);
            }
            list.appendChild(li);
          });
          el('gameAwareNote').hidden = !s.game_aware_limited;
          var unsafeNote = el('unsafeSkipNote');
          if (unsafe.length > 0) {
            var names = unsafe.slice(0, 5).map(function (u) { return u.title || u.game_id; }).join(', ');
            unsafeNote.textContent = unsafe.length + ' matched game(s) point at a broad system folder GSBS refuses to watch for safety (' +
              names + (unsafe.length > 5 ? ', …' : '') + '). Add the game manually with its exact save folder to sync it.';
            unsafeNote.hidden = false;
          } else {
            unsafeNote.hidden = true;
          }
        }
      }

      var updateCard = el('updateCard');
      if (s.update_status === 'available' && s.update_available_tag) {
        el('updateTag').textContent = s.update_available_tag;
        updateCard.hidden = false;
      } else {
        updateCard.hidden = true;
      }
      var checkedRow = el('updateCheckedRow');
      if (s.update_last_checked_ago) {
        el('updateCheckedAgo').textContent = s.update_last_checked_ago;
        checkedRow.hidden = false;
        el('updateUpToDateBadge').hidden = s.update_status !== 'up_to_date';
      } else if (s.update_status === 'checking') {
        el('updateCheckedAgo').textContent = 'checking…';
        checkedRow.hidden = false;
        el('updateUpToDateBadge').hidden = true;
      } else {
        checkedRow.hidden = true;
      }

      // Matched games list — built from cloned nodes, no HTML string concat.
      // Rebuilt only when the content actually changed: rewriting the same
      // list every 5s churned the DOM (and, formerly, an aria-live region).
      var ul = el('gameList');
      var titles = s.game_titles || [];
      var gameListSig = (s.logged_in ? 'in' : 'out') + '\u0000' + titles.join('\u0000');
      if (gameListSig === lastGameListSig) return;
      lastGameListSig = gameListSig;
      ul.textContent = '';
      if (titles.length === 0) {
        var empty = document.createElement('li');
        empty.className = 'panel-empty';
        empty.textContent = s.logged_in ? 'No games matched yet — discovery runs automatically after login.'
                                        : 'Log in first, then discovery finds your installed games.';
        ul.appendChild(empty);
      } else {
        titles.slice(0, 25).forEach(function (t) {
          var li = document.createElement('li');
          li.className = 'client-game-item';
          var check = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
          check.setAttribute('viewBox', '0 0 16 16');
          check.setAttribute('class', 'client-game-check');
          check.setAttribute('aria-hidden', 'true');
          var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
          path.setAttribute('d', 'm3 8.5 3.2 3.2L13 4.5');
          path.setAttribute('fill', 'none');
          path.setAttribute('stroke', 'currentColor');
          path.setAttribute('stroke-width', '1.8');
          path.setAttribute('stroke-linecap', 'round');
          path.setAttribute('stroke-linejoin', 'round');
          check.appendChild(path);
          var span = document.createElement('span');
          span.textContent = t;
          li.appendChild(check);
          li.appendChild(span);
          ul.appendChild(li);
        });
        if (titles.length > 25) {
          var more = document.createElement('li');
          more.className = 'panel-empty';
          more.textContent = '… and ' + (titles.length - 25) + ' more';
          ul.appendChild(more);
        }
      }
    }

    function refresh() {
      fetch('/status').then(function (r) { return r.json(); }).then(function (s) {
        failures = 0;
        apply(s);
        // While the SSE stream is open, state changes push instantly — the
        // poll is only a safety net, so stretch it to 60s.
        schedule(sseOpen ? 60000 : 5000);
      }).catch(function () {
        failures++;
        setText('refreshLabel', 'Could not reach the local client — retrying…');
        setHero('error', 'Client unreachable', 'The tray app may be shutting down. Retrying automatically.');
        schedule(Math.min(30000, 5000 * Math.pow(2, failures)));
      });
    }
    refreshStatus = refresh;

    function schedule(ms) {
      clearTimeout(timer);
      timer = setTimeout(refresh, ms);
    }
    document.addEventListener('visibilitychange', function () {
      if (document.hidden) clearTimeout(timer);
      else refresh();
    });

    /* SSE from the local daemon: instant refresh on tray-state changes. On
       any stream error EventSource reconnects by itself; until it re-opens,
       the visibility-gated poll falls back to the normal 5s cadence. */
    var sseOpen = false;
    if (window.EventSource) {
      try {
        var es = new EventSource('/events');
        es.addEventListener('open', function () { sseOpen = true; });
        es.addEventListener('error', function () {
          // EventSource retries on its own; until it re-opens, pull the
          // fallback poll back in from 60s to the normal cadence.
          sseOpen = false;
          if (!document.hidden) schedule(5000);
        });
        es.addEventListener('state-changed', function () {
          if (!document.hidden) refresh();
        });
      } catch (err) { /* plain polling remains */ }
    }
    refresh();
  }

  /* ---- Quick actions: show the server-dashboard card when known ---- */

  function initQuickActions() {
    var card = document.querySelector('[data-server-dash]');
    if (!card) return;
    fetch('/status').then(function (r) { return r.json(); }).then(function (s) {
      if (s.server_url) {
        card.href = s.server_url.replace(/\/$/, '') + '/dashboard';
        card.hidden = false;
      }
    }).catch(function () { /* stays hidden */ });
  }

  /* ---- Setup: discovery polling with honest step advance ---- */

  function initSetupDiscovery() {
    var panel = document.querySelector('[data-discovery-poll]');
    if (!panel) return;
    var status = document.getElementById('discovery-status');
    var list = document.getElementById('discovery-games');
    var doneStep = document.getElementById('step-done');
    var discoverStep = document.getElementById('step-discover');
    // Visibility-gated: a hidden tab stops the 2s chain and resumes (with one
    // immediate catch-up poll) when the tab comes back.
    var gatedPoll = gatedTick(poll);
    function poll() {
      fetch('/status').then(function (r) { return r.json(); }).then(function (s) {
        var titles = s.game_titles || [];
        if (titles.length > 0) {
          status.textContent = 'Found ' + titles.length + ' game' + (titles.length === 1 ? '' : 's') + ' — syncing is active.';
          list.textContent = '';
          titles.slice(0, 12).forEach(function (t) {
            var li = document.createElement('li');
            li.textContent = '✓ ' + t;
            list.appendChild(li);
          });
          if (discoverStep) discoverStep.className = 'wizard-step wizard-step-done';
          if (doneStep) doneStep.className = 'wizard-step wizard-step-active';
          return; // stop polling — discovery done
        }
        if (s.last_scan_at) status.textContent = 'Scan finished — no known games matched yet. You can add folders manually.';
        setTimeout(gatedPoll, 2000);
      }).catch(function () { setTimeout(gatedPoll, 4000); });
    }
    gatedPoll();
  }

  /* ---- Games page: catalog search ---- */

  function initGamesSearch() {
    var box = document.querySelector('[data-games-search]');
    if (!box) return;
    var resultsEl = document.getElementById('results');
    var hint = document.getElementById('results-hint');
    var countEl = document.getElementById('result-count');
    var rowTemplate = document.getElementById('result-row-template');
    var timer = null;

    box.addEventListener('input', function () {
      clearTimeout(timer);
      timer = setTimeout(run, 250);
    });

    function run() {
      var q = box.value.trim();
      if (q === '') {
        resultsEl.textContent = '';
        if (hint) resultsEl.appendChild(hint);
        if (countEl) countEl.textContent = '';
        return;
      }
      if (countEl) countEl.textContent = 'Searching…';
      fetch('/games/search?q=' + encodeURIComponent(q)).then(function (r) { return r.json(); }).then(function (d) {
        var list = d.results || [];
        resultsEl.textContent = '';
        if (countEl) countEl.textContent = list.length ? list.length + ' match' + (list.length === 1 ? '' : 'es') : 'No matches — add the folder manually below.';
        list.forEach(function (g) {
          var node = rowTemplate.content.firstElementChild.cloneNode(true);
          node.querySelector('.game-result-title').textContent = g.title;
          var badge = node.querySelector('.game-result-badge');
          if (g.unsafe) { badge.textContent = 'too broad'; badge.className = 'game-result-badge badge-failed'; }
          else if (g.exists) { badge.textContent = 'folder found'; badge.className = 'game-result-badge badge-success'; }
          else { badge.textContent = 'folder missing'; badge.className = 'game-result-badge badge-warn'; }
          node.querySelector('.game-result-meta').textContent = g.game_id + (g.directory ? ' · ' + g.directory : ' · (no path resolved)');
          var useBtn = node.querySelector('.use');
          var warning = node.querySelector('.game-result-warning');
          if (g.unsafe) {
            useBtn.remove();
            warning.hidden = false;
          } else {
            useBtn.addEventListener('click', function () {
              document.getElementById('game_id').value = g.game_id;
              document.getElementById('title').value = g.title;
              document.getElementById('directory').value = g.directory || '';
              document.getElementById('directory').focus();
              toast('Form filled — review the folder and click Add game', 'info', 2500);
            });
          }
          resultsEl.appendChild(node);
        });
      }).catch(function () {
        if (countEl) countEl.textContent = 'Search failed — is the tray app running?';
      });
    }
  }

  /* ---- Settings: dirty-state hint ---- */

  function initDirtyTracking() {
    document.querySelectorAll('form[data-dirty-track]').forEach(function (form) {
      var hint = form.querySelector('[data-dirty-hint]');
      function markDirty() { if (hint) hint.hidden = false; }
      form.addEventListener('input', markDirty);
      form.addEventListener('change', markDirty);
    });
  }

  /* ---- Logs page: filters, auto-refresh, persisted choices ---- */

  function initLogsPage() {
    var form = document.getElementById('logs-filters');
    var table = document.getElementById('client-logs-table');
    if (!form || !table) return;
    var refreshBtn = document.getElementById('logs-refresh-btn');
    var csvBtn = document.getElementById('logs-csv-btn');
    var tailing = document.getElementById('logs-tailing');
    var offsetInput = document.getElementById('logs-offset');
    var KEY = 'gsbs.client.logs.filters';

    // Restore persisted filters (only when the URL carries none). The paging
    // offset is a position, not a filter — never persisted or restored.
    try {
      if (!window.location.search) {
        var saved = JSON.parse(localStorage.getItem(KEY) || 'null');
        if (saved) {
          Object.keys(saved).forEach(function (k) {
            if (k === 'offset') return;
            var field = form.elements[k];
            if (!field) return;
            if (field.type === 'checkbox') field.checked = !!saved[k];
            else field.value = saved[k];
          });
        }
      }
    } catch (e) { /* private mode */ }

    function persist() {
      try {
        var out = {};
        Array.prototype.forEach.call(form.elements, function (f) {
          if (!f.name || f.name === 'offset') return;
          out[f.name] = f.type === 'checkbox' ? f.checked : f.value;
        });
        localStorage.setItem(KEY, JSON.stringify(out));
      } catch (e) { /* ignore */ }
    }

    function query() {
      var params = new URLSearchParams(new FormData(form));
      return params.toString();
    }
    function updateCsvLink() {
      if (csvBtn) csvBtn.href = '/logs/export.csv?' + query();
    }
    function refreshLogs() {
      fetch('/partial/logs?' + query()).then(function (r) { return r.text(); }).then(function (html) {
        table.innerHTML = html;
        table.classList.remove('refresh-pulse');
        void table.offsetWidth;
        table.classList.add('refresh-pulse');
      }).catch(function () { toast('Could not load logs', 'error'); });
    }

    // Newer/Older paging (same [data-logs-page] pattern as the server admin
    // logs page): buttons live inside the swapped table region, so delegate.
    // The hidden offset input keeps the position across refreshes.
    function currentOffset() {
      return offsetInput ? (parseInt(offsetInput.value, 10) || 0) : 0;
    }
    function currentLimit() {
      var sel = form.querySelector('select[name="limit"]');
      return sel ? (parseInt(sel.value, 10) || 200) : 200;
    }
    document.addEventListener('click', function (evt) {
      var pageBtn = evt.target.closest('[data-logs-page]');
      if (!pageBtn || !offsetInput) return;
      var next = pageBtn.getAttribute('data-logs-page') === 'older'
        ? currentOffset() + currentLimit()
        : Math.max(0, currentOffset() - currentLimit());
      offsetInput.value = String(next);
      refreshLogs();
    });

    var timer = null;
    // Auto-refresh ticks are visibility-gated (hidden tabs stop fetching) and
    // only run on the newest page — refreshing while paged back would yank
    // the view.
    var autoTick = gatedTick(function () {
      if (currentOffset() > 0) return;
      refreshLogs();
    });
    function syncAutoRefresh() {
      if (timer) { clearInterval(timer); timer = null; }
      var enabled = form.querySelector('input[name="auto"]');
      var seconds = form.querySelector('select[name="refresh"]');
      var interval = seconds ? Math.max(2000, parseInt(seconds.value || '5', 10) * 1000) : 5000;
      var on = enabled && enabled.checked;
      if (on) timer = setInterval(autoTick, interval);
      if (tailing) tailing.hidden = !on;
    }

    form.addEventListener('submit', function (evt) {
      evt.preventDefault();
      persist();
      refreshLogs();
      updateCsvLink();
    });
    form.addEventListener('change', function (evt) {
      persist();
      syncAutoRefresh();
      updateCsvLink();
      if (evt.target.name !== 'auto' && evt.target.name !== 'refresh') {
        if (offsetInput) offsetInput.value = '0'; // filters changed: back to newest
        refreshLogs();
      }
    });
    if (refreshBtn) refreshBtn.addEventListener('click', refreshLogs);
    syncAutoRefresh();
    updateCsvLink();
  }

  /* ---- Setup login: prevent double submit ---- */

  function initSetupLogin() {
    var form = document.querySelector('[data-setup-login]');
    if (!form) return;
    form.addEventListener('submit', function () {
      var btn = form.querySelector('button[type="submit"]');
      if (btn) { btn.disabled = true; btn.textContent = 'Logging in…'; }
    });
  }

  onReady(function () {
    syncThemeColorMeta();
    applyDataWidths(document);
    bindSyncButtons();
    initDashboard();
    initQuickActions();
    initSetupDiscovery();
    initGamesSearch();
    initDirtyTracking();
    initLogsPage();
    initSetupLogin();
  });
})();
