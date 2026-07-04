# Disaster recovery — restoring a GSBS server from backup

GSBS's built-in backup job (Admin → Settings → Backups, or `GSBS_BACKUP_DIR`) produces self-contained archives named `gsbs-backup-YYYYMMDD-HHMMSS.mmm.tar.zst` containing:

| Entry | Contents |
|---|---|
| `gsbs.db` | Consistent database snapshot (`VACUUM INTO`) — users, devices, save versions, settings |
| `gsbs-keys/` | At-rest encryption keys (2FA secrets are unreadable without them) |
| `gamesaves/` | Save files, when filesystem storage (`GSBS_SAVE_ROOT`) is enabled |
| `covers/` | Cover-art cache (optional; re-downloadable) |

Archives are written locally and, when the `GSBS_BACKUP_S3_*` variables are set, uploaded to any S3-compatible bucket.

## Restore procedure

1. **Stop the server** (`docker compose down`, or stop the service/binary).
2. Fetch the archive you want (from the backup directory or your S3 bucket).
3. Run the restore command with the server binary:

   ```bash
   gsbs-server restore --data-dir /app/data gsbs-backup-20260704-050000.000.tar.zst
   ```

   - `--data-dir` defaults to the directory of `GSBS_DB` when set.
   - The command refuses to overwrite an existing `gsbs.db` unless you pass `--force`.
   - With Docker, run it inside the container image against the mounted volume:

     ```bash
     docker run --rm -v gsbs_data:/app/data -v "$PWD:/backup" dendlomm/gsbs-server \
       restore --data-dir /app/data --force /backup/gsbs-backup-20260704-050000.000.tar.zst
     ```

4. Point the server at the restored layout (usually already correct):
   - `GSBS_DB=/app/data/gsbs.db`
   - `GSBS_SAVE_ROOT=/app/data/gamesaves` (when the archive contains `gamesaves/`)
5. **Start the server** and log in.
6. Run **Admin → Data Integrity → Verify now** — it re-hashes every stored save against the database and reports any mismatch.
7. Spot-check: pull a save on one client and confirm the content is what you expect.

## Notes & gotchas

- **`gsbs-keys/` is required for 2FA.** Restoring a database without it makes TOTP fail closed; the recovery runbook is in [TROUBLESHOOTING.md](TROUBLESHOOTING.md).
- Clients keep working across a restore: tokens live in the database, so a restored DB has the tokens that existed at backup time. Devices enrolled *after* that backup must log in again.
- Restoring an older backup rolls back save versions; clients whose local saves are newer will surface **conflicts** on their next sync rather than silently losing progress — resolve them per device.
- Encrypted saves restore as ciphertext and decrypt on the clients with the passphrase; the server never has it.
- Test your restores: the whole procedure takes a few minutes on a scratch directory (`--data-dir /tmp/gsbs-restore-test`).
