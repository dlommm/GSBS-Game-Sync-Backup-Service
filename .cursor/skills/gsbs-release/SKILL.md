---
name: gsbs-release
description: Builds and releases GSBS server and client: version ldflags, script/release.sh, Dockerfile, multi-arch or platform-specific builds. Use when creating releases, updating version, building Docker images, or modifying release or deploy docs.
---

# GSBS Build & Release

## Scope

- **Version**: `server/version.go` and `client/version.go` expose Version, BuildDate, Commit (set via ldflags at build). Main packages read these for `--version` / `-v` output.
- **script/release.sh**: Builds server and client for Windows amd64 with ldflags, tags git, creates GitHub release, uploads gsbs-server-windows-amd64.exe and gsbs-client-windows-amd64.exe. Usage: `./script/release.sh [VERSION]`. Requires go, git, gh, clean working tree for tagging. Client built with `-H windowsgui` for GUI.
- **Docker**: Dockerfile at repo root; builds server. See `docs/DOCKER.md` for build, run, volumes (GSBS_DB, GSBS_SESSION_SECRET), and optional Docker Hub push / reverse proxy.

## Ldflags

```bash
LDFLAGS="-X main.Version=${VERSION_VALUE} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}"
```

Use `main.Version` etc. if the version vars are in main package; match the import path of the package where Version/BuildDate/Commit are defined (e.g. if in server/main.go, use the package path that defines them).

## Conventions

- Bump version for releases; tag with v semver (e.g. v1.0.4). release.sh can prompt for version or take first arg.
- Docker: persist DB via volume; set GSBS_SESSION_SECRET in production. Document any new env in docs/DOCKER.md.
- Adding Linux or other arch: extend release script or add separate targets; document in README or release notes.

## Checklist for release

- [ ] Version value and tag agreed (e.g. v1.0.4).
- [ ] script/release.sh builds and uploads; or run Docker build and push per docs/DOCKER.md.
- [ ] Release notes mention platforms (e.g. Windows amd64) and any new env or config.
