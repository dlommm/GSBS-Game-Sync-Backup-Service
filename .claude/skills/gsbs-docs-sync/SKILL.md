---
name: gsbs-docs-sync
description: Keep GSBS's README, banner, wiki, changelogs, upgrade notes, version pins and screenshots in step with the code. Use after any user-visible change (UI, features, removed options, env vars), before or during a version bump or release, and whenever the user asks whether the docs or README are up to date.
---

# GSBS docs sync

The README, wiki and screenshots drift every release: the v5 banner and a
retired "live API sync" option outlived their versions by months. This skill is
the checklist that stops that. Work through every step; each one names the
files and the command that proves it is done.

## 1. Establish the facts first

- Current version: `grep -n 'Version   =' server/version.go client/version.go` (both must match).
- What changed: read `CHANGELOG.md` from the top down to the previous release,
  especially **Removed**, **Changed** and upgrade notes. Every removed option or
  renamed screen is a claim to hunt for in the docs.
- If the UI changed, note the new screen and menu names (the top bar in
  `server/webui/templates/partials/topbar.html`, the client bar in
  `client/webui/templates/partials/topbar.html`).

## 2. Version pins — every place a version appears

When the version changes, all of these move together in one commit (the
release workflow does not rewrite them):

| File | What |
|---|---|
| `server/version.go`, `client/version.go` | `Version = "X.Y.Z"` |
| `CHANGELOG.md` | `## [X.Y.Z] - YYYY-MM-DD` (no leftover `[Unreleased]` content for the release) |
| `docs/wiki/Changelog.md` | the same release, summarised (checked by `script/check-wiki.sh`) |
| `docs/UPGRADE.md`, `docs/wiki/Upgrading.md` | `image: dendlomm/gsbs-server:X.Y.Z` and a row in the version-notes table |
| `docker-compose.yml` | `image: dendlomm/gsbs-server:X.Y.Z` |
| `flatpak/io.github.dlommm.GSBS.metainfo.xml` | a new `<release version=… date=…/>` on top |
| `README.md` | the banner `<strong>GSBS N …</strong>` and its one-line summary |

Prove it: `grep -rn 'OLD_VERSION' --exclude=CHANGELOG.md --exclude-dir=.git .`
should only hit history (changelog tables, design records).
The Docker tag only exists after `script/release.sh` runs, so the version-pin
commit must land with the release, not weeks before it.

## 3. README claims

Read `README.md` top to bottom against step 1:

- **Banner**: names the current major version and today's headline features,
  not the previous release's.
- **Feature list and comparison table**: nothing that was removed (e.g. a
  retired sync source, a dropped layout), nothing new missing.
- **Quick install / first sync**: steps match how sign-in actually works now.
- **"See CHANGELOG for the vN notes"**-style version-specific wording.
- **Screenshot gallery**: every image exists and shows the current UI.

Prove it: `grep -nE 'v[0-9]+ —|GSBS v[0-9]' README.md` and grep each removed
feature's name from the changelog's **Removed** section across `README.md`,
`docs/*.md` and `docs/wiki/*.md`.

## 4. Screenshots (only when the UI changed)

- The referenced set: `grep -rhoE 'screenshots/[a-z0-9-]+\.png' README.md docs | sort -u`.
  Re-capture each one; also refresh any unreferenced `example-*` capture whose
  screen changed, and list them in `docs/images/screenshots/README.md`.
- Capture from a **throwaway server with synthetic data only** (invented
  games and devices; art you may publish — never real users' saves, real
  tokens, hostnames, or a page that prints a local path such as the client's
  open-log page). How: run the built server with `GSBS_DB`, `GSBS_SAVE_ROOT`
  and `GSBS_COVER_ROOT` in a temp dir; finish `/setup`; push saves with
  `POST /api/saves` from a few `/api/login` device tokens; drop art at
  `<cover root>/<game_id>.jpg` (portrait) and `<game_id>-hero.jpg` (wide).
  Screenshot with Playwright using `waitUntil: 'load'` (the SSE stream never
  goes idle) and `localStorage gsbs.tour.done = "1"` (skips the tour).
- Settings › Appearance previews (`server/webui/static/previews/`) must match
  the current designs and layouts; `script/gen-appearance-previews.py` lists
  them (monogram tiles only — no third-party art ships in the binary).
- Desktop 1440×900 for pages, 390×844 for phone views; crop tall pages.

## 5. Design and reference docs

- `docs/design/ui-overhaul/00-OVERVIEW.md` reading-order table lists every
  design doc; the newest one says it is the current look.
- `docs/CLIENT.md`, `docs/ARCHITECTURE.md`: fonts, screen names, file layout.
- New env vars: `README.md`, `docs/DOCKER.md` and the wiki's server
  configuration page (project rule: never only in code).

## 6. Gate

```bash
bash script/check-wiki.sh        # wiki structure + changelog staleness
python3 -I script/check-contrast.py   # if tokens changed
```

Then state, in the reply: which files changed, which screenshots were
recaptured, and anything left stale on purpose (with the reason).
