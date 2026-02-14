# GSBS Client

Windows and Linux client that syncs game saves to the GSBS server.

**Build and run (from repo root):**
```bash
go build -o gsbs-client ./client && ./gsbs-client login
```

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

```bash
GOOS=windows GOARCH=amd64 go build -o gsbs-client.exe .
GOOS=linux   GOARCH=amd64 go build -o gsbs-client .
```
