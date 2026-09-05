package meta

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// writeOK replies with an identifier, the way every Graph write endpoint does.
func writeOK(id string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	}
}

func TestPublishPostToFeed(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /page-1/feed", writeOK("page-1_999"))

	id, err := g.newTestClient().PublishPost(t.Context(), "PT", "page-1", domain.PublishPostRequest{
		Message: "Bonjour", Link: "https://example.re/actu",
	})
	if err != nil {
		t.Fatalf("PublishPost: %v", err)
	}
	if id != "page-1_999" {
		t.Fatalf("id = %q", id)
	}

	form := g.calls("/page-1/feed")[0].Form
	if form.Get("message") != "Bonjour" || form.Get("link") != "https://example.re/actu" {
		t.Fatalf("formulaire = %v", form)
	}
	if form.Get("published") != "" {
		t.Fatalf("published ne doit pas être envoyé pour une publication immédiate: %v", form)
	}
}

func TestPublishPostPhotoUsesPhotosEndpoint(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /page-1/photos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Photos answer with both ids; the post id is the useful one.
		_, _ = w.Write([]byte(`{"id":"photo-1","post_id":"page-1_777"}`))
	})

	id, err := g.newTestClient().PublishPost(t.Context(), "PT", "page-1", domain.PublishPostRequest{
		Message: "Légende", PhotoURL: "https://cdn.test/a.jpg",
	})
	if err != nil {
		t.Fatalf("PublishPost: %v", err)
	}
	if id != "page-1_777" {
		t.Fatalf("id = %q, le post_id doit primer", id)
	}

	form := g.calls("/page-1/photos")[0].Form
	if form.Get("url") != "https://cdn.test/a.jpg" || form.Get("caption") != "Légende" {
		t.Fatalf("formulaire = %v", form)
	}
}

func TestPublishPostScheduled(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /page-1/feed", writeOK("page-1_555"))

	when := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	if _, err := g.newTestClient().PublishPost(t.Context(), "PT", "page-1", domain.PublishPostRequest{
		Message: "Programmé", ScheduledAt: when,
	}); err != nil {
		t.Fatalf("PublishPost: %v", err)
	}

	form := g.calls("/page-1/feed")[0].Form
	if form.Get("published") != "false" {
		t.Fatalf("published = %q", form.Get("published"))
	}
	if form.Get("scheduled_publish_time") != strconv.FormatInt(when.Unix(), 10) {
		t.Fatalf("scheduled_publish_time = %q", form.Get("scheduled_publish_time"))
	}
}

func TestPublishPostWithoutIDIsAnError(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /page-1/feed", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := g.newTestClient().PublishPost(t.Context(), "PT", "page-1",
		domain.PublishPostRequest{Message: "x"}); err == nil {
		t.Fatal("une réponse sans identifiant a été acceptée")
	}
}

func TestReplyToComment(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /111_1/comments", writeOK("111_1_reply"))

	id, err := g.newTestClient().ReplyToComment(t.Context(), "PT", "111_1", "Merci !")
	if err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	if id != "111_1_reply" {
		t.Fatalf("id = %q", id)
	}
	if g.calls("/111_1/comments")[0].Form.Get("message") != "Merci !" {
		t.Fatalf("formulaire = %v", g.calls("/111_1/comments")[0].Form)
	}
}

func TestIGReplyToCommentUsesReplies(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /igc-1/replies", writeOK("igc-1-reply"))

	id, err := g.newTestClient().IGReplyToComment(t.Context(), "PT", "igc-1", "Merci")
	if err != nil {
		t.Fatalf("IGReplyToComment: %v", err)
	}
	if id != "igc-1-reply" {
		t.Fatalf("id = %q", id)
	}
}

func TestIGPublishImageWaitsForTheContainer(t *testing.T) {
	g := newFakeGraph(t)
	var polls atomic.Int32

	g.handle("POST /ig-1/media", writeOK("container-1"))
	g.handle("GET /container-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Meta processes asynchronously: the first poll is still running.
		if polls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"status_code":"IN_PROGRESS","status":"En cours"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status_code":"FINISHED","status":"Prêt"}`))
	})
	g.handle("POST /ig-1/media_publish", writeOK("media-42"))

	id, err := g.newTestClient().IGPublish(t.Context(), "PT", "ig-1", domain.IGPublishRequest{
		MediaType: domain.IGMediaTypeImage, ImageURL: "https://cdn.test/a.jpg", Caption: "Salut",
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if id != "media-42" {
		t.Fatalf("id = %q", id)
	}
	if polls.Load() != 2 {
		t.Fatalf("%d sondages, attendu 2", polls.Load())
	}

	form := g.calls("/ig-1/media")[0].Form
	if form.Get("image_url") != "https://cdn.test/a.jpg" || form.Get("caption") != "Salut" {
		t.Fatalf("formulaire = %v", form)
	}
	if g.calls("/ig-1/media_publish")[0].Form.Get("creation_id") != "container-1" {
		t.Fatalf("creation_id absent")
	}
}

func TestIGPublishReels(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /ig-1/media", writeOK("container-r"))
	g.handle("GET /container-r", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status_code":"FINISHED"}`))
	})
	g.handle("POST /ig-1/media_publish", writeOK("reel-1"))

	if _, err := g.newTestClient().IGPublish(t.Context(), "PT", "ig-1", domain.IGPublishRequest{
		MediaType: domain.IGMediaTypeReels, VideoURL: "https://cdn.test/v.mp4",
	}); err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	form := g.calls("/ig-1/media")[0].Form
	if form.Get("media_type") != "REELS" || form.Get("video_url") != "https://cdn.test/v.mp4" {
		t.Fatalf("formulaire = %v", form)
	}
}

func TestIGPublishCarouselCreatesChildContainers(t *testing.T) {
	g := newFakeGraph(t)
	var created atomic.Int32
	g.handle("POST /ig-1/media", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := created.Add(1)
		_, _ = w.Write([]byte(`{"id":"container-` + strconv.Itoa(int(n)) + `"}`))
	})
	g.handle("GET /container-3", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status_code":"FINISHED"}`))
	})
	g.handle("POST /ig-1/media_publish", writeOK("carousel-1"))

	id, err := g.newTestClient().IGPublish(t.Context(), "PT", "ig-1", domain.IGPublishRequest{
		MediaType: domain.IGMediaTypeCarousel,
		Children:  []string{"https://cdn.test/1.jpg", "https://cdn.test/2.jpg"},
		Caption:   "Série",
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if id != "carousel-1" {
		t.Fatalf("id = %q", id)
	}

	calls := g.calls("/ig-1/media")
	if len(calls) != 3 {
		t.Fatalf("%d conteneurs créés, attendu 3 (2 enfants + 1 carrousel)", len(calls))
	}
	if calls[0].Form.Get("is_carousel_item") != "true" {
		t.Fatalf("premier enfant = %v", calls[0].Form)
	}
	parent := calls[2].Form
	if parent.Get("media_type") != "CAROUSEL" || parent.Get("children") != "container-1,container-2" {
		t.Fatalf("conteneur parent = %v", parent)
	}
}

func TestIGPublishSurfacesContainerError(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /ig-1/media", writeOK("container-e"))
	g.handle("GET /container-e", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status_code":"ERROR","status":"Format non supporté"}`))
	})

	_, err := g.newTestClient().IGPublish(t.Context(), "PT", "ig-1", domain.IGPublishRequest{
		MediaType: domain.IGMediaTypeImage, ImageURL: "https://cdn.test/a.jpg",
	})
	if err == nil {
		t.Fatal("aucune erreur alors que Meta a rejeté le média")
	}
	if !strings.Contains(err.Error(), "Format non supporté") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestScheduledPostsNormalizesTheDate(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /page-1/scheduled_posts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Meta returns the date as a unix timestamp on this edge.
		_, _ = w.Write([]byte(`{"data":[
			{"id":"page-1_1","message":"Bientôt","created_time":"2026-09-01T10:00:00+0000","scheduled_publish_time":1789030800},
			{"id":"page-1_2","message":"Plus tard","scheduled_publish_time":"2026-09-12T09:00:00+0000"}
		]}`))
	})

	posts, err := g.newTestClient().ScheduledPosts(t.Context(), "PT", "page-1", 25)
	if err != nil {
		t.Fatalf("ScheduledPosts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("%d publications", len(posts))
	}
	if posts[0].PostID != "page-1_1" || posts[0].Message != "Bientôt" {
		t.Fatalf("publication = %+v", posts[0])
	}
	if !strings.HasPrefix(posts[0].ScheduledAt, "2026-") {
		t.Fatalf("date normalisée = %q", posts[0].ScheduledAt)
	}
	if posts[1].ScheduledAt != "2026-09-12T09:00:00+0000" {
		t.Fatalf("date ISO non conservée: %q", posts[1].ScheduledAt)
	}
}

func TestSetCommentHiddenUsesThePlatformParameter(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /c-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	})
	c := g.newTestClient()

	if err := c.SetCommentHidden(t.Context(), "PT", "c-1", true, false); err != nil {
		t.Fatalf("SetCommentHidden facebook: %v", err)
	}
	if got := g.calls("/c-1")[0].Form.Get("is_hidden"); got != "true" {
		t.Fatalf("Facebook attend is_hidden, reçu %v", g.calls("/c-1")[0].Form)
	}

	if err := c.SetCommentHidden(t.Context(), "PT", "c-1", true, true); err != nil {
		t.Fatalf("SetCommentHidden instagram: %v", err)
	}
	form := g.calls("/c-1")[1].Form
	if form.Get("hide") != "true" || form.Get("is_hidden") != "" {
		t.Fatalf("Instagram attend hide, reçu %v", form)
	}
}

func TestDeleteObjectUsesTheMethodOverride(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /c-9", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	})

	if err := g.newTestClient().DeleteObject(t.Context(), "PT", "c-9"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if got := g.calls("/c-9")[0].Form.Get("method"); got != "delete" {
		t.Fatalf("method = %q", got)
	}
}

func TestDeleteObjectSurfacesGraphErrors(t *testing.T) {
	g := newFakeGraph(t)
	g.fail("POST /c-9", "error_190.json", http.StatusBadRequest, nil)

	err := g.newTestClient().DeleteObject(t.Context(), "PT", "c-9")
	if err == nil {
		t.Fatal("aucune erreur")
	}
	if ge, ok := domain.AsGraphError(err); !ok || !ge.IsAuth() {
		t.Fatalf("erreur = %v", err)
	}
}

func TestIGStoriesAsksForNarrowFields(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /ig-1/stories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"s-1","media_type":"IMAGE","media_product_type":"STORY","timestamp":"2026-09-05T08:00:00+0000","permalink":"https://instagram.test/s/1"}]}`))
	})

	stories, err := g.newTestClient().IGStories(t.Context(), "PT", "ig-1")
	if err != nil {
		t.Fatalf("IGStories: %v", err)
	}
	if len(stories) != 1 || stories[0].MediaID != "s-1" || stories[0].ProductType != "STORY" {
		t.Fatalf("stories = %+v", stories)
	}
	// like_count and comments_count do not exist on a story, and asking for
	// them would fail the whole request.
	fields := g.calls("/ig-1/stories")[0].Query.Get("fields")
	if strings.Contains(fields, "like_count") || strings.Contains(fields, "insights") {
		t.Fatalf("champs demandés = %q", fields)
	}
}

func TestIGPublishStory(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("POST /ig-1/media", writeOK("container-s"))
	g.handle("GET /container-s", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status_code":"FINISHED"}`))
	})
	g.handle("POST /ig-1/media_publish", writeOK("story-42"))

	id, err := g.newTestClient().IGPublish(t.Context(), "PT", "ig-1", domain.IGPublishRequest{
		MediaType: domain.IGMediaTypeStories, ImageURL: "https://cdn.test/s.jpg",
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if id != "story-42" {
		t.Fatalf("id = %q", id)
	}
	form := g.calls("/ig-1/media")[0].Form
	if form.Get("media_type") != "STORIES" || form.Get("image_url") != "https://cdn.test/s.jpg" {
		t.Fatalf("formulaire = %v", form)
	}
	if form.Get("caption") != "" {
		t.Fatalf("une story ne porte pas de légende: %v", form)
	}
}
