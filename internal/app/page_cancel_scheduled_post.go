package app

import (
	"context"
	"errors"
	"strings"
)

// CancelScheduledPostInput are the parameters of the
// page_cancel_scheduled_post tool.
type CancelScheduledPostInput struct {
	PostID  string
	PageID  string
	Confirm bool
}

// CancelScheduledPostOutput is both the preview and the result.
type CancelScheduledPostOutput struct {
	Preview  bool   `json:"preview"`
	Canceled bool   `json:"canceled,omitempty"`
	PostID   string `json:"post_id"`
	PageID   string `json:"page_id"`
	PageName string `json:"page_name"`
	Notice   string `json:"notice,omitempty"`
}

// PageCancelScheduledPost deletes a post Meta had not published yet.
//
// This is the one tool that destroys something rather than adding to it, so
// the confirmation gate matters more here than anywhere else: a cancelled
// scheduled post cannot be recovered, only written again.
func (s *Service) PageCancelScheduledPost(ctx context.Context, tenantID string, in CancelScheduledPostInput) (*CancelScheduledPostOutput, error) {
	if strings.TrimSpace(in.PostID) == "" {
		return nil, errors.New("post_id est obligatoire")
	}
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.PostID)
	if err != nil {
		return nil, err
	}

	out := &CancelScheduledPostOutput{
		PostID:   in.PostID,
		PageID:   page.PageID,
		PageName: page.Name,
	}
	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice + " L'annulation est définitive : la publication programmée sera supprimée, pas mise en pause."
		return out, nil
	}

	if err := s.graph.DeleteObject(ctx, page.PageToken, in.PostID); err != nil {
		return nil, err
	}
	out.Canceled = true
	return out, nil
}
