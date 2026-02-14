# Deploying GSBS Server with Docker

This guide covers building and running the GSBS server in Docker, plus optional production setup (volumes, env, reverse proxy).

## Quick start

### Build the image

From the repository root (where `Dockerfile` and `go.mod` are):

```bash
docker build -t gsbs-server:latest .
```

### Run the server

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret-at-least-32-chars" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

- **Port**: The server listens on `:8080` inside the container. `-p 8080:8080` exposes it on the host.
- **Database**: Set the DB path to a volume mount so data persists. The server uses `GSBS_DB` (default `gsbs.db`), so use a path inside the container that is backed by a volume, e.g. `/app/data/gsbs.db`.
- **Session secret**: Set `GSBS_SESSION_SECRET` in production so session cookies are signed. Omit or leave empty only for local testing.

### Run with database in a volume (recommended)

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_SESSION_SECRET="your-secret-at-least-32-chars" \
  -e GSBS_DB="/app/data/gsbs.db" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

Create the named volume if needed (Docker creates it on first use). The SQLite file will be stored in `gsbs-data` and persist across container restarts.

---

## Pushing to Docker Hub

1. **Log in** (use your Docker Hub username and password or a [Personal Access Token](https://hub.docker.com/settings/security)):
   ```bash
   docker login
   ```

2. **Build and tag** the image with your Docker Hub username and repository name:
   ```bash
   docker build -t YOUR_DOCKERHUB_USERNAME/gsbs-server:latest .
   docker tag YOUR_DOCKERHUB_USERNAME/gsbs-server:latest YOUR_DOCKERHUB_USERNAME/gsbs-server:v1.0.1   # optional version tag
   ```
   Replace `YOUR_DOCKERHUB_USERNAME` with your actual Docker Hub username.

3. **Push** the image:
   ```bash
   docker push YOUR_DOCKERHUB_USERNAME/gsbs-server:latest
   docker push YOUR_DOCKERHUB_USERNAME/gsbs-server:v1.0.1   # if you tagged a version
   ```

**Pre-built image:** The official image is published at [dendlomm/gsbs-server](https://hub.docker.com/r/dendlomm/gsbs-server) on Docker Hub. Anyone can run:
```bash
docker pull dendlomm/gsbs-server:latest
docker run -d -p 8080:8080 -e GSBS_SESSION_SECRET=xxx -v gsbs-data:/app/data -e GSBS_DB=/app/data/gsbs.db dendlomm/gsbs-server:latest
```

---

## Environment variables

| Variable | Default | Description |
|---------|---------|-------------|
| `GSBS_ADDR` | `:8080` | Listen address inside the container (e.g. `:8080` or `0.0.0.0:8080`). |
| `GSBS_DB` | `gsbs.db` | Path to the SQLite database file. Use a path under a mounted volume for persistence. |
| `GSBS_SESSION_SECRET` | (insecure default) | Secret used to sign WebUI session cookies. **Set in production.** |
| `GSBS_ADMIN_USERNAME` | (empty) | If set, only this user can access the `/admin` page (stats and revoke client tokens). |

Example with all options:

```bash
docker run -d \
  --name gsbs \
  -p 8080:8080 \
  -e GSBS_ADDR=":8080" \
  -e GSBS_DB="/app/data/gsbs.db" \
  -e GSBS_SESSION_SECRET="your-long-random-secret" \
  -e GSBS_ADMIN_USERNAME="admin" \
  -v gsbs-data:/app/data \
  gsbs-server:latest
```

---

## Docker Compose

Save as `docker-compose.yml` in the repo root (or next to your deployment config):

```yaml
services:
  gsbs:
    build: .
    image: gsbs-server:latest
    container_name: gsbs
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - GSBS_ADDR=:8080
      - GSBS_DB=/app/data/gsbs.db
      - GSBS_SESSION_SECRET=${GSBS_SESSION_SECRET}
      - GSBS_ADMIN_USERNAME=${GSBS_ADMIN_USERNAME:-}
    volumes:
      - gsbs-data:/app/data

volumes:
  gsbs-data:
```

Create a `.env` file (do not commit secrets):

```env
GSBS_SESSION_SECRET=your-long-random-secret-here
GSBS_ADMIN_USERNAME=admin
```

Then run:

```bash
docker compose up -d
```

To rebuild after code changes:

```bash
docker compose up -d --build
```

---

## Production tips

### TLS / HTTPS

The server does not terminate TLS. In production, run it behind a reverse proxy (Caddy, Nginx, Traefik) that handles HTTPS and forwards to the container.

Example with Caddy (add to your Caddyfile):

```text
your-domain.com {
  reverse_proxy localhost:8080
}
```

Then bind the server to a local port (e.g. `-p 127.0.0.1:8080:8080`) so only the proxy can reach it.

### Restart policy

Use `--restart unless-stopped` (or Compose `restart: unless-stopped`) so the container restarts after a crash or host reboot.

### Health check (optional)

You can add a health check so the orchestrator knows the server is up:

```yaml
healthcheck:
  test: ["CMD", "wget", "-q", "-O", "-", "http://localhost:8080/login"]
  interval: 30s
  timeout: 5s
  retries: 3
  start_period: 5s
```

(Install `wget` in the image if needed, or use a small Go health endpoint and `curl`.)

### Data backup

The SQLite database is stored in the volume (e.g. `gsbs-data`). Back up that volume (or the host path it’s bound to) regularly. You can use `docker cp`, a volume backup tool, or your host’s backup solution.

---

## Summary

1. **Build**: `docker build -t gsbs-server:latest .`
2. **Run**: Expose port `8080`, set `GSBS_SESSION_SECRET` and `GSBS_DB` to a path in a mounted volume.
3. **Persist**: Use a volume for `/app/data` (or wherever `GSBS_DB` points).
4. **Production**: Put the server behind HTTPS (reverse proxy), set a strong session secret, and optionally set `GSBS_ADMIN_USERNAME` for the admin UI.
