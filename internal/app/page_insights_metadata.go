package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// PageInsightsMetadata lists the metrics Meta currently exposes for a page,
// which is the reliable way to discover what page_insights can ask for.
func (s *Service) PageInsightsMetadata(ctx context.Context, tenantID, pageID string) ([]domain.InsightMeta, error) {
	page, err := s.page(ctx, tenantID, pageID)
	if err != nil {
		return nil, err
	}
	return s.graph.PageInsightsMetadata(ctx, page.PageToken, page.PageID)
}
