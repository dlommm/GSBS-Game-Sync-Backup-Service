package job

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsbs/gsbs/server/store"
	"github.com/klauspost/compress/zstd"
)

// Full round trip: seed a file-backed store (filesystem save mode + key
// file), run a backup, extract the archive, and verify every component a
// disaster recovery needs is present and intact.
func TestRunBackup_RoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	saveRoot := filepath.Join(dataDir, "gamesaves")
	t.Setenv("GSBS_SAVE_ROOT", saveRoot)
	dbPath := filepath.Join(dataDir, "gsbs.db")

	st, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	userID, err := st.CreateUser(ctx, "u", "h")
	if err != nil {
		t.Fatal(err)
	}
	saveContent := []byte("precious-save-bytes")
	if _, err := st.UpsertSaveWithMeta(ctx, userID, "g1", "pk1", saveContent, &store.SaveMeta{RelativePath: "saves/slot.dat"}); err != nil {
		t.Fatal(err)
	}
	// TOTP key file (gsbs-keys/) must ride along in the archive.
	if err := st.SetTOTPSecret(ctx, userID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}

	backupDir := filepath.Join(dataDir, "backups")
	res, err := RunBackup(ctx, st, BackupConfig{Dir: backupDir, Keep: 2})
	if err != nil {
		t.Fatalf("RunBackup: %v", err)
	}
	if res.Files < 3 { // db + key file + save file
		t.Fatalf("archive has %d files, want >= 3", res.Files)
	}

	got := extractArchive(t, res.Path)
	if !strings.Contains(string(got["gamesaves/"+filepath.ToSlash(filepath.Join(userID, "g1", "saves", "slot.dat"))]), "precious-save-bytes") {
		// Fall back: locate the save by suffix (layout detail may vary).
		found := false
		for name, data := range got {
			if strings.HasPrefix(name, "gamesaves/") && strings.HasSuffix(name, "slot.dat") && string(data) == string(saveContent) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("save file missing from archive; entries: %v", keys(got))
		}
	}
	if _, ok := got["gsbs.db"]; !ok {
		t.Fatalf("gsbs.db missing from archive; entries: %v", keys(got))
	}
	if _, ok := got["gsbs-keys/totp.key"]; !ok {
		t.Fatalf("gsbs-keys/totp.key missing from archive; entries: %v", keys(got))
	}

	// Retention: two more runs with Keep=2 leave exactly 2 archives.
	if _, err := RunBackup(ctx, st, BackupConfig{Dir: backupDir, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := RunBackup(ctx, st, BackupConfig{Dir: backupDir, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(backupDir)
	archives := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tar.zst") {
			archives++
		}
		if strings.HasPrefix(e.Name(), ".staging-") || strings.HasSuffix(e.Name(), ".partial") {
			t.Fatalf("staging residue left behind: %s", e.Name())
		}
	}
	if archives != 2 {
		t.Fatalf("retention kept %d archives, want 2", archives)
	}
}

func TestRunBackup_RefusesInMemory(t *testing.T) {
	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := RunBackup(context.Background(), st, BackupConfig{Dir: t.TempDir()}); err == nil {
		t.Fatal("in-memory database must be rejected")
	}
}

func extractArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.ToSlash(hdr.Name)] = data
	}
	return out
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
