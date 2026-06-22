# Installing GSBS

This guide covers end-user installation of the GSBS server and client. For building from source, see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Server

### Option A: Docker Compose (recommended)

Production deployment with Caddy TLS reverse proxy:

```bash
cp .env.example .env
# Set GSBS_SESSION_SECRET to a long random string (openssl rand -hex 32)
docker compose up -d
```

Edit [docs/Caddyfile](Caddyfile) with your domain before exposing to the internet. See [COMPOSE.md](COMPOSE.md) for details, health checks, and reverse-proxy alternatives (nginx, Traefik).

**Local development** (direct HTTP on port 8080):

```bash
docker compose -f docker-compose.dev.yml up --build
```

### Option B: Pre-built Docker image

```bash
docker pull dendlomm/gsbs-server:latest
docker run -d \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret" \
  -e GSBS_DB=/app/data/gsbs.db \
  -v gsbs-data:/app/data \
  dendlomm/gsbs-server:latest
```

See [DOCKER.md](DOCKER.md) for environment variables and production tips.

### Option C: Binary (advanced)

Download `gsbs-server-linux-amd64` or `gsbs-server-windows-amd64.exe` from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest), set `GSBS_SESSION_SECRET`, and run the binary.

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
sudo apt install libayatana-appindicator3-1 xdg-utils   # or libappindicator3-1
sudo dpkg -i gsbs-client_X.Y.Z_amd64.deb
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
chmod +x gsbs-client-linux-amd64
./gsbs-client-linux-amd64 login
./gsbs-client-linux-amd64
```

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
