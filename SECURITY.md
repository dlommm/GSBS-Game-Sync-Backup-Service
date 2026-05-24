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

- Set a strong `GSBS_SESSION_SECRET` (32+ random bytes). Never commit secrets.
- Run behind HTTPS (Caddy, nginx, or Traefik). See [docs/COMPOSE.md](docs/COMPOSE.md).
- Restrict admin access with `GSBS_ADMIN_USERNAME`.
- Disable public registration in production: `GSBS_ALLOW_REGISTER=false`.
- Keep Docker images updated: `docker pull dendlomm/gsbs-server:latest`.

### Client

- API tokens live in local config (`config.json`, mode `0600`). Do not share config files.
- `encryption_passphrase` (optional E2E) never leaves the client.
- Client auto-update verifies SHA256 checksums from `latest-client.json` before applying.
- Windows installers are unsigned; verify downloads from official GitHub Releases only.

### Secrets in the repository

GSBS does not commit passwords, API keys, or session secrets. Use environment variables and local config files. See [.cursor/rules/gsbs-no-secrets.mdc](.cursor/rules/gsbs-no-secrets.mdc) for contributor guidelines.
