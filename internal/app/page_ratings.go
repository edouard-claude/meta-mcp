package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Bounds of the page_ratings limit.
const (
	defaultRatingLimit = 25
	maxRatingLimit     = 100
)

// PageRatingsInput are the parameters of the page_ratings tool.
type PageRatingsInput struct {
	PageID string
	Limit  int
}

// PageRatings reads the rating summary of a page and its latest
// recommendations, newest first.
//
// There is no date window: Meta serves the edge in reverse chronological
// order and a page collects a handful of reviews a year, so limit is the only
// bound that makes sense. The caller filters on created_time if it needs a
// period.
func (s *Service) PageRatings(ctx context.Context, tenantID string, in PageRatingsInput) (domain.PageRatings, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return domain.PageRatings{}, err
	}
	limit := clampLimit(in.Limit, defaultRatingLimit, maxRatingLimit)
	return s.graph.PageRatings(ctx, page.PageToken, page.PageID, limit)
}
