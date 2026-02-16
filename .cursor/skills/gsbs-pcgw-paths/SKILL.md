---
name: gsbs-pcgw-paths
description: Works with PCGamingWiki integration and path resolution: pkg/pcgw (API, wikitext parsing), pkg/paths (resolver, placeholders, Steam VDF), manifest and game_save_locations. Use when adding placeholders, fixing path resolution, parsing PCGW templates, or extending the PCGW sync or manifest.
---

# GSBS PCGW & Paths

**To keep context low:** For implementation or multi-file work in pkg/pcgw, pkg/paths, or the PCGW job/cmd tools, invoke the **gsbs-pcgw-paths** subagent (`.cursor/agents/gsbs-pcgw-paths.md` or `/gsbs-pcgw-paths`) at the start of the task instead of doing it in the main chat.

## Scope

- **pkg/pcgw/** — client.go (MediaWiki API, ListGamePages, ParsePageWikitext), parse_wikitext.go (ParseSaveLocationsFromWikitext). Output normalized path templates and platform (windows/linux).
- **pkg/paths/** — resolve.go (Resolver, ReplacePlaceholders, PathExists, CurrentOS), steam_vdf.go (libraryfolders.vdf parsing for Steam library roots). Placeholders must match what PCGW and config use.
- **Manifest**: Server table `game_save_locations`; columns: game_id, pcgw_page_id, game_title, platform, path_template, is_config, updated_at, source. Unique (game_id, platform, path_template). Filled by `server/job/pcgw.go` or `cmd/pcgw-sync`; read by `GET /api/manifest`.
- **cmd/pcgw-sync**, **cmd/pcgw-fetch** — standalone tools for syncing or fetching one game's save locations from PCGW.

## Placeholders (normalized form)

- Windows env: `%USERPROFILE%`, `%LOCALAPPDATA%`, `%APPDATA%`.
- Cross-platform: `<SteamLibrary-folder>`, `<Ubisoft-Connect-folder>`, `<user-id>`.
- Linux: same; Resolver maps to `$HOME`, `~/.local/share`, `~/.config` and Steam/Proton paths. Proton: `<SteamLibrary-folder>/steamapps/compatdata/<AppID>/pfx/...`.
- Resolver fills: SteamLibraries (from libraryfolders.vdf or defaults), UbisoftConnect, UserID, Home, LocalAppData, AppData.

## Conventions

- PCGW rate limit: 1 request/second when calling API from job or cmd.
- Parse wikitext for "Save game data location" / "Configuration file(s) location"; normalize to resolver placeholders so client can expand. Store platform as "windows" or "linux" (pcgw.SystemToPlatform).
- Adding a new placeholder: add to `pkg/paths` Resolver and ReplacePlaceholders; document in `docs/EXAMPLE_CONFIG.md` and ARCHITECTURE path resolution section.

## Checklist for PCGW/path changes

- [ ] Path templates in DB and manifest use the same placeholder names as Resolver.
- [ ] New launcher/platform: add resolver field and ReplacePlaceholders logic; update docs.
- [ ] PCGW job: use store.UpsertGameSaveLocations; keep rate limit.
