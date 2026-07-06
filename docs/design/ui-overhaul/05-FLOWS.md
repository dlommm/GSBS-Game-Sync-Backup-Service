# Core User Flows — GSBS UI/UX Overhaul

Nine flows, step-by-step, with failure paths. Screen references are to `03-SCREENS-WEB.md`
(U*/A*) and `04-SCREENS-CLIENT.md` (T*/C*). Every step names the real mechanism so the flow is
testable against the implementation, not an idealized storyboard.

---

## F1. First-time server setup → first client login → first sync

1. Admin runs `docker compose up` (zero required env). Visiting any URL redirects to `/setup` (U4).
2. Wizard: Account → Access → Storage (GB) → Extras (backups, webhook) → Review → submit. First
   user becomes admin, auto-logged-in, lands on U5 (empty state) with the onboarding tour.
   - *Failure:* >60 min since boot → "Locked — restart to run setup" (SW-4). Two racing visitors →
     one wins, other gets a friendly login redirect (SW-3).
3. U5 empty state shows "connect a client" instructions (download links + the server URL).
4. User installs the client; tray starts → "setup required" toast → C1 opens.
   - *macOS failure:* Gatekeeper blocks the ad-hoc-signed app → C10 "Open Anyway" path.
5. C1: server URL + credentials (+ TOTP if enabled) → `POST /api/login` → device token; discovery
   panel fills with found launchers/games (CB-10).
   - *Failure:* wrong server URL → "server unreachable" with the URL echoed; bad credentials →
     server's error reason verbatim (v4.3.0 fix); TOTP needed → field highlighted.
6. First sync runs (initial pull → watcher up). Tray goes green; C2 hero "All synced"; server U5
   stats/activity update live via SSE ≤2s.
   - *Failure:* watch-safety refusal for a discovered path → game marked "not ready" with reason
     in tray discovered-submenu and C3 (CB-3); user picks the specific folder in C3.

## F2. Adding a new device that pulls existing saves

1. Install client on device B → C1 login (same account, distinct client_name).
2. Server: `device_registered` notification fires (AD-22/WU-49); device appears on U9 ≤30s
   (clients-list refresh) and in the (Stage D) inbox.
3. Device B discovery matches installed games; initial pull writes save files; `.gsbs.bak`
   backups per config (CB-13).
   - *Failure — path differs by OS:* manifest platform rules + Proton/macOS translation resolve
     (CB-2); unresolvable → game listed "not ready: no path for this OS" in tray/C3.
   - *Failure — quota:* pull unaffected (quota gates pushes only).
4. Device B's first push: fleet `crypto_v2_ready` recomputed (OPS-8); if B is older than v4 the
   fleet drops to legacy crypto — visible in U17 (Stage D) as "device B holds the fleet back."

## F3. Resolving a sync conflict

*Trigger:* device A and B both edit a save while A is offline; A comes back and pushes; server
409s (If-Hash mismatch) → conflict recorded client-side (CB-6) + `conflict` notification; Stage D
also persists server-side (U15).

**Tray path (today, preserved):**
1. Tray shows "⚠ Resolve 1 conflict" (TR-9); OS toast fires (T2).
2. Bulk: keep-all-local (pushes) or use-all-server (pulls, forced keep_server) — one click.
3. Review: "Review each in browser…" → C4 conflicts table → per-row keep-local / use-server with
   local-vs-server metadata (hashes, times, policy applied).
   - *Failure:* resolution push itself 409s (moving target) → conflict re-recorded with fresh
     metadata; C4 row updates.

**Web path (Stage D, new):**
1. U16 inbox item / U15 badge → U15 queue row (game, file, devices, both sides' metadata).
2. Choose keep-local/keep-server/keep-both-export; resolution stored server-side; affected
   clients converge on their next sync cycle (SSE `save-updated` accelerates online clients).
   - *Failure — device offline:* row shows "waiting for <device> to reconnect" (U15 state).
3. Optional: adjust per-game conflict policy in the same panel (FIX-4).

## F4. Restoring an old save version

1. U6 → game card → U7 → file row → Versions (U8). (Or tray synced-game click deep-links to U8.)
2. Timeline: pick version (size, signed delta, authoring device visible) → Restore → dialog with
   review step ("server rolls back to v3; devices pull it on next sync").
3. Confirm → `POST /dashboard/save/versions/restore` → flash on U7; audit `restore_version`;
   clients pull the restored content (SSE-nudged).
   - *Failure — read-only mode:* Restore disabled with banner (OPS-1). *Failure — version blob
     missing (integrity finding):* error names the finding and points at A1 integrity panel.
4. E2E accounts: identical flow — server moves opaque ciphertext; only preview is unavailable.

## F5. Enabling E2E encryption

1. U11 → Encryption → toggle on → consequence dialog: "server previews disabled; passphrase set
   per device; losing it = unrecoverable data."
2. Client side: user sets the passphrase in the client config (C5 note); new pushes encrypt.
   Format chosen automatically: `gsbs2:` Argon2id when the fleet is v2-ready, legacy otherwise
   (OPS-8 — invisible, but U17 makes it legible in Stage D).
3. Existing plaintext saves re-encrypt as they next change (client re-push); U17 (Stage D) shows
   coverage % climbing; U7 explorer flips lock badges per save as they convert.
   - *Failure — mixed fleet:* old client keeps pushing plaintext → coverage stalls; U17 names the
     device (F2 step 4). *Failure — wrong passphrase on a second device:* decrypt fails on pull →
     client error surfaces in tray last-error + C4; docs point at passphrase mismatch.

## F6. Logging in with a recovery code (authenticator lost)

1. U1 → password ok → U2 → "Use a recovery code instead" → enter one of 10 codes.
2. Code hash-checked, consumed; session opens; count badge on U11 decrements; ≤2 remaining fires
   the low-codes notification (rides `login` event — plan correction #4).
3. U11 → regenerate (U13 show-once, copy/download) → old codes void.
   - *Failure:* code already used → inline error, remaining attempts unaffected; no codes left →
     admin path: admin disables/re-enables the user's 2FA via A2 (documented in Help).

## F7. Configuring and verifying a backup

1. A7 Backups section: enable, cron (default nightly), keep-N, include-covers → save (sticky bar).
2. "Backup now" (A1/A7) → job runs (VACUUM INTO → tar.zst → optional S3) → `backup` notification
   with result; jobs panel shows it live (`job-finished`).
3. Verify: A1 integrity "Verify now" (blob re-hash; skips encrypted) + Restore Confidence panel
   (Stage C) shows last-backup time/size/destination on the user side too.
4. Disaster drill (docs/RESTORE.md): `gsbs-server restore <archive>` on a scratch instance;
   refuses to clobber without `--force`.
   - *Failure — S3 creds wrong:* backup job fails → notification + red jobs row + A1 alert.
   - *Failure — disk low:* 507 preflight also guards backup staging; job errors visibly.

## F8. Recovering after the server was offline (offline queue drain)

1. Server down; device A saves games. Watcher pushes fail retryably → outbox entries (CB-4);
   tray "N uploads pending" (TR-10); C2 hero "Attention needed: server unreachable, retrying";
   C4 lists queued files with ages.
2. Server returns. Outbox drains on the 2-minute ticker (or next manual Sync now); entries
   dedup per (game,path) so only newest content pushes.
3. Tray count falls to zero; server U5 activity shows the burst; per-device attribution on U10.
   - *Failure — content conflicts created while down:* 409 → F3.
   - *Failure — token expired during outage:* 401 → outbox pauses entirely ("uploads paused until
     you log in again", CB-4) → C1 re-login → drain resumes. (Stage C's proactive token refresh,
     FIX-2, makes this rare.)
   - *Failure — >7 days queued:* entries age out (outboxMaxAge) — C4 explains the 7-day window.

## F9. Admin onboarding a new user

1. A2 → Create user (8–72 char password, strength meter) → optional quota via GB dialog.
2. Admin shares server URL + credentials; user logs in (U1), changes password (U11 — revokes
   nothing else for self-set), optionally enables 2FA (U12/U13).
3. User connects their client (F1 steps 4–6). Admin watches A2 storage/clients columns fill;
   per-user drill-down (A3) shows their insights.
   - *Failure — user over-quota later:* growth blocked, shrink allowed (OPS-3); user sees the
     over-quota banner (U5) + `quota` notifications at 80%/100%; admin raises quota in A2.
   - *Failure — user disabled:* sessions/tokens rejected; tray shows auth error; re-enable
     restores access.
