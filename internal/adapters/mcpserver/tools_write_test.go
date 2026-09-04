package mcpserver

import (
	"strings"
	"testing"

	"github.com/edouard-claude/meta-mcp/internal/app"
)

// TestWriteToolsNeverWriteWithoutConfirm is the acceptance criterion of the
// SPEC: no confirm, no call to Meta.
func TestWriteToolsNeverWriteWithoutConfirm(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"page_publish_post", map[string]any{"page_id": "page-a", "message": "Bonjour"}},
		{"page_reply_comment", map[string]any{"comment_id": "page-a_1", "message": "Merci"}},
		{"ig_publish", map[string]any{"page_id": "page-a", "image_url": "https://cdn.test/a.jpg"}},
		{"ig_reply_comment", map[string]any{"comment_id": "igc1", "page_id": "page-a", "message": "Merci"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			h := newServerHarness(t)
			session := h.connect(t, "token-a")

			payload, isErr := call(t, session, tc.tool, tc.args)
			if isErr {
				t.Fatalf("erreur: %s", payload)
			}
			if !strings.Contains(payload, `"preview":true`) {
				t.Fatalf("ce n'est pas un aperçu: %s", payload)
			}
			if strings.Contains(payload, `"post_id"`) || strings.Contains(payload, `"media_id"`) ||
				strings.Contains(payload, `"reply_id"`) {
				t.Fatalf("un identifiant a été renvoyé sans confirmation: %s", payload)
			}
			if len(h.graph.recorded()) != 0 {
				t.Fatalf("appels Graph effectués sans confirmation: %+v", h.graph.recorded())
			}
		})
	}
}

func TestPagePublishPostWithConfirm(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_publish_post", map[string]any{
		"page_id": "page-a", "message": "Nouvelle fournée", "confirm": true,
	})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	out := decodeJSON[app.PublishPostOutput](t, payload)
	if out.Preview || out.PostID != "page-a_published" || out.Kind != "feed" {
		t.Fatalf("résultat = %+v", out)
	}

	calls := h.graph.recorded()
	if len(calls) != 1 || calls[0].Method != "PublishPost" || calls[0].Token != "PT-A" {
		t.Fatalf("appels Graph = %+v", calls)
	}
}

func TestPagePublishPostPhotoAndSchedule(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_publish_post", map[string]any{
		"page_id":      "page-a",
		"message":      "Coucher de soleil",
		"photo_url":    "https://cdn.test/soleil.jpg",
		"scheduled_at": "2026-09-10T09:00:00Z",
	})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	out := decodeJSON[app.PublishPostOutput](t, payload)
	if out.Kind != "photo" || out.PhotoURL != "https://cdn.test/soleil.jpg" {
		t.Fatalf("aperçu = %+v", out)
	}
	if !strings.HasPrefix(out.ScheduledAt, "2026-09-10T09:00:00") {
		t.Fatalf("scheduled_at = %q", out.ScheduledAt)
	}
}

func TestPagePublishPostValidation(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	cases := map[string]struct {
		args   map[string]any
		expect string
	}{
		"contenu vide": {
			map[string]any{"page_id": "page-a"}, "message, link ou photo_url",
		},
		"lien et photo": {
			map[string]any{"page_id": "page-a", "link": "https://a.test", "photo_url": "https://b.test/x.jpg"},
			"s'excluent",
		},
		"lien non https": {
			map[string]any{"page_id": "page-a", "link": "http://a.test"}, "https",
		},
		"date invalide": {
			map[string]any{"page_id": "page-a", "message": "x", "scheduled_at": "demain"}, "ISO 8601",
		},
		"date trop proche": {
			map[string]any{"page_id": "page-a", "message": "x", "scheduled_at": "2026-09-04T12:05:00Z"},
			"10 minutes",
		},
		"date trop lointaine": {
			map[string]any{"page_id": "page-a", "message": "x", "scheduled_at": "2028-09-04T12:00:00Z"},
			"six mois",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			payload, isErr := call(t, session, "page_publish_post", tc.args)
			if !isErr {
				t.Fatalf("l'appel a réussi: %s", payload)
			}
			if !strings.Contains(payload, tc.expect) {
				t.Fatalf("message = %q, attendu contenant %q", payload, tc.expect)
			}
			if len(h.graph.recorded()) != 0 {
				t.Fatalf("appel Graph malgré une entrée invalide: %+v", h.graph.recorded())
			}
		})
	}
}

func TestIGPublishValidation(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	cases := map[string]struct {
		args   map[string]any
		expect string
	}{
		"image sans url": {
			map[string]any{"page_id": "page-a", "media_type": "IMAGE"}, "image_url",
		},
		"reels sans vidéo": {
			map[string]any{"page_id": "page-a", "media_type": "REELS"}, "video_url",
		},
		"carrousel trop court": {
			map[string]any{"page_id": "page-a", "media_type": "CAROUSEL",
				"children": []any{"https://cdn.test/1.jpg"}}, "entre 2 et 10",
		},
		"type inconnu": {
			map[string]any{"page_id": "page-a", "media_type": "STORY"}, "media_type doit valoir",
		},
		"url non https": {
			map[string]any{"page_id": "page-a", "image_url": "http://cdn.test/a.jpg"}, "https",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			payload, isErr := call(t, session, "ig_publish", tc.args)
			if !isErr {
				t.Fatalf("l'appel a réussi: %s", payload)
			}
			if !strings.Contains(payload, tc.expect) {
				t.Fatalf("message = %q, attendu contenant %q", payload, tc.expect)
			}
		})
	}
	if len(h.graph.recorded()) != 0 {
		t.Fatalf("appel Graph malgré une entrée invalide: %+v", h.graph.recorded())
	}
}

func TestIGPublishWithConfirm(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "ig_publish", map[string]any{
		"page_id": "page-a", "media_type": "CAROUSEL", "caption": "Série",
		"children": []any{"https://cdn.test/1.jpg", "https://cdn.test/2.jpg"},
		"confirm":  true,
	})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	out := decodeJSON[app.IGPublishOutput](t, payload)
	if out.Preview || out.MediaID != "ig-a_media" || out.MediaType != "CAROUSEL" {
		t.Fatalf("résultat = %+v", out)
	}
	calls := h.graph.recorded()
	if len(calls) != 1 || calls[0].Method != "IGPublish" || calls[0].Object != "ig-a" {
		t.Fatalf("appels Graph = %+v", calls)
	}
}

func TestReplyCommentWithConfirm(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_reply_comment", map[string]any{
		"comment_id": "page-a_1", "message": "Merci !", "confirm": true,
	})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	out := decodeJSON[app.ReplyCommentOutput](t, payload)
	if out.Preview || out.ReplyID != "page-a_1_reply" || out.PageID != "page-a" {
		t.Fatalf("résultat = %+v", out)
	}
}

func TestReplyCommentRejectsEmptyMessage(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_reply_comment", map[string]any{
		"comment_id": "page-a_1", "message": "   ", "confirm": true,
	})
	if !isErr || !strings.Contains(payload, "message est obligatoire") {
		t.Fatalf("réponse = %q (erreur=%v)", payload, isErr)
	}
	if len(h.graph.recorded()) != 0 {
		t.Fatalf("appel Graph malgré un message vide: %+v", h.graph.recorded())
	}
}

// TestWriteToolsRespectTenantIsolation checks the confirmation gate does not
// become a way around the tenant check.
func TestWriteToolsRespectTenantIsolation(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_publish_post", map[string]any{
		"page_id": "page-b", "message": "Intrusion", "confirm": true,
	})
	if !isErr {
		t.Fatalf("publication acceptée sur la page d'un autre tenant: %s", payload)
	}
	if len(h.graph.recorded()) != 0 {
		t.Fatalf("appel Graph vers un autre tenant: %+v", h.graph.recorded())
	}
}
