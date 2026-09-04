package app

import (
	"context"

	"github.com/edouard/metasocial-mcp/internal/domain"
)

// Bounds of the comment limits.
const (
	defaultCommentLimit = 50
	maxCommentLimit     = 100
)

// PageCommentsInput are the parameters of the page_post_comments tool.
type PageCommentsInput struct {
	PostID string
	PageID string
	Limit  int
}

// PagePostComments lists the comments left on a Facebook Page post.
func (s *Service) PagePostComments(ctx context.Context, tenantID string, in PageCommentsInput) ([]domain.Comment, error) {
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.PostID)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(in.Limit, defaultCommentLimit, maxCommentLimit)
	return s.graph.PostComments(ctx, page.PageToken, in.PostID, limit)
}
