# GSBS Client — Flatpak

The GSBS client is distributed as a Flatpak from a **custom repository**, which
is the recommended way to install it on SteamOS, Bazzite, the Steam Deck, and
other immutable / atomic distributions where Flatpak (Bazaar, Discover, GNOME
Software) is the primary app source.

- **App ID:** `io.github.dlommm.GSBS`
- **Branch:** `stable`
- **Repo URL:** `https://dlommm.github.io/gsbs-flatpak/repo`

---

## For users

### Install

Add the GSBS remote once, then install:

```bash
flatpak remote-add --if-not-exists gsbs \
  https://dlommm.github.io/gsbs-flatpak/repo/gsbs.flatpakrepo
flatpak install gsbs io.github.dlommm.GSBS
```

Run it:

```bash
flatpak run io.github.dlommm.GSBS
```

### Update

```bash
flatpak update io.github.dlommm.GSBS
```

Or just let your software center handle it — on Steam Deck / Bazzite, **Bazaar**
(or **Discover**) will list and update GSBS automatically once the remote is
added. In-app update checking is disabled in the Flatpak build because the store
owns updates.

### Steam Deck / Bazzite (Desktop Mode)

1. Switch to **Desktop Mode**.
2. Run the two `flatpak` commands above in **Konsole** (or add the remote via the
   store).
3. Launch **GSBS** from the application menu and complete first-run login
   (a browser tab opens to the local setup page).
4. To keep it running in Game Mode sessions, enable autostart (see Limitations
   below) or add it as a non-Steam game.

### First run

GSBS starts in the system tray in a **setup** state and opens a local login page
in your browser. Enter your GSBS server URL and credentials; the client then
begins watching your save folders.

### Granting access to extra game folders

The Flatpak sandbox already exposes your home directory plus the sandboxed trees
used by Flatpak Steam, Heroic, Lutris, and Bottles, and `/run/media` for
SD-card / external libraries. If you keep games or saves somewhere unusual and
GSBS can't see them, grant access with **Flatseal**:

```bash
flatpak install flathub com.github.tchx84.Flatseal
```

Open Flatseal → **GSBS** → **Filesystem** → add the extra path. Or from a terminal:

```bash
flatpak override --user io.github.dlommm.GSBS --filesystem=/path/to/SteamLibrary
```
 GSBS surfaces a
tray warning when a configured save folder isn't accessible in the sandbox.

---

## Sandbox permissions

The manifest (`flatpak/io.github.dlommm.GSBS.yaml`) requests:

| Permission | Why |
| --- | --- |
| `--share=network` | Reach the GSBS server |
| `--socket=wayland` / `--socket=fallback-x11`, `--device=dri` | Tray + display |
| `--talk-name=org.kde.StatusNotifierWatcher` | System-tray icon (SNI/AppIndicator) |
| `--talk-name=org.freedesktop.Notifications` | Desktop notifications |
| `--talk-name=org.freedesktop.portal.Background` | Autostart / run-in-background |
| `--talk-name=org.freedesktop.secrets` | OS keyring for the auth token |
| `--filesystem=home` | Native game saves under `$HOME` |
| `--filesystem=~/.var/app/{Steam,Heroic,Lutris,Bottles}` | Flatpak launcher saves |
| `--filesystem=/run/media` | SD-card / external libraries |

This is the **"home + targeted host dirs"** posture: broad enough that nearly all
real-world saves are watchable, while system directories stay outside the
sandbox.

---

## Steam Deck quick start

1. **Install** (Desktop Mode): add the GSBS repo and install as described under
   *For users* above, or download the bundle from Releases.
2. **Log in**: launch GSBS from the application menu (Desktop Mode); the tray
   icon opens the local setup page in the browser — enter your server URL and
   credentials once.
3. **Autostart**: enable *Run at startup* from the tray. Since 5.4 this goes
   through the Background portal (with a direct fallback), so it survives
   SteamOS updates.
4. **SD-card game libraries** under `/run/media` are covered out of the box.
   A Steam library on a second *internal* drive or another mount needs a
   one-time grant:
   ```bash
   flatpak override --user --filesystem="/path/to/SteamLibrary" io.github.dlommm.GSBS
   ```
   The client's **Dashboard → "Folders that need access"** panel lists any
   blocked folder with the exact command to run (Flatseal works too).
5. **Game-aware sync on Deck**: since 5.4 GSBS reads Steam's own state file to
   see which Steam game is running and defers its sync until you quit — this
   works in both Desktop and Gaming Mode. Non-Steam games (Heroic, Lutris, …)
   are not detected under Flatpak and simply sync immediately.

---

## Limitations

- **Autostart** uses the Background portal since 5.4, falling back to a host
  autostart entry (`flatpak run io.github.dlommm.GSBS --minimized`) when the
  portal is unavailable.
- **Self-update is disabled** in the Flatpak build — the tray shows "Updates
  managed by your software center"; use `flatpak update`.
- **Game-aware sync detects Steam games only** under Flatpak (via Steam's
  `registry.vdf`): the sandbox's PID namespace hides host processes, so the
  process-scan detector used on other platforms cannot run. Non-Steam games
  sync immediately instead of deferring.
- Blocked save folders are listed on the client Dashboard ("Folders that need
  access") with per-folder fix commands; unusual locations may need a
  Flatseal grant (see above).

---

## For maintainers

### Prerequisites

```bash
flatpak install -y flathub \
  org.freedesktop.Platform//24.08 \
  org.freedesktop.Sdk//24.08 \
  org.freedesktop.Sdk.Extension.golang//24.08
sudo apt-get install -y flatpak-builder   # or your distro's package
```

### Build locally

```bash
flatpak/seed-repo.sh                   # optional: start from the published repo
                                       # so the build appends to its history
flatpak/build-flatpak.sh v3.0.4        # vendors deps, builds into flatpak/repo
# quick test install of the result:
flatpak --user install flatpak/repo io.github.dlommm.GSBS
```

`build-flatpak.sh` runs `go mod vendor` for an offline, reproducible build and
injects the version/date/commit into a generated manifest. It also prepends a
`<release>` entry for the version being built to the AppStream metainfo if the
committed list lags behind, so software centers never show a stale version —
though keeping `flatpak/io.github.dlommm.GSBS.metainfo.xml` current by hand is
still preferred (release notes can only come from the committed file).

### Publish / update the repo

```bash
export GSBS_GPG_KEY=<your-key-id>          # sign the repo (strongly recommended)
export GSBS_REPO_URL=https://dlommm.github.io/gsbs-flatpak/repo
flatpak/update-repo.sh                      # static deltas, summary, .flatpakrepo
```

`update-repo.sh` signs only commits that aren't signed yet (seeded history
already is), generates static deltas, and prunes history to each ref's last
three generations (`--prune-depth=2`). When the build was seeded with
`seed-repo.sh`, the previous release stays in the repo, so clients get an
old→new **delta** instead of re-downloading the whole app.

Then publish the `flatpak/repo` directory to your static host. The recommended
setup is a dedicated **GitHub Pages** repo:

1. Create `dlommm/gsbs-flatpak` with Pages enabled (serving from `gh-pages`).
2. Push the contents of `flatpak/repo` to that branch (CI does this on release).
3. The repo is reachable at `https://dlommm.github.io/gsbs-flatpak/repo`.

Any static host works (Cloudflare Pages/R2, S3, a plain web server) — an ostree
repo is just files.

### GPG key management

- Generate a dedicated signing key; store the **private** key as the
  `GSBS_GPG_PRIVATE` CI secret and keep an offline backup.
- The **public** key is embedded in `gsbs.flatpakrepo` (`GPGKey=`) so users
  verify updates automatically.
- Losing the key means users must re-add the remote — guard it and document
  rotation.

### CI

The release workflow seeds the build with the published repo (for delta
updates), builds the Flatpak, signs the repo, and pushes it to the GitHub
Pages repo as a **single orphan commit** (`git push --force`) — gh-pages keeps
no history, since every snapshot is reproducible from a release tag, and
accumulated snapshots would grow the git repo indefinitely. See the
`build-flatpak` job in `.github/workflows/release.yml`.

**Provisioned setup** (already configured):

- `dlommm/gsbs-flatpak` (public) serves Pages from its `gh-pages` branch at
  `https://dlommm.github.io/gsbs-flatpak/`.
- A dedicated RSA-4096 signing key (`GSBS Flatpak Signing <dennis@lomet.me>`,
  fingerprint `B421E28FFE625ED1CC2E2CE56FBDC1DCD30B21F7`) is stored on the main
  repo as the `GSBS_GPG_PRIVATE` secret, with its fingerprint in
  `GSBS_GPG_KEY_ID`. The private key also lives in the maintainer's local GnuPG
  keyring — **back it up offline**; losing it forces users to re-add the remote.
- Publishing uses an **SSH deploy key** (write) on `gsbs-flatpak`; its private
  half is the `GSBS_FLATPAK_DEPLOY_KEY` secret. No personal access token is
  needed. Sign and publish steps run only when these secrets are present.

To rotate the signing key: generate a new key, update `GSBS_GPG_PRIVATE` /
`GSBS_GPG_KEY_ID`, and announce that users must re-add the remote.
