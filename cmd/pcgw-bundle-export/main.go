// Command pcgw-bundle-export exports a GSBS PCGW manifest bundle for publishing.
//
// It always writes a single full bundle (manifest.json.gz) plus its metadata and,
// when --base-url is given, advances index.json (the monotonic version pointer
// consuming servers fetch). Delta bundles are no longer produced: servers always
// merge the full bundle, which is cheap because the import skips unchanged rows
// and reconciles deletions against the bundle's complete catalog.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gsbs/gsbs/pkg/atomicio"
	"github.com/gsbs/gsbs/server/store"
)

func main() {
	dbPath := flag.String("db", envOr("GSBS_DB", "gsbs.db"), "SQLite database path")
	outDir := flag.String("out", ".", "Output directory")
	lite := flag.Bool("lite", true, "Omit full wikitext metadata (recommended for publish)")
	version := flag.String("version", "dev", "GSBS version string embedded in bundle")
	baseURL := flag.String("base-url", "", "Public directory URL where artifacts are hosted; when set, writes/updates index.json (versioned sync)")
	flag.Parse()

	st, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	metaPath := filepath.Join(*outDir, "manifest.meta.json")

	data, meta, err := st.ExportPCGWManifestBundleWithOpts(ctx, *version, store.PCGWBundleExportOpts{Lite: *lite})
	if err != nil {
		log.Fatal(err)
	}

	// Atomic writes throughout: a crash mid-export previously left a
	// half-written bundle sitting next to a valid index, which consuming
	// servers would then fetch and fail to import.
	gzPath := filepath.Join(*outDir, "manifest.json.gz")
	if err := atomicio.WriteFile(gzPath, data, 0o644); err != nil {
		log.Fatal(err)
	}

	rawMeta, _ := json.MarshalIndent(meta, "", "  ")
	if err := atomicio.WriteFile(metaPath, rawMeta, 0o644); err != nil {
		log.Fatal(err)
	}

	releasesPath := filepath.Join(*outDir, "manifest.releases.json")
	if err := store.UpdatePCGWManifestReleases(releasesPath, store.PCGWManifestReleaseEntry{
		Type:           "full",
		ExportedAt:     meta.ExportedAt,
		FullExportedAt: meta.FullExportedAt,
		SHA256:         meta.FullSHA256,
	}); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Wrote %s (%d bytes, sha256=%s)\n", gzPath, len(data), meta.FullSHA256)
	fmt.Printf("Wrote %s\n", metaPath)
	fmt.Printf("Wrote %s\n", releasesPath)

	// Versioned-index publish: advance and write index.json so consuming servers
	// can do atomic, monotonic catch-up by merging the current full bundle.
	if strings.TrimSpace(*baseURL) != "" {
		indexPath := filepath.Join(*outDir, "index.json")
		var prevIndex store.PCGWBundleIndex
		if raw, err := os.ReadFile(indexPath); err == nil {
			// Do NOT ignore this error. A truncated index.json parsed as the
			// zero value, so the next export republished manifest_version 1 and
			// broke every consumer that gates on a monotonically increasing
			// version. Refuse rather than silently restart versioning.
			if err := json.Unmarshal(raw, &prevIndex); err != nil {
				log.Fatalf("read %s: %v (refusing to restart versioning from 1 — restore or delete the file deliberately)", indexPath, err)
			}
		} else if !os.IsNotExist(err) {
			log.Fatalf("read %s: %v", indexPath, err)
		}
		nextIndex, err := store.AdvanceBundleIndex(prevIndex, meta.FullSHA256, len(data), *baseURL, meta.ExportedAt)
		if err != nil {
			log.Fatalf("advance index: %v", err)
		}
		rawIndex, _ := json.MarshalIndent(nextIndex, "", "  ")
		if err := atomicio.WriteFile(indexPath, rawIndex, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Wrote %s (manifest_version=%d)\n", indexPath, nextIndex.ManifestVersion)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
