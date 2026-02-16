# GSBS Client

Windows and Linux client that syncs game saves to the GSBS server.

## How sync works (server → client → games on this machine)

1. **Server**: A job (e.g. PCGW sync) fills the server’s **game save locations** (manifest) from PCGamingWiki: which games have known save paths per platform.
2. **Manifest**: The client fetches the manifest from the server (`GET /api/manifest`). No login required for the manifest.
3. **Client checks this machine**: For each manifest entry for your OS, the client resolves the path (e.g. `%APPDATA%\...`, `<SteamLibrary-folder>\...`). It only watches and syncs paths where **the directory actually exists** — i.e. the game is installed on this computer.
4. **Push**: When a watched save file changes, the client uploads it to the server.
5. **Pull**: When the client runs a sync (or “Sync now”), it downloads all your saves from the server and writes each one only if the target directory exists locally (game installed). If the game isn’t installed, that save is skipped.

So “what games can be saved and pushed” = games from the manifest (plus any manual `watch_paths` in config) where the save folder exists on this machine. Use **`gsbs-client list`** to see that list.

## Checking what games can be saved and synced

From a terminal (or `gsbs-client.exe --console` on Windows):

```bash
gsbs-client list
```

This will:

- Use your configured **server URL** (and cached or live manifest from the server).
- Resolve all known game save paths for your OS and show only games where the save **directory exists** on this machine (i.e. the game is installed).
- If you are logged in (`token` in config), it also shows whether each game has a save **on the server** (`[synced on server]`) or not (`[not on server]`).

Example output:

```
Games that can be saved and synced on this machine:
(Save directory exists locally; client will watch and push changes, and pull server saves here.)

  Elden Ring  game_id=1245620 path_key=abc123 [synced on server]
    C:\Users\You\AppData\Roaming\Elden Ring\...
  Some Game  game_id=456 path_key=def456 [not on server] (from config)
    D:\Games\SomeGame\saves
```

Requires `server_url` in config (e.g. from `gsbs-client login`). The server must be running and should have run the PCGW sync job so the manifest is populated.

## Windows: system tray

On Windows, double‑click **gsbs-client.exe** (or run it from Start). It runs in the **system tray** (notification area). No console window.

**First launch:** Config is blank (no server, no token). A **Login** dialog opens automatically so you can connect to your server:
- **Server URL** — e.g. `http://localhost:8080`, `https://your-server:8080`, or your Docker/remote server URL.
- **Username** and **Password** — your GSBS account. Click **Login** to save the token and start syncing.

**Tray menu (quick actions at top):**
- **Server: (not set)** or **Server: your-url** — shows current server (click **Login** to change).
- **Sync now** — run a sync immediately (quick action).
- **Open server in browser** — open the server WebUI (quick action; only when a server is set).
- **Login...** — open the login dialog to connect to a server or switch account. Sync restarts after a successful login.
- **Edit config file** — open `%APPDATA%\gsbs\config.json` (uses EDITOR/VISUAL if set, else tries VS Code, else default app e.g. Notepad).
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

   Optional top-level config: `max_sync_kbps` (KiB/s limit for sync; 0 = no limit), `verbose_log` (extra log detail; restart after change). Path resolution:
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

3. **See which games can be synced** (optional):
   ```bash
   ./gsbs-client list
   ```
   Shows games from the manifest (and config) where the save folder exists on this machine.

4. **Run**:
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
