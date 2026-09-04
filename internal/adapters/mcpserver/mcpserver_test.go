package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/adapters/crypto"
	"github.com/edouard-claude/meta-mcp/internal/adapters/sqlite"
	"github.com/edouard-claude/meta-mcp/internal/app"
	"github.com/edouard-claude/meta-mcp/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tokens maps a bearer token to the tenant it authorizes, standing in for the
// real JWT verification.
var tokens = map[string]string{
	"token-a": "tenant-a",
	"token-b": "tenant-b",
}

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }

// bearerTransport adds the Authorization header to every client request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

type serverHarness struct {
	store *sqlite.Store
	graph *fakeGraph
	url   string
}

func newServerHarness(t *testing.T) *serverHarness {
	t.Helper()
	cipher, err := crypto.New(bytes.Repeat([]byte{11}, 32))
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	store, err := sqlite.New(t.Context(), filepath.Join(t.TempDir(), "mcp.db"), cipher)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	now := testClock{}.Now()
	seed := func(tenantID, metaUserID string, pages []domain.Page) {
		t.Helper()
		if err := store.UpsertTenant(t.Context(), &domain.Tenant{
			ID: tenantID, MetaUserID: metaUserID, DisplayName: tenantID,
			UserToken: "USER-" + tenantID, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("UpsertTenant: %v", err)
		}
		if err := store.ReplacePages(t.Context(), tenantID, pages); err != nil {
			t.Fatalf("ReplacePages: %v", err)
		}
	}
	seed("tenant-a", "meta-a", []domain.Page{{
		PageID: "page-a", Name: "Page A", PageToken: "PT-A",
		IGUserID: "ig-a", IGUsername: "pagea", SyncedAt: now,
	}})
	seed("tenant-b", "meta-b", []domain.Page{{
		PageID: "page-b", Name: "Page B", PageToken: "PT-B", SyncedAt: now,
	}})

	graph := &fakeGraph{}
	svc := app.NewService(store, graph, testClock{}, "https://mcp.example.re")
	handler := Handler(
		New(svc, slog.New(slog.DiscardHandler)),
		func(token string) (string, time.Time, error) {
			tenant, ok := tokens[token]
			if !ok {
				return "", time.Time{}, errors.New("jeton inconnu")
			}
			return tenant, time.Now().Add(time.Hour), nil
		},
		HandlerOptions{ResourceMetadataURL: "https://mcp.example.re/.well-known/oauth-protected-resource"},
		slog.New(slog.DiscardHandler),
	)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &serverHarness{store: store, graph: graph, url: srv.URL}
}

// connect opens an MCP session authenticated with the given bearer token.
func (h *serverHarness) connect(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:             h.url,
		HTTPClient:           &http.Client{Transport: &bearerTransport{token: token, base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connexion MCP: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// call runs a tool and returns its single text block.
func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("CallTool %s: aucun contenu", name)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool %s: contenu de type %T", name, res.Content[0])
	}
	return text.Text, res.IsError
}

func decodeJSON[T any](t *testing.T, payload string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("décodage %q: %v", payload, err)
	}
	return out
}

func TestToolsAreListed(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{
		"list_pages", "sync_pages", "page_insights", "page_insights_metadata",
		"page_posts", "page_post_comments", "ig_account_insights",
		"ig_follower_demographics", "ig_media", "ig_media_comments", "reconnect_url",
	} {
		if !names[want] {
			t.Errorf("outil manquant: %s", want)
		}
	}
}

func TestListPagesIsScopedToTheTenant(t *testing.T) {
	h := newServerHarness(t)

	payload, isErr := call(t, h.connect(t, "token-a"), "list_pages", nil)
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	pages := decodeJSON[[]app.PageView](t, payload)
	if len(pages) != 1 || pages[0].PageID != "page-a" {
		t.Fatalf("pages du tenant A = %+v", pages)
	}
	if strings.Contains(payload, "page-b") || strings.Contains(payload, "PT-") {
		t.Fatalf("fuite dans la réponse: %s", payload)
	}

	payload, _ = call(t, h.connect(t, "token-b"), "list_pages", nil)
	pages = decodeJSON[[]app.PageView](t, payload)
	if len(pages) != 1 || pages[0].PageID != "page-b" {
		t.Fatalf("pages du tenant B = %+v", pages)
	}
}

// TestTenantIsolation is the guarantee the whole design exists for: a token
// of tenant A must never reach a page of tenant B, even with its exact id.
func TestTenantIsolation(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	tools := []struct {
		name string
		args map[string]any
	}{
		{"page_insights", map[string]any{"page_id": "page-b"}},
		{"page_insights_metadata", map[string]any{"page_id": "page-b"}},
		{"page_posts", map[string]any{"page_id": "page-b"}},
		{"page_post_comments", map[string]any{"post_id": "page-b_1", "page_id": "page-b"}},
		{"ig_account_insights", map[string]any{"page_id": "page-b"}},
		{"ig_media", map[string]any{"page_id": "page-b"}},
		{"ig_follower_demographics", map[string]any{"page_id": "page-b", "breakdown": "city"}},
		{"ig_media_comments", map[string]any{"media_id": "m1", "page_id": "page-b"}},
	}
	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			payload, isErr := call(t, session, tc.name, tc.args)
			if !isErr {
				t.Fatalf("l'appel a réussi alors qu'il vise un autre tenant: %s", payload)
			}
			if !strings.Contains(payload, domain.ErrUnknownPage.Error()) {
				t.Fatalf("message = %q", payload)
			}
		})
	}

	// Not a single Graph call must have been made with tenant B's token.
	for _, c := range h.graph.recorded() {
		if c.Token == "PT-B" || c.Object == "page-b" {
			t.Fatalf("appel Graph vers le tenant B: %+v", c)
		}
	}
}

func TestPageToolsUseTheirOwnPageToken(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_insights", map[string]any{"page_id": "page-a"})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	insights := decodeJSON[[]domain.Insight](t, payload)
	if len(insights) != len(app.DefaultPageMetrics) {
		t.Fatalf("%d métriques, attendu %d", len(insights), len(app.DefaultPageMetrics))
	}
	if insights[0].Metric != app.DefaultPageMetrics[0] {
		t.Fatalf("métriques par défaut non appliquées: %+v", insights[0])
	}

	calls := h.graph.recorded()
	last := calls[len(calls)-1]
	if last.Method != "PageInsights" || last.Token != "PT-A" || last.Object != "page-a" {
		t.Fatalf("appel Graph = %+v", last)
	}
}

func TestInstagramToolsRequireALinkedAccount(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-b") // page-b has no Instagram account

	payload, isErr := call(t, session, "ig_account_insights", map[string]any{"page_id": "page-b"})
	if !isErr {
		t.Fatalf("l'appel a réussi: %s", payload)
	}
	if !strings.Contains(payload, "Instagram") {
		t.Fatalf("message = %q", payload)
	}
}

func TestInstagramToolsUseTheIGUserID(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "ig_media", map[string]any{"page_id": "page-a"})
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	calls := h.graph.recorded()
	last := calls[len(calls)-1]
	if last.Method != "IGMedia" || last.Token != "PT-A" || last.Object != "ig-a" {
		t.Fatalf("appel Graph = %+v", last)
	}
}

func TestDateWindowIsValidated(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_insights", map[string]any{
		"page_id": "page-a", "since": "hier",
	})
	if !isErr || !strings.Contains(payload, "AAAA-MM-JJ") {
		t.Fatalf("réponse = %q (erreur=%v)", payload, isErr)
	}

	payload, isErr = call(t, session, "page_insights", map[string]any{
		"page_id": "page-a", "since": "2026-09-01", "until": "2026-08-01",
	})
	if !isErr || !strings.Contains(payload, "antérieur") {
		t.Fatalf("réponse = %q (erreur=%v)", payload, isErr)
	}
}

func TestGraphAuthErrorBecomesReconnectMessage(t *testing.T) {
	h := newServerHarness(t)
	h.graph.err = &domain.GraphError{HTTPStatus: 400, Code: 190, Message: "expired"}
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "page_insights", map[string]any{"page_id": "page-a"})
	if !isErr {
		t.Fatalf("l'appel a réussi: %s", payload)
	}
	if !strings.Contains(payload, "reconnect_url") {
		t.Fatalf("message = %q", payload)
	}
}

func TestSyncPagesReplacesStoredPages(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "sync_pages", nil)
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	pages := decodeJSON[[]app.PageView](t, payload)
	if len(pages) != 1 || pages[0].PageID != "page-sync" {
		t.Fatalf("pages = %+v", pages)
	}
	stored, err := h.store.ListPages(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(stored) != 1 || stored[0].PageID != "page-sync" {
		t.Fatalf("pages stockées = %+v", stored)
	}
	// Tenant B is untouched.
	other, err := h.store.ListPages(t.Context(), "tenant-b")
	if err != nil || len(other) != 1 || other[0].PageID != "page-b" {
		t.Fatalf("pages du tenant B modifiées: %+v (err %v)", other, err)
	}
}

func TestReconnectURLIsSingleUseLink(t *testing.T) {
	h := newServerHarness(t)
	session := h.connect(t, "token-a")

	payload, isErr := call(t, session, "reconnect_url", nil)
	if isErr {
		t.Fatalf("erreur: %s", payload)
	}
	out := decodeJSON[map[string]string](t, payload)
	if !strings.HasPrefix(out["url"], "https://mcp.example.re/meta/login?state=") {
		t.Fatalf("url = %q", out["url"])
	}
	state := strings.TrimPrefix(out["url"], "https://mcp.example.re/meta/login?state=")
	if _, err := h.store.ConsumeLoginState(t.Context(), state); err != nil {
		t.Fatalf("le state n'a pas été enregistré: %v", err)
	}
}

func TestUnauthenticatedRequestIsRejected(t *testing.T) {
	h := newServerHarness(t)

	resp, err := http.Post(h.url, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, attendu 401", resp.StatusCode)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(challenge, "resource_metadata=") {
		t.Fatalf("WWW-Authenticate = %q", challenge)
	}
}

func TestUnknownTokenIsRejected(t *testing.T) {
	h := newServerHarness(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	_, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             h.url,
		HTTPClient:           &http.Client{Transport: &bearerTransport{token: "faux", base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}, nil)
	if err == nil {
		t.Fatal("un jeton inconnu a été accepté")
	}
}
