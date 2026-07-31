package handlers

import "github.com/xzb177/yimao/internal/services"

type embyIdentityLookup func(int, services.MediaType) (*services.EmbySearchResult, error)
type embySeasonLookup func(int, int) (bool, error)

// requestExistsInEmby applies the same scope as the request itself. Movies and
// full-show TV requests are work-level checks; season-specific TV requests only
// block when that exact season exists.
func requestExistsInEmby(tmdbID int, mediaType services.MediaType, season int, identity embyIdentityLookup, seasonLookup embySeasonLookup) (*services.EmbySearchResult, bool, error) {
	if mediaType == services.MediaTypeTV && season > 0 {
		exists, err := seasonLookup(tmdbID, season)
		return nil, exists, err
	}
	item, err := identity(tmdbID, mediaType)
	return item, item != nil, err
}
