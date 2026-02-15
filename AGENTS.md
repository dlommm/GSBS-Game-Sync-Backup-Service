# Agent guide — GSBS

**GSBS** (Game Sync & Backup Service) is a Go project: a central server plus Windows/Linux clients that sync game saves using path keys and PCGamingWiki-based save locations.

## For new agents

1. **Project context** is in Cursor rules under `.cursor/rules/`:
   - **gsbs-project.mdc** (always applied): architecture, repo layout, key concepts (path_key, path resolution, PCGW), where to find auth, API, client sync, config.
   - **go-conventions.mdc** (when editing `**/*.go`): module layout, shared `pkg/types`, server store interface, client OS-specific code, style.

2. **Skills** (use when working in that area) are in `.cursor/skills/`:
   - **gsbs-server** — API, auth, store, WebUI, server-side PCGW job.
   - **gsbs-client** — Config, sync, watcher, tray, list; folder-exists rule.
   - **gsbs-pcgw-paths** — PCGW API/wikitext, path resolution, placeholders, manifest.
   - **gsbs-release** — Version, release script, Docker, builds.

3. **Subagents** (delegate for focused or parallel work) are in `.cursor/agents/`: `gsbs-server.md`, `gsbs-client.md`, `gsbs-pcgw-paths.md`, `gsbs-release.md`. **Delegate without being asked** when the task clearly fits (API/auth/store/WebUI → gsbs-server; client sync/config/tray → gsbs-client; PCGW/paths → gsbs-pcgw-paths; build/release/Docker → gsbs-release). Rule **use-subagents-when-needed.mdc** defines when to delegate. You can also invoke explicitly with `/gsbs-server`, `/gsbs-client`, etc.

4. **Keeping skills and subagents in sync**: Rule **skills-subagents-sync.mdc** in `.cursor/rules/` — when you change API, client, PCGW/paths, or release behavior, update the matching skill and subagent so they stay accurate.

5. **Authoritative design** is in `docs/ARCHITECTURE.md`: data model, sync flow, path resolution, PCGW integration, DB schema, security.

6. **When changing behavior**: Prefer extending `pkg/types` and the server `Store` interface rather than duplicating types. Client must only write pulled saves when the target directory exists.

7. **Build**: From repo root, `go build -o gsbs-server ./server` and `go build -o gsbs-client ./client`. See `README.md` and `docs/DOCKER.md` for run/deploy.

Use the rules, skills, and `docs/ARCHITECTURE.md` as the source of truth for the plan and code layout.
