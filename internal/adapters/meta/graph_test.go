package meta

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

var (
	testSince = time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	testUntil = time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
)

func TestPageInsights(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /page-1/insights", "page_insights.json", "")

	set, err := g.newTestClient().PageInsights(t.Context(), "PT", "page-1",
		[]string{"page_post_engagements", "page_follows"}, testSince, testUntil)
	if err != nil {
		t.Fatalf("PageInsights: %v", err)
	}
	if len(set.Rejected) != 0 {
		t.Fatalf("métriques refusées à tort: %v", set.Rejected)
	}
	insights := set.Insights
	if len(insights) != 2 {
		t.Fatalf("%d métriques", len(insights))
	}
	if insights[0].Metric != "page_post_engagements" || insights[0].Period != "day" {
		t.Fatalf("métrique = %+v", insights[0])
	}
	if len(insights[0].Values) != 2 {
		t.Fatalf("%d valeurs", len(insights[0].Values))
	}
	var value int64
	if err := json.Unmarshal(insights[0].Values[0].Value, &value); err != nil || value != 1234 {
		t.Fatalf("valeur = %s (err %v)", insights[0].Values[0].Value, err)
	}
	if insights[0].Values[0].EndTime != "2026-08-30T07:00:00+0000" {
		t.Fatalf("end_time = %q", insights[0].Values[0].EndTime)
	}

	q := g.calls("/page-1/insights")[0].Query
	if q.Get("metric") != "page_post_engagements,page_follows" {
		t.Fatalf("metric = %q", q.Get("metric"))
	}
	if q.Get("since") == "" || q.Get("until") == "" {
		t.Fatalf("fenêtre absente: %v", q)
	}
}

// TestPageInsightsRetriesMetricByMetric covers the case that motivated the
// fallback: Meta answers #100 for the whole batch because one name is
// deprecated, and the caller should still get the others.
func TestPageInsightsRetriesMetricByMetric(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /page-1/insights", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		metric := r.URL.Query().Get("metric")
		if strings.Contains(metric, ",") || metric == "page_impressions_unique" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"(#100) metric[0] must be a valid insights metric","type":"OAuthException","code":100}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"name":"` + metric + `","period":"day","values":[{"value":7,"end_time":"2026-08-30T07:00:00+0000"}]}]}`))
	})

	set, err := g.newTestClient().PageInsights(t.Context(), "PT", "page-1",
		[]string{"page_impressions_unique", "page_follows", "page_views_total"}, testSince, testUntil)
	if err != nil {
		t.Fatalf("PageInsights: %v", err)
	}
	if len(set.Insights) != 2 {
		t.Fatalf("métriques retenues = %+v", set.Insights)
	}
	if len(set.Rejected) != 1 || set.Rejected[0] != "page_impressions_unique" {
		t.Fatalf("rejected = %v", set.Rejected)
	}
	// One batch attempt plus one call per metric.
	if n := len(g.calls("/page-1/insights")); n != 4 {
		t.Fatalf("%d appels, attendu 4", n)
	}
}

// TestPageInsightsDoesNotSwallowAuthErrors makes sure the fallback only
// tolerates unsupported metrics, never an expired token.
func TestPageInsightsDoesNotSwallowAuthErrors(t *testing.T) {
	g := newFakeGraph(t)
	first := true
	g.handle("GET /page-1/insights", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if first {
			first = false
			_, _ = w.Write([]byte(`{"error":{"message":"(#100) bad metric","type":"OAuthException","code":100}}`))
			return
		}
		_, _ = w.Write([]byte(g.fixture("error_190.json")))
	})

	_, err := g.newTestClient().PageInsights(t.Context(), "PT", "page-1",
		[]string{"a", "b"}, testSince, testUntil)
	if err == nil {
		t.Fatal("une erreur d'autorisation a été avalée")
	}
	ge, ok := domain.AsGraphError(err)
	if !ok || !ge.IsAuth() {
		t.Fatalf("erreur = %v", err)
	}
}

func TestPagePostsFlattensInsights(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /page-1/posts", "page_posts.json", "")

	posts, err := g.newTestClient().PagePosts(t.Context(), "PT", "page-1", testSince, 25)
	if err != nil {
		t.Fatalf("PagePosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("%d publications", len(posts))
	}
	first := posts[0]
	if first.PostID != "page-1_111" || first.Message != "Nouvelle fournée" {
		t.Fatalf("publication = %+v", first)
	}
	if first.ImpressionsUnique != 540 || first.Clicks != 23 {
		t.Fatalf("statistiques = %+v", first)
	}
	// Reactions come back as an object keyed by type and must be summed.
	if first.Reactions != 37 {
		t.Fatalf("réactions = %d, attendu 37", first.Reactions)
	}
	if posts[1].Reactions != 0 || posts[1].ImpressionsUnique != 0 {
		t.Fatalf("publication sans insights = %+v", posts[1])
	}

	q := g.calls("/page-1/posts")[0].Query
	if q.Get("limit") != "25" || q.Get("since") == "" {
		t.Fatalf("paramètres = %v", q)
	}
}

func TestPostComments(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /111/comments", "page_comments.json", "")

	comments, err := g.newTestClient().PostComments(t.Context(), "PT", "111", 50)
	if err != nil {
		t.Fatalf("PostComments: %v", err)
	}
	if len(comments) != 2 || comments[0].From != "Alice" || comments[0].Message != "Miam" {
		t.Fatalf("commentaires = %+v", comments)
	}
	if comments[0].CreatedTime != "2026-08-30T10:00:00+0000" {
		t.Fatalf("date = %q", comments[0].CreatedTime)
	}
}

func TestIGAccountInsightsNormalizesTotalValue(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /ig-1/insights", "ig_insights.json", "")

	set, err := g.newTestClient().IGAccountInsights(t.Context(), "PT", "ig-1",
		[]string{"reach", "profile_views"}, testSince, testUntil)
	if err != nil {
		t.Fatalf("IGAccountInsights: %v", err)
	}
	if len(set.Insights) != 2 {
		t.Fatalf("%d métriques", len(set.Insights))
	}
	// Instagram returns total_value, which must land in Values like Facebook.
	if len(set.Insights[0].Values) != 1 {
		t.Fatalf("valeurs = %+v", set.Insights[0])
	}
	var value int64
	if err := json.Unmarshal(set.Insights[0].Values[0].Value, &value); err != nil || value != 4210 {
		t.Fatalf("valeur = %s (err %v)", set.Insights[0].Values[0].Value, err)
	}

	q := g.calls("/ig-1/insights")[0].Query
	if q.Get("metric_type") != "total_value" || q.Get("period") != "day" {
		t.Fatalf("paramètres = %v", q)
	}
}

// TestIGAccountInsightsSplitsFollowerCount pins the reason the client makes
// two requests: follower_count is refused under metric_type=total_value.
func TestIGAccountInsightsSplitsFollowerCount(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /ig-1/insights", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		metric := q.Get("metric")
		if strings.Contains(metric, "follower_count") && q.Get("metric_type") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"(#100) follower_count does not support metric_type=total_value","type":"OAuthException","code":100}}`))
			return
		}
		if metric == "follower_count" {
			_, _ = w.Write([]byte(`{"data":[{"name":"follower_count","period":"day","values":[{"value":38,"end_time":"2026-08-30T07:00:00+0000"}]}]}`))
			return
		}
		_, _ = w.Write([]byte(g.fixture("ig_insights.json")))
	})

	set, err := g.newTestClient().IGAccountInsights(t.Context(), "PT", "ig-1",
		[]string{"reach", "follower_count"}, testSince, testUntil)
	if err != nil {
		t.Fatalf("IGAccountInsights: %v", err)
	}
	if len(set.Rejected) != 0 {
		t.Fatalf("follower_count refusée alors qu'elle doit passer à part: %v", set.Rejected)
	}

	var sawFollowerAlone bool
	for _, call := range g.calls("/ig-1/insights") {
		if call.Query.Get("metric") == "follower_count" {
			sawFollowerAlone = true
			if call.Query.Get("metric_type") != "" {
				t.Fatalf("follower_count demandée avec metric_type: %v", call.Query)
			}
			if call.Query.Get("period") != "day" {
				t.Fatalf("follower_count sans period=day: %v", call.Query)
			}
		}
	}
	if !sawFollowerAlone {
		t.Fatal("follower_count n'a pas été demandée séparément")
	}
	if len(set.Insights) == 0 {
		t.Fatal("aucune métrique renvoyée")
	}
}

func TestIGFollowerDemographics(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /ig-1/insights", "ig_demographics.json", "")

	breakdowns, err := g.newTestClient().IGFollowerDemographics(t.Context(), "PT", "ig-1", "city")
	if err != nil {
		t.Fatalf("IGFollowerDemographics: %v", err)
	}
	if len(breakdowns) != 2 {
		t.Fatalf("%d entrées", len(breakdowns))
	}
	if breakdowns[0].Key != "Saint-Denis, Réunion" || breakdowns[0].Value != 812 {
		t.Fatalf("entrée = %+v", breakdowns[0])
	}

	q := g.calls("/ig-1/insights")[0].Query
	if q.Get("metric") != "follower_demographics" || q.Get("breakdown") != "city" ||
		q.Get("period") != "lifetime" {
		t.Fatalf("paramètres = %v", q)
	}
}

func TestIGMediaFlattensInsights(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /ig-1/media", "ig_media.json", "")

	media, err := g.newTestClient().IGMedia(t.Context(), "PT", "ig-1", testSince, 25)
	if err != nil {
		t.Fatalf("IGMedia: %v", err)
	}
	if len(media) != 1 {
		t.Fatalf("%d médias", len(media))
	}
	m := media[0]
	if m.MediaID != "media-1" || m.Type != "IMAGE" || m.ProductType != "FEED" {
		t.Fatalf("média = %+v", m)
	}
	if m.LikeCount != 210 || m.CommentsCount != 14 {
		t.Fatalf("compteurs = %+v", m)
	}
	if m.Reach != 3100 || m.Views != 4200 || m.Saved != 45 || m.Shares != 12 || m.TotalInteractions != 281 {
		t.Fatalf("statistiques = %+v", m)
	}
}

func TestIGMediaComments(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /media-1/comments", "ig_comments.json", "")

	comments, err := g.newTestClient().IGMediaComments(t.Context(), "PT", "media-1", 50)
	if err != nil {
		t.Fatalf("IGMediaComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("%d commentaires", len(comments))
	}
	// Instagram uses username/text/timestamp where Facebook uses from/message.
	if comments[0].Username != "bob974" || comments[0].Message != "Magnifique" {
		t.Fatalf("commentaire = %+v", comments[0])
	}
	if comments[0].CreatedTime != "2026-08-29T18:00:00+0000" {
		t.Fatalf("date = %q", comments[0].CreatedTime)
	}
}

func TestLimitCapsTheNumberOfItems(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /page-1/posts", "page_posts.json", "")

	posts, err := g.newTestClient().PagePosts(t.Context(), "PT", "page-1", testSince, 1)
	if err != nil {
		t.Fatalf("PagePosts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("%d publications, attendu 1", len(posts))
	}
}
