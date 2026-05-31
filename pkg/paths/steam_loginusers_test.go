package paths

import (
	"os"
	"path/filepath"
	"testing"
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
