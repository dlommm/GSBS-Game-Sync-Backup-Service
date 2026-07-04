package webui

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gsbs/gsbs/pkg/savepath"
	"github.com/gsbs/gsbs/server/logx"
	"github.com/gsbs/gsbs/server/store"
)

// exportManifestName identifies GSBS-produced archives; import requires it.
const exportManifestName = "gsbs-manifest.json"

// exportEntry describes one save in an export archive.
type exportEntry struct {
	GameID       string `json:"game_id"`
	PathKey      string `json:"path_key"`
	RelativePath string `json:"relative_path"`
	Zip          string `json:"zip"` // exact archive member holding the bytes
	ContentHash  string `json:"content_hash"`
	UpdatedAt    string `json:"updated_at"`
	Encrypted    bool   `json:"encrypted"`
}

type exportManifest struct {
	Format  string        `json:"format"` // "gsbs-export/1"
	Entries []exportEntry `json:"entries"`
}

// handleExportZip streams a zip of the latest save content — one game
// (?game_id=...) or the whole account. ?versions=all additionally includes
// retained version history. Encrypted saves export as ciphertext (the server
// cannot decrypt them); clients decrypt on pull as usual after an import.
func (h *WebHandler) handleExportZip(w http.ResponseWriter, r *http.Request) {
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	gameID := strings.TrimSpace(r.URL.Query().Get("game_id"))
	includeVersions := r.URL.Query().Get("versions") == "all"

	summaries, err := h.store.ListSaveSummaries(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load saves", http.StatusInternalServerError)
		return
	}
	var selected []store.SaveSummary
	for _, s := range summaries {
		if gameID == "" || s.GameID == gameID {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		Redirect(w, r, "/dashboard/games?error=export_empty")
		return
	}

	base := "gsbs-export"
	if gameID != "" {
		base = "gsbs-" + sanitizeFilenamePart(gameID) + "-export"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".zip"))

	zw := zip.NewWriter(w)
	manifest := exportManifest{Format: "gsbs-export/1"}
	used := map[string]bool{}
	anyEncrypted := false
	for _, s := range selected {
		blob, err := h.store.GetSave(r.Context(), userID, s.GameID, s.PathKey)
		if err != nil || blob == nil {
			continue
		}
		rel := s.RelativePath
		if rel == "" {
			rel = s.PathKey
		}
		member := "files/" + sanitizeZipPath(s.GameID, rel, gameID == "")
		for used[member] {
			member += "_"
		}
		used[member] = true
		f, err := zw.Create(member)
		if err != nil {
			logx.Logger().Warn().Err(err).Msg("export: zip create")
			return // headers already sent; stream is broken either way
		}
		if _, err := f.Write(blob.Content); err != nil {
			return
		}
		if s.Encrypted {
			anyEncrypted = true
		}
		manifest.Entries = append(manifest.Entries, exportEntry{
			GameID: s.GameID, PathKey: s.PathKey, RelativePath: s.RelativePath,
			Zip: member, ContentHash: s.ContentHash, UpdatedAt: s.UpdatedAt, Encrypted: s.Encrypted,
		})
		if includeVersions {
			versions, verr := h.store.ListSaveVersions(r.Context(), userID, s.GameID, s.PathKey, 50)
			if verr != nil {
				continue
			}
			for _, v := range versions {
				vb, gerr := h.store.GetSaveVersion(r.Context(), userID, s.GameID, s.PathKey, v.Version)
				if gerr != nil || vb == nil {
					continue
				}
				vf, cerr := zw.Create(fmt.Sprintf("versions/%s/v%d", sanitizeZipPath(s.GameID, rel, gameID == ""), v.Version))
				if cerr != nil {
					return
				}
				if _, werr := vf.Write(vb.Content); werr != nil {
					return
				}
			}
		}
	}
	if mf, err := zw.Create(exportManifestName); err == nil {
		enc := json.NewEncoder(mf)
		enc.SetIndent("", "  ")
		_ = enc.Encode(manifest)
	}
	if anyEncrypted {
		if rf, err := zw.Create("README.txt"); err == nil {
			_, _ = io.WriteString(rf, "Some files in this archive are end-to-end encrypted; the server stores only ciphertext.\nImport the archive back into a GSBS server and let your client (with the passphrase) pull and decrypt them.\n")
		}
	}
	_ = zw.Close()
	h.appendAuditBroadcast(r.Context(), userID, username, "export_saves", gameID, fmt.Sprintf("%d files", len(manifest.Entries)))
}

// sanitizeZipPath produces a safe archive member path from a save's relative
// path (validated at push time, but sanitized again defensively).
func sanitizeZipPath(gameID, rel string, prefixGame bool) string {
	parts := strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "." || p == ".." {
			continue
		}
		clean = append(clean, sanitizeFilenamePart(p))
	}
	out := strings.Join(clean, "/")
	if out == "" {
		out = "unnamed"
	}
	if prefixGame {
		out = sanitizeFilenamePart(gameID) + "/" + out
	}
	return out
}

// maxImportBytes caps the uploaded archive (multipart body).
const maxImportBytes = 512 << 20

// handleImportSaves ingests a GSBS export archive: every entry from
// gsbs-manifest.json is validated exactly like a client push (path
// validation, size caps, quota inside the write transaction) and stored as a
// new version — nothing is deleted.
func (h *WebHandler) handleImportSaves(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(64 << 20); err != nil { //nolint:gosec // G120: body capped by MaxBytesReader above; the arg only bounds the in-memory portion
		Redirect(w, r, "/dashboard/games?error=import_too_large")
		return
	}
	if !ValidateCSRF(r, h.secret) {
		http.Error(w, "Invalid security token.", http.StatusBadRequest)
		return
	}
	userID, username, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if h.readOnly {
		Redirect(w, r, "/dashboard/games?error=read_only")
		return
	}
	file, _, err := r.FormFile("archive")
	if err != nil {
		Redirect(w, r, "/dashboard/games?error=import_no_file")
		return
	}
	defer file.Close()

	// zip needs ReaderAt: spool to a temp file (the multipart reader may be
	// disk-backed already, but the interface doesn't guarantee it).
	tmp, err := os.CreateTemp("", "gsbs-import-*.zip")
	if err != nil {
		Redirect(w, r, "/dashboard/games?error=import_failed")
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer tmp.Close()
	size, err := io.Copy(tmp, file)
	if err != nil {
		Redirect(w, r, "/dashboard/games?error=import_failed")
		return
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		Redirect(w, r, "/dashboard/games?error=import_not_zip")
		return
	}

	var manifest exportManifest
	mf, err := zr.Open(exportManifestName)
	if err != nil {
		Redirect(w, r, "/dashboard/games?error=import_no_manifest")
		return
	}
	err = json.NewDecoder(io.LimitReader(mf, 32<<20)).Decode(&manifest)
	_ = mf.Close()
	if err != nil || !strings.HasPrefix(manifest.Format, "gsbs-export/") {
		Redirect(w, r, "/dashboard/games?error=import_bad_manifest")
		return
	}
	if len(manifest.Entries) == 0 || len(manifest.Entries) > 10000 {
		Redirect(w, r, "/dashboard/games?error=import_bad_manifest")
		return
	}

	var userQuota int64
	if q, qErr := h.store.UserQuotaBytes(r.Context(), userID); qErr == nil {
		userQuota = q
	}
	imported, failed := 0, 0
	for _, e := range manifest.Entries {
		if e.GameID == "" || e.PathKey == "" || e.Zip == "" {
			failed++
			continue
		}
		if e.RelativePath != "" {
			if verr := savepath.ValidateRelativePath(e.RelativePath); verr != nil {
				failed++
				continue
			}
		}
		zf, oerr := zr.Open(e.Zip)
		if oerr != nil {
			failed++
			continue
		}
		content, rerr := io.ReadAll(io.LimitReader(zf, int64(52<<20)))
		_ = zf.Close()
		if rerr != nil || len(content) == 0 || len(content) > 50<<20 {
			failed++
			continue
		}
		meta := &store.SaveMeta{
			RelativePath:     e.RelativePath,
			Encrypted:        e.Encrypted,
			QuotaBytes:       userQuota,
			GlobalLimitBytes: h.maxStorageBytes,
		}
		if e.Encrypted {
			// The plaintext hash cannot be recomputed server-side; trust the
			// manifest like a push's X-Content-Hash for encrypted payloads.
			meta.ContentHash = e.ContentHash
		}
		if _, uerr := h.store.UpsertSaveWithMeta(r.Context(), userID, e.GameID, e.PathKey, content, meta); uerr != nil {
			failed++
			continue
		}
		imported++
	}
	h.appendAuditBroadcast(r.Context(), userID, username, "import_saves", "", fmt.Sprintf("imported=%d failed=%d", imported, failed))
	Redirect(w, r, fmt.Sprintf("/dashboard/games?imported=%d&import_failed=%d", imported, failed))
}
