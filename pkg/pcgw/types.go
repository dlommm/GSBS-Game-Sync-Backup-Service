package pcgw

// GameBundle is the full parsed result of ingesting one PCGW game page.
type GameBundle struct {
	PageID            int64
	PageInfo          PageInfo
	RevisionID        int64
	RevisionTimestamp string
	FullWikitext      string
	FullWikitextZstd  []byte
	Infobox           map[string]string
	Sections          map[string]SectionResult
	SaveLocations     []SaveLocationTemplate
	AllTemplates      []string
	FailedSections    []string
	ParseStatus       string // ok, partial, failed
}

// SectionResult holds raw and structured data for one wiki section.
type SectionResult struct {
	Key             string
	RawTitle        string
	SectionWikitext string
	AllTemplates    []string
	Data            map[string]interface{}
	ParseError      string
}

// IngestResult wraps a GameBundle with top-level ingest metadata.
type IngestResult struct {
	Bundle           GameBundle
	FailedSections   []string
	Errors           []string
}
