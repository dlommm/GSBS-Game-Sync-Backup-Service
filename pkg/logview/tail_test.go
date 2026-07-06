package logview

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEntriesPageOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	var lines string
	for i := 0; i < 10; i++ {
		lines += fmt.Sprintf(`{"level":"info","time":"2026-07-05T00:00:%02dZ","message":"entry %d"}`, i, i) + "\n"
	}
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	q := Query{Level: "all", Component: "all", Limit: 4}
	page1, total, err := LoadEntriesPage(path, q, ParseZerologLine)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || len(page1) != 4 {
		t.Fatalf("page1: total=%d len=%d", total, len(page1))
	}
	// Newest first: first page starts with entry 9.
	if page1[0].Message != "entry 9" || page1[3].Message != "entry 6" {
		t.Fatalf("page1 order: %q .. %q", page1[0].Message, page1[3].Message)
	}

	q.Offset = 8
	page3, total, err := LoadEntriesPage(path, q, ParseZerologLine)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || len(page3) != 2 {
		t.Fatalf("page3: total=%d len=%d", total, len(page3))
	}
	if page3[0].Message != "entry 1" || page3[1].Message != "entry 0" {
		t.Fatalf("page3 order: %q, %q", page3[0].Message, page3[1].Message)
	}

	// Offset beyond the window yields no entries but the true total.
	q.Offset = 50
	empty, total, err := LoadEntriesPage(path, q, ParseZerologLine)
	if err != nil || total != 10 || len(empty) != 0 {
		t.Fatalf("offset beyond: total=%d len=%d err=%v", total, len(empty), err)
	}

	// LoadEntries respects Offset too (compat wrapper).
	q.Offset = 9
	one, err := LoadEntries(path, q, ParseZerologLine)
	if err != nil || len(one) != 1 || one[0].Message != "entry 0" {
		t.Fatalf("LoadEntries offset: %+v err=%v", one, err)
	}
}
