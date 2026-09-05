package app

import (
	"context"
	"errors"
	"strings"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// DefaultPostMetrics is what page_post_insights reads by default: the detail
// behind the three numbers page_posts already flattens into its listing.
// The impressions family is absent on purpose: measured against the live
// Graph API, post_impressions, post_impressions_unique, post_impressions_organic,
// post_engaged_users and post_activity are all rejected with code 100.
var DefaultPostMetrics = []string{
	"post_clicks",
	"post_reactions_by_type_total",
	"post_reactions_like_total",
	"post_video_views",
}

// DefaultIGMediaMetrics is what ig_media_insights reads by default.
var DefaultIGMediaMetrics = []string{
	"reach",
	"views",
	"likes",
	"comments",
	"saved",
	"shares",
	"total_interactions",
}

// ObjectInsightsInput are the parameters of page_post_insights and
// ig_media_insights.
type ObjectInsightsInput struct {
	ObjectID string
	PageID   string
	Metrics  []string
}

// PagePostInsights reads the metrics of one Page post.
func (s *Service) PagePostInsights(ctx context.Context, tenantID string, in ObjectInsightsInput) (domain.InsightSet, error) {
	if strings.TrimSpace(in.ObjectID) == "" {
		return domain.InsightSet{}, errors.New("post_id est obligatoire")
	}
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.ObjectID)
	if err != nil {
		return domain.InsightSet{}, err
	}
	return s.graph.PostInsights(ctx, page.PageToken, in.ObjectID,
		metricsOrDefault(in.Metrics, DefaultPostMetrics))
}

// IGMediaInsights reads the metrics of one Instagram media.
func (s *Service) IGMediaInsights(ctx context.Context, tenantID string, in ObjectInsightsInput) (domain.InsightSet, error) {
	if strings.TrimSpace(in.ObjectID) == "" {
		return domain.InsightSet{}, errors.New("media_id est obligatoire")
	}
	page, err := s.ownerIGPage(ctx, tenantID, in.PageID, in.ObjectID)
	if err != nil {
		return domain.InsightSet{}, err
	}
	return s.graph.IGMediaInsights(ctx, page.PageToken, in.ObjectID,
		metricsOrDefault(in.Metrics, DefaultIGMediaMetrics))
}
