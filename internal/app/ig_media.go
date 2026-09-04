package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Bounds of the ig_media limit.
const (
	defaultMediaLimit = 25
	maxMediaLimit     = 100
)

// IGMediaInput are the parameters of the ig_media tool.
type IGMediaInput struct {
	PageID string
	Since  string
	Limit  int
}

// IGMedia lists the recent Instagram media of an account with their insights.
func (s *Service) IGMedia(ctx context.Context, tenantID string, in IGMediaInput) ([]domain.Media, error) {
	page, err := s.igPage(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	// As for page_posts, no since means no lower bound rather than the 28
	// day default, so a rarely posting account does not come back empty.
	since, err := optionalDay(in.Since, "since")
	if err != nil {
		return nil, err
	}
	limit := clampLimit(in.Limit, defaultMediaLimit, maxMediaLimit)
	return s.graph.IGMedia(ctx, page.PageToken, page.IGUserID, since, limit)
}
