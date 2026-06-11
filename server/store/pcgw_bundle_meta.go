package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PCGWBundleMetaURLFromSettings derives manifest.meta.json URL from the full bundle URL.
func PCGWBundleMetaURLFromSettings(settings map[string]string) string {
	url := PCGWBundleURLFromSettings(settings)
	return bundleMetaURLFromBundleURL(url)
}

func bundleMetaURLFromBundleURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	switch {
	case strings.HasSuffix(url, "manifest.json.gz"):
		return url[:len(url)-len("manifest.json.gz")] + "manifest.meta.json"
	case strings.HasSuffix(url, "/manifest.json.gz"):
		return strings.TrimSuffix(url, "manifest.json.gz") + "manifest.meta.json"
	default:
		if i := strings.LastIndex(url, "/"); i >= 0 {
			return url[:i+1] + "manifest.meta.json"
		}
		return url + ".meta.json"
	}
}

// ParsePCGWBundleMeta unmarshals manifest.meta.json bytes.
func ParsePCGWBundleMeta(data []byte) (PCGWBundleMeta, error) {
	var meta PCGWBundleMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return PCGWBundleMeta{}, fmt.Errorf("invalid manifest.meta.json: %w", err)
	}
	return meta, nil
}

// CanApplyRemoteDelta reports whether the server can safely apply a remote delta bundle.
// fullExportedAt is the remote cumulative anchor (typically meta.FullExportedAt).
func CanApplyRemoteDelta(lastExported, fullExportedAt string, meta PCGWBundleMeta) bool {
	lastExported = strings.TrimSpace(lastExported)
	fullExportedAt = strings.TrimSpace(fullExportedAt)
	if lastExported == "" {
		return false
	}
	anchor := strings.TrimSpace(meta.FullExportedAt)
	if anchor != "" {
		if fullExportedAt == "" {
			fullExportedAt = anchor
		}
		return lastExported >= fullExportedAt
	}
	return lastExported == strings.TrimSpace(meta.PreviousExportedAt)
}
