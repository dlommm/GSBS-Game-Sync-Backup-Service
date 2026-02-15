# GSBS Server

Central server for game save sync. Stores saves per user and exposes push/pull API.

## Run

```bash
go build -o gsbs-server .
./gsbs-server
```

- `GSBS_ADDR` — listen address (default `:8080`)
- `GSBS_DB` — SQLite path (default `gsbs.db`)
- `GSBS_SESSION_SECRET` — secret for WebUI session cookies (set in production)
- `GSBS_ALLOW_REGISTER` — `false` to disable registration
- `GSBS_ADMIN_USERNAME` — restrict `/admin` to this user

## API

- **GET /api/health** — Health check (no auth). Returns `{"status":"ok"}`.

- **POST /api/register** — Create user  
  Body: `{"username":"...","password":"..."}`

- **POST /api/login** — Login and get client token  
  Body: `{"username":"...","password":"...","client_name":"...","client_os":"windows|linux"}`  
  Response: `{"token":"..."}`

- **GET /api/saves** — List/pull all saves for the authenticated user  
  Header: `Authorization: Bearer <token>`  
  Response: `{"saves":[{"game_id","path_key","updated_at","content":"<base64>"}]}`

- **GET /api/manifest** — Game save locations (no auth). Optional query `?since=<RFC3339>` for delta.

- **POST /api/saves** — Upload a save  
  Header: `Authorization: Bearer <token>`  
  Headers: `X-Game-ID`, `X-Path-Key`, optional `X-File-Path`  
  Body: raw file bytes (max 50 MiB)

## Windows exe icon

`icon.ico` is the application icon for the Windows build. To embed it in the .exe, install [rsrc](https://github.com/akavel/rsrc) and run (when building for Windows):

```bash
rsrc -ico server/icon.ico -o server/rsrc.syso
GOOS=windows GOARCH=amd64 go build -o gsbs-server.exe ./server
```
