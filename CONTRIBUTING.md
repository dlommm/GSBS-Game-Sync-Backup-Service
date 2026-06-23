# Contributing to GSBS

Thanks for your interest in contributing. This document explains how to build, test, and where to look when making changes.

## Building and running

From the repo root:

```bash
go mod tidy
./script/build-webui.sh   # requires Node.js (npx tailwindcss)
go build -o gsbs-server ./server
go build -o gsbs-client ./client
```

### WebUI assets

The server embeds compiled CSS and static JS. After changing templates or `server/webui/static/src/input.css`, rebuild assets:

```bash
./script/build-webui.sh    # requires Node.js (npx tailwindcss)
go run ./cmd/gen-branding  # regenerates all icons/branding from assets/images/
```

`script/release.sh` runs `build-webui.sh` automatically before building server binaries.

The server needs CGO enabled for SQLite (`CGO_ENABLED=1`, which is the default). See [README.md](README.md) and [docs/DOCKER.md](docs/DOCKER.md) for run instructions and environment variables.

## Running tests

```bash
go test ./server/... ./pkg/... ./client/...
```

With race detector and coverage (matches CI):

```bash
go test -race -coverprofile=coverage.out ./server/... ./pkg/... ./client/...
go tool cover -func=coverage.out
```

Tests live under `server/` (e.g. `server/auth/auth_test.go`, `server/store/sqlite_test.go`, `server/api/handler_test.go`). Use an in-memory SQLite DB (`:memory:`) in tests where possible.

## Lint

CI runs [golangci-lint](https://golangci-lint.run/) with config in [.golangci.yml](.golangci.yml). Run locally before pushing:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
golangci-lint run --timeout=5m
```

Release builds also require lint to pass (see [.github/workflows/release.yml](.github/workflows/release.yml)).

## Where to work (skills and subagents)

The project uses **skills** and **subagents** to scope work. Use the right one so conventions and context stay consistent:

| Focus | Skill / subagent | Where to look |
|-------|-------------------|----------------|
| Server API, auth, store, WebUI, jobs | **gsbs-server** | `server/`, `server/api/handler.go`, `server/store/`, `server/auth/`, `server/webui/`, `server/job/` |
| Client sync, watcher, config, tray, list | **gsbs-client** | `client/`, `client/sync/`, `client/config.go`, `client/manifest.go` |
| PCGW, path resolution, placeholders, manifest | **gsbs-pcgw-paths** | `pkg/pcgw/`, `pkg/paths/`, `server/job/pcgw.go` |
| Build, release script, Docker, version | **gsbs-release** | `script/release.sh`, `Dockerfile`, `docs/DOCKER.md` |

- **Skills**: `.cursor/skills/<name>/SKILL.md` — quick reference and checklists.
- **Subagents**: `.cursor/agents/<name>.md` — invoke for focused multi-file work (e.g. `/gsbs-server`).
- **Rules**: `.cursor/rules/` and [AGENTS.md](AGENTS.md) — when to delegate and how to keep skills/subagents in sync.

When you change behavior in one of these areas, update the matching skill and subagent (see `.cursor/rules/skills-subagents-sync.mdc`).

## Conventions

- **API**: Extend `server/api/handler.go`; use `withAuth` for authenticated routes; keep request/response types in `pkg/types` when shared.
- **Client**: Never write pulled saves when the target directory does not exist (folder-exists rule). Use `pkg/paths.Resolver` for placeholders.
- **Store**: Add new methods to `server/store/store.go` and implement in `server/store/sqlite.go`; use `context.Context` on all store calls.
- **Docs**: Update [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for data model or flow changes; [docs/EXAMPLE_CONFIG.md](docs/EXAMPLE_CONFIG.md) for client config options.

## Documentation and wiki

The **[GSBS GitHub Wiki](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/wiki)** is the primary user-facing documentation hub.

**Source of truth:** `docs/wiki/` in this repository. The wiki is a published view — automatically synced from source on every push to `main` (when docs files change) and on every release tag.

**Do not edit wiki pages directly on GitHub** — changes will be overwritten on the next automatic sync.

### Authoring wiki pages

1. Edit or create files in `docs/wiki/` following the [wiki style guide](docs/wiki/README.md):
   - Every page needs a `# Title`, lead paragraph, `---` before the first section, and `## Related pages` at the bottom.
   - Use full GitHub raw URLs for images.
   - Use wiki page slugs for internal links (e.g. `[Upgrading](Upgrading)`).
   - All upgrade procedures must live in `docs/wiki/Upgrading.md`; other pages link there.
2. Run quality checks locally:
   ```bash
   bash script/check-wiki.sh
   ```
3. Push to `main`. The `sync-wiki.yml` workflow runs checks and publishes automatically.

### Manual sync

```bash
gh workflow run sync-wiki.yml
```

Dry-run (transform pages, do not push):

```bash
gh workflow run sync-wiki.yml -f dry_run=true
```

### Rollback a bad publish

Fix the source in `docs/wiki/` and push to `main`. Or trigger `gh workflow run sync-wiki.yml`. As a last resort, use the GitHub Wiki web UI to revert a page to a previous revision.

## API reference

A short API reference for the server is in [docs/API.md](docs/API.md). Use it for integrating third-party clients or debugging requests.
