# GSBS on Unraid

Run GSBS server in Docker on Unraid **without a reverse proxy** — WebUI and API on port 8080. All settings are in the compose file (no `.env`).

## Compose Manager

1. Create stack folder, e.g. `/boot/config/plugins/compose.manager/projects/gsbs/`
2. Copy [compose-unraid.yml](compose-unraid.yml) into that folder
3. Edit the file — set `GSBS_SESSION_SECRET` to a long random string:
   ```bash
   openssl rand -hex 32
   ```
4. Start the stack in Compose Manager (or SSH):
   ```bash
   docker compose -f compose-unraid.yml up -d
   ```

## Access

- **WebUI:** `http://YOUR_UNRAID_IP:8080`
- **Clients:** same URL (e.g. `http://192.168.1.50:8080`)

## Data

SQLite and all server state: `/mnt/user/appdata/gsbs-server`

## Customize (edit compose-unraid.yml)

| Setting | Default | Notes |
|---------|---------|--------|
| Host port | `8080:8080` | Change left side, e.g. `"9080:8080"` |
| Appdata | `/mnt/user/appdata/gsbs-server` | Volume mount path |
| `GSBS_ALLOW_REGISTER` | `true` | Set `"false"` after creating your account |
| `GSBS_ADMIN_USERNAME` | (commented) | Uncomment and set to restrict `/admin` |

For TLS or a public hostname, use SWAG/NPM on Unraid or [compose-caddy.yml](compose-caddy.yml) on a VPS.
