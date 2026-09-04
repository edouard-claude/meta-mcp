package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// DefaultPageMetrics is the metric set page_insights reads when the caller
// asks for none.
//
// page_impressions_unique is deliberately absent: Meta deprecated it and now
// rejects the whole batch it appears in.
var DefaultPageMetrics = []string{
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

// PageInsights reads the organic metrics of one Facebook Page. Metrics Meta
// refuses come back in the Rejected list rather than failing the call.
func (s *Service) PageInsights(ctx context.Context, tenantID string, in PageInsightsInput) (domain.InsightSet, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return domain.InsightSet{}, err
	}
	since, until, err := s.dateWindow(in.Since, in.Until)
	if err != nil {
		return domain.InsightSet{}, err
	}
	return s.graph.PageInsights(ctx, page.PageToken, page.PageID,
		metricsOrDefault(in.Metrics, DefaultPageMetrics), since, until)
}
