package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// IGStories lists the stories still live on the Instagram account. Meta drops
// a story from this edge once it is 24 hours old, so an empty answer means
// nothing is currently published rather than an error.
func (s *Service) IGStories(ctx context.Context, tenantID, pageID string) ([]domain.Media, error) {
	page, err := s.igPage(ctx, tenantID, pageID)
	if err != nil {
		return nil, err
	}
	return s.graph.IGStories(ctx, page.PageToken, page.IGUserID)
}
