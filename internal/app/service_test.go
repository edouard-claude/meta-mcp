package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edouard/metasocial-mcp/internal/domain"
)

// fakeGraph is a domain.GraphClient that records what it was called with.
type fakeGraph struct {
	fakeMetaOAuth

	err   error
	calls []graphCall
}

type graphCall struct {
	Method  string
	Token   string
	Object  string
	Metrics []string
	Since   time.Time
	Until   time.Time
	Limit   int
}

var _ domain.GraphClient = (*fakeGraph)(nil)

func newFakeGraph() *fakeGraph {
	return &fakeGraph{fakeMetaOAuth: *newFakeMeta()}
}

func (f *fakeGraph) record(c graphCall) error {
	f.calls = append(f.calls, c)
	return f.err
}

func (f *fakeGraph) last() graphCall {
	if len(f.calls) == 0 {
		return graphCall{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeGraph) PageInsights(_ context.Context, token, pageID string, metrics []string, since, until time.Time) ([]domain.Insight, error) {
	if err := f.record(graphCall{Method: "PageInsights", Token: token, Object: pageID,
		Metrics: metrics, Since: since, Until: until}); err != nil {
		return nil, err
	}
	return []domain.Insight{{Metric: metrics[0], Period: "day",
		Values: []domain.InsightValue{{Value: json.RawMessage(`1`)}}}}, nil
}

func (f *fakeGraph) PageInsightsMetadata(_ context.Context, token, pageID string) ([]domain.InsightMeta, error) {
	if err := f.record(graphCall{Method: "PageInsightsMetadata", Token: token, Object: pageID}); err != nil {
		return nil, err
	}
	return []domain.InsightMeta{{Name: "page_views_total"}}, nil
}

func (f *fakeGraph) PagePosts(_ context.Context, token, pageID string, since time.Time, limit int) ([]domain.Post, error) {
	if err := f.record(graphCall{Method: "PagePosts", Token: token, Object: pageID,
		Since: since, Limit: limit}); err != nil {
		return nil, err
	}
	return []domain.Post{{PostID: pageID + "_1"}}, nil
}

func (f *fakeGraph) PostComments(_ context.Context, token, postID string, limit int) ([]domain.Comment, error) {
	if err := f.record(graphCall{Method: "PostComments", Token: token, Object: postID, Limit: limit}); err != nil {
		return nil, err
	}
	return []domain.Comment{{CommentID: "c1"}}, nil
}

func (f *fakeGraph) PublishPost(_ context.Context, token, pageID string, req domain.PublishPostRequest) (string, error) {
	if err := f.record(graphCall{Method: "PublishPost", Token: token, Object: pageID}); err != nil {
		return "", err
	}
	return pageID + "_new", nil
}

func (f *fakeGraph) ReplyToComment(_ context.Context, token, commentID, _ string) (string, error) {
	if err := f.record(graphCall{Method: "ReplyToComment", Token: token, Object: commentID}); err != nil {
		return "", err
	}
	return commentID + "_r", nil
}

func (f *fakeGraph) IGAccountInsights(_ context.Context, token, igUserID string, metrics []string, since, until time.Time) ([]domain.Insight, error) {
	if err := f.record(graphCall{Method: "IGAccountInsights", Token: token, Object: igUserID,
		Metrics: metrics, Since: since, Until: until}); err != nil {
		return nil, err
	}
	return []domain.Insight{{Metric: metrics[0]}}, nil
}

func (f *fakeGraph) IGFollowerDemographics(_ context.Context, token, igUserID, breakdown string) ([]domain.Breakdown, error) {
	if err := f.record(graphCall{Method: "IGFollowerDemographics", Token: token, Object: igUserID}); err != nil {
		return nil, err
	}
	return []domain.Breakdown{{Key: breakdown, Value: 1}}, nil
}

func (f *fakeGraph) IGMedia(_ context.Context, token, igUserID string, since time.Time, limit int) ([]domain.Media, error) {
	if err := f.record(graphCall{Method: "IGMedia", Token: token, Object: igUserID,
		Since: since, Limit: limit}); err != nil {
		return nil, err
	}
	return []domain.Media{{MediaID: "m1"}}, nil
}

func (f *fakeGraph) IGMediaComments(_ context.Context, token, mediaID string, limit int) ([]domain.Comment, error) {
	if err := f.record(graphCall{Method: "IGMediaComments", Token: token, Object: mediaID, Limit: limit}); err != nil {
		return nil, err
	}
	return []domain.Comment{{CommentID: "igc1"}}, nil
}

func (f *fakeGraph) IGPublish(_ context.Context, token, igUserID string, _ domain.IGPublishRequest) (string, error) {
	if err := f.record(graphCall{Method: "IGPublish", Token: token, Object: igUserID}); err != nil {
		return "", err
	}
	return igUserID + "_m", nil
}

func (f *fakeGraph) IGReplyToComment(_ context.Context, token, commentID, _ string) (string, error) {
	if err := f.record(graphCall{Method: "IGReplyToComment", Token: token, Object: commentID}); err != nil {
		return "", err
	}
	return commentID + "_r", nil
}

// newServiceHarness builds a Service over two tenants: A owns a page with
// Instagram, B owns a page without.
func newServiceHarness(t *testing.T) (*Service, *fakeStore, *fakeGraph, *fakeClock) {
	t.Helper()
	store, graph, clk := newFakeStore(), newFakeGraph(), newFakeClock()
	store.seedTenant("tenant-a", "meta-a", "USER-A", domain.Page{
		PageID: "page-a", Name: "Page A", PageToken: "PT-A",
		IGUserID: "ig-a", IGUsername: "pagea", SyncedAt: clk.Now(),
	})
	store.seedTenant("tenant-b", "meta-b", "USER-B", domain.Page{
		PageID: "page-b", Name: "Page B", PageToken: "PT-B", SyncedAt: clk.Now(),
	})
	return NewService(store, graph, clk, "https://mcp.example.re"), store, graph, clk
}

func TestListPagesReturnsOnlyTheTenantPages(t *testing.T) {
	svc, _, _, _ := newServiceHarness(t)

	pages, err := svc.ListPages(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 1 || pages[0].PageID != "page-a" || pages[0].IGUsername != "pagea" {
		t.Fatalf("pages = %+v", pages)
	}
	if pages[0].SyncedAt == "" {
		t.Fatal("synced_at absent")
	}

	empty, err := svc.ListPages(t.Context(), "tenant-inconnu")
	if err != nil || len(empty) != 0 {
		t.Fatalf("pages = %+v (err %v)", empty, err)
	}
}

func TestSyncPagesReplacesAndReturns(t *testing.T) {
	svc, store, _, _ := newServiceHarness(t)

	pages, err := svc.SyncPages(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("SyncPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("pages = %+v", pages)
	}
	stored, _ := store.ListPages(t.Context(), "tenant-a")
	if len(stored) != 2 {
		t.Fatalf("pages stockées = %+v", stored)
	}
	if _, err := svc.SyncPages(t.Context(), "inconnu"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant inconnu: %v", err)
	}
}

func TestPageResolutionIsTenantScoped(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.PageInsights(t.Context(), "tenant-a", PageInsightsInput{PageID: "page-b"}); !errors.Is(err, domain.ErrUnknownPage) {
		t.Fatalf("erreur = %v, attendu ErrUnknownPage", err)
	}
	if _, err := svc.PageInsights(t.Context(), "tenant-a", PageInsightsInput{}); err == nil {
		t.Fatal("page_id vide accepté")
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appels Graph = %+v", graph.calls)
	}
}

func TestPageInsightsDefaultsAndWindow(t *testing.T) {
	svc, _, graph, clk := newServiceHarness(t)

	if _, err := svc.PageInsights(t.Context(), "tenant-a", PageInsightsInput{PageID: "page-a"}); err != nil {
		t.Fatalf("PageInsights: %v", err)
	}
	call := graph.last()
	if call.Token != "PT-A" || call.Object != "page-a" {
		t.Fatalf("appel = %+v", call)
	}
	if len(call.Metrics) != len(DefaultPageMetrics) {
		t.Fatalf("métriques = %v", call.Metrics)
	}
	if !call.Until.Equal(clk.Now()) {
		t.Fatalf("until = %v, attendu maintenant", call.Until)
	}
	if want := clk.Now().AddDate(0, 0, -defaultWindowDays); !call.Since.Equal(want) {
		t.Fatalf("since = %v, attendu %v", call.Since, want)
	}

	// Explicit metrics and window win.
	if _, err := svc.PageInsights(t.Context(), "tenant-a", PageInsightsInput{
		PageID: "page-a", Metrics: []string{"page_follows", "  "},
		Since: "2026-08-01", Until: "2026-08-31",
	}); err != nil {
		t.Fatalf("PageInsights: %v", err)
	}
	call = graph.last()
	if len(call.Metrics) != 1 || call.Metrics[0] != "page_follows" {
		t.Fatalf("métriques = %v", call.Metrics)
	}
	if call.Since.Format(dayLayout) != "2026-08-01" || call.Until.Format(dayLayout) != "2026-08-31" {
		t.Fatalf("fenêtre = %v → %v", call.Since, call.Until)
	}
}

func TestDateWindowErrors(t *testing.T) {
	svc, _, _, _ := newServiceHarness(t)

	cases := map[string]PageInsightsInput{
		"since invalide": {PageID: "page-a", Since: "01/08/2026"},
		"until invalide": {PageID: "page-a", Until: "hier"},
		"ordre inversé":  {PageID: "page-a", Since: "2026-09-01", Until: "2026-08-01"},
		"fenêtre nulle":  {PageID: "page-a", Since: "2026-08-01", Until: "2026-07-31"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.PageInsights(t.Context(), "tenant-a", in); err == nil {
				t.Fatal("aucune erreur")
			}
		})
	}
}

func TestPageInsightsMetadataAndPosts(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.PageInsightsMetadata(t.Context(), "tenant-a", "page-a"); err != nil {
		t.Fatalf("PageInsightsMetadata: %v", err)
	}
	if graph.last().Method != "PageInsightsMetadata" {
		t.Fatalf("appel = %+v", graph.last())
	}

	if _, err := svc.PagePosts(t.Context(), "tenant-a", PagePostsInput{PageID: "page-a"}); err != nil {
		t.Fatalf("PagePosts: %v", err)
	}
	if got := graph.last().Limit; got != defaultPostLimit {
		t.Fatalf("limite par défaut = %d", got)
	}
	if _, err := svc.PagePosts(t.Context(), "tenant-a", PagePostsInput{PageID: "page-a", Limit: 5000}); err != nil {
		t.Fatalf("PagePosts: %v", err)
	}
	if got := graph.last().Limit; got != maxPostLimit {
		t.Fatalf("limite plafonnée = %d", got)
	}
}

func TestOwnerPageResolution(t *testing.T) {
	svc, store, graph, clk := newServiceHarness(t)

	// The "{page-id}_{object-id}" prefix identifies the page on its own.
	if _, err := svc.PagePostComments(t.Context(), "tenant-a", PageCommentsInput{PostID: "page-a_42"}); err != nil {
		t.Fatalf("PagePostComments: %v", err)
	}
	if graph.last().Token != "PT-A" {
		t.Fatalf("appel = %+v", graph.last())
	}

	// A single page is used even without any hint.
	if _, err := svc.PagePostComments(t.Context(), "tenant-a", PageCommentsInput{PostID: "42"}); err != nil {
		t.Fatalf("PagePostComments: %v", err)
	}

	// With two pages and no hint, the caller must be explicit.
	_ = store.ReplacePages(t.Context(), "tenant-a", []domain.Page{
		{PageID: "page-a", Name: "A", PageToken: "PT-A", IGUserID: "ig-a", SyncedAt: clk.Now()},
		{PageID: "page-a2", Name: "A2", PageToken: "PT-A2", IGUserID: "ig-a2", SyncedAt: clk.Now()},
	})
	_, err := svc.PagePostComments(t.Context(), "tenant-a", PageCommentsInput{PostID: "42"})
	if err == nil || !strings.Contains(err.Error(), "précisez page_id") {
		t.Fatalf("erreur = %v", err)
	}

	// And a tenant with no page at all is told to sync.
	store.seedTenant("tenant-c", "meta-c", "USER-C")
	_, err = svc.PagePostComments(t.Context(), "tenant-c", PageCommentsInput{PostID: "42"})
	if err == nil || !strings.Contains(err.Error(), "sync_pages") {
		t.Fatalf("erreur = %v", err)
	}

	if _, err := svc.PagePostComments(t.Context(), "tenant-a", PageCommentsInput{PostID: ""}); err == nil {
		t.Fatal("post_id vide accepté")
	}
}

func TestInstagramToolsRequireALinkedAccount(t *testing.T) {
	svc, _, _, _ := newServiceHarness(t)

	if _, err := svc.IGAccountInsights(t.Context(), "tenant-b", IGInsightsInput{PageID: "page-b"}); !errors.Is(err, domain.ErrNoInstagram) {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := svc.IGMedia(t.Context(), "tenant-b", IGMediaInput{PageID: "page-b"}); !errors.Is(err, domain.ErrNoInstagram) {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := svc.IGMediaComments(t.Context(), "tenant-b", IGCommentsInput{MediaID: "m1"}); !errors.Is(err, domain.ErrNoInstagram) {
		t.Fatalf("erreur = %v", err)
	}
}

func TestInstagramReads(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.IGAccountInsights(t.Context(), "tenant-a", IGInsightsInput{PageID: "page-a"}); err != nil {
		t.Fatalf("IGAccountInsights: %v", err)
	}
	call := graph.last()
	if call.Object != "ig-a" || call.Token != "PT-A" || len(call.Metrics) != len(DefaultIGMetrics) {
		t.Fatalf("appel = %+v", call)
	}

	if _, err := svc.IGMedia(t.Context(), "tenant-a", IGMediaInput{PageID: "page-a", Limit: 3}); err != nil {
		t.Fatalf("IGMedia: %v", err)
	}
	if graph.last().Limit != 3 {
		t.Fatalf("limite = %d", graph.last().Limit)
	}

	if _, err := svc.IGMediaComments(t.Context(), "tenant-a", IGCommentsInput{MediaID: "m1"}); err != nil {
		t.Fatalf("IGMediaComments: %v", err)
	}
	if graph.last().Object != "m1" || graph.last().Token != "PT-A" {
		t.Fatalf("appel = %+v", graph.last())
	}
}

func TestIGFollowerDemographicsValidatesBreakdown(t *testing.T) {
	svc, _, _, _ := newServiceHarness(t)

	for _, breakdown := range AllowedBreakdowns {
		if _, err := svc.IGFollowerDemographics(t.Context(), "tenant-a", "page-a", breakdown); err != nil {
			t.Fatalf("breakdown %s: %v", breakdown, err)
		}
	}
	if _, err := svc.IGFollowerDemographics(t.Context(), "tenant-a", "page-a", "planet"); err == nil {
		t.Fatal("un breakdown inconnu a été accepté")
	}
}

func TestOwnerIGPageNeedsAHintWithSeveralAccounts(t *testing.T) {
	svc, store, _, clk := newServiceHarness(t)
	_ = store.ReplacePages(t.Context(), "tenant-a", []domain.Page{
		{PageID: "page-a", Name: "A", PageToken: "PT-A", IGUserID: "ig-a", SyncedAt: clk.Now()},
		{PageID: "page-a2", Name: "A2", PageToken: "PT-A2", IGUserID: "ig-a2", SyncedAt: clk.Now()},
	})

	_, err := svc.IGMediaComments(t.Context(), "tenant-a", IGCommentsInput{MediaID: "m1"})
	if err == nil || !strings.Contains(err.Error(), "précisez page_id") {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := svc.IGMediaComments(t.Context(), "tenant-a",
		IGCommentsInput{MediaID: "m1", PageID: "page-a2"}); err != nil {
		t.Fatalf("avec page_id: %v", err)
	}
}

func TestPublishPostPreviewDoesNotCallMeta(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	out, err := svc.PublishPost(t.Context(), "tenant-a", PublishPostInput{
		PageID: "page-a", Message: "Bonjour",
	})
	if err != nil {
		t.Fatalf("PublishPost: %v", err)
	}
	if !out.Preview || out.PostID != "" || out.Notice == "" {
		t.Fatalf("aperçu = %+v", out)
	}
	if out.PageName != "Page A" || out.Kind != "feed" {
		t.Fatalf("aperçu = %+v", out)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appels Graph = %+v", graph.calls)
	}
}

func TestPublishPostConfirmed(t *testing.T) {
	svc, _, graph, clk := newServiceHarness(t)

	out, err := svc.PublishPost(t.Context(), "tenant-a", PublishPostInput{
		PageID: "page-a", Message: "Légende", PhotoURL: "https://cdn.test/a.jpg",
		ScheduledAt: clk.Now().Add(2 * time.Hour).Format(time.RFC3339), Confirm: true,
	})
	if err != nil {
		t.Fatalf("PublishPost: %v", err)
	}
	if out.Preview || out.PostID != "page-a_new" || out.Kind != "photo" {
		t.Fatalf("résultat = %+v", out)
	}
	if out.ScheduledAt == "" {
		t.Fatal("scheduled_at absent du résultat")
	}
	if graph.last().Method != "PublishPost" || graph.last().Token != "PT-A" {
		t.Fatalf("appel = %+v", graph.last())
	}
}

func TestPublishPostValidation(t *testing.T) {
	svc, _, _, clk := newServiceHarness(t)

	cases := map[string]PublishPostInput{
		"rien":                {PageID: "page-a"},
		"lien et photo":       {PageID: "page-a", Link: "https://a.test", PhotoURL: "https://b.test/x.jpg"},
		"lien http":           {PageID: "page-a", Link: "http://a.test"},
		"photo http":          {PageID: "page-a", PhotoURL: "http://a.test/x.jpg"},
		"lien illisible":      {PageID: "page-a", Link: "https://"},
		"date illisible":      {PageID: "page-a", Message: "x", ScheduledAt: "demain"},
		"date trop proche":    {PageID: "page-a", Message: "x", ScheduledAt: clk.Now().Add(time.Minute).Format(time.RFC3339)},
		"date trop lointaine": {PageID: "page-a", Message: "x", ScheduledAt: clk.Now().Add(400 * 24 * time.Hour).Format(time.RFC3339)},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.PublishPost(t.Context(), "tenant-a", in); err == nil {
				t.Fatal("entrée invalide acceptée")
			}
		})
	}
}

func TestReplyToComment(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	out, err := svc.ReplyToComment(t.Context(), "tenant-a", ReplyCommentInput{
		CommentID: "page-a_1", Message: "Merci",
	})
	if err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	if !out.Preview || out.ReplyID != "" || len(graph.calls) != 0 {
		t.Fatalf("aperçu = %+v, appels = %+v", out, graph.calls)
	}

	out, err = svc.ReplyToComment(t.Context(), "tenant-a", ReplyCommentInput{
		CommentID: "page-a_1", Message: " Merci ", Confirm: true,
	})
	if err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	if out.Preview || out.ReplyID != "page-a_1_r" || out.Message != "Merci" {
		t.Fatalf("résultat = %+v", out)
	}

	if _, err := svc.ReplyToComment(t.Context(), "tenant-a",
		ReplyCommentInput{CommentID: "page-a_1", Message: "  "}); err == nil {
		t.Fatal("message vide accepté")
	}
}

func TestIGReplyToComment(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	out, err := svc.IGReplyToComment(t.Context(), "tenant-a", ReplyCommentInput{
		CommentID: "igc1", Message: "Merci", Confirm: true,
	})
	if err != nil {
		t.Fatalf("IGReplyToComment: %v", err)
	}
	if out.ReplyID != "igc1_r" || graph.last().Method != "IGReplyToComment" {
		t.Fatalf("résultat = %+v, appel = %+v", out, graph.last())
	}

	if _, err := svc.IGReplyToComment(t.Context(), "tenant-a",
		ReplyCommentInput{CommentID: "igc1", Message: ""}); err == nil {
		t.Fatal("message vide accepté")
	}
	if _, err := svc.IGReplyToComment(t.Context(), "tenant-b",
		ReplyCommentInput{CommentID: "igc1", Message: "x"}); !errors.Is(err, domain.ErrNoInstagram) {
		t.Fatalf("erreur = %v", err)
	}
}

func TestIGPublishValidationAndConfirm(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	invalid := map[string]IGPublishInput{
		"image sans url":  {PageID: "page-a", MediaType: "IMAGE"},
		"image http":      {PageID: "page-a", ImageURL: "http://cdn/a.jpg"},
		"reels sans url":  {PageID: "page-a", MediaType: "REELS"},
		"reels http":      {PageID: "page-a", MediaType: "REELS", VideoURL: "http://cdn/v.mp4"},
		"carrousel court": {PageID: "page-a", MediaType: "CAROUSEL", Children: []string{"https://cdn/1.jpg"}},
		"carrousel http":  {PageID: "page-a", MediaType: "CAROUSEL", Children: []string{"http://cdn/1.jpg", "https://cdn/2.jpg"}},
		"type inconnu":    {PageID: "page-a", MediaType: "STORY"},
	}
	for name, in := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.IGPublish(t.Context(), "tenant-a", in); err == nil {
				t.Fatal("entrée invalide acceptée")
			}
		})
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appels Graph = %+v", graph.calls)
	}

	// media_type defaults to IMAGE.
	out, err := svc.IGPublish(t.Context(), "tenant-a", IGPublishInput{
		PageID: "page-a", ImageURL: "https://cdn.test/a.jpg", Caption: "Salut",
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if !out.Preview || out.MediaType != domain.IGMediaTypeImage || out.IGUsername != "pagea" {
		t.Fatalf("aperçu = %+v", out)
	}

	out, err = svc.IGPublish(t.Context(), "tenant-a", IGPublishInput{
		PageID: "page-a", MediaType: "carousel",
		Children: []string{"https://cdn.test/1.jpg", " ", "https://cdn.test/2.jpg"},
		Confirm:  true,
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if out.Preview || out.MediaID != "ig-a_m" || len(out.Children) != 2 {
		t.Fatalf("résultat = %+v", out)
	}
}

func TestReconnectURLIsStoredAndSingleUse(t *testing.T) {
	svc, store, _, _ := newServiceHarness(t)

	link, err := svc.ReconnectURL(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ReconnectURL: %v", err)
	}
	const prefix = "https://mcp.example.re/meta/login?state="
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("lien = %q", link)
	}
	state := strings.TrimPrefix(link, prefix)
	parked, err := store.ConsumeLoginState(t.Context(), state)
	if err != nil {
		t.Fatalf("ConsumeLoginState: %v", err)
	}
	// A reconnection carries no MCP client: that is what tells the callback
	// to show a confirmation page instead of redirecting.
	if parked.Request.ClientID != "" {
		t.Fatalf("requête OAuth non vide: %+v", parked.Request)
	}
	if _, err := store.ConsumeLoginState(t.Context(), state); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("le lien est réutilisable: %v", err)
	}
}

func TestGraphErrorsPropagate(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)
	graph.err = &domain.GraphError{HTTPStatus: 400, Code: 190}

	calls := map[string]func() error{
		"page_insights": func() error {
			_, e := svc.PageInsights(t.Context(), "tenant-a", PageInsightsInput{PageID: "page-a"})
			return e
		},
		"page_posts": func() error {
			_, e := svc.PagePosts(t.Context(), "tenant-a", PagePostsInput{PageID: "page-a"})
			return e
		},
		"ig_media": func() error { _, e := svc.IGMedia(t.Context(), "tenant-a", IGMediaInput{PageID: "page-a"}); return e },
		"page_publish_post": func() error {
			_, e := svc.PublishPost(t.Context(), "tenant-a", PublishPostInput{PageID: "page-a", Message: "x", Confirm: true})
			return e
		},
		"ig_publish": func() error {
			_, e := svc.IGPublish(t.Context(), "tenant-a", IGPublishInput{PageID: "page-a", ImageURL: "https://cdn/a.jpg", Confirm: true})
			return e
		},
	}
	for name, run := range calls {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, graph.err) {
				t.Fatalf("erreur = %v", err)
			}
		})
	}
}

func TestStoreFailuresAreWrapped(t *testing.T) {
	svc, store, _, _ := newServiceHarness(t)
	boom := errors.New("disque plein")
	store.failOn("ListPages", boom)

	if _, err := svc.ListPages(t.Context(), "tenant-a"); !errors.Is(err, boom) {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := svc.PagePostComments(t.Context(), "tenant-a", PageCommentsInput{PostID: "42"}); !errors.Is(err, boom) {
		t.Fatalf("erreur = %v", err)
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct{ in, def, max, want int }{
		{0, 25, 100, 25},
		{-3, 25, 100, 25},
		{10, 25, 100, 10},
		{500, 25, 100, 100},
	}
	for _, tc := range cases {
		if got := clampLimit(tc.in, tc.def, tc.max); got != tc.want {
			t.Errorf("clampLimit(%d) = %d, attendu %d", tc.in, got, tc.want)
		}
	}
}
