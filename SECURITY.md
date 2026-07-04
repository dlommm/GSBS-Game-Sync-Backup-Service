# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |

Install updates from [GitHub Releases](https://github.com/dlommm/GSBS--Game-Sync---Backup-Service-/releases/latest) or use the client tray **Check for updates**.

## Reporting a vulnerability

**Do not** open public GitHub issues for security vulnerabilities.

Email the maintainer privately (use the contact on the GitHub profile for [dlommm](https://github.com/dlommm)) with:

- Description of the issue
- Steps to reproduce
- Impact assessment
- Suggested fix (if any)

We aim to acknowledge reports within 72 hours and provide a fix or mitigation timeline when confirmed.

## Security practices

### Server

- Set a strong `GSBS_SESSION_SECRET` (32+ characters, e.g. `openssl rand -base64 32`). Never commit secrets. Since 4.0.0 the server **refuses to start** with a shorter or placeholder secret; `GSBS_INSECURE_DEV_SECRET=1` bypasses the check for local development only. This one secret signs sessions, CSRF tokens, and TOTP login tokens — rotate it if it may have leaked (rotating logs out WebUI sessions; API clients are unaffected).
- Run behind HTTPS (Caddy, nginx, or Traefik). See [docs/COMPOSE.md](docs/COMPOSE.md).
- **`GSBS_TRUST_PROXY` caveat:** when set, the server trusts the first `X-Forwarded-For` hop for client IPs (rate limiting, audit logs). Only enable it behind a proxy that **overwrites** client-supplied `X-Forwarded-For` — the bundled Caddy/nginx examples do. Never enable it when the server is directly reachable.
- Restrict admin access with `GSBS_ADMIN_USERNAME`.
- Disable public registration in production: `GSBS_ALLOW_REGISTER=false`. Login, TOTP verification, and registration are rate-limited per IP (`GSBS_RATE_LIMIT_AUTH`, default 20/min).
- If you enable `/metrics` (`GSBS_METRICS=1`), set `GSBS_METRICS_TOKEN` explicitly; the auto-generated fallback token is never logged and changes each run.
- The bundled `docker-compose.yml` runs the server as a non-root user with `no-new-privileges`, a memory limit, and a PID limit; keep those if you adapt it. Keep images updated: `docker pull dendlomm/gsbs-server:latest`.
- Back up `gsbs.db` (and `GSBS_SAVE_ROOT` if set) regularly; saves and versions live there.

### Client

- API tokens live in local config (`config.json`, mode `0600`). Do not share config files.
- `encryption_passphrase` (optional E2E) never leaves the client.
- Client auto-update verifies SHA256 checksums from `latest-client.json` before applying.
- Windows installers are unsigned; verify downloads from official GitHub Releases only.

### Secrets in the repository

GSBS does not commit passwords, API keys, or session secrets. Use environment variables and local config files. See [.cursor/rules/gsbs-no-secrets.mdc](.cursor/rules/gsbs-no-secrets.mdc) for contributor guidelines.

## Known limitations / roadmap

Tracked hardening work, in rough priority order:

- **CSP allows `'unsafe-inline'` scripts** — removing it requires refactoring the WebUI's inline scripts; XSS is currently mitigated by template escaping and CSRF tokens.
- **TOTP secrets are stored unencrypted in the database** — a full DB compromise defeats 2FA; protect `gsbs.db` accordingly.
- **First-push overwrite guard is inactive under the default `last_write_wins` policy** (by design); switch to `keep_local`/`keep_server` if multi-device first-push safety matters to you.
- **Storage quota checks are advisory under high concurrency** — simultaneous pushes can briefly overshoot a quota.
- **Windows installers are not code-signed**; verify `SHA256SUMS` from GitHub Releases.
