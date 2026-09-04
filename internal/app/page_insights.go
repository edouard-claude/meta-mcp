package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// DefaultPageMetrics is the metric set page_insights reads when the caller
// asks for none.
var DefaultPageMetrics = []string{
	"page_impressions_unique",
	"page_post_engagements",
	"page_daily_follows",
	"page_follows",
	"page_views_total",
}

// PageInsightsInput are the parameters of the page_insights tool.
type PageInsightsInput struct {
	PageID  string
	Since   string
	Until   string
	Metrics []string
}

// PageInsights reads the organic metrics of one Facebook Page.
func (s *Service) PageInsights(ctx context.Context, tenantID string, in PageInsightsInput) ([]domain.Insight, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	since, until, err := s.dateWindow(in.Since, in.Until)
	if err != nil {
		return nil, err
	}
	return s.graph.PageInsights(ctx, page.PageToken, page.PageID,
		metricsOrDefault(in.Metrics, DefaultPageMetrics), since, until)
}
