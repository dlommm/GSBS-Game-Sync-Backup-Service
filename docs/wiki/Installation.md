# Installation

> Step-by-step install guide for the GSBS server and client on all supported platforms.

---

## Server

The server is a single Go binary backed by SQLite. The recommended deployment is Docker Compose with Caddy for automatic TLS.

### Option A: Docker Compose (recommended)

Production deployment with Caddy TLS reverse proxy and auto-HTTPS:

```bash
git clone https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-.git
cd GSBS--Game-Sync---Backup-Service-
cp .env.example .env
```

Edit `.env` and set at minimum:

```bash
GSBS_SESSION_SECRET=<output of: openssl rand -hex 32>
```

Start the server:

```bash
docker compose up -d
```

Edit `docs/Caddyfile` with your domain before exposing to the internet. For full Compose options (health checks, TLS alternatives, nginx, Traefik) see [Server Configuration](Server-Configuration).

**Local development** (plain HTTP on port 8080, no TLS):

```bash
docker compose -f docker-compose.dev.yml up --build
```

### Option B: Pre-built Docker image

```bash
docker pull dendlomm/gsbs-server:latest
docker run -d \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret-at-least-32-chars" \
  -e GSBS_DB="/app/data/gsbs.db" \
  -v gsbs-data:/app/data \
  dendlomm/gsbs-server:latest
```

### Option C: Binary (advanced)

Download `gsbs-server-linux-amd64` or `gsbs-server-windows-amd64.exe` from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest), then:

```bash
export GSBS_SESSION_SECRET="your-secret-at-least-32-chars"
export GSBS_DB="/path/to/gsbs.db"
./gsbs-server-linux-amd64
```

### Option D: Windows server installer wizard

1. Download `gsbs-server-setup-X.Y.Z-windows-amd64.exe` from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest).
2. Run the installer as Administrator.
3. In the wizard, set at least `GSBS_ADDR`, `GSBS_DB`, and `GSBS_SESSION_SECRET` (or click **Generate secure secret**).
4. By default, leave **Install and run GSBS Server as a Windows Service (recommended)** enabled.
5. Finish install and optionally open GSBS Admin in your browser.

The installer writes runtime config to `C:\ProgramData\GSBS\server.env`, installs binaries to `C:\Program Files\GSBS`, and stores launcher logs under `C:\ProgramData\GSBS\logs`. Uninstall removes installed binaries and the `GSBS Server` Windows Service, but keeps `ProgramData` config/database by default.

### Platform support

| Component | linux/amd64 | linux/arm64 | windows/amd64 |
|---|---|---|---|
| Server | Yes | Yes (Docker only) | Binary only |
| Client | Yes | No | Yes |

> **Note:** macOS is not officially supported. The server may build from source on macOS for development, but is not tested or released for it.

---

## Client

Download client assets from the [latest GitHub Release](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest).

### Windows

1. Download `gsbs-client-setup-X.Y.Z-windows-amd64.exe`.
2. Run the installer. If Windows SmartScreen shows a warning, click **More info → Run anyway** (installers are not code-signed yet — see note below).
3. Optional: enable **Run at startup** during install.
4. Launch **GSBS Client** from the Start Menu or system tray.

> **SmartScreen note:** Windows may warn about unsigned executables. Download only from the official [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) page and verify the SHA256 checksum in `SHA256SUMS` if you want extra assurance.

**Auto-update:** The tray menu checks GitHub Releases daily. Use **Install update…** to update without re-running the installer.

### Linux — Flatpak (recommended for Steam Deck / immutable distros)

```bash
flatpak remote-add --if-not-exists gsbs \
  https://dlommm.github.io/gsbs-flatpak/repo/gsbs.flatpakrepo
flatpak install gsbs io.github.dlommm.GSBS
flatpak run io.github.dlommm.GSBS
```

Updates arrive via `flatpak update` or your software center (the in-app self-updater is intentionally disabled in the sandbox). The sandbox grants access to the home folder, the default Steam/Heroic/Lutris/Bottles data locations, and SD cards under `/run/media`; for a Steam library on another internal drive, grant access with:

```bash
flatpak override --user io.github.dlommm.GSBS --filesystem=/path/to/SteamLibrary
```

See [FLATPAK.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/FLATPAK.md) for the Steam Deck Desktop-Mode walkthrough, the full permission list, and Flatseal guidance.

### Linux — Debian/Ubuntu (.deb)

```bash
sudo apt install ./gsbs-client_X.Y.Z_amd64.deb   # resolves the xdg-utils dependency
gsbs-client   # or launch from your application menu
```

> **GNOME:** install the *AppIndicator and KStatusNotifierItem Support* extension so the tray icon shows. No appindicator libraries are needed — the tray is pure-Go D-Bus.

### Linux — AppImage (portable)

```bash
chmod +x gsbs-client-X.Y.Z-x86_64.AppImage
./gsbs-client-X.Y.Z-x86_64.AppImage
```

Requires AppIndicator/status notifier support on your desktop environment:

| Desktop | Status |
|---|---|
| KDE Plasma | Works out of the box |
| Xfce | Works out of the box |
| GNOME | Requires a tray extension (e.g. *AppIndicator and KStatusNotifierItem Support*) |
| Cinnamon | Works out of the box |

### Linux — raw binary

```bash
chmod +x gsbs-client-linux-amd64
./gsbs-client-linux-amd64
```

---

## First run

After server and client are installed:

1. Open the server WebUI (e.g. `https://your-domain` or `http://localhost:8080`).
2. Click **Register** to create your account.
3. In **Settings → API tokens**, create a token for your client.
4. On the client, open the tray menu → **Login…**. Enter the server URL and credentials.
5. GSBS stores the token in `config.json` and starts syncing. Use **Sync now** or **Open local status** from the tray to verify.

Config and logs are stored in:
- **Windows:** `%APPDATA%\gsbs\`
- **Linux:** `~/.config/gsbs/`

---

## Network and firewall

| Direction | Port | Protocol | Purpose |
|---|---|---|---|
| Clients → Server | 443 (HTTPS) or 8080 (HTTP) | TCP | API, WebUI |
| Clients → Server | 443 or 8080 | TCP | SSE event stream |

No inbound ports are needed on client machines. The server must be reachable by all clients.

---

## Disable client update checks

Set in `config.json` to disable daily update checks:

```json
{
  "update_check_enabled": false
}
```

To point at a fork or mirror instead:

```json
{
  "update_repo": "your-org/your-fork"
}
```

---

## Unraid

See the [Unraid deployment guide](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/UNRAID.md) and the example [compose-unraid.yml](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/examples/compose-unraid.yml).

---

## Related pages

- [Server Configuration](Server-Configuration)
- [Client Setup & Usage](Client-Setup-and-Usage)
- [Upgrading](Upgrading)
- [Troubleshooting](Troubleshooting)
