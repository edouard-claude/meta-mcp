package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// AllowedBreakdowns are the dimensions Meta accepts for
// follower_demographics.
var AllowedBreakdowns = []string{"city", "country", "age", "gender"}

// IGFollowerDemographics reads one demographic breakdown of the followers.
func (s *Service) IGFollowerDemographics(ctx context.Context, tenantID, pageID, breakdown string) ([]domain.Breakdown, error) {
	if !slices.Contains(AllowedBreakdowns, breakdown) {
		return nil, fmt.Errorf("breakdown doit valoir %v", AllowedBreakdowns)
	}
	page, err := s.igPage(ctx, tenantID, pageID)
	if err != nil {
		return nil, err
	}
	return s.graph.IGFollowerDemographics(ctx, page.PageToken, page.IGUserID, breakdown)
}
