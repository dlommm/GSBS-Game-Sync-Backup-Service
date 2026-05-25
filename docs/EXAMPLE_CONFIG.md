# Example client config

Example `watch_paths` for **Assassin's Creed Rogue** (Steam App ID 311560), based on [PCGamingWiki](https://www.pcgamingwiki.com/wiki/Assassin%27s_Creed_Rogue):

- **Ubisoft Connect (Windows):** `<Ubisoft-Connect-folder>\savegames\<user-id>\895\` (Worldwide), `1186\` (Russia), `1661\` (Asia)
- **Steam (Windows):** same base path with `934\`, `1187\`, `1662\`
- **Steam Play (Linux):** `<SteamLibrary-folder>/steamapps/compatdata/311560/pfx/` (then follow [Note 1] on the wiki for the actual path inside the prefix)

You must set the resolver’s `UbisoftConnect` and optionally `UserID` (or rely on default Steam library paths). The client’s path resolver uses:

- `%USERPROFILE%`, `%LOCALAPPDATA%`, `%APPDATA%` (Windows)
- `$HOME`, `~/.local/share`, `~/.config` (Linux)
- `<SteamLibrary-folder>` → first existing of: `C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`, or `~/.steam/steam` / `~/.local/share/Steam` (Linux)
- `<Ubisoft-Connect-folder>`, `<GOG-Galaxy-folder>`, `<Epic-Games-folder>`, `<Xbox-App-folder>`, `<EA-App-folder>`, `<Heroic-folder>`, `<Lutris-folder>`, `<Bottles-folder>`, `<Prism-folder>`, `<Flatpak-Steam-folder>` → auto-detected on startup or set in config

**Near-zero-config example** (auto-watch discovered games, no manual `watch_paths`):

```json
{
  "server_url": "https://your-gsbs-server.example.com",
  "token": "YOUR_TOKEN_FROM_LOGIN",
  "client_name": "My-Linux-PC",
  "sync_interval": "5m",
  "auto_watch_mode": "discovered",
  "conflict_policy": "last_write_wins",
  "encryption_passphrase": "your-local-passphrase-if-e2e-enabled",
  "discovery_interval": "4h"
}
```

Existing configs without `auto_watch_mode` keep legacy behavior (watch any manifest path whose directory exists).

If the client logs `sync: no watch paths — skipped discovered=… platform=… missing_dir=… malformed=…`, check: games discovered (tray **Discovered games** / `discovery.json`), save folders exist on disk, and manifest entries match your OS. See [CLIENT.md](CLIENT.md#auto-watch-modes).

**Pattern-filtered watch path** (upload only `*.sav` under a directory):

```json
{
  "watch_paths": [
    {
      "game_id": "311560",
      "path_key": "ac_rogue_saves",
      "directory": "<SteamLibrary-folder>/steamapps/compatdata/311560/pfx/drive_c/users/steamuser/Documents/MySaves",
      "include_patterns": ["*.sav"],
      "sync_all": false
    }
  ]
}
```

Use `sync_all: true` (and omit `include_patterns`) to push every file in the directory. Multiple patterns or wildcards get a per-file `path_key` on the server (see sync behavior in [CLIENT.md](CLIENT.md)).

**Linux launcher paths** — auto-detected on startup; override when detection fails (Flatpak, custom install locations):

```json
{
  "server_url": "https://your-gsbs-server.example.com",
  "token": "YOUR_TOKEN_FROM_LOGIN",
  "client_name": "My-Linux-PC",
  "auto_watch_mode": "discovered",
  "bottles_folder": "/home/you/.var/app/com.usebottles.bottles/data/bottles",
  "prism_folder": "/home/you/.local/share/PrismLauncher",
  "flatpak_steam_folder": "/home/you/.var/app/com.valvesoftware.Steam/.local/share/Steam",
  "heroic_folder": "/home/you/Games/Heroic",
  "lutris_folder": "/home/you/Games/lutris"
}
```

Use tray **Detect launcher paths** to merge auto-detected values into `config.json`.

To support multiple Steam libraries (e.g. another drive), extend the resolver’s `SteamLibraries` from `libraryfolders.vdf` in the Steam install directory.

**Minimal Windows example** (one save folder, Worldwide):

```json
{
  "server_url": "https://your-gsbs-server.example.com",
  "token": "YOUR_TOKEN_FROM_LOGIN",
  "client_name": "My-Desktop",
  "sync_interval": "5m",
  "watch_paths": [
    {
      "game_id": "311560",
      "path_key": "ac_rogue_ubisoft_895",
      "path_templates": [
        "<Ubisoft-Connect-folder>\\savegames\\<user-id>\\895\\"
      ]
    }
  ]
}
```

`sync_interval` accepts human-friendly durations: `"30s"`, `"5m"`, `"1h"`, `"2h30m"`. The client will pull saves from the server at this interval. Optional `manifest_include`: `"saves"` (save locations only), `"config"` (config/settings paths only), or `"both"` (default). Optional `max_sync_kbps`: cap sync bandwidth in KiB/s (e.g. `256` or `1024`); omit or `0` for no limit. Optional `skip_sync_when_metered`: (Windows) when true, do not run pull or push when Windows reports the connection as metered (requires Windows 10 2004+). Optional `watch_exclude`: list of glob patterns for files to ignore when watching (e.g. `["*.tmp", "*.bak", "*.log"]`); matching files are not pushed to the server. Optional `use_compression`: when true, use gzip for push body and request gzip for pull (server must support it). File changes are pushed immediately (with a 2-second debounce). Set `"backup_on_pull": true` to copy an existing file to `<path>.gsbs.bak` before overwriting it on pull. Set `"skip_overwrite_when_local_newer": true` to avoid overwriting a local file when it is newer than the server version (useful when you have local-only changes).

**Linux (Steam Play)** — sync the Proton prefix save folder (you may need to resolve `[Note 1]` from the wiki, often `drive_c/users/steamuser/Documents/...` or similar):

```json
{
  "watch_paths": [
    {
      "game_id": "311560",
      "path_key": "ac_rogue_steam_linux",
      "path_templates": [
        "<SteamLibrary-folder>/steamapps/compatdata/311560/pfx/drive_c/users/steamuser/Documents/Assassin's Creed Rogue"
      ]
    }
  ]
}
```

To dump save location templates from PCGamingWiki for this game, run from the repo root:
`go run ./cmd/pcgw-fetch 311560`
(or build once: `go build -o pcgw-fetch ./cmd/pcgw-fetch`, then `./pcgw-fetch 311560`). Use the output to adapt `path_templates` and `path_key` to your setup.
