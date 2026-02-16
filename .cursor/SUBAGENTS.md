# GSBS Subagents & Skills

Use these **roles** to scope work and apply the right **project skill** and **subagent**. Each domain has a matching skill (`.cursor/skills/<name>/SKILL.md`) and subagent (`.cursor/agents/<name>.md`). Skills give quick reference; subagents get their own context and are good for multi-file or parallel work. When a task spans multiple areas, use the relevant skill/subagent in order (e.g. add a new API endpoint → gsbs-server; then if the client must call it → gsbs-client).

**Invoke subagents**: `/gsbs-server`, `/gsbs-client`, `/gsbs-pcgw-paths`, `/gsbs-release` in chat, or ask the agent to use the appropriate subagent.

## When to use which skill / subagent

| Focus / task | Skill & subagent | Where to look |
|--------------|---------------|---------------|
| **Server API, auth, store, WebUI, cron** | **gsbs-server** | `server/`, `server/api/handler.go`, `server/store/`, `server/auth/`, `server/webui/`, `server/job/` |
| **Client sync, watcher, config, tray, list** | **gsbs-client** | `client/`, `client/sync/`, `client/config.go`, `client/manifest.go`, `client/list.go` |
| **PCGW, path resolution, placeholders, manifest** | **gsbs-pcgw-paths** | `pkg/pcgw/`, `pkg/paths/`, `server/job/pcgw.go`, `cmd/pcgw-sync`, `cmd/pcgw-fetch` |
| **Build, release, Docker, version** | **gsbs-release** | `script/release.sh`, `Dockerfile`, `docs/DOCKER.md`, `server/version.go`, `client/version.go` |

Keep skills and subagents in sync when the codebase or plan changes; see rule **skills-subagents-sync.mdc** in `.cursor/rules/`.

## Role summaries

- **Server agent**: Adds or changes API routes, store methods, auth, WebUI pages, SSE hub, job runner, or the PCGW sync job. Keeps manifest and save APIs consistent with `pkg/types` and the folder-exists rule on the client. Admin page: stats, jobs dashboard, manifest viewer, fetch log, users, clients. SSE push to clients via `server/sse/hub.go`. Job tracking via `server/job/runner.go` and `job_runs` table.
- **Client agent**: Adds or changes sync behavior, watch logic, config shape, list output, SSE listener, or tray. Never writes pulled saves when the target directory does not exist; uses `pkg/paths` for resolution. SSE listener auto-reconnects and re-fetches manifest on `manifest-updated` events.
- **PCGW/paths agent**: Adds placeholders, fixes path parsing, improves wikitext parsing, or extends manifest/sync job. Keeps placeholder names in sync between PCGW output, DB, and `pkg/paths.Resolve`.
- **Release agent**: Cuts releases, updates version/ldflags, maintains Docker and release docs. Ensures server and client version info and artifacts are correct.

## Completing the project

- **API completeness**: Use **gsbs-server** to add any missing endpoints (e.g. manifest `?since=`, client list by user, health).
- **Client robustness**: Use **gsbs-client** for retries, backoff, conflict handling, and clearer list/tray feedback.
- **Manifest & discovery**: Use **gsbs-pcgw-paths** to improve PCGW coverage, placeholder set, and resolver (e.g. more launchers).
- **Ship it**: Use **gsbs-release** for versioning, multi-platform builds if needed, and Docker/production notes.

Always combine with the Cursor rules (`.cursor/rules/`) and `docs/ARCHITECTURE.md` for the full plan and conventions.
