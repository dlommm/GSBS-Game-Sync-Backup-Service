---
name: gsbs-release
description: Builds and releases GSBS server and client: version ldflags, script/build.sh, script/release.sh, GitHub Actions release.yml, Dockerfile, Inno Setup, .deb/AppImage packaging. Use when creating releases, updating version, building Docker images, or modifying release or deploy docs.
---

# GSBS Build & Release

**To keep context low:** For release cuts, version/ldflags, CI, packaging, or Docker changes, invoke the **gsbs-release** subagent at the start of the task.

## Scope

- **Version**: `server/version.go` and `client/version.go` — `Version`, `BuildDate`, `Commit` via ldflags.
- **Build**: `script/build.sh VERSION [OUT_DIR] [PLATFORMS]` — WebUI CSS, four release binaries.
- **Manifests**: `script/release-assets.sh VERSION [DIR]` — `SHA256SUMS`, `latest-client.json`.
- **Release (local)**: `script/release.sh [VERSION]` — build, tag, gh release, Docker Hub push.
- **Release (CI)**: `.github/workflows/release.yml` on tag `v*` — GitHub Release + Docker Hub; `ci.yml` has no Docker job; see `docs/RELEASE.md`.
- **CI runners**: `.github/workflows/runner-resolve.yml` — Linux jobs use `self-hosted` when online, else `ubuntu-latest`; override with repo var `GSBS_USE_SELF_HOSTED=false`.
- **Packaging**:
  - Windows: `script/packaging/windows/gsbs-client.iss`, `build-installer.sh` (Inno Setup)
  - Linux: `script/packaging/linux/nfpm.yaml`, `build-deb.sh`, `build-appimage.sh`
- **Docker**: `Dockerfile`, `script/docker-entrypoint.sh` (non-root + volume chown), `docker-compose.yml`, `docker-compose.dev.yml`, `docs/DOCKER.md`, `docs/COMPOSE.md`, `docs/examples/`
- **Rule**: `.cursor/rules/push-releases.mdc` — tag push triggers CI; local script is fallback.

## Ldflags

```bash
LDFLAGS="-X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"
```

Windows client adds `-H windowsgui`.

## Artifact naming (client auto-update contract)

- `gsbs-client-windows-amd64.exe`
- `gsbs-client-linux-amd64`
- `latest-client.json` with per-platform SHA256

## Checklist for release

- [ ] CHANGELOG entry for version.
- [ ] Tag `vX.Y.Z` pushed to origin (or `./script/release.sh` locally).
- [ ] CI release workflow green; GitHub Release has binaries + installer + deb + AppImage + manifests.
- [ ] Docker Hub image updated (release workflow on tag push, or local `release.sh`).
- [ ] Smoke: client **Check for updates** finds release.
