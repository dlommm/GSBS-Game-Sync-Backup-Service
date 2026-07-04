package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gsbs/gsbs/client/sync"
	"github.com/gsbs/gsbs/pkg/paths"
)

// runExportCommand implements `gsbs-client export [--game ID] [--out DIR]`:
// it downloads the account's saves (decrypting end-to-end-encrypted ones with
// the local passphrase) into a zip archive with a gsbs-manifest.json, the
// same format the server's WebUI export produces — so the archive can be
// re-imported into any GSBS server.
func runExportCommand(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	game := fs.String("game", "", "export only this game ID (default: all games)")
	outDir := fs.String("out", ".", "directory to write the archive into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w (run `gsbs-client login` first)", err)
	}
	resolver := configureResolverFromConfig(cfg)
	client, err := sync.NewClient(cfg.ServerURL, cfg.Token, resolver, paths.CurrentOS(), 0, cfg.UseCompression, false)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if enc, err := client.FetchAccountSettings(ctx); err == nil {
		client.SetEncryption(enc, cfg.EncryptionPassphrase)
	}

	saves, err := client.DownloadAll(ctx, *game)
	if err != nil {
		return err
	}
	if len(saves) == 0 {
		return fmt.Errorf("no saves found%s", map[bool]string{true: " for game " + *game, false: ""}[*game != ""])
	}

	name := "gsbs-export-" + time.Now().Format("20060102-150405") + ".zip"
	path := filepath.Join(*outDir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	type entry struct {
		GameID       string `json:"game_id"`
		PathKey      string `json:"path_key"`
		RelativePath string `json:"relative_path"`
		Zip          string `json:"zip"`
		ContentHash  string `json:"content_hash"`
		UpdatedAt    string `json:"updated_at"`
		Encrypted    bool   `json:"encrypted"`
	}
	manifest := struct {
		Format  string  `json:"format"`
		Entries []entry `json:"entries"`
	}{Format: "gsbs-export/1"}

	used := map[string]bool{}
	for _, s := range saves {
		rel := s.RelativePath
		if rel == "" {
			rel = s.PathKey
		}
		member := "files/" + sanitizeExportPath(s.GameID+"/"+rel)
		for used[member] {
			member += "_"
		}
		used[member] = true
		zf, err := zw.Create(member)
		if err != nil {
			return err
		}
		if _, err := zf.Write(s.Content); err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, entry{
			GameID: s.GameID, PathKey: s.PathKey, RelativePath: s.RelativePath,
			Zip: member, ContentHash: sync.FileHash(s.Content), UpdatedAt: s.UpdatedAt,
		})
	}
	mf, err := zw.Create("gsbs-manifest.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(mf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	fmt.Printf("Exported %d save file(s) to %s\n", len(manifest.Entries), path)
	return nil
}

// sanitizeExportPath keeps zip members safe and relative.
func sanitizeExportPath(p string) string {
	parts := strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "unnamed"
	}
	return strings.Join(clean, "/")
}
