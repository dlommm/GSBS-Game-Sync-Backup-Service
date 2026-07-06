# Client Screen Specifications — GSBS UI/UX Overhaul

Covers the tray (per-OS), the loopback WebUI (127.0.0.1:41234–41239), notifications, first-run,
and updater UX. The client WebUI inherits the §0 rules from `03-SCREENS-WEB.md` (same design
system via the shared `app.css`), with one difference: navigation is the compact topbar, not the
sidebar — an 8-item local tool does not need a nav rail. "Parity rows" reference
`01-PARITY-INVENTORY.md`.

**Architecture note (per 07):** the tray remains native menus (fyne.io/systray) — it is a launcher
and status surface; anything richer than a menu row lives in the loopback WebUI. This is the
incumbent model and it is kept deliberately.

---

## T1. Tray menu (all platforms)

- **Purpose:** ambient status + one-click actions without opening a window.
- **Entry:** system tray / menu bar icon, always present while the client runs.
- **Data:** `GetTraySnapshot()` (tray state), sync engine callbacks, updater state.
- **Layout (top→bottom, unchanged structure — TR-1..17):** status header → progress line (while
  syncing) → Sync now → Pause/Resume → Synced games (12 slots, glyph + title + ago; footer "View
  all in dashboard →") → Discovered games (8 checkbox slots + Add manually… + Rescan) → Open
  dashboard ↗ → **⚠ Resolve N conflicts** (hidden at 0; keep-all-local / use-all-server / review
  each in browser) → pending uploads (hidden at 0) → last error (hidden unless error) → Account &
  Setup → Advanced (config, log, data folder, local pages, Copy diagnostics, Run at startup,
  updater block) → Quit.
- **Redesign deltas (Phase 1 only):** status icon variants re-tinted to the new token palette
  (02 §1.8); glyph set unchanged (menu rows are OS-rendered; no custom drawing); wording pass for
  consistency with the web ("Devices", "My Games" naming).
- **Per-OS conventions:** Windows — title "GSBS", ICO icons, native Walk login dialog + console
  login, metered awareness; Linux — StatusNotifierItem (pure Go, pixmap icons; GNOME needs
  AppIndicator extension — first-run help mentions it); macOS — icon-only menu bar
  (`SetTitle("")`), template-style monochrome icon appropriate to menu bar conventions.
- **States:** setup required (amber icon + "Login…" prominent); paused (grey); in-game deferral
  ("In game: sync deferred", TR-1); offline (retry countdown in tooltip); error (red + truncated
  message row); conflicts (submenu appears); update available ("Install update <tag>…" /
  Flatpak: "Updates managed by your software center").
- **Acceptance:** every TR row reachable on all three OSes; conflict submenu hidden at zero;
  no regression of the v4.1.1 refresh race (refreshMu accessors preserved).
- **Parity rows:** TR-1..TR-20, CB-11, CB-12.

## T2. Tray notifications (OS toasts)

- Existing beeep set preserved verbatim (TR-16): sync complete/failed, push uploaded (30s
  debounce), conflict, discovery summary (≤3 names), N new games, setup required (once), config
  warnings, auth error, push error, quota error, already-running.
- **Redesign delta:** none functional; copy pass for tone consistency ("GSBS synced 3 games" not
  "Sync OK"). Notifications never contain file paths (privacy in shoulder-surfing contexts).
- **Parity rows:** TR-16.

## C1. Setup / Login page

- **Purpose:** connect this device to a server; the client's front door.
- **Entry:** tray "Login…" (browser opens loopback URL); first run; `auth_failed` re-login.
- **Data:** `POST /login` (server_url, username, password, client_name, totp optional);
  `/status` discovery poll after `done=1`.
- **Layout:** standalone auth card (matches web U1 visual language): 3-step explainer (Log in →
  Discover → Syncing), form, TOTP field labeled "only if your account has 2FA"; after login:
  discovery panel polling found games with launcher badges, link to dashboard.
- **States:** *error* — server unreachable / bad credentials / TOTP required (friendly mapped
  messages); *already logged in* — status summary + "switch account"; *discovery running* —
  progressive list.
- **Acceptance:** TOTP accounts log in (v4.1.0 fix preserved); wizard poll lands on dashboard;
  passwords never trimmed (v4.3.0 fix preserved).
- **Parity rows:** CW-1, CW-2, plus login flows CB-side.

## C2. Dashboard (local status)

- **Purpose:** is this device healthy, and what happens next.
- **Entry:** topbar "Status"; tray Advanced → Local status page; post-login redirect.
- **Data:** `/status` poll (5s → 30s backoff): all CW-3 fields; `POST /api/sync-now`;
  `POST /api/check-update`.
- **Layout:** status hero — "All synced" (success) / "Attention needed" (warning + reason) /
  "In game — sync deferred" / "Checking…"; actions: Sync now, Open server dashboard, **Log in
  again** (auth-failed only); stat cards: Connection, Last sync, Next sync (ETA countdown),
  Watcher, Games; conditional cards: Update available (install instructions → tray), Pending
  uploads (→ insights), Conflicts (→ insights).
- **States:** *setup needed* → redirect C1; *server offline* — hero warning + outbox explanation
  + retry ETA; *auth failed* — "Log in again" primary; *in-game* — deferral note + which games
  (games_running); *update available* — card with tag.
- **Acceptance:** ETA counts down between polls; hero state transitions match `/status` truth
  (no stuck "Checking…" — the v4.2.0 tray-state deadlock class is regression-tested).
- **Parity rows:** CW-3, CW-4, FIX-6 (documented tray-only apply).

## C3. Add a game

- **Purpose:** start syncing a game the scanner didn't enable — catalog search or manual path.
- **Entry:** topbar "Games"; tray "Add a game manually…"; quick action.
- **Data:** `/games/search` (manifest, cap 40), `POST /games/add`.
- **Layout:** search-first (catalog results with launcher badge and resolved-path preview;
  **unsafe-path warnings** rendered inline per result); manual form below (game_id, title,
  absolute directory, include patterns) with live validation.
- **States:** *refusal* — the watch-safety message verbatim-equivalent: explains WHY the folder
  was refused (top-level/home root) and what to do (pick the specific save folder or list exact
  filenames, no wildcards) — CB-3; *Flatpak permission denied* — "Grant access with Flatseal,
  then restart GSBS"; *no results* — manual form spotlighted; *game-aware unavailable (Flatpak)*
  — note that pause-while-playing won't apply (CB-1).
- **Acceptance:** refused roots never get watched (safety guard rows); added game appears in tray
  synced submenu after next sync.
- **Parity rows:** CW-5, CB-1, CB-2, CB-3.

## C4. Insights

- **Purpose:** local sync history, pending work, conflicts, per-game state.
- **Entry:** topbar "Insights"; tray review-conflicts and pending/conflict cards deep-link here.
- **Data:** sync history (500-cap store), `ListOutbox()`, conflicts.json, per-game state,
  `/open-folder`.
- **Layout:** summary cards (cycles, success %, saves 7d, last failure); 7-day cycle bar chart;
  **Pending uploads** table (game/file/size/queued/attempts + "retried up to 7 days" note);
  **Conflicts** table (detected/game/file/resolution policy applied/local vs server changed)
  with per-row resolve actions (keep local / use server — same semantics as tray bulk actions);
  per-game state table with Reveal folder.
- **States:** *healthy* — conflicts/outbox panels collapse to a success strip; *auth-failed* —
  outbox paused banner ("uploads paused until you log in again", CB-4); *offline* — retry ages
  ticking.
- **Acceptance:** resolving a conflict here clears it from tray count; outbox rows disappear as
  drained; reveal-folder only for allowlisted paths.
- **Parity rows:** CW-8, CB-4, CB-5, CB-6, CB-7, CB-8, TR-9 (review path).

## C5. Settings

- **Purpose:** local sync behavior.
- **Entry:** topbar; tray Advanced → Settings page.
- **Data:** `POST /settings/save` → restartSync.
- **Layout:** Sync interval; Conflict policy (global; per-game overrides note → becomes an
  editor in Stage D, FIX-4); Sync content (both/saves/config); Max bandwidth; toggles: backup on
  pull, gzip, skip on metered (Windows-only row); dirty-tracking hint.
- **States:** invalid values inline; saved → toast + engine restart note.
- **Parity rows:** CW-7, CB-7 (global half), CB-11, CB-13.

## C6. Logs  /  C7. Help & About  /  C8. Quick actions  /  C9. Open-log

- **Logs:** component/level/search/limit filters (persisted), auto-refresh w/ interval, refresh
  now; **FIX-1 resolved here: implement `/logs/export.csv` on the loopback server** (mirror the
  admin logs CSV shape) — the button already exists; the route ships in Stage B.
- **Help:** status reference + troubleshooting (updated for new visuals); **About:** version,
  platform, built, commit, server + repo links. **Quick actions:** sync now / check updates /
  add game / open log / server dashboard / insights. **Open-log:** path + copy + OS-editor open.
- **Parity rows:** CW-6, CW-9, CW-10, CW-11, CW-12, FIX-1.

## C10. First-run experience (per platform)

- **Windows:** installer → tray starts minimized → toast "setup required" → C1. Metered note if
  applicable.
- **Linux (deb/AppImage/Flatpak):** same flow; GNOME AppIndicator hint on first run if no
  StatusNotifierItem host detected; Flatpak: filesystem grants explained when discovery finds
  nothing (Flatseal pointer), game-aware-sync limitation stated once (CB-1).
- **macOS:** DMG is ad-hoc signed — first launch requires right-click → Open or System Settings →
  Privacy & Security → **Open Anyway** (Sequoia+). The DMG background image (Stage B asset)
  carries these instructions visually; C1's help collapse repeats them + the `xattr -cr`
  alternative. LaunchAgent autostart offered via tray checkbox.
- **Steam Deck:** Desktop Mode flow = Linux Flatpak; loopback WebUI must be fully usable with
  touch (44px targets — inherited from token sizing) and gamepad-as-mouse; Gaming Mode is
  explicitly out of scope for the tray (no system tray exists there) — documented; the client
  runs as a background service and the WebUI is reachable from Desktop Mode.
- **Parity rows:** TR-18..20, CB-1, CW-1; INSTALL.md cross-links.

## C11. Updater UX

- **Check:** background 30s-after-start + 24h; manual from tray + C2 card + quick action.
- **States:** available (tray "Install update <tag>…" + WebUI card pointing to tray) /
  manual-download fallback (GitHub) / up-to-date / metered-skip / Flatpak ("managed by your
  software center") / network/API error (quiet — log only) / manifest-mismatch (refuse, log).
- **Apply:** per-OS as today (bat swap / rename dance / in-bundle swap + ad-hoc re-sign +
  rollback + relaunch). SHA256 always verified; 128 MiB cap.
- **Acceptance:** macOS in-place apply keeps the app launchable (re-sign verified — first live
  run after each release per ops note); failed apply rolls back and surfaces the GitHub fallback.
- **Parity rows:** TR-14, CB-12, CW-12, FIX-6.

---

## Coverage check

TR-1..20 (T1/T2/C10/C11) · CW-1..13 (C1..C9) · CB-1..13 (C2/C3/C4/C5/C10/C11) · CL-1..5
(CLI unchanged; documented in Help, About links) · FIX-1 (C6), FIX-2 (Stage C: proactive token
refresh lands in the sync engine; C2's auth-failed state then becomes rare), FIX-6 (C2/C11).
Web-side rows are in `03-SCREENS-WEB.md`.
