package job

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/sse"
	"github.com/gsbs/gsbs/server/store"
	"github.com/klauspost/compress/zstd"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const backupJobName = "backup"

// Admin-settings keys for the backup feature (env vars override, see
// BackupConfigFrom). The backup DIRECTORY is env-only (GSBS_BACKUP_DIR) so a
// web session can never point server writes at an arbitrary path.
const (
	SettingBackupEnabled       = "backup_enabled"
	SettingBackupCron          = "backup_cron"
	SettingBackupKeep          = "backup_keep"
	SettingBackupIncludeCovers = "backup_include_covers"
)

// DefaultBackupCron runs the nightly backup at 05:00.
const DefaultBackupCron = "0 5 * * *"

// OnBackupFinished, when set, is called after every backup run (success or
// failure) — wired to the notification system.
var OnBackupFinished func(success bool, detail string)

// BackupConfig resolves where and how backups are written.
type BackupConfig struct {
	Dir           string // destination directory (env GSBS_BACKUP_DIR; default <db dir>/backups)
	Keep          int    // newest archives kept locally (default 7)
	IncludeCovers bool
	CoversDir     string
	S3            *S3Config // nil = local only
}

// S3Config is env-only (credentials never touch the database).
type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Prefix    string
	Insecure  bool
}

// BackupEnabled reports whether scheduled backups are on: the admin setting
// or the presence of GSBS_BACKUP_DIR.
func BackupEnabled(settings map[string]string) bool {
	if v := settings[SettingBackupEnabled]; v == "true" || v == "1" {
		return true
	}
	return strings.TrimSpace(os.Getenv("GSBS_BACKUP_DIR")) != ""
}

// BackupCronExpr returns the schedule: GSBS_BACKUP_CRON > admin setting > default.
func BackupCronExpr(settings map[string]string) string {
	if v := strings.TrimSpace(os.Getenv("GSBS_BACKUP_CRON")); v != "" {
		return v
	}
	if v := strings.TrimSpace(settings[SettingBackupCron]); v != "" {
		return v
	}
	return DefaultBackupCron
}

// BackupConfigFrom resolves the effective backup configuration.
func BackupConfigFrom(settings map[string]string, dbPath string) BackupConfig {
	cfg := BackupConfig{Keep: 7}
	if v := strings.TrimSpace(os.Getenv("GSBS_BACKUP_DIR")); v != "" {
		cfg.Dir = v
	} else {
		cfg.Dir = filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if n, err := strconv.Atoi(strings.TrimSpace(settings[SettingBackupKeep])); err == nil && n > 0 {
		cfg.Keep = n
	}
	if v := settings[SettingBackupIncludeCovers]; v == "true" || v == "1" {
		cfg.IncludeCovers = true
		cfg.CoversDir = strings.TrimSpace(os.Getenv("GSBS_COVER_ROOT"))
		if cfg.CoversDir == "" {
			cfg.CoversDir = "/app/data/covers"
		}
	}
	if ep := strings.TrimSpace(os.Getenv("GSBS_BACKUP_S3_ENDPOINT")); ep != "" {
		cfg.S3 = &S3Config{
			Endpoint:  ep,
			Bucket:    strings.TrimSpace(os.Getenv("GSBS_BACKUP_S3_BUCKET")),
			AccessKey: os.Getenv("GSBS_BACKUP_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("GSBS_BACKUP_S3_SECRET_KEY"),
			Prefix:    strings.Trim(strings.TrimSpace(os.Getenv("GSBS_BACKUP_S3_PREFIX")), "/"),
			Insecure:  os.Getenv("GSBS_BACKUP_S3_INSECURE") == "1",
		}
	}
	return cfg
}

// BackupResult summarizes one backup run.
type BackupResult struct {
	Path     string
	Bytes    int64
	Files    int
	Uploaded bool
}

// TryRunBackup starts a backup if none is running. The archive contains the
// database snapshot (VACUUM INTO), the gsbs-keys directory, the filesystem
// save root when configured, and optionally the covers cache — everything a
// restore needs.
func (r *Runner) TryRunBackup(ctx context.Context) (bool, error) {
	r.mu.Lock()
	if r.running[backupJobName] {
		r.mu.Unlock()
		return false, ErrJobAlreadyRunning
	}
	r.running[backupJobName] = true
	r.mu.Unlock()

	r.wg.Add(1)
	go r.runBackup(ctx)
	return true, nil
}

func (r *Runner) runBackup(parentCtx context.Context) {
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		r.running[backupJobName] = false
		delete(r.cancelFuncs, backupJobName)
		delete(r.jobRunIDs, backupJobName)
		r.mu.Unlock()
	}()

	logx.Logger().Info().Str("component", "job").Str("job", backupJobName).Msg("job: started")
	jobCtx, cancel := context.WithTimeout(parentCtx, 2*time.Hour)
	defer cancel()
	r.mu.Lock()
	r.cancelFuncs[backupJobName] = cancel
	r.mu.Unlock()

	runID, err := r.store.LogJobStart(jobCtx, backupJobName)
	if err == nil {
		r.mu.Lock()
		r.jobRunIDs[backupJobName] = runID
		r.mu.Unlock()
	}

	settings, _ := r.store.ListAdminSettings(jobCtx)
	cfg := BackupConfigFrom(settings, r.store.DatabasePath())
	result, backupErr := RunBackup(jobCtx, r.store, cfg)

	status := JobSuccess
	detail := ""
	if backupErr != nil {
		if errors.Is(backupErr, context.Canceled) {
			status = JobCanceled
		} else {
			status = JobFailed
		}
		detail = backupErr.Error()
		logx.Logger().Error().Str("component", "job").Err(backupErr).Msg("job runner: backup failed")
	} else {
		detail = fmt.Sprintf("%s (%d files, %.1f MiB)", filepath.Base(result.Path), result.Files, float64(result.Bytes)/(1<<20))
		logx.Logger().Info().Str("component", "job").Str("archive", result.Path).
			Int("files", result.Files).Int64("bytes", result.Bytes).Bool("uploaded", result.Uploaded).
			Msg("job runner: backup complete")
	}

	if runID != "" {
		_ = r.store.LogJobFinish(jobCtx, runID, status, detail, result.Files)
	}
	if r.hub != nil {
		r.hub.Broadcast(sse.Event{Type: "job-finished", Data: `{"job":"backup","status":"` + status + `"}`})
	}
	if OnBackupFinished != nil {
		OnBackupFinished(backupErr == nil, detail)
	}
}

// RunBackup performs one backup with the given configuration.
func RunBackup(ctx context.Context, st store.Store, cfg BackupConfig) (BackupResult, error) {
	var res BackupResult
	dbPath := st.DatabasePath()
	if dbPath == "" || strings.Contains(dbPath, ":memory:") {
		return res, errors.New("backup requires a file-backed database")
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return res, fmt.Errorf("create backup dir: %w", err)
	}

	staging, err := os.MkdirTemp(cfg.Dir, ".staging-*")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	snapshot := filepath.Join(staging, "gsbs.db")
	if err := st.VacuumInto(ctx, snapshot); err != nil {
		return res, fmt.Errorf("vacuum into: %w", err)
	}

	// Millisecond suffix keeps names unique for rapid manual runs while
	// preserving lexicographic == chronological ordering for retention.
	now := time.Now().UTC()
	name := fmt.Sprintf("gsbs-backup-%s.%03d.tar.zst", now.Format("20060102-150405"), now.Nanosecond()/1e6)
	finalPath := filepath.Join(cfg.Dir, name)
	partial := finalPath + ".partial"
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return res, err
	}
	defer func() { _ = os.Remove(partial) }() // no-op after successful rename

	zw, err := zstd.NewWriter(f)
	if err != nil {
		_ = f.Close()
		return res, err
	}
	tw := tar.NewWriter(zw)

	files := 0
	add := func(src, dest string) error {
		n, err := tarAddPath(ctx, tw, src, dest)
		files += n
		return err
	}
	if err := add(snapshot, "gsbs.db"); err != nil {
		_ = tw.Close()
		_ = zw.Close()
		_ = f.Close()
		return res, err
	}
	keysDir := filepath.Join(filepath.Dir(dbPath), "gsbs-keys")
	if _, statErr := os.Stat(keysDir); statErr == nil {
		if err := add(keysDir, "gsbs-keys"); err != nil {
			_ = tw.Close()
			_ = zw.Close()
			_ = f.Close()
			return res, err
		}
	}
	if root := st.SaveRootPath(); root != "" {
		if _, statErr := os.Stat(root); statErr == nil {
			if err := add(root, "gamesaves"); err != nil {
				_ = tw.Close()
				_ = zw.Close()
				_ = f.Close()
				return res, err
			}
		}
	}
	if cfg.IncludeCovers && cfg.CoversDir != "" {
		if _, statErr := os.Stat(cfg.CoversDir); statErr == nil {
			if err := add(cfg.CoversDir, "covers"); err != nil {
				_ = tw.Close()
				_ = zw.Close()
				_ = f.Close()
				return res, err
			}
		}
	}

	if err := tw.Close(); err != nil {
		_ = zw.Close()
		_ = f.Close()
		return res, err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return res, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return res, err
	}
	if err := f.Close(); err != nil {
		return res, err
	}
	if err := os.Rename(partial, finalPath); err != nil {
		return res, err
	}
	fi, _ := os.Stat(finalPath)
	res.Path = finalPath
	res.Files = files
	if fi != nil {
		res.Bytes = fi.Size()
	}

	pruneOldBackups(cfg.Dir, cfg.Keep)

	if cfg.S3 != nil {
		if err := uploadBackupS3(ctx, cfg.S3, finalPath); err != nil {
			// The local archive is intact; surface the upload failure.
			return res, fmt.Errorf("backup written to %s but S3 upload failed: %w", finalPath, err)
		}
		res.Uploaded = true
	}
	return res, nil
}

// tarAddPath adds a file, or a directory tree, under destRoot in the archive.
func tarAddPath(ctx context.Context, tw *tar.Writer, src, destRoot string) (int, error) {
	info, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 1, tarAddFile(tw, src, destRoot, info)
	}
	count := 0
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries; a backup should capture what it can
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		// Never recurse into staging/partial artifacts.
		base := d.Name()
		if strings.HasPrefix(base, ".staging-") || strings.HasSuffix(base, ".partial") {
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if addErr := tarAddFile(tw, path, filepath.ToSlash(filepath.Join(destRoot, rel)), fi); addErr != nil {
			return addErr
		}
		count++
		return nil
	})
	return count, err
}

func tarAddFile(tw *tar.Writer, src, dest string, info os.FileInfo) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = dest
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	// Copy exactly the header size: a file growing mid-copy would corrupt the
	// tar stream (the DB snapshot is already immutable; saves rarely move).
	_, err = io.CopyN(tw, f, info.Size())
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%s shrank while archiving", src)
	}
	return err
}

// pruneOldBackups keeps the newest keep archives (lexicographic order matches
// chronological for the timestamped names).
func pruneOldBackups(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var archives []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "gsbs-backup-") && strings.HasSuffix(e.Name(), ".tar.zst") {
			archives = append(archives, e.Name())
		}
	}
	if len(archives) <= keep {
		return
	}
	sort.Strings(archives)
	for _, name := range archives[:len(archives)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			logx.Logger().Info().Str("component", "job").Str("archive", name).Msg("backup retention: removed old archive")
		}
	}
}

func uploadBackupS3(ctx context.Context, s3 *S3Config, path string) error {
	if s3.Bucket == "" {
		return errors.New("GSBS_BACKUP_S3_BUCKET is not set")
	}
	client, err := minio.New(s3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(s3.AccessKey, s3.SecretKey, ""),
		Secure: !s3.Insecure,
	})
	if err != nil {
		return err
	}
	object := filepath.Base(path)
	if s3.Prefix != "" {
		object = s3.Prefix + "/" + object
	}
	_, err = client.FPutObject(ctx, s3.Bucket, object, path, minio.PutObjectOptions{ContentType: "application/zstd"})
	return err
}
