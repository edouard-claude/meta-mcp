package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

func TestModerationPreviewTouchesNothing(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	for _, action := range AllowedModerationActions {
		out, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
			CommentID: "page-a_1", Action: action,
		})
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if !out.Preview || out.Done {
			t.Fatalf("%s: aperçu = %+v", action, out)
		}
		if action == domain.ModerationDelete && !strings.Contains(out.Notice, "irréversible") {
			t.Fatalf("la suppression ne prévient pas de son caractère définitif: %q", out.Notice)
		}
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appels Graph sans confirmation: %+v", graph.calls)
	}
}

func TestModerationHideAndUnhideUseTheRightFlag(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "page-a_1", Action: domain.ModerationHide, Confirm: true,
	}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	if graph.last().Method != "SetCommentHidden" || !graph.lastHidden || graph.lastInstagram {
		t.Fatalf("appel = %+v hidden=%v ig=%v", graph.last(), graph.lastHidden, graph.lastInstagram)
	}

	if _, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "page-a_1", Action: domain.ModerationUnhide, Confirm: true,
	}); err != nil {
		t.Fatalf("unhide: %v", err)
	}
	if graph.lastHidden {
		t.Fatal("unhide a masqué le commentaire")
	}

	// The Instagram variant must flag itself, because Meta spells the
	// parameter differently there.
	if _, err := svc.IGModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "igc-1", Action: domain.ModerationHide, Confirm: true,
	}); err != nil {
		t.Fatalf("ig hide: %v", err)
	}
	if !graph.lastInstagram {
		t.Fatal("la variante Instagram n'a pas été signalée au client Graph")
	}
}

func TestModerationDeleteGoesThroughDeleteObject(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	out, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "page-a_1", Action: domain.ModerationDelete, Confirm: true,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !out.Done || out.Preview {
		t.Fatalf("résultat = %+v", out)
	}
	if graph.last().Method != "DeleteObject" || graph.last().Object != "page-a_1" {
		t.Fatalf("appel = %+v", graph.last())
	}
}

func TestModerationValidation(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "page-a_1", Action: "burn",
	}); err == nil {
		t.Fatal("une action inconnue a été acceptée")
	}
	if _, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "  ", Action: domain.ModerationHide, PageID: "page-a",
	}); err == nil {
		t.Fatal("un comment_id vide a été accepté")
	}
	// Another tenant's page stays out of reach, confirm or not.
	if _, err := svc.ModerateComment(t.Context(), "tenant-a", ModerateCommentInput{
		CommentID: "x", PageID: "page-b", Action: domain.ModerationDelete, Confirm: true,
	}); !errors.Is(err, domain.ErrUnknownPage) {
		t.Fatalf("erreur = %v", err)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appels Graph malgré une entrée invalide: %+v", graph.calls)
	}
}

func TestScheduledPostsAndCancel(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	posts, err := svc.PageScheduledPosts(t.Context(), "tenant-a", ScheduledPostsInput{PageID: "page-a"})
	if err != nil {
		t.Fatalf("PageScheduledPosts: %v", err)
	}
	if len(posts) != 1 || posts[0].ScheduledAt == "" {
		t.Fatalf("publications = %+v", posts)
	}
	if graph.last().Limit != defaultScheduledLimit {
		t.Fatalf("limite = %d", graph.last().Limit)
	}

	out, err := svc.PageCancelScheduledPost(t.Context(), "tenant-a",
		CancelScheduledPostInput{PostID: "page-a_prog"})
	if err != nil {
		t.Fatalf("PageCancelScheduledPost: %v", err)
	}
	if !out.Preview || out.Canceled {
		t.Fatalf("aperçu = %+v", out)
	}
	if !strings.Contains(out.Notice, "définitive") {
		t.Fatalf("l'aperçu ne prévient pas: %q", out.Notice)
	}

	before := len(graph.calls)
	out, err = svc.PageCancelScheduledPost(t.Context(), "tenant-a",
		CancelScheduledPostInput{PostID: "page-a_prog", Confirm: true})
	if err != nil {
		t.Fatalf("PageCancelScheduledPost: %v", err)
	}
	if !out.Canceled || len(graph.calls) != before+1 || graph.last().Method != "DeleteObject" {
		t.Fatalf("résultat = %+v, appel = %+v", out, graph.last())
	}

	if _, err := svc.PageCancelScheduledPost(t.Context(), "tenant-a",
		CancelScheduledPostInput{PostID: ""}); err == nil {
		t.Fatal("un post_id vide a été accepté")
	}
}

func TestObjectInsightsAndStories(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	if _, err := svc.PagePostInsights(t.Context(), "tenant-a",
		ObjectInsightsInput{ObjectID: "page-a_1"}); err != nil {
		t.Fatalf("PagePostInsights: %v", err)
	}
	if graph.last().Token != "PT-A" || len(graph.last().Metrics) != len(DefaultPostMetrics) {
		t.Fatalf("appel = %+v", graph.last())
	}

	if _, err := svc.IGMediaInsights(t.Context(), "tenant-a",
		ObjectInsightsInput{ObjectID: "m1"}); err != nil {
		t.Fatalf("IGMediaInsights: %v", err)
	}
	if graph.last().Method != "IGMediaInsights" || graph.last().Object != "m1" {
		t.Fatalf("appel = %+v", graph.last())
	}

	stories, err := svc.IGStories(t.Context(), "tenant-a", "page-a")
	if err != nil {
		t.Fatalf("IGStories: %v", err)
	}
	if len(stories) != 1 || graph.last().Object != "ig-a" {
		t.Fatalf("stories = %+v, appel = %+v", stories, graph.last())
	}

	// Instagram tools stay unavailable without a linked account.
	if _, err := svc.IGStories(t.Context(), "tenant-b", "page-b"); !errors.Is(err, domain.ErrNoInstagram) {
		t.Fatalf("erreur = %v", err)
	}
	if _, err := svc.PagePostInsights(t.Context(), "tenant-a",
		ObjectInsightsInput{ObjectID: ""}); err == nil {
		t.Fatal("un post_id vide a été accepté")
	}
}

func TestIGPublishAcceptsStories(t *testing.T) {
	svc, _, _, _ := newServiceHarness(t)

	out, err := svc.IGPublish(t.Context(), "tenant-a", IGPublishInput{
		PageID: "page-a", MediaType: "stories", ImageURL: "https://cdn.test/s.jpg",
		Caption: "ignorée", Confirm: true,
	})
	if err != nil {
		t.Fatalf("IGPublish: %v", err)
	}
	if out.MediaType != domain.IGMediaTypeStories {
		t.Fatalf("media_type = %s", out.MediaType)
	}
	// A story carries no caption, so it must not be echoed back either.
	if out.Caption != "" {
		t.Fatalf("légende = %q, attendue vide", out.Caption)
	}

	for name, in := range map[string]IGPublishInput{
		"sans média": {PageID: "page-a", MediaType: "STORIES"},
		"les deux": {PageID: "page-a", MediaType: "STORIES",
			ImageURL: "https://cdn.test/a.jpg", VideoURL: "https://cdn.test/v.mp4"},
		"non https": {PageID: "page-a", MediaType: "STORIES", ImageURL: "http://cdn.test/a.jpg"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.IGPublish(t.Context(), "tenant-a", in); err == nil {
				t.Fatal("entrée invalide acceptée")
			}
		})
	}
}

func TestDeletePost(t *testing.T) {
	svc, _, graph, _ := newServiceHarness(t)

	out, err := svc.PageDeletePost(t.Context(), "tenant-a", DeletePostInput{PostID: "page-a_1"})
	if err != nil {
		t.Fatalf("PageDeletePost: %v", err)
	}
	if !out.Preview || out.Deleted {
		t.Fatalf("aperçu = %+v", out)
	}
	if !strings.Contains(out.Notice, "définitive") || !strings.Contains(out.Notice, "commentaires") {
		t.Fatalf("l'aperçu ne dit pas ce qui disparaît: %q", out.Notice)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("appel Graph sans confirmation: %+v", graph.calls)
	}

	out, err = svc.PageDeletePost(t.Context(), "tenant-a",
		DeletePostInput{PostID: "page-a_1", Confirm: true})
	if err != nil {
		t.Fatalf("PageDeletePost: %v", err)
	}
	if !out.Deleted || graph.last().Method != "DeleteObject" || graph.last().Token != "PT-A" {
		t.Fatalf("résultat = %+v, appel = %+v", out, graph.last())
	}

	if _, err := svc.PageDeletePost(t.Context(), "tenant-a", DeletePostInput{PostID: " "}); err == nil {
		t.Fatal("un post_id vide a été accepté")
	}
	// And a post on another tenant's page stays out of reach.
	if _, err := svc.PageDeletePost(t.Context(), "tenant-a",
		DeletePostInput{PostID: "x", PageID: "page-b", Confirm: true}); !errors.Is(err, domain.ErrUnknownPage) {
		t.Fatalf("erreur = %v", err)
	}
}
