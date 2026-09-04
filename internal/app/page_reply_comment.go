package app

import (
	"context"
	"errors"
	"strings"
)

// ReplyCommentInput are the parameters of page_reply_comment and
// ig_reply_comment.
type ReplyCommentInput struct {
	CommentID string
	PageID    string
	Message   string
	Confirm   bool
}

// ReplyCommentOutput is both the preview and the result of a reply.
type ReplyCommentOutput struct {
	Preview   bool   `json:"preview"`
	ReplyID   string `json:"reply_id,omitempty"`
	CommentID string `json:"comment_id"`
	PageID    string `json:"page_id"`
	PageName  string `json:"page_name"`
	Message   string `json:"message"`
	Notice    string `json:"notice,omitempty"`
}

// ReplyToComment answers a comment on a Facebook Page post, behind the same
// confirmation gate as every other write.
func (s *Service) ReplyToComment(ctx context.Context, tenantID string, in ReplyCommentInput) (*ReplyCommentOutput, error) {
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return nil, errors.New("message est obligatoire")
	}
	page, err := s.ownerPage(ctx, tenantID, in.PageID, in.CommentID)
	if err != nil {
		return nil, err
	}

	out := &ReplyCommentOutput{
		CommentID: in.CommentID,
		PageID:    page.PageID,
		PageName:  page.Name,
		Message:   message,
	}
	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice
		return out, nil
	}

	replyID, err := s.graph.ReplyToComment(ctx, page.PageToken, in.CommentID, message)
	if err != nil {
		return nil, err
	}
	out.ReplyID = replyID
	return out, nil
}
