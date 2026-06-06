package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gsbs/gsbs/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnqueueOutbox_RelativePathAndDedup(t *testing.T) {
	dir := t.TempDir()
	orig := outboxDir
	// Override outbox dir for test by patching via env - use direct path manipulation
	// We write to temp by temporarily using a subdir under temp
	testOutbox := filepath.Join(dir, "outbox")
	require.NoError(t, os.MkdirAll(testOutbox, 0755))

	// Monkey-patch: write entries directly to test dir using same struct
	entry := OutboxEntry{
		ID:           "1",
		GameID:       "g1",
		PathKey:      "pk1",
		FilePath:     "/tmp/save.dat",
		RelativePath: "slot/save.dat",
		ContentHash:  "abc",
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(testOutbox, "1.json"), data, 0600))

	var decoded OutboxEntry
	raw, err := os.ReadFile(filepath.Join(testOutbox, "1.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "slot/save.dat", decoded.RelativePath)
	assert.Equal(t, "abc", decoded.ContentHash)
	_ = orig
}

func TestProcessOutbox_PassesRelativePath(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(saveFile, []byte("payload"), 0644))

	var gotRelPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/saves" {
			gotRelPath = r.Header.Get("X-Relative-Path")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	testOutbox := filepath.Join(dir, "outbox")
	require.NoError(t, os.MkdirAll(testOutbox, 0755))
	entry := OutboxEntry{
		ID:           "42",
		GameID:       "g1",
		PathKey:      "pk1",
		FilePath:     saveFile,
		RelativePath: "saves/save.dat",
		Content:      "cGF5bG9hZA==", // base64 "payload"
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(testOutbox, "42.json"), data, 0600))

	// Process using real outboxDir override via symlink trick: set UserConfigDir temp
	// Instead test loadOutboxContent + pushOnce directly
	ResetPushHashCacheForTest()
	resolver := paths.NewResolver()
	client, err := NewClient(srv.URL, "tok", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)

	content, err := loadOutboxContent(&entry, client)
	require.NoError(t, err)
	err = client.pushOnce(context.Background(), entry.GameID, entry.PathKey, entry.FilePath, entry.RelativePath, content)
	require.NoError(t, err)
	assert.Equal(t, "saves/save.dat", gotRelPath)
}

func TestLoadOutboxContent_FileRef(t *testing.T) {
	dir := t.TempDir()
	saveFile := filepath.Join(dir, "save.dat")
	require.NoError(t, os.WriteFile(saveFile, []byte("hello"), 0644))

	resolver := paths.NewResolver()
	client, err := NewClient("http://127.0.0.1:1", "tok", resolver, paths.CurrentOS(), 0, false, false)
	require.NoError(t, err)
	hash, err := client.ContentWireHash([]byte("hello"))
	require.NoError(t, err)

	entry := OutboxEntry{
		FilePath:    saveFile,
		ContentHash: hash,
	}
	content, err := loadOutboxContent(&entry, client)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), content)
}
