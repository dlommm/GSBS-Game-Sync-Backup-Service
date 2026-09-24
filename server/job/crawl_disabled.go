//go:build !pcgwcrawl

package job

// CrawlEnabled reports whether this build may crawl PCGamingWiki directly.
//
// Stock builds leave it false. Exactly one machine in the fleet — the VPS
// publisher, built with `-tags pcgwcrawl` — mirrors PCGamingWiki; every other
// GSBS server takes that mirror as a published bundle from R2. Keeping the
// crawler off by default means a self-hosted or forked GSBS cannot become an
// uncoordinated second crawler, which is what prompted PCGW to withdraw
// anonymous Cargo access in the first place.
//
// This gates the sync entry point rather than excluding the crawler's source:
// the producer and the bundle consumer still compile together, so a change to
// the bundle format cannot silently break the servers that import it.
const CrawlEnabled = false
