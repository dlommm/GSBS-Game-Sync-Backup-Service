package paths

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLoginUsersVDF_MostRecent(t *testing.T) {
	vdf := `
"users"
{
	"76561198011111111"
	{
		"AccountName"		"alice"
		"MostRecent"		"0"
	}
	"76561198022222222"
	{
		"AccountName"		"bob"
		"MostRecent"		"1"
	}
}
`
	got := parseLoginUsersVDF(vdf)
	if got != "76561198022222222" {
		t.Fatalf("got %q want 76561198022222222", got)
	}
}

func TestParseLoginUsersVDF_FirstWhenNoMostRecent(t *testing.T) {
	vdf := `
"users"
{
	"76561198033333333"
	{
		"AccountName"		"carol"
	}
}
`
	got := parseLoginUsersVDF(vdf)
	if got != "76561198033333333" {
		t.Fatalf("got %q want 76561198033333333", got)
	}
}

func TestDetectSteamUserID_FromUserdataFallback(t *testing.T) {
	root := t.TempDir()
	userdata := filepath.Join(root, "userdata", "76561198044444444")
	if err := os.MkdirAll(userdata, 0755); err != nil {
		t.Fatal(err)
	}
	got := DetectSteamUserID([]string{root})
	if got != "76561198044444444" {
		t.Fatalf("got %q want 76561198044444444", got)
	}
}

func TestDetectSteamUserID_MultiAccount_PicksMostRecentlyModified(t *testing.T) {
	root := t.TempDir()
	olderID := "76561198011111111"
	newerID := "76561198022222222"
	olderDir := filepath.Join(root, "userdata", olderID)
	newerDir := filepath.Join(root, "userdata", newerID)
	if err := os.MkdirAll(olderDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newerDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Set olderDir's mtime to a fixed time in the past.
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(olderDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	// Set newerDir's mtime to a more recent time.
	newTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(newerDir, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	// No loginusers.vdf — fallback to userdata dir modification time.
	got := DetectSteamUserID([]string{root})
	if got != newerID {
		t.Fatalf("got %q want %q (most recently modified userdata dir)", got, newerID)
	}
}

func TestDetectSteamUserID_FromLoginUsersVDF(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	vdf := `"users"
{
	"76561198055555555"
	{
		"MostRecent"		"1"
	}
}`
	if err := os.WriteFile(filepath.Join(configDir, "loginusers.vdf"), []byte(vdf), 0644); err != nil {
		t.Fatal(err)
	}
	got := DetectSteamUserID([]string{root})
	if got != "76561198055555555" {
		t.Fatalf("got %q want 76561198055555555", got)
	}
}
