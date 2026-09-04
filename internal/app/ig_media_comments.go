package app

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// IGCommentsInput are the parameters of the ig_media_comments tool.
type IGCommentsInput struct {
	MediaID string
	PageID  string
	Limit   int
}

// IGMediaComments lists the comments left on an Instagram media.
func (s *Service) IGMediaComments(ctx context.Context, tenantID string, in IGCommentsInput) ([]domain.Comment, error) {
	page, err := s.ownerIGPage(ctx, tenantID, in.PageID, in.MediaID)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(in.Limit, defaultCommentLimit, maxCommentLimit)
	return s.graph.IGMediaComments(ctx, page.PageToken, in.MediaID, limit)
}
