//go:build pcgwcrawl

package job

// CrawlEnabled reports whether this build may crawl PCGamingWiki directly.
//
// It is true only under the `pcgwcrawl` build tag, which the VPS publisher
// builds with. See crawl_disabled.go for why stock builds leave it off.
const CrawlEnabled = true
