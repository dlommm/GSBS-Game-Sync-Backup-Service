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

	gzPath := filepath.Join(*outDir, "manifest.json.gz")
	if err := os.WriteFile(gzPath, data, 0o644); err != nil {
		log.Fatal(err)
	}

	rawMeta, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, rawMeta, 0o644); err != nil {
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
			_ = json.Unmarshal(raw, &prevIndex)
		}
		nextIndex, err := store.AdvanceBundleIndex(prevIndex, meta.FullSHA256, len(data), *baseURL, meta.ExportedAt)
		if err != nil {
			log.Fatalf("advance index: %v", err)
		}
		rawIndex, _ := json.MarshalIndent(nextIndex, "", "  ")
		if err := os.WriteFile(indexPath, rawIndex, 0o644); err != nil {
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
