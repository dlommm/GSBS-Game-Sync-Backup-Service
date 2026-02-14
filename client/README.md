# GSBS Client

Windows and Linux client that syncs game saves to the GSBS server.

## Windows: system tray

On Windows, double‑click **gsbs-client.exe** (or run it from Start). It runs in the **system tray** (notification area). No console window.

**First launch:** Config is blank (no server, no token). A **Login** dialog opens automatically so you can connect to your server:
- **Server URL** — e.g. `http://localhost:8080`, `https://your-server:8080`, or your Docker/remote server URL.
- **Username** and **Password** — your GSBS account. Click **Login** to save the token and start syncing.

**Tray menu:**
- **Server: (not set)** or **Server: your-url** — shows current server (click **Login** to change).
- **Open server in browser** — open the server WebUI (only when a server is set).
- **Login...** — open the login dialog to connect to a server or switch account (server URL, username, password). Sync restarts after a successful login.
- **Edit config file** — open `%APPDATA%\gsbs\config.json` in Notepad for advanced options (e.g. watch_paths).
- **Sync now** — run a sync immediately.
- **Quit** — exit the client.

To run with a **console** (e.g. for debugging):  
`gsbs-client.exe --console`

## First-time setup

1. **Login** (saves token to config):
   ```bash
   ./gsbs-client login
   ```
   Enter server URL, username, password. Config is stored in:
   - Windows: `%APPDATA%\gsbs\config.json`
   - Linux: `~/.config/gsbs/config.json`

2. **Add watch paths** by editing the config file. Each entry needs:
   - `game_id` — e.g. Steam App ID or PCGW page name (e.g. `Assassin's_Creed_Rogue`)
   - `path_key` — stable key for this save (e.g. `save_895`)
   - `path_templates` — path templates for your OS (from PCGamingWiki or manual)

   Optional top-level config for path resolution:
   - `ubisoft_connect_folder` — e.g. `C:\Program Files (x86)\Ubisoft\Ubisoft Game Launcher` (for `<Ubisoft-Connect-folder>`)
   - `launcher_user_id` — your launcher user ID (for `<user-id>` in paths like `savegames\<user-id>\895`)

   Steam library paths are discovered automatically from `steamapps/libraryfolders.vdf` (including extra drives).

Example for Assassin's Creed Rogue (Ubisoft Connect, Windows):

```json
{
  "server_url": "http://localhost:8080",
  "token": "<from login>",
  "sync_interval": "5m",
  "ubisoft_connect_folder": "C:\\Program Files (x86)\\Ubisoft\\Ubisoft Game Launcher",
  "launcher_user_id": "your-ubisoft-user-id",
  "watch_paths": [
    {
      "game_id": "311560",
      "path_key": "ubisoft_895",
      "path_templates": [
        "<Ubisoft-Connect-folder>\\savegames\\<user-id>\\895\\"
      ]
    }
  ]
}
```

3. **Run**:
   ```bash
   ./gsbs-client
   ```
   - Pulls all saves and writes only where the target folder exists (game installed).
   - Watches configured paths and uploads on file change.
   - Periodically pulls again (default every 5 minutes).

## Building

**Linux:**
```bash
go build -o gsbs-client ./client
```

**Windows (from repo root):**
```bash
# Tray app (no console window when run)
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o gsbs-client.exe ./client
```
For a console build (shows a window): omit the `-ldflags "-H windowsgui"`.

**Cross-compile from Linux/macOS:**  
The same commands work. For full tray behavior on Windows, build the Windows client on a Windows machine with `CGO_ENABLED=1` if the tray icon does not appear.
