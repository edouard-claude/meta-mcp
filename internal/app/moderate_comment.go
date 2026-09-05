package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// AllowedModerationActions are what the moderation tools accept.
var AllowedModerationActions = []string{
	domain.ModerationHide,
	domain.ModerationUnhide,
	domain.ModerationDelete,
}

// ModerateCommentInput are the parameters of page_moderate_comment and
// ig_moderate_comment.
type ModerateCommentInput struct {
	CommentID string
	PageID    string
	Action    string
	Confirm   bool
}

// ModerateCommentOutput is both the preview and the result.
type ModerateCommentOutput struct {
	Preview   bool   `json:"preview"`
	Done      bool   `json:"done,omitempty"`
	CommentID string `json:"comment_id"`
	Action    string `json:"action"`
	PageID    string `json:"page_id"`
	PageName  string `json:"page_name"`
	Notice    string `json:"notice,omitempty"`
}

// ModerateComment hides, unhides or deletes a comment on a Page post.
func (s *Service) ModerateComment(ctx context.Context, tenantID string, in ModerateCommentInput) (*ModerateCommentOutput, error) {
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.CommentID)
	if err != nil {
		return nil, err
	}
	return s.moderate(ctx, page, in, false)
}

// IGModerateComment does the same on an Instagram media comment.
func (s *Service) IGModerateComment(ctx context.Context, tenantID string, in ModerateCommentInput) (*ModerateCommentOutput, error) {
	page, err := s.ownerIGPage(ctx, tenantID, in.PageID, in.CommentID)
	if err != nil {
		return nil, err
	}
	return s.moderate(ctx, page, in, true)
}

func (s *Service) moderate(ctx context.Context, page *domain.Page, in ModerateCommentInput, instagram bool) (*ModerateCommentOutput, error) {
	if strings.TrimSpace(in.CommentID) == "" {
		return nil, errors.New("comment_id est obligatoire")
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if !slices.Contains(AllowedModerationActions, action) {
		return nil, fmt.Errorf("action doit valoir %v", AllowedModerationActions)
	}

	out := &ModerateCommentOutput{
		CommentID: in.CommentID,
		Action:    action,
		PageID:    page.PageID,
		PageName:  page.Name,
	}
	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice
		if action == domain.ModerationDelete {
			out.Notice += " La suppression est définitive et irréversible."
		}
		return out, nil
	}

	var err error
	switch action {
	case domain.ModerationDelete:
		err = s.graph.DeleteObject(ctx, page.PageToken, in.CommentID)
	default:
		err = s.graph.SetCommentHidden(ctx, page.PageToken, in.CommentID,
			action == domain.ModerationHide, instagram)
	}
	if err != nil {
		return nil, err
	}
	out.Done = true
	return out, nil
}
