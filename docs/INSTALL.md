# Installing GSBS

This guide covers end-user installation of the GSBS server and client. For building from source, see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Server

GSBS needs **no configuration to start**. On first run it generates its own session secret and opens a browser **setup wizard** where you create the admin account (the first user is the administrator) and pick registration/storage/backup options. Environment variables are optional overrides.

### Option A: Docker Compose (recommended)

Production deployment with Caddy TLS reverse proxy:

```bash
docker compose up -d
```

Then open your server in a browser and complete the setup wizard. Edit [docs/Caddyfile](Caddyfile) with your domain before exposing to the internet. See [COMPOSE.md](COMPOSE.md) for details, health checks, and reverse-proxy alternatives (nginx, Traefik). Copy `.env.example` to `.env` only if you want to pin optional settings.

> **Security note:** the setup wizard is only open until the first account is created, and it locks 60 minutes after the server starts. Complete setup promptly, and don't expose an un-configured instance to the internet longer than necessary.

**Local development** (direct HTTP on port 8080):

```bash
docker compose -f docker-compose.dev.yml up --build
```

### Option B: Pre-built Docker image

```bash
docker pull dendlomm/gsbs-server:latest
docker run -d \
  -p 8080:8080 \
  -e GSBS_DB=/app/data/gsbs.db \
  -v gsbs-data:/app/data \
  dendlomm/gsbs-server:latest
```

Open `http://your-host:8080` and complete the setup wizard.

See [DOCKER.md](DOCKER.md) for environment variables and production tips.

### Option C: Binary (advanced)

Download `gsbs-server-linux-amd64` or `gsbs-server-windows-amd64.exe` from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) and run the binary — it starts with no configuration and opens the setup wizard on first run. To restore from a backup, use `gsbs-server restore <archive>` (see [RESTORE.md](RESTORE.md)).

## Client

Download assets from the [latest GitHub Release](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest).

### Windows

1. Download `gsbs-client-setup-X.Y.Z-windows-amd64.exe`.
2. Run the installer (accept SmartScreen warning if unsigned — see note below).
3. Optional: enable **Run at startup** during install.
4. Launch **GSBS Client** from the Start Menu; use **Login…** in the tray to connect.

**SmartScreen:** Installers are not code-signed yet. Choose *More info → Run anyway* if Windows SmartScreen blocks the installer.

**Updates:** The tray menu checks GitHub daily. Use **Install update…** to download and replace the client without re-running the installer.

### Linux — Flatpak (recommended for Steam Deck / Bazzite / immutable distros)

```bash
flatpak remote-add --if-not-exists gsbs \
  https://dlommm.github.io/gsbs-flatpak/repo/gsbs.flatpakrepo
flatpak install gsbs io.github.dlommm.GSBS
flatpak run io.github.dlommm.GSBS
```

Updates come from `flatpak update` (or your software center / Bazaar). See
[FLATPAK.md](FLATPAK.md) for the Steam Deck Desktop-Mode walkthrough, sandbox
permissions, and granting access to extra game folders with Flatseal.

### Linux — Debian/Ubuntu (.deb)

```bash
sudo apt install ./gsbs-client_X.Y.Z_amd64.deb   # resolves the xdg-utils dependency
# GNOME only: install the "AppIndicator and KStatusNotifierItem Support"
# extension so the tray icon shows (no extra libraries are needed).
gsbs-client   # or launch from your application menu
```

### Linux — AppImage (portable)

```bash
chmod +x gsbs-client-X.Y.Z-x86_64.AppImage
./gsbs-client-X.Y.Z-x86_64.AppImage
```

Requires AppIndicator/tray support on your desktop (see [CLIENT.md](CLIENT.md)).

### Linux — raw binary

```bash
chmod +x gsbs-client-linux-amd64     # or gsbs-client-linux-arm64 on ARM
./gsbs-client-linux-amd64 login
./gsbs-client-linux-amd64
```

### macOS

The macOS client ships as a `.dmg` disk image. Grab the one for your Mac:

- **Apple Silicon** (M1/M2/M3/…): `gsbs-client-<version>-darwin-arm64.dmg`
- **Intel**: `gsbs-client-<version>-darwin-amd64.dmg`

1. Open the `.dmg` and drag **GSBS** into the **Applications** folder.
2. Approve the app once (the build is not notarized — see below). Either:
   - right-click **GSBS.app** in Applications → **Open** → **Open** (on macOS 15 Sequoia: launch it once, then go to **System Settings → Privacy & Security** and click **Open Anyway**), **or**
   - clear the quarantine flag from Terminal:

     ```bash
     xattr -cr /Applications/GSBS.app
     ```

3. Launch **GSBS** from Applications (or Spotlight). It runs as a menu-bar app — no Dock icon — so look for its icon in the top-right menu bar. On first launch, use the tray menu to **Log in** to your server.

The tray menu's **Run at startup** installs a per-user LaunchAgent (`~/Library/LaunchAgents/io.github.dlommm.GSBS.plist`). Updates are manual on macOS (download the newer `.dmg` and drag it over the old app); the in-app self-updater is Windows/Linux only.

> **Gatekeeper:** the app is ad-hoc signed but not notarized (notarization needs a paid Apple Developer account), so macOS asks for a one-time approval on first launch. If you see the harsher **"GSBS is damaged and can't be opened"** dialog (v4.1.0 DMGs, whose bundle was unsigned), nothing is actually damaged — that's the same quarantine flag; the `xattr -cr /Applications/GSBS.app` command clears it. Download only from the official [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest), and verify the download against `SHA256SUMS` if you want to be sure it wasn't tampered with.

## First run

1. Open the server WebUI and register (or use an existing account).
2. On the client, choose **Login…** (tray) or run `gsbs-client login`.
3. Enter server URL and credentials; GSBS stores your API token locally.
4. Sync starts automatically. Use **Sync now** or **Open dashboard** from the tray.

Config and logs: `%APPDATA%\gsbs` (Windows) or `~/.config/gsbs` (Linux). See [CLIENT.md](CLIENT.md).

## Disable client update checks

In `config.json`:

```json
{
  "update_check_enabled": false
}
```

Optional override for a fork or mirror:

```json
{
  "update_repo": "your-org/your-fork"
}
```
