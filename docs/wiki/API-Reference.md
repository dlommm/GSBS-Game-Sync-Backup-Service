# API Reference

> Full REST API reference for the GSBS server. Base URL: your server root (e.g. `https://gsbs.example.com`).

---

## Authentication

All endpoints except health, register, login, and manifest require a `Bearer` token.

### Register

```
POST /api/register
```

**Body (JSON):**
```json
{ "username": "alice", "password": "atleast8chars" }
```

**Response:** `{"status":"ok"}` or 4xx with `{"error":"..."}`.

> **Note:** Disabled when `GSBS_ALLOW_REGISTER=false`.

### Login

```
POST /api/login
```

**Body (JSON):**
```json
{
  "username": "alice",
  "password": "...",
  "client_name": "My Desktop",
  "client_os": "windows"
}
```

**Response (no 2FA):** `{"token":"..."}` — use this token in `Authorization: Bearer <token>`.

**Response (2FA enabled):** `{"totp_required":true,"totp_token":"..."}` — proceed to TOTP step.

### Login — TOTP step (2FA)

```
POST /api/login/totp
```

**Body (JSON):**
```json
{
  "totp_token": "...",
  "code": "123456",
  "client_name": "My Desktop",
  "client_os": "windows"
}
```

`totp_token` comes from the login response. The code is the current 6-digit authenticator app code. The `totp_token` expires in ~5 minutes.

**Response:** `{"token":"..."}`.

### Auth header

```
Authorization: Bearer <token>
```

Query-string `?token=` is rejected. Use the header only.

### Account settings

```
GET /api/account
```

Returns `{"encryption_enabled": bool}` for the authenticated user.

### Change password

```
POST /api/change-password
```

**Body (JSON):**
```json
{ "current_password": "...", "new_password": "newatleast8" }
```

> **Warning:** Password change revokes all active client tokens. All clients must re-login.

---

## Health

### Liveness

```
GET /api/health
```

No auth required. Returns `{"status":"ok"}` (200) when the server is running.

### Readiness (DB check)

```
GET /api/health?ready=1
```

No auth required. Returns `{"status":"ok","db":"ok"}` (200) or `{"status":"unhealthy","db":"error"}` (503). Use for Kubernetes/Docker readiness probes; times out in 2s.

---

## Saves

All endpoints require authentication.

### Pull all saves (summaries)

```
GET /api/saves?summaries=1
```

Returns metadata only — no file content. Use for conditional sync.

**Response:**
```json
{
  "saves": [
    {
      "game_id": "elden-ring",
      "path_key": "elden-ring-0",
      "game_title": "Elden Ring",
      "size_bytes": 8192,
      "updated_at": "2026-06-01T12:00:00Z",
      "content_hash": "abc123...",
      "encrypted": false
    }
  ],
  "total": 42
}
```

**Pagination:** `?limit=N&offset=M` (max limit 500).

### Pull all saves (with content)

```
GET /api/saves
```

**Response:** Same shape as summaries but with `"content": "<base64>"` instead of `size_bytes`. `encrypted` is true when the blob was stored with client-side E2E encryption.

Supports gzip when `Accept-Encoding: gzip`.

### Pull a single save

```
GET /api/saves?game_id=elden-ring&path_key=elden-ring-0
```

### Push a save

```
POST /api/saves
```

**Body:** Raw file bytes. Max 50 MiB.

**Required headers:**
- `X-Game-ID: <game_id>`
- `X-Path-Key: <path_key>`
- `X-Relative-Path: <path>` — required when the server uses `GSBS_SAVE_ROOT`

**Optional headers:**
- `Content-Encoding: gzip`
- `X-Content-Hash: <sha256-hex>` — SHA256 of the wire bytes
- `X-Content-Size: <bytes>`
- `X-Encrypted: 1` — when content is client-encrypted
- `X-File-Path: <path>` — log metadata only
- `X-GSBS-If-Hash: <expected_hash>` — optimistic concurrency guard (see below)

**Responses:**
- `{"status":"ok"}` — saved
- `{"status":"unchanged"}` — hash matches existing record; no write
- 409 `{"error":"conflict","current_hash":"...","current_version":N}` — hash guard mismatch

**Optimistic concurrency:** Include `X-GSBS-If-Hash` to guard against overwriting a newer version. Omit for last-write-wins (default).

### Delete a save

```
DELETE /api/saves?game_id=elden-ring&path_key=elden-ring-0
```

**Response:** `{"status":"ok"}` or 4xx.

---

## Save versions

### List versions

```
GET /api/saves/versions?game_id=elden-ring&path_key=elden-ring-0
```

**Response:**
```json
{
  "versions": [
    { "version": 3, "updated_at": "2026-06-01T12:00:00Z", "size_bytes": 8192 }
  ]
}
```

### Download a version

```
GET /api/saves/versions/download?game_id=elden-ring&path_key=elden-ring-0&version=2
```

**Response:** JSON with `"content": "<base64>"`.

### Restore a version

```
POST /api/saves/versions/restore
```

**Body (JSON):**
```json
{ "game_id": "elden-ring", "path_key": "elden-ring-0", "version": 2 }
```

**Response:** `{"status":"ok"}` or 404.

---

## Token

### Refresh client token

```
POST /api/token/refresh
```

Returns `{"token":"...","expires_in":7776000}`. Old token is invalidated.

---

## Clients

### List my clients

```
GET /api/clients
```

**Response:**
```json
{
  "clients": [
    { "id": "abc", "name": "My Desktop", "os": "windows", "last_seen": "2026-06-01T..." }
  ]
}
```

### Revoke a client token

```
POST /api/clients/revoke
```

**Body (JSON):**
```json
{ "client_id": "abc" }
```

Owner-only: revokes the token for one of your own clients. The revoked client must re-login.

**Response:** `{"status":"ok"}`, 403 if not your client, or 4xx.

---

## Manifest

### v1 (full, backward-compatible)

```
GET /api/manifest
```

Returns `{"entries":[...]}` of `GameSaveLocation`. Each entry includes `game_id`, `game_title`, `platform`, `path_template`, optional `save_rules`, launcher IDs, and metadata.

**Filters/pagination:**
- `?limit=N&offset=M` — paginate (max 500)
- `?include=saves` or `?include=config` — filter by type
- `?since=<RFC3339>` — delta: only entries updated after this time

**Response headers:** `ETag`, `X-Manifest-Version`.

### v2 (rich, preferred)

```
GET /api/manifest/v2
```

Returns grouped per-game JSON with taxonomy, `save_locations`, `config_locations`, `save_rules`, `proton_support_level`, and more. Clients should prefer v2.

**Query params:** `?since=<RFC3339>`, `?platform=windows|linux|macos`

Supports `If-None-Match` → 304 (no response body when unchanged).

---

## Server-Sent Events

```
GET /api/events
Authorization: Bearer <token>
```

Long-lived SSE stream. At most 5 concurrent connections per user. Reconnect with exponential backoff.

**Heartbeat:** `: heartbeat` comment every 30 seconds.

**Event types:**

| Event | Data | Meaning |
|---|---|---|
| `save-updated` | `{"game_id":"...","path_key":"..."}` | A save was pushed; re-pull |
| `manifest-updated` | `{}` | PCGW manifest updated; re-fetch manifest |
| `job-progress` | `{"job":"pcgw_sync","pages":N,"total":N,"phase":"catalog"\|"ingest","eta_seconds":N}` | PCGW sync progress |
| `job-finished` | `{"job":"pcgw_sync","status":"ok"\|"error"}` | PCGW sync completed |
| `audit-updated` | `{}` | Admin audit log updated |
| `server-shutting-down` | `{}` | Server about to stop |

---

## Admin PCGW endpoints

Session auth required (admin role). These are WebUI-facing endpoints used by the admin interface.

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/pcgw/sync` | Run incremental PCGW sync (`full=1` forces full catalog rescan + backlog/changed ingest) |
| `POST` | `/admin/pcgw/sync/refresh-new` | Force full catalog rescan, then process missing entries |
| `POST` | `/admin/pcgw/sync/auto-catch-up` | Repeat budgeted incremental cycles until Phase 2 backlog cleared |
| `POST` | `/admin/pcgw/sync/missing-local` | Phase 2 only — ingest IDs not yet in `pcgw_games` (skips Phase 1) |
| `POST` | `/admin/pcgw/sync/catalog-only` | Phase 1 only — refresh `pcgw_catalog` |
| `POST` | `/admin/pcgw/sync/retry-failed` | Phase 2 only — re-process failed/partial pages (skips Phase 1) |
| `POST` | `/admin/pcgw/reset-dead-letter` | Clear dead-letter flags on blocked catalog entries |
| `POST` | `/admin/pcgw/rebuild-manifest` | Bump manifest version without fetching pages |
| `POST` | `/admin/pcgw/wipe` | Execute wipe (`mode=mirror_only\|mirror_and_manifest`; confirmed in WebUI popup) |
| `GET` | `/admin/pcgw/export/manifest.json.gz` | Download gzip manifest+mirror bundle |
| `POST` | `/admin/pcgw/import` | Upload bundle (`merge` or `full_replace`) |

---

## Metrics (optional)

```
GET /metrics
```

Returns Prometheus text format when `GSBS_METRICS=1`. Includes request counts, storage, users, clients, saves. Requires `Authorization: Bearer <GSBS_METRICS_TOKEN>` when `GSBS_METRICS_TOKEN` is set.

---

## Errors

All error responses use JSON:

```json
{ "error": "description of the error" }
```

| Code | Meaning |
|---|---|
| 400 | Bad request (missing/invalid params) |
| 401 | Unauthorized (missing or invalid token) |
| 403 | Forbidden (not your resource) |
| 404 | Not found |
| 409 | Conflict (optimistic concurrency hash mismatch) |
| 413 | Payload too large (exceeds 50 MiB or quota) |
| 429 | Too many requests (rate limited) |
| 500 | Internal server error |
| 503 | Service unavailable (DB locked or storage quota error) |

---

## Related pages

- [How It Works](How-It-Works)
- [Server Configuration](Server-Configuration)
- [Client Setup & Usage](Client-Setup-and-Usage)
- [Contributing](Contributing)
