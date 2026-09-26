package job

import (
	"context"
	"errors"
	"testing"

	"github.com/gsbs/gsbs/pkg/pcgw"
	"github.com/gsbs/gsbs/server/store"
)

// enableCrawlForTest lets a test drive the crawler paths in a build without the
// `pcgwcrawl` tag, which is how the suite normally runs.
func enableCrawlForTest(t *testing.T) {
	t.Helper()
	prev := crawlEnabled
	crawlEnabled = true
	t.Cleanup(func() { crawlEnabled = prev })
}

// TestCrawlGateBlocksSyncWhenDisabled pins the property the build tag exists
// for: a stock build refuses to crawl PCGamingWiki at all. It must fail before
// the seeded gate, so an empty mirror is not what is being observed here.
func TestCrawlGateBlocksSyncWhenDisabled(t *testing.T) {
	prev := crawlEnabled
	crawlEnabled = false
	t.Cleanup(func() { crawlEnabled = prev })

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, err = PCGWSync(context.Background(), st, pcgw.NewClient(), nil, PCGWSyncOptions{})
	if !errors.Is(err, ErrPCGWCrawlDisabled) {
		t.Fatalf("err=%v want ErrPCGWCrawlDisabled", err)
	}
}

// TestCrawlGateAllowsManifestRebuild covers the carve-out: reprojecting the
// manifest reads the local mirror only, so it stays available in stock builds.
func TestCrawlGateAllowsManifestRebuild(t *testing.T) {
	prev := crawlEnabled
	crawlEnabled = false
	t.Cleanup(func() { crawlEnabled = prev })

	st, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, err = PCGWSync(context.Background(), st, pcgw.NewClient(), nil, PCGWSyncOptions{RebuildManifestOnly: true})
	if errors.Is(err, ErrPCGWCrawlDisabled) {
		t.Fatal("manifest rebuild must not be blocked by the crawl gate")
	}
}
