package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// DefaultIGMetrics is the metric set ig_account_insights reads by default.
var DefaultIGMetrics = []string{
	"reach",
	"views",
	"profile_views",
	"accounts_engaged",
	"total_interactions",
	"follower_count",
}

// IGInsightsInput are the parameters of the ig_account_insights tool.
type IGInsightsInput struct {
	PageID  string
	Since   string
	Until   string
	Metrics []string
}

// IGAccountInsights reads the account level metrics of the Instagram account
// linked to a page.
func (s *Service) IGAccountInsights(ctx context.Context, tenantID string, in IGInsightsInput) ([]domain.Insight, error) {
	page, err := s.igPage(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	since, until, err := s.dateWindow(in.Since, in.Until)
	if err != nil {
		return nil, err
	}
	return s.graph.IGAccountInsights(ctx, page.PageToken, page.IGUserID,
		metricsOrDefault(in.Metrics, DefaultIGMetrics), since, until)
}
