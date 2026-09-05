package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Bounds of the scheduled post listing.
const (
	defaultScheduledLimit = 25
	maxScheduledLimit     = 100
)

// ScheduledPostsInput are the parameters of the page_scheduled_posts tool.
type ScheduledPostsInput struct {
	PageID string
	Limit  int
}

// PageScheduledPosts lists the publications Meta is holding for a future
// date. It closes the loop opened by page_publish_post with scheduled_at,
// which could otherwise create posts nobody could see or cancel afterwards.
func (s *Service) PageScheduledPosts(ctx context.Context, tenantID string, in ScheduledPostsInput) ([]domain.ScheduledPost, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(in.Limit, defaultScheduledLimit, maxScheduledLimit)
	return s.graph.ScheduledPosts(ctx, page.PageToken, page.PageID, limit)
}
