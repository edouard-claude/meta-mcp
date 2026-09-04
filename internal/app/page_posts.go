package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Bounds of the page_posts limit.
const (
	defaultPostLimit = 25
	maxPostLimit     = 100
)

// PagePostsInput are the parameters of the page_posts tool.
type PagePostsInput struct {
	PageID string
	Since  string
	Limit  int
}

// PagePosts lists the recent posts of a page with their organic performance.
func (s *Service) PagePosts(ctx context.Context, tenantID string, in PagePostsInput) ([]domain.Post, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	since, _, err := s.dateWindow(in.Since, "")
	if err != nil {
		return nil, err
	}
	limit := clampLimit(in.Limit, defaultPostLimit, maxPostLimit)
	return s.graph.PagePosts(ctx, page.PageToken, page.PageID, since, limit)
}
