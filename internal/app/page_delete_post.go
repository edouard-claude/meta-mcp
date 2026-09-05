package app

import (
	"context"
	"errors"
	"strings"
)

// DeletePostInput are the parameters of the page_delete_post tool.
type DeletePostInput struct {
	PostID  string
	PageID  string
	Confirm bool
}

// DeletePostOutput is both the preview and the result.
type DeletePostOutput struct {
	Preview  bool   `json:"preview"`
	Deleted  bool   `json:"deleted,omitempty"`
	PostID   string `json:"post_id"`
	PageID   string `json:"page_id"`
	PageName string `json:"page_name"`
	Notice   string `json:"notice,omitempty"`
}

// PageDeletePost removes a published post from a Page.
//
// It is the counterpart of page_publish_post, and the most destructive tool
// here: unlike hiding a comment, a deleted post takes its reactions, comments
// and shares with it, and no amount of republishing brings them back.
func (s *Service) PageDeletePost(ctx context.Context, tenantID string, in DeletePostInput) (*DeletePostOutput, error) {
	if strings.TrimSpace(in.PostID) == "" {
		return nil, errors.New("post_id est obligatoire")
	}
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.PostID)
	if err != nil {
		return nil, err
	}

	out := &DeletePostOutput{
		PostID:   in.PostID,
		PageID:   page.PageID,
		PageName: page.Name,
	}
	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice +
			" La suppression est définitive : la publication, ses réactions et ses commentaires disparaissent."
		return out, nil
	}

	if err := s.graph.DeleteObject(ctx, page.PageToken, in.PostID); err != nil {
		return nil, err
	}
	out.Deleted = true
	return out, nil
}
