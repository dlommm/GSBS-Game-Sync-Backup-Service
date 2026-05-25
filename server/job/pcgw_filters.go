package job

import (
	"context"
	"strings"

	"github.com/gsbs/gsbs/server/store"
)

// PCGWFilters holds title/path substring excludes loaded from admin_settings.
type PCGWFilters struct {
	TitleExcludes []string
	PathExcludes  []string
}

// LoadPCGWFilters reads exclude lists from the store (with defaults for path excludes).
func LoadPCGWFilters(ctx context.Context, st store.Store) PCGWFilters {
	settings, err := st.ListAdminSettings(ctx)
	if err != nil {
		settings = map[string]string{}
	}
	return PCGWFilters{
		TitleExcludes: store.PCGWTitleExcludesFromSettings(settings),
		PathExcludes:  store.PCGWPathExcludesFromSettings(settings),
	}
}

func (f PCGWFilters) ShouldSkipTitle(title string) bool {
	lower := strings.ToLower(title)
	for _, ex := range f.TitleExcludes {
		if ex == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(ex)) {
			return true
		}
	}
	return false
}

func (f PCGWFilters) ShouldExcludePath(path string) bool {
	lower := strings.ToLower(path)
	for _, ex := range f.PathExcludes {
		if ex == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(ex)) {
			return true
		}
	}
	return false
}
