package main

import (
	"archive/tar"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// runRestore implements `gsbs-server restore <archive.tar.zst>`: it unpacks a
// backup produced by the built-in backup job into a data directory. Run it
// while the server is STOPPED, then point GSBS_DB / GSBS_SAVE_ROOT at the
// restored layout (docs/RESTORE.md walks through the full procedure).
func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory to restore into (default: directory of GSBS_DB, else current directory)")
	force := fs.Bool("force", false, "overwrite existing files in the data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	archive := fs.Arg(0)
	if archive == "" {
		return errors.New("usage: gsbs-server restore [--data-dir DIR] [--force] <gsbs-backup-*.tar.zst>")
	}
	dest := strings.TrimSpace(*dataDir)
	if dest == "" {
		if db := strings.TrimSpace(os.Getenv("GSBS_DB")); db != "" {
			dest = filepath.Dir(db)
		} else {
			dest = "."
		}
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if !*force {
		if _, err := os.Stat(filepath.Join(absDest, "gsbs.db")); err == nil {
			return fmt.Errorf("%s already contains gsbs.db — stop the server and re-run with --force to overwrite", absDest)
		}
	}
	if err := os.MkdirAll(absDest, 0o750); err != nil {
		return err
	}

	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a zstd archive: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	counts := map[string]int{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		root := name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			root = name[:i]
		}
		switch root {
		case "gsbs.db", "gsbs-keys", "gamesaves", "covers":
		default:
			return fmt.Errorf("unexpected entry %q — not a GSBS backup archive", hdr.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe path %q in archive", hdr.Name)
		}
		target := filepath.Join(absDest, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		mode := os.FileMode(0o640)
		if root == "gsbs.db" || root == "gsbs-keys" {
			mode = 0o600
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil { //nolint:gosec // G110: archive comes from the operator's own backup
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		counts[root]++
	}
	if counts["gsbs.db"] == 0 {
		return errors.New("archive contains no gsbs.db — aborting (nothing was started)")
	}
	// Tighten the keys directory (created 0750 above, but it must be 0700).
	if counts["gsbs-keys"] > 0 {
		_ = os.Chmod(filepath.Join(absDest, "gsbs-keys"), 0o700)
	}

	fmt.Printf("Restored to %s:\n", absDest)
	fmt.Printf("  database:   gsbs.db\n")
	fmt.Printf("  key files:  %d (gsbs-keys/)\n", counts["gsbs-keys"])
	fmt.Printf("  save files: %d (gamesaves/)\n", counts["gamesaves"])
	if counts["covers"] > 0 {
		fmt.Printf("  covers:     %d (covers/)\n", counts["covers"])
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Point GSBS_DB at %s\n", filepath.Join(absDest, "gsbs.db"))
	if counts["gamesaves"] > 0 {
		fmt.Printf("  2. Point GSBS_SAVE_ROOT at %s\n", filepath.Join(absDest, "gamesaves"))
	}
	fmt.Println("  3. Start the server and run Admin → Data Integrity → Verify now")
	return nil
}
