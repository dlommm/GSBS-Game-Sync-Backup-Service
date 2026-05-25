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

## Self-hosted runner (optional)

Linux CI and release jobs prefer a **self-hosted** runner when one is **online**; otherwise they fall back to `ubuntu-latest`. Windows release builds always use `windows-latest`.

Resolution runs in `.github/workflows/runner-resolve.yml` at the start of each workflow (always on `ubuntu-latest`) so jobs never queue on an offline self-hosted runner.

**Runner setup**

1. Register the runner on the repo with label `self-hosted` (GitHub’s default).
2. Install on the host: **Docker** (with Buildx for multi-arch release images), **Go 1.25** (or let `setup-go` install it), **Node 22+**, GitHub Actions runner **≥ 2.327.1** (required for Node 24 action runtimes), and Linux client build deps (`libayatana-appindicator3-dev`, `libgtk-3-dev`, `pkg-config`, `gcc`, `file`). Jobs use `sudo apt-get` when deps are missing. AppImage builds use `APPIMAGE_EXTRACT_AND_RUN` (no FUSE required).
3. Ensure the runner user can run `sudo` non-interactively for apt, or pre-install the packages above.
4. **Mark the runner online** when it starts (so CI routes Linux jobs here instead of GitHub-hosted):

   ```bash
   # On the runner host — requires gh CLI + repo admin auth
   ./script/ci-runner-online.sh true
   ```

   Hook into your runner service (systemd example):

   ```ini
   ExecStartPre=/path/to/GSBS/script/ci-runner-online.sh true
   ExecStopPost=/path/to/GSBS/script/ci-runner-online.sh false
   ```

   Or set repository variable **GSBS_RUNNER_ONLINE** to `true` manually in GitHub → Settings → Variables.

**Optional:** add secret **GSBS_RUNNER_CHECK_TOKEN** (classic PAT with `repo` scope) to auto-detect online runners via API instead of the variable.

**Force GitHub-hosted Linux**

Set repository variable **GSBS_USE_SELF_HOSTED** to `false` (Settings → Secrets and variables → Actions → Variables).

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
