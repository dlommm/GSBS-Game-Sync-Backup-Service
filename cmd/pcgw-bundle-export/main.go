// Command pcgw-bundle-export exports GSBS PCGW manifest bundles for publishing.
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
	full := flag.Bool("full", false, "Export full lite bundle (default when not delta)")
	delta := flag.Bool("delta", false, "Export delta bundle (requires --since or existing meta)")
	since := flag.String("since", "", "RFC3339 timestamp for delta export (default: full_exported_at from meta)")
	lite := flag.Bool("lite", true, "Omit full wikitext metadata (recommended for publish)")
	version := flag.String("version", "dev", "GSBS version string embedded in bundle")
	prevExported := flag.String("previous-exported-at", "", "Override previous_exported_at for delta metadata")
	flag.Parse()

	if !*delta && !*full {
		*full = true
	}

	st, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	metaPath := filepath.Join(*outDir, "manifest.meta.json")

	var existingMeta store.PCGWBundleMeta
	if raw, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(raw, &existingMeta)
	}

	opts := store.PCGWBundleExportOpts{Lite: *lite}
	if *delta {
		anchor := deltaAnchor(&existingMeta, strings.TrimSpace(*since))
		if anchor == "" {
			log.Fatal("--delta requires --since or existing manifest.meta.json with full_exported_at or exported_at from last full")
		}
		opts.Since = anchor
		opts.FullExportedAt = anchor
		opts.PreviousExportedAt = anchor
		if v := strings.TrimSpace(*prevExported); v != "" {
			opts.PreviousExportedAt = v
		}
	}

	data, meta, err := st.ExportPCGWManifestBundleWithOpts(ctx, *version, opts)
	if err != nil {
		log.Fatal(err)
	}

	name := "manifest.json.gz"
	releaseType := "full"
	sha256 := meta.FullSHA256
	if *delta {
		name = "manifest.delta.json.gz"
		meta.DeltaSHA256 = meta.FullSHA256
		meta.DeltaBytes = meta.FullBytes
		meta.FullSHA256 = existingMeta.FullSHA256
		meta.FullBytes = existingMeta.FullBytes
		if meta.FullExportedAt == "" {
			meta.FullExportedAt = deltaAnchor(&existingMeta, "")
		}
		if meta.PreviousExportedAt == "" {
			meta.PreviousExportedAt = deltaAnchor(&existingMeta, "")
		}
		sha256 = meta.DeltaSHA256
		releaseType = "delta"
	} else if v := strings.TrimSpace(*prevExported); v != "" {
		meta.PreviousExportedAt = v
	}

	gzPath := filepath.Join(*outDir, name)
	if err := os.WriteFile(gzPath, data, 0o644); err != nil {
		log.Fatal(err)
	}

	rawMeta, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(metaPath, rawMeta, 0o644); err != nil {
		log.Fatal(err)
	}

	releasesPath := filepath.Join(*outDir, "manifest.releases.json")
	if err := store.UpdatePCGWManifestReleases(releasesPath, store.PCGWManifestReleaseEntry{
		Type:           releaseType,
		ExportedAt:     meta.ExportedAt,
		FullExportedAt: meta.FullExportedAt,
		SHA256:         sha256,
	}); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Wrote %s (%d bytes, sha256=%s)\n", gzPath, len(data), sha256)
	fmt.Printf("Wrote %s\n", metaPath)
	fmt.Printf("Wrote %s\n", releasesPath)
}

func deltaAnchor(existing *store.PCGWBundleMeta, sinceOverride string) string {
	if sinceOverride != "" {
		return sinceOverride
	}
	if existing == nil {
		return ""
	}
	if v := strings.TrimSpace(existing.FullExportedAt); v != "" {
		return v
	}
	if strings.TrimSpace(existing.FullSHA256) != "" && strings.TrimSpace(existing.DeltaSHA256) == "" {
		return strings.TrimSpace(existing.ExportedAt)
	}
	if v := strings.TrimSpace(existing.PreviousExportedAt); v != "" {
		return v
	}
	return strings.TrimSpace(existing.ExportedAt)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
