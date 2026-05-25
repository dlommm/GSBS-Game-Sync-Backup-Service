# .cursor — GSBS agent setup

This folder configures Cursor rules, subagents, and skills so work stays scoped and context usage stays low.

## Quick orientation

| What | Where | Purpose |
|------|--------|---------|
| **Rules** | `.cursor/rules/*.mdc` | When to delegate, path-based hints, conventions, testing, security |
| **Subagents** | `.cursor/agents/*.md` | Focused agents per domain; invoke with `/gsbs-server`, `/gsbs-client`, etc. |
| **Skills** | `.cursor/skills/<name>/SKILL.md` | Quick reference and checklists per domain |
| **This index** | `.cursor/README.md` | You are here |
| **Role table** | `.cursor/SUBAGENTS.md` | When to use which skill/subagent |
| **Agent guide** | Repo root `AGENTS.md` | Entry point for new agents; points to rules and subagents |

## Rules (summary)

- **Always applied**: `gsbs-project.mdc`, `use-subagents-when-needed.mdc`, `skills-subagents-sync.mdc`, `push-releases.mdc`, `github-push-includes-dockerhub.mdc`
- **When editing Go**: `go-conventions.mdc`, `gsbs-testing.mdc`
- **Path-scoped** (apply when you're in that path): `delegate-server.mdc` (server/), `delegate-client.mdc` (client/), `delegate-pcgw-paths.mdc` (pkg/), `delegate-release.mdc` (script/), `delegate-release-version.mdc` (**/version.go), `delegate-release-docker.mdc` (Dockerfile), `delegate-cmd.mdc` (cmd/)
- **Security**: `gsbs-no-secrets.mdc` — never commit secrets; use env vars

## Key project docs (outside .cursor)

- **docs/ARCHITECTURE.md** — Data model, sync flow, path resolution, PCGW, DB schema
- **docs/API.md** — Endpoint reference (auth, saves, manifest, SSE, admin)
- **docs/CLIENT.md** — Client behavior and usage
- **docs/EXAMPLE_CONFIG.md** — Config shape and path templates
- **docs/DOCKER.md** — Build, run, env, Docker Hub
- **CONTRIBUTING.md** — Build, test, and full skill/subagent table

## How to use

1. **Starting a task**: If it fits one domain (server, client, PCGW/paths, release), invoke that subagent at the start (e.g. `/gsbs-server`). Do not do the work in the main chat first — subagents keep context low.
2. **Path-scoped rules**: When you open a file under `server/`, `client/`, `pkg/`, `script/`, `cmd/`, or a `version.go` / `Dockerfile`, the matching delegate rule is in context; use the suggested subagent for implementation.
3. **After code changes**: Run `go test ./server/... ./pkg/...` when you change server or pkg code; see `gsbs-testing.mdc` and CONTRIBUTING.md.
4. **Keeping .cursor accurate**: When you change API, client, PCGW/paths, or release behavior, update the matching skill and subagent (see `skills-subagents-sync.mdc`). When you add a new domain, consider adding a path-scoped `delegate-*.mdc` rule and updating this README and SUBAGENTS.md.

## Optional: .cursorignore (repo root)

To reduce context noise and avoid indexing secrets, create a **`.cursorignore`** at the **repo root** (same level as `go.mod`) with content like:

```
# Build outputs and binaries
gsbs-server
gsbs-client
gsbs-server-*
gsbs-client-*
*.exe
/dist/
/build/

# Environment and secrets (do not index)
.env
.env.*
*.env.local

# Git and IDE
.git/
.idea/
.vscode/

# Logs and local DBs
*.log
*.db
*.db-shm
*.db-wal
```

This keeps Cursor from sending build artifacts and env files into context.
