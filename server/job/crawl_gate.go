package job

// crawlEnabled is the effective crawl gate consulted by PCGWSyncEx.
//
// It is seeded from the CrawlEnabled build-tag constant and is a variable only
// so that tests can exercise the crawler paths in an untagged build. Nothing in
// production assigns to it — flipping the behavior for real means building with
// `-tags pcgwcrawl`.
var crawlEnabled = CrawlEnabled
