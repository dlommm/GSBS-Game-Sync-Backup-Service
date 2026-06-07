package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sampleLibraryFoldersVDF = `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
		"contentid"		"123"
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"label"		""
		"contentid"		"456"
	}
	"2"
	{
		"path" "/home/user/SteamLibrary"
	}
}
`

func TestParseLibraryFoldersVDF(t *testing.T) {
	dir := t.TempDir()
	vdfPath := filepath.Join(dir, "libraryfolders.vdf")
	if err := os.WriteFile(vdfPath, []byte(sampleLibraryFoldersVDF), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := parseLibraryFoldersVDF(vdfPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`C:\Program Files (x86)\Steam`,
		`D:\SteamLibrary`,
		`/home/user/SteamLibrary`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseLibraryFoldersVDF_MissingFile(t *testing.T) {
	_, err := parseLibraryFoldersVDF(filepath.Join(t.TempDir(), "missing.vdf"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseLibraryFoldersVDF_NoPaths(t *testing.T) {
	dir := t.TempDir()
	vdfPath := filepath.Join(dir, "libraryfolders.vdf")
	if err := os.WriteFile(vdfPath, []byte(`"libraryfolders" { "0" { "label" "x" } }`), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := parseLibraryFoldersVDF(vdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestAppendSteamLibrariesFromVDF(t *testing.T) {
	root := t.TempDir()
	extraLib := filepath.Join(root, "ExtraLibrary")
	if err := os.MkdirAll(extraLib, 0755); err != nil {
		t.Fatal(err)
	}

	vdfContent := `"libraryfolders"
{
	"1"
	{
		"path"		"` + filepath.ToSlash(extraLib) + `"
	}
	"2"
	{
		"path"		"` + filepath.ToSlash(extraLib) + `"
	}
}
`
	steamapps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(steamapps, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(steamapps, libraryfoldersVDFName), []byte(vdfContent), 0644); err != nil {
		t.Fatal(err)
	}

	got := appendSteamLibrariesFromVDF([]string{root})
	if len(got) != 2 {
		t.Fatalf("got %d roots, want 2 (root + extra, deduped)", len(got))
	}
	if got[0] != root {
		t.Fatalf("first root %q, want %q", got[0], root)
	}
	if filepath.FromSlash(got[1]) != extraLib {
		t.Fatalf("second root %q, want %q", got[1], extraLib)
	}
}

func TestAppendSteamLibrariesFromVDF_SkipsNonexistent(t *testing.T) {
	root := t.TempDir()
	vdfContent := `"libraryfolders"
{
	"1"
	{
		"path"		"/nonexistent/steam/library"
	}
}
`
	steamapps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(steamapps, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(steamapps, libraryfoldersVDFName), []byte(vdfContent), 0644); err != nil {
		t.Fatal(err)
	}

	got := appendSteamLibrariesFromVDF([]string{root})
	if len(got) != 1 || got[0] != root {
		t.Fatalf("got %v, want only existing root %q", got, root)
	}
}
