package paths

import (
	"testing"
)

func TestExpandProgramData(t *testing.T) {
	r := &Resolver{
		Home:         "/home/user",
		LocalAppData: "/home/user/.local/share",
		AppData:      "/home/user/.config",
		ProgramData:  "C:\\ProgramData",
		ProgramFiles: "C:\\Program Files",
	}
	got := r.expandOne("%PROGRAMDATA%\\Game\\save", Windows)
	if got != "C:\\ProgramData\\Game\\save" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandHeroic(t *testing.T) {
	r := &Resolver{
		Heroic: "/home/user/.config/heroic",
	}
	got := r.expandOne("<Heroic-folder>/Games", Linux)
	if got != "/home/user/.config/heroic/Games" {
		t.Fatalf("got %q", got)
	}
}
