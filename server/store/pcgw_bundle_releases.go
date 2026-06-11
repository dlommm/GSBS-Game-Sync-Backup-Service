package store

import (
	"encoding/json"
	"os"
	"strings"
)

const pcgwManifestReleasesSchemaV1 = 1

// PCGWManifestReleaseEntry is one row in manifest.releases.json.
type PCGWManifestReleaseEntry struct {
	Type           string `json:"type"` // "full" or "delta"
	ExportedAt     string `json:"exported_at"`
	FullExportedAt string `json:"full_exported_at,omitempty"`
	SHA256         string `json:"sha256"`
}

// PCGWManifestReleases is the release history sidecar (schema v1).
type PCGWManifestReleases struct {
	SchemaVersion int                        `json:"schema_version"`
	Releases      []PCGWManifestReleaseEntry `json:"releases"`
}

// UpdatePCGWManifestReleases appends or updates an entry in manifest.releases.json.
func UpdatePCGWManifestReleases(path string, entry PCGWManifestReleaseEntry) error {
	var doc PCGWManifestReleases
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &doc)
	}
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = pcgwManifestReleasesSchemaV1
	}
	entry.Type = strings.TrimSpace(entry.Type)
	entry.ExportedAt = strings.TrimSpace(entry.ExportedAt)
	entry.FullExportedAt = strings.TrimSpace(entry.FullExportedAt)
	entry.SHA256 = strings.TrimSpace(entry.SHA256)

	replaced := false
	for i := range doc.Releases {
		if doc.Releases[i].ExportedAt == entry.ExportedAt && doc.Releases[i].Type == entry.Type {
			doc.Releases[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Releases = append(doc.Releases, entry)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
