package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// minSchedule is Meta's own lower bound for a scheduled post.
const minSchedule = 10 * time.Minute

// maxSchedule is Meta's upper bound: six months ahead.
const maxSchedule = 180 * 24 * time.Hour

// previewNotice is what a preview tells the caller to do next. It is written
// for the model, which must show the preview to the user before confirming.
const previewNotice = "Aperçu uniquement, rien n'a été publié. " +
	"Montrez ce contenu à l'utilisateur et attendez son accord explicite, " +
	"puis rappelez le même outil avec confirm=true."

// PublishPostInput are the parameters of the page_publish_post tool.
type PublishPostInput struct {
	PageID      string
	Message     string
	Link        string
	PhotoURL    string
	ScheduledAt string
	Confirm     bool
}

// PublishPostOutput is both the preview and the result: only Preview and
// PostID differ between the two.
type PublishPostOutput struct {
	Preview     bool   `json:"preview"`
	PostID      string `json:"post_id,omitempty"`
	PageID      string `json:"page_id"`
	PageName    string `json:"page_name"`
	Kind        string `json:"kind"`
	Message     string `json:"message,omitempty"`
	Link        string `json:"link,omitempty"`
	PhotoURL    string `json:"photo_url,omitempty"`
	ScheduledAt string `json:"scheduled_at,omitempty"`
	Notice      string `json:"notice,omitempty"`
}

// PublishPost publishes on a Facebook Page, or returns a preview when the
// caller has not confirmed. Nothing reaches Meta without confirm=true.
func (s *Service) PublishPost(ctx context.Context, tenantID string, in PublishPostInput) (*PublishPostOutput, error) {
	page, err := s.page(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}

	req, err := s.buildPublishRequest(in)
	if err != nil {
		return nil, err
	}

	out := &PublishPostOutput{
		PageID:   page.PageID,
		PageName: page.Name,
		Kind:     "feed",
		Message:  req.Message,
		Link:     req.Link,
		PhotoURL: req.PhotoURL,
	}
	if req.IsPhoto() {
		out.Kind = "photo"
	}
	if req.IsScheduled() {
		out.ScheduledAt = req.ScheduledAt.Format(time.RFC3339)
	}

	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice
		return out, nil
	}

	postID, err := s.graph.PublishPost(ctx, page.PageToken, page.PageID, req)
	if err != nil {
		return nil, err
	}
	out.PostID = postID
	return out, nil
}

// buildPublishRequest validates the parameters and turns them into the domain
// request.
func (s *Service) buildPublishRequest(in PublishPostInput) (domain.PublishPostRequest, error) {
	req := domain.PublishPostRequest{
		Message:  strings.TrimSpace(in.Message),
		Link:     strings.TrimSpace(in.Link),
		PhotoURL: strings.TrimSpace(in.PhotoURL),
	}
	if req.Message == "" && req.Link == "" && req.PhotoURL == "" {
		return req, errors.New("fournissez au moins message, link ou photo_url")
	}
	if req.PhotoURL != "" && req.Link != "" {
		return req, errors.New("link et photo_url s'excluent: une photo ne porte pas de lien")
	}
	if req.Link != "" {
		if err := validateHTTPSURL(req.Link, "link"); err != nil {
			return req, err
		}
	}
	if req.PhotoURL != "" {
		if err := validateHTTPSURL(req.PhotoURL, "photo_url"); err != nil {
			return req, err
		}
	}

	if in.ScheduledAt != "" {
		when, err := parseSchedule(in.ScheduledAt, s.clock.Now())
		if err != nil {
			return req, err
		}
		req.ScheduledAt = when
	}
	return req, nil
}

// parseSchedule accepts an RFC 3339 timestamp and checks it against Meta's
// scheduling window.
func parseSchedule(value string, now time.Time) (time.Time, error) {
	when, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("scheduled_at doit être une date ISO 8601, par exemple 2026-09-10T09:00:00Z")
	}
	switch delay := when.Sub(now); {
	case delay < minSchedule:
		return time.Time{}, fmt.Errorf("scheduled_at doit être au moins %d minutes dans le futur", int(minSchedule.Minutes()))
	case delay > maxSchedule:
		return time.Time{}, errors.New("scheduled_at ne peut pas dépasser six mois")
	}
	return when.UTC(), nil
}

// validateHTTPSURL rejects anything Meta would not fetch anyway, and keeps
// the server from being used to probe internal addresses.
func validateHTTPSURL(raw, field string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s n'est pas une URL valide", field)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s doit être une URL https", field)
	}
	return nil
}
