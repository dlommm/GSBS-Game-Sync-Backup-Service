# GSBS API reference

Base URL: your server root (e.g. `https://gsbs.example.com`). All endpoints except health, register, login, and manifest (for anonymous fetch) require authentication.

## Authentication

- **Registration**: `POST /api/register` with JSON `{"username":"...","password":"..."}`. Password min 8 chars. Returns `{"status":"ok"}` or 4xx with `{"error":"..."}`. Disabled when `GSBS_ALLOW_REGISTER` is false.
- **Login**: `POST /api/login` with JSON `{"username":"...","password":"...","client_name":"...","client_os":"windows|linux"}`. Returns `{"token":"..."}`. Use this token for subsequent requests. If the user has two-factor authentication (2FA) enabled, the response is `{"totp_required":true,"totp_token":"..."}` instead; then call `POST /api/login/totp` with that token and the current code.
- **Login (2FA step)**: `POST /api/login/totp` with JSON `{"totp_token":"...","code":"123456","client_name":"...","client_os":"..."}`. Use the `totp_token` from the login response and the 6-digit code from the user's authenticator app. Returns `{"token":"..."}`. The `totp_token` is short-lived (about 5 minutes).
- **Auth header**: `Authorization: Bearer <token>` on requests. Query-string `?token=` is rejected (use Bearer only).
- **Account settings**: `GET /api/account` — returns `{"encryption_enabled": bool}` for the authenticated user.
- **Change password**: `POST /api/change-password` (authenticated) with JSON `{"current_password":"...","new_password":"..."}`. New password must be at least 8 characters. Returns 200 `{"status":"ok"}` or 400/401/500 with `{"error":"..."}`.

## Health and readiness

- **Liveness**: `GET /api/health` — no auth. Returns 200 `{"status":"ok"}`. When built with version ldflags, includes `"version":"..."`.
- **Readiness** (DB check): `GET /api/health?ready=1` — no auth. Returns 200 `{"status":"ok","db":"ok","version":"..."}` or 503 `{"status":"unhealthy","db":"error","version":"..."}`. Use for Kubernetes/Docker readiness probes; the 2s DB timeout causes 503 if the store is slow or down.

## Saves (authenticated)

- **Pull all saves**: `GET /api/saves` — returns JSON `{"saves":[{"game_id","path_key","updated_at","content","encrypted"}]}`. `content` is base64-encoded. `encrypted` is true when the blob was stored with client-side E2E encryption. Supports gzip when `Accept-Encoding: gzip`.
- **Pull single save**: `GET /api/saves?game_id=...&path_key=...` — returns one save in the same format.
- **Save summaries only**: `GET /api/saves?summaries=1` — returns `{"saves":[{"game_id","path_key","game_title","size_bytes","updated_at","content_hash","encrypted"}]}` (no content). Use for conditional sync.
- **Push a save**: `POST /api/saves` with body = raw file bytes (optional `Content-Encoding: gzip`). Headers: `X-Game-ID`, `X-Path-Key` (required), `X-File-Path` (optional), `X-Content-Hash` (SHA256 hex of wire bytes), `X-Content-Size`, `X-Encrypted: 1` when content is client-encrypted. Max body 50 MiB. Returns 200 `{"status":"ok"}`, `{"status":"unchanged"}` if hash matches, or 4xx.
- **Delete a save**: `DELETE /api/saves?game_id=...&path_key=...`. Returns 200 `{"status":"ok"}` or 4xx.
- **List versions**: `GET /api/saves/versions?game_id=...&path_key=...` — returns `{"versions":[{"version","updated_at","size_bytes"}]}`.
- **Get a version**: `GET /api/saves/versions/download?game_id=...&path_key=...&version=N` — returns JSON with `content` (base64).
- **Restore version**: `POST /api/saves/versions/restore` with JSON `{"game_id","path_key","version"}`. Returns 200 `{"status":"ok"}` or 404.

## Token refresh (authenticated)

- **Refresh client token**: `POST /api/token/refresh` — returns `{"token":"...","expires_in":7776000}`. Old token is invalidated.

## Clients (authenticated)

- **List my clients**: `GET /api/clients` — returns `{"clients":[{"id","name","os","last_seen"}]}`.

## Manifest (public or authenticated)

- **Full manifest (v1)**: `GET /api/manifest` — returns `{"entries":[...]}` of `GameSaveLocation`. Each entry includes `game_id` (PCGW page ID), `game_title`, `platform`, `path_template`, optional launcher IDs (`steam_app_ids`, `gog_id`, `epic_id`, `ubisoft_id`), and metadata. Cached on server ~10 min. Response headers: `ETag`, `X-Manifest-Version`.
- **Delta**: `GET /api/manifest?since=<RFC3339>` — returns only entries updated after the given time.
- **Manifest v2 (rich)**: `GET /api/manifest/v2` — returns grouped per-game JSON (`games[]` with taxonomy, engines, `save_locations`, `config_locations`, `has_save_data`, `proton_support_level`, etc.). Query: `?since=<RFC3339>`, `?platform=windows|linux|macos`. Supports `If-None-Match` → 304. Clients should prefer v2 when available.

## Server-Sent Events (authenticated)

- **Stream**: `GET /api/events` with `Authorization: Bearer <token>`. Long-lived stream; event type e.g. `manifest-updated`. Clients can reconnect with exponential backoff.

## Metrics (optional)

- When `GSBS_METRICS=1`, `GET /metrics` returns Prometheus text format (request counts, storage, users, clients, saves). No auth by default; guard in production if needed.

## Errors

Responses use JSON `{"error":"..."}` with appropriate HTTP status (400 Bad Request, 401 Unauthorized, 404 Not Found, 429 Too Many Requests, 500 Internal Server Error).
