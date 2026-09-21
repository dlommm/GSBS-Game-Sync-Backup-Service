# Release workflow

Every release is a **two-step** publish: code on `main`, then a **semver git tag**. Pushing only to `main` runs [CI](../.github/workflows/ci.yml) (tests and builds); it does **not** publish GitHub Release assets or Docker Hub.

## Primary path (always do both steps)

1. **Update [CHANGELOG.md](../CHANGELOG.md)** for the version.
2. **Commit and push to `main`** — wait for CI green:
   ```bash
   git push origin main
   ```
3. **Create and push the version tag** (from the same `main` commit you just pushed):
   ```bash
   git tag -a v1.0.14 -m "Release v1.0.14"
   git push origin v1.0.14
   ```
   The [Release workflow](../.github/workflows/release.yml) runs on the tag. It always publishes:
   - **GitHub Release** `vX.Y.Z` with binaries, installer, `.deb`, AppImage, manifests
   - **Docker Hub** `dendlomm/gsbs-server:X.Y.Z` and `dendlomm/gsbs-server:latest`
4. **Monitor** the [Release workflow](https://github.com/dlommm/GSBS-Game-Sync-Backup-Service/actions/workflows/release.yml).
5. **Verify** the GitHub Release contains:
   - `gsbs-server-windows-amd64.exe`, `gsbs-client-windows-amd64.exe`
   - `gsbs-server-linux-amd64`, `gsbs-client-linux-amd64`
   - `gsbs-client-setup-X.Y.Z-windows-amd64.exe` (Inno Setup)
   - `gsbs-server-setup-X.Y.Z-windows-amd64.exe` (Inno Setup)
   - `gsbs-client_X.Y.Z_amd64.deb`
   - `gsbs-client-X.Y.Z-x86_64.AppImage`
   - `SHA256SUMS`, `latest-client.json`
6. **Smoke test:** `docker pull dendlomm/gsbs-server:X.Y.Z`, Windows installer, Linux `.deb` or AppImage, client **Check for updates**.

## Manual workflow dispatch

Re-publish an existing version from current `main` (e.g. after a CI fix) without moving the git tag:

```bash
gh workflow run release.yml -f version=v1.1.0
```

This builds from the `main` commit, uploads GitHub Release assets for that tag name, and pushes Docker Hub **`X.Y.Z`** and **`latest`** (same as a tag-triggered run). Prefer **tag push** for normal releases so the git tag points at the shipped commit.

## GitHub secrets

Configure in repository **Settings → Secrets and variables → Actions**:

| Secret | Purpose |
|--------|---------|
| `DOCKERHUB_USERNAME` | Docker Hub login for server image push (release workflow) |
| `DOCKERHUB_TOKEN` | Docker Hub access token |
| `GITHUB_TOKEN` | Provided automatically for release upload |

CI (`ci.yml`) does not use Docker secrets. See [DOCKER.md](DOCKER.md) for local image builds.

## Runners

Every job runs on a GitHub-hosted runner: Linux jobs on `ubuntu-latest`, Windows
builds on `windows-latest`, macOS builds on `macos-14`. There is nothing to
register or keep online.

GSBS previously routed Linux jobs to a self-hosted runner when one was marked
online, resolved by a `runner-resolve.yml` reusable workflow. That was removed:
the runner was deregistered while its `GSBS_RUNNER_ONLINE` repo variable stayed
`true`, so every Linux job — CI *and* release — queued forever against a runner
that no longer existed, with no failure to alert on. Pinning `ubuntu-latest`
directly removes the class of failure. If you reintroduce a self-hosted runner,
gate it on a liveness check that fails fast rather than a manually-set variable.

## Local fallback

```bash
./script/release.sh v1.0.14
```

Requires: `go`, `git`, `gh`, `docker` (buildx), `docker login`, and a Linux host for the Linux client binary (or use CI).

Build only (no release upload):

```bash
./script/build.sh v1.0.14 dist
./script/release-assets.sh v1.0.14 dist
```

## Wiki sync

The GitHub Wiki is automatically synced from `docs/wiki/` in the repository.

| Trigger | What runs |
|---|---|
| Push to `main` (docs files changed) | `sync-wiki.yml` runs `script/sync-wiki.sh`, publishes updated pages |
| Push of `vX.Y.Z` tag | Same workflow runs from the tagged commit — wiki reflects the release snapshot |
| `workflow_dispatch` | Manual re-sync or dry-run: `gh workflow run sync-wiki.yml` or `gh workflow run sync-wiki.yml -f dry_run=true` |

**Source of truth:** `docs/wiki/*.md`. Do not edit wiki pages directly on the GitHub Wiki web UI — changes will be overwritten on the next sync.

**Quality gate:** `script/check-wiki.sh` runs before every sync. It checks required pages, level-1 headings, Related pages sections, and duplicate upgrade procedures. Sync does not run if the check fails.

**Rollback a bad wiki publish:**
1. Fix the source in `docs/wiki/`.
2. Push to `main` — the workflow re-runs automatically.
3. Or: `gh workflow run sync-wiki.yml` to trigger immediately.
4. As a last resort, use the GitHub Wiki web UI to revert to a previous page revision.

## Version policy

- Tags: `vMAJOR.MINOR.PATCH` (semver).
- Pre-releases: `vX.Y.Z-rc.N` — clients skip these unless explicitly supported later.
- `latest-client.json` on each release drives client auto-update checksums.

## Artifact naming contract

Client auto-update expects these asset names:

| Platform | Binary |
|----------|--------|
| Windows amd64 | `gsbs-client-windows-amd64.exe` |
| Linux amd64 | `gsbs-client-linux-amd64` |

Do not rename these without updating `client/update.go` and this document.
