package mcpserver

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// allTools is the complete surface the server is expected to expose.
var allTools = []string{
	// read
	"list_pages", "sync_pages", "page_insights", "page_insights_metadata",
	"page_posts", "page_post_comments", "ig_account_insights",
	"ig_follower_demographics", "ig_media", "ig_media_comments", "reconnect_url",
	// detail and diagnostics
	"connection_status", "page_post_insights", "ig_media_insights",
	"ig_stories", "page_scheduled_posts",
	// write
	"page_publish_post", "page_reply_comment", "ig_publish", "ig_reply_comment",
	// moderation
	"page_moderate_comment", "ig_moderate_comment", "page_cancel_scheduled_post",
}

func TestEveryToolIsRegistered(t *testing.T) {
	h := newServerHarness(t)
	res, err := h.connect(t, "token-a").ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	for _, name := range allTools {
		tool, ok := got[name]
		if !ok {
			t.Errorf("outil manquant: %s", name)
			continue
		}
		if tool.Description == "" {
			t.Errorf("%s: description vide", name)
		}
	}
	if len(got) != len(allTools) {
		t.Errorf("%d outils exposés, %d attendus", len(got), len(allTools))
	}

	// The tools that destroy content must say so through their annotations,
	// which is what lets a client warn before running them.
	for _, name := range []string{"page_moderate_comment", "ig_moderate_comment", "page_cancel_scheduled_post"} {
		tool := got[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.DestructiveHint == nil ||
			!*tool.Annotations.DestructiveHint {
			t.Errorf("%s n'est pas annoté comme destructif", name)
		}
	}
}

func TestModerationToolsNeedConfirmation(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"page_moderate_comment", map[string]any{"comment_id": "page-a_1", "action": "hide"}},
		{"ig_moderate_comment", map[string]any{"comment_id": "igc1", "page_id": "page-a", "action": "delete"}},
		{"page_cancel_scheduled_post", map[string]any{"post_id": "page-a_prog"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			h := newServerHarness(t)
			payload, isErr := call(t, h.connect(t, "token-a"), tc.tool, tc.args)
			if isErr {
				t.Fatalf("erreur: %s", payload)
			}
			if !strings.Contains(payload, `"preview":true`) {
				t.Fatalf("ce n'est pas un aperçu: %s", payload)
			}
			if len(h.graph.recorded()) != 0 {
				t.Fatalf("appels Graph sans confirmation: %+v", h.graph.recorded())
			}
		})
	}
}

func TestModerationRespectsTenantIsolation(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_moderate_comment", map[string]any{
		"comment_id": "x", "page_id": "page-b", "action": "delete", "confirm": true,
	})
	if !isErr {
		t.Fatalf("modération acceptée sur la page d'un autre tenant: %s", payload)
	}
	if len(h.graph.recorded()) != 0 {
		t.Fatalf("appel Graph vers un autre tenant: %+v", h.graph.recorded())
	}
}

func TestResourcesAreTenantScoped(t *testing.T) {
	h := newServerHarness(t)

	res, err := h.connect(t, "token-a").ListResources(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res.Resources) != 2 {
		t.Fatalf("%d ressources", len(res.Resources))
	}

	read, err := h.connect(t, "token-a").ReadResource(t.Context(),
		&mcp.ReadResourceParams{URI: uriPages})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("contenus = %+v", read.Contents)
	}
	text := read.Contents[0].Text
	if !strings.Contains(text, "page-a") {
		t.Fatalf("ressource du tenant A = %s", text)
	}
	if strings.Contains(text, "page-b") || strings.Contains(text, "PT-") {
		t.Fatalf("fuite dans la ressource: %s", text)
	}

	// The same URI gives tenant B its own pages, never A's.
	read, err = h.connect(t, "token-b").ReadResource(t.Context(),
		&mcp.ReadResourceParams{URI: uriPages})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if !strings.Contains(read.Contents[0].Text, "page-b") ||
		strings.Contains(read.Contents[0].Text, "page-a") {
		t.Fatalf("ressource du tenant B = %s", read.Contents[0].Text)
	}
}

func TestConnectionResourceReportsStatus(t *testing.T) {
	h := newServerHarness(t)
	read, err := h.connect(t, "token-a").ReadResource(t.Context(),
		&mcp.ReadResourceParams{URI: uriConnection})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if !strings.Contains(read.Contents[0].Text, `"healthy"`) {
		t.Fatalf("contenu = %s", read.Contents[0].Text)
	}
}

func TestPromptsAreOffered(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	list, err := session.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	names := map[string]bool{}
	for _, p := range list.Prompts {
		names[p.Name] = true
	}
	for _, want := range []string{"bilan_mensuel", "revue_commentaires"} {
		if !names[want] {
			t.Errorf("prompt manquant: %s", want)
		}
	}

	got, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{
		Name:      "bilan_mensuel",
		Arguments: map[string]string{"page_id": "page-a", "mois": "2026-08"},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %+v", got.Messages)
	}
	text, ok := got.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("contenu de type %T", got.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "page-a") || !strings.Contains(text.Text, "2026-08") {
		t.Fatalf("le prompt n'a pas pris les arguments: %s", text.Text)
	}
	// A reporting prompt must not invite the model to publish anything.
	if !strings.Contains(text.Text, "Ne publie rien") {
		t.Fatalf("le prompt n'interdit pas l'écriture: %s", text.Text)
	}

	// Omitted arguments fall back rather than leaving an empty hole.
	got, err = session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "revue_commentaires"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	text = got.Messages[0].Content.(*mcp.TextContent)
	if !strings.Contains(text.Text, "list_pages") {
		t.Fatalf("prompt = %s", text.Text)
	}
}
