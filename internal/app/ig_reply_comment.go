package app

import (
	"context"
	"errors"
	"strings"
)

// IGReplyToComment answers a comment on an Instagram media.
func (s *Service) IGReplyToComment(ctx context.Context, tenantID string, in ReplyCommentInput) (*ReplyCommentOutput, error) {
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return nil, errors.New("message est obligatoire")
	}
	page, err := s.ownerIGPage(ctx, tenantID, in.PageID, in.CommentID)
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

	replyID, err := s.graph.IGReplyToComment(ctx, page.PageToken, in.CommentID, message)
	if err != nil {
		return nil, err
	}
	out.ReplyID = replyID
	return out, nil
}
