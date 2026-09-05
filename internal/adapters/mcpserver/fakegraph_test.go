package mcpserver

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// fakeGraph is a domain.GraphClient that records the token and object it was
// called with, so the tests can prove which tenant's credentials were used.
type fakeGraph struct {
	mu    sync.Mutex
	calls []graphCall
	err   error
}

// graphCall is one recorded call.
type graphCall struct {
	Method string
	Token  string
	Object string
}

var _ domain.GraphClient = (*fakeGraph)(nil)

func (f *fakeGraph) record(method, token, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, graphCall{Method: method, Token: token, Object: object})
	return f.err
}

func (f *fakeGraph) recorded() []graphCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]graphCall(nil), f.calls...)
}

func (f *fakeGraph) AuthorizeURL(string, string) string { return "https://facebook.test/dialog" }

func (f *fakeGraph) ExchangeCode(context.Context, string, string) (string, error) {
	return "SHORT", f.record("ExchangeCode", "", "")
}

func (f *fakeGraph) ExchangeLongLivedToken(context.Context, string) (domain.LongLivedToken, error) {
	return domain.LongLivedToken{Token: "LONG", ExpiresIn: 60 * 24 * time.Hour},
		f.record("ExchangeLongLivedToken", "", "")
}

func (f *fakeGraph) DebugToken(_ context.Context, token string) (domain.TokenStatus, error) {
	if err := f.record("DebugToken", token, ""); err != nil {
		return domain.TokenStatus{}, err
	}
	return domain.TokenStatus{
		Valid:     true,
		ExpiresAt: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
		Scopes:    []string{"pages_show_list", "instagram_basic"},
	}, nil
}

func (f *fakeGraph) Me(context.Context, string) (domain.MetaUser, error) {
	return domain.MetaUser{ID: "meta", Name: "Test"}, f.record("Me", "", "")
}

func (f *fakeGraph) Accounts(_ context.Context, token string) ([]domain.Page, error) {
	if err := f.record("Accounts", token, ""); err != nil {
		return nil, err
	}
	return []domain.Page{
		{PageID: "page-sync", Name: "Page resynchronisée", PageToken: "PT-SYNC", SyncedAt: time.Now().UTC()},
	}, nil
}

func (f *fakeGraph) PageInsights(_ context.Context, token, pageID string, metrics []string, _, _ time.Time) (domain.InsightSet, error) {
	if err := f.record("PageInsights", token, pageID); err != nil {
		return domain.InsightSet{}, err
	}
	out := make([]domain.Insight, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, domain.Insight{
			Metric: m, Period: "day",
			Values: []domain.InsightValue{{EndTime: "2026-09-01T07:00:00+0000", Value: json.RawMessage(`42`)}},
		})
	}
	return domain.InsightSet{Insights: out}, nil
}

func (f *fakeGraph) PagePosts(_ context.Context, token, pageID string, _ time.Time, _ int) ([]domain.Post, error) {
	if err := f.record("PagePosts", token, pageID); err != nil {
		return nil, err
	}
	return []domain.Post{{PostID: pageID + "_1", Message: "Bonjour", ImpressionsUnique: 10}}, nil
}

func (f *fakeGraph) PostComments(_ context.Context, token, postID string, _ int) ([]domain.Comment, error) {
	if err := f.record("PostComments", token, postID); err != nil {
		return nil, err
	}
	return []domain.Comment{{CommentID: "c1", From: "Alice", Message: "Bravo"}}, nil
}

func (f *fakeGraph) PublishPost(_ context.Context, token, pageID string, req domain.PublishPostRequest) (string, error) {
	if err := f.record("PublishPost", token, pageID); err != nil {
		return "", err
	}
	return pageID + "_published", nil
}

func (f *fakeGraph) ReplyToComment(_ context.Context, token, commentID, _ string) (string, error) {
	if err := f.record("ReplyToComment", token, commentID); err != nil {
		return "", err
	}
	return commentID + "_reply", nil
}

func (f *fakeGraph) IGAccountInsights(_ context.Context, token, igUserID string, metrics []string, _, _ time.Time) (domain.InsightSet, error) {
	if err := f.record("IGAccountInsights", token, igUserID); err != nil {
		return domain.InsightSet{}, err
	}
	out := make([]domain.Insight, 0, len(metrics))
	for _, m := range metrics {
		out = append(out, domain.Insight{Metric: m, Period: "day",
			Values: []domain.InsightValue{{Value: json.RawMessage(`7`)}}})
	}
	return domain.InsightSet{Insights: out}, nil
}

func (f *fakeGraph) IGFollowerDemographics(_ context.Context, token, igUserID, breakdown string) ([]domain.Breakdown, error) {
	if err := f.record("IGFollowerDemographics", token, igUserID); err != nil {
		return nil, err
	}
	return []domain.Breakdown{{Key: breakdown + ":Saint-Denis", Value: 120}}, nil
}

func (f *fakeGraph) IGMedia(_ context.Context, token, igUserID string, _ time.Time, _ int) ([]domain.Media, error) {
	if err := f.record("IGMedia", token, igUserID); err != nil {
		return nil, err
	}
	return []domain.Media{{MediaID: "m1", Type: "IMAGE", Reach: 300}}, nil
}

func (f *fakeGraph) IGMediaComments(_ context.Context, token, mediaID string, _ int) ([]domain.Comment, error) {
	if err := f.record("IGMediaComments", token, mediaID); err != nil {
		return nil, err
	}
	return []domain.Comment{{CommentID: "igc1", Username: "bob", Message: "top"}}, nil
}

func (f *fakeGraph) IGPublish(_ context.Context, token, igUserID string, _ domain.IGPublishRequest) (string, error) {
	if err := f.record("IGPublish", token, igUserID); err != nil {
		return "", err
	}
	return igUserID + "_media", nil
}

func (f *fakeGraph) IGReplyToComment(_ context.Context, token, commentID, _ string) (string, error) {
	if err := f.record("IGReplyToComment", token, commentID); err != nil {
		return "", err
	}
	return commentID + "_reply", nil
}
