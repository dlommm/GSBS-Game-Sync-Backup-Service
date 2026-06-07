# Contributing / Development

> How to build GSBS from source, run tests, contribute code, and keep documentation in sync.

---

## Building from source

**Requirements:**
- Go 1.25+
- Node.js (for WebUI CSS compilation via Tailwind)
- CGO enabled (default; required for SQLite)
- Linux build deps for the client: `libayatana-appindicator3-dev`, `libgtk-3-dev`, `pkg-config`, `gcc`

```bash
git clone https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-.git
cd GSBS--Game-Sync---Backup-Service-
go mod tidy
./script/build-webui.sh   # compile Tailwind CSS (requires Node.js)
go build -o gsbs-server ./server
go build -o gsbs-client ./client
```

### WebUI assets

After changing templates or `server/webui/static/src/input.css`, rebuild assets:

```bash
./script/build-webui.sh
go run ./cmd/resize-icon   # regenerates favicon.png and logo.png from docs/images/
```

`script/release.sh` runs `build-webui.sh` automatically before building server binaries.

---

## Running the server locally

```bash
export GSBS_SESSION_SECRET="dev-secret-not-for-production"
export GSBS_DB="./gsbs-dev.db"
./gsbs-server
```

Or use Docker:

```bash
docker compose -f docker-compose.dev.yml up --build
```

Open `http://localhost:8080`.

---

## Running tests

```bash
go test ./server/... ./pkg/... ./client/...
```

With race detector and coverage (matches CI):

```bash
go test -race -coverprofile=coverage.out ./server/... ./pkg/... ./client/...
go tool cover -func=coverage.out
```

Tests use an in-memory SQLite database (`:memory:`) where possible. Key test files:

| File | Covers |
|---|---|
| `server/auth/auth_test.go` | Auth, tokens, 2FA |
| `server/store/sqlite_test.go` | Store interface, migrations |
| `server/api/handler_test.go` | API endpoints |
| `client/update_test.go` | Update check logic |

---

## Lint

CI runs [golangci-lint](https://golangci-lint.run/) with config in `.golangci.yml`. Run locally before pushing:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint run --timeout=5m
```

Also run vulnerability scan:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## Code conventions

### API and server

- Add new endpoints in `server/api/handler.go`; use `withAuth` for authenticated routes.
- Keep request/response types in `pkg/types` when shared across server and client.
- Add new store methods to `server/store/store.go` (interface) and implement in `server/store/sqlite.go`.
- Always use `context.Context` on store calls.

### Client

- Never write pulled saves when the target directory does not exist (**folder-exists rule**).
- Use `pkg/paths.Resolver` for all placeholder resolution.
- Platform-specific code goes in `_windows.go` / `_linux.go` files.

### Database

- Add schema changes as a new migration step using `PRAGMA user_version`.
- Write the migration idempotently; test against an existing DB in `sqlite_test.go`.
- Document the migration in the `[Upgrading](Upgrading)` wiki page.

### Documentation

- Update [docs/ARCHITECTURE.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/ARCHITECTURE.md) for data model or sync flow changes.
- Update [docs/API.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/API.md) for API endpoint changes.
- Wiki pages in `docs/wiki/` are the published docs — update them when behavior changes. See the [wiki style guide](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/wiki/README.md).
- All upgrade instructions must go in [Upgrading](Upgrading); other pages must link there.

---

## Where to work (skills and subagents)

The project uses Cursor **skills** and **subagents** to scope AI-assisted work:

| Focus | Skill / subagent | Key paths |
|---|---|---|
| Server API, auth, store, WebUI, jobs | **gsbs-server** | `server/`, `server/api/handler.go`, `server/store/`, `server/webui/` |
| Client sync, watcher, config, tray, list | **gsbs-client** | `client/`, `client/sync/`, `client/config.go` |
| PCGW, path resolution, placeholders, manifest | **gsbs-pcgw-paths** | `pkg/pcgw/`, `pkg/paths/`, `server/job/pcgw.go` |
| Build, release script, Docker, version | **gsbs-release** | `script/release.sh`, `Dockerfile`, `docs/DOCKER.md` |

Skills: `.cursor/skills/<name>/SKILL.md`. Subagents: `.cursor/agents/<name>.md`.

When you change behavior in any area, update the matching skill and subagent so future agents stay accurate.

---

## Wiki and documentation

### How the wiki works

- `docs/wiki/` in the repository is the **canonical authoring source** for all wiki pages.
- The GitHub Wiki (`*.wiki.git`) is the **published view** — automatically synced by [`.github/workflows/sync-wiki.yml`](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/.github/workflows/sync-wiki.yml).

**Sync triggers:**
- Push to `main` when files in `docs/wiki/`, `docs/*.md`, `README.md`, or `CONTRIBUTING.md` change.
- Push of a version tag (`v*`) for a release snapshot.
- Manual: `gh workflow run sync-wiki.yml` or via **Actions → sync-wiki → Run workflow**.

### Wiki authoring rules

1. Edit files in `docs/wiki/` (not directly on the GitHub Wiki web UI).
2. Follow the [wiki style guide](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/wiki/README.md) — headings, callouts, internal links, image URLs.
3. All upgrade procedures must be in [Upgrading](Upgrading); other pages link there.
4. Run the quality checks locally before pushing:

```bash
./script/check-wiki.sh
```

This checks for: required page titles, broken relative links, missing language tags on code blocks, and duplicate upgrade procedures outside `Upgrading.md`.

### Rollback a bad wiki sync

If a sync publishes incorrect content:

1. Fix the source in `docs/wiki/`.
2. Push to `main` — the sync workflow re-runs automatically.
3. Or manually trigger: `gh workflow run sync-wiki.yml`.

The GitHub Wiki web UI can also be used to revert to a previous revision (wiki pages have revision history).

---

## Release workflow

The release workflow is documented for maintainers in [docs/RELEASE.md](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/blob/main/docs/RELEASE.md). Summary:

1. Update `CHANGELOG.md`.
2. Commit and push to `main`; wait for CI green.
3. Push a semver tag: `git tag -a vX.Y.Z -m "Release vX.Y.Z" && git push origin vX.Y.Z`
4. The release workflow publishes GitHub Release assets and Docker Hub images.
5. The sync-wiki workflow publishes a tagged doc snapshot to the wiki.

---

## Security

**Do not open public GitHub issues for security vulnerabilities.**

Report security issues privately by emailing the maintainer (contact on the [GitHub profile](https://github.com/dlommm)). Include:

- Description of the issue
- Steps to reproduce
- Impact assessment
- Suggested fix (optional)

We aim to acknowledge reports within 72 hours.

---

## Related pages

- [Home](Home)
- [API Reference](API-Reference)
- [How It Works](How-It-Works)
- [Upgrading](Upgrading)
