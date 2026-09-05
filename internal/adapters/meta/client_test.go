package meta

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

func TestAuthorizeURL(t *testing.T) {
	g := newFakeGraph(t)
	c := g.newTestClient()

	raw := c.AuthorizeURL("https://mcp.example.re/meta/callback", "state-123")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL illisible: %v", err)
	}
	if !strings.HasSuffix(u.Path, "/"+testVersion+"/dialog/oauth") {
		t.Fatalf("chemin du dialogue = %s", u.Path)
	}
	q := u.Query()
	if q.Get("client_id") != "app-id" || q.Get("state") != "state-123" ||
		q.Get("response_type") != "code" || q.Get("scope") != "pages_show_list" {
		t.Fatalf("paramètres du dialogue: %v", q)
	}
	if q.Get("redirect_uri") != "https://mcp.example.re/meta/callback" {
		t.Fatalf("redirect_uri = %s", q.Get("redirect_uri"))
	}
}

func TestExchangeCodeAndLongLivedToken(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("grant_type") == "fb_exchange_token" {
			_, _ = w.Write([]byte(`{"access_token":"LONG-TOKEN","token_type":"bearer","expires_in":5183944}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"SHORT-TOKEN","token_type":"bearer","expires_in":3600}`))
	})
	c := g.newTestClient()

	short, err := c.ExchangeCode(t.Context(), "the-code", "https://mcp.example.re/meta/callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if short != "SHORT-TOKEN" {
		t.Fatalf("jeton court = %q", short)
	}

	long, err := c.ExchangeLongLivedToken(t.Context(), short)
	if err != nil {
		t.Fatalf("ExchangeLongLivedToken: %v", err)
	}
	if long.Token != "LONG-TOKEN" {
		t.Fatalf("jeton long = %q", long.Token)
	}
	// The lifetime is what lets the server renew before the token dies.
	if long.ExpiresIn != 5183944*time.Second {
		t.Fatalf("durée de vie = %v", long.ExpiresIn)
	}

	calls := g.calls("/oauth/access_token")
	if len(calls) != 2 {
		t.Fatalf("%d appels, attendu 2", len(calls))
	}
	if calls[0].Query.Get("code") != "the-code" || calls[0].Query.Get("client_secret") != "app-secret" {
		t.Fatalf("premier appel: %v", calls[0].Query)
	}
	if calls[1].Query.Get("fb_exchange_token") != "SHORT-TOKEN" {
		t.Fatalf("second appel: %v", calls[1].Query)
	}
	// The token exchange has no access token yet, so no proof must be sent.
	if calls[0].Query.Get("appsecret_proof") != "" {
		t.Fatal("appsecret_proof envoyé sans jeton d'accès")
	}
}

func TestExchangeCodeRejectsEmptyToken(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := g.newTestClient().ExchangeCode(t.Context(), "c", "u"); err == nil {
		t.Fatal("une réponse sans access_token a été acceptée")
	}
}

func TestMe(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /me", "me.json", "")
	user, err := g.newTestClient().Me(t.Context(), "USER-TOKEN")
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if user.ID != "9876543210" || user.Name != "Édouard Test" {
		t.Fatalf("profil = %+v", user)
	}

	calls := g.calls("/me")
	if calls[0].Query.Get("access_token") != "USER-TOKEN" {
		t.Fatal("le jeton n'a pas été transmis")
	}
	if calls[0].Query.Get("appsecret_proof") == "" {
		t.Fatal("appsecret_proof absent d'un appel authentifié")
	}
	if calls[0].Query.Get("fields") != "id,name" {
		t.Fatalf("fields = %s", calls[0].Query.Get("fields"))
	}
}

func TestAccountsFollowsPagination(t *testing.T) {
	g := newFakeGraph(t)
	g.json("GET /me/accounts", "accounts_page1.json", "/"+testVersion+"/me/accounts/page2")
	g.json("GET /me/accounts/page2", "accounts_page2.json", "")

	pages, err := g.newTestClient().Accounts(t.Context(), "USER-TOKEN")
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	// Four items are returned across two pages, but the one without a page
	// token is unusable and must be dropped.
	if len(pages) != 3 {
		t.Fatalf("%d pages, attendu 3: %+v", len(pages), pages)
	}
	if pages[0].PageID != "page-1" || pages[0].IGUserID != "ig-1" ||
		pages[0].IGUsername != "boulangerieduport" || pages[0].PageToken != "PAGE-TOKEN-1" {
		t.Fatalf("page 1 = %+v", pages[0])
	}
	if pages[1].HasInstagram() {
		t.Fatalf("page 2 ne devrait pas avoir d'Instagram: %+v", pages[1])
	}
	if pages[2].PageID != "page-3" || pages[2].Name != "Garage des Hauts" {
		t.Fatalf("page 3 = %+v", pages[2])
	}
	for _, p := range pages {
		if p.SyncedAt.IsZero() {
			t.Fatalf("synced_at non renseigné: %+v", p)
		}
	}
}

func TestGraphErrorAuthIsDecoded(t *testing.T) {
	g := newFakeGraph(t)
	g.fail("GET /me", "error_190.json", http.StatusBadRequest, nil)

	_, err := g.newTestClient().Me(t.Context(), "EXPIRED")
	if err == nil {
		t.Fatal("aucune erreur")
	}
	ge, ok := domain.AsGraphError(err)
	if !ok {
		t.Fatalf("erreur non typée: %v", err)
	}
	if ge.Code != 190 || ge.Subcode != 463 || ge.Type != "OAuthException" {
		t.Fatalf("erreur décodée: %+v", ge)
	}
	if !ge.IsAuth() || ge.IsRateLimit() {
		t.Fatalf("classification: auth=%v rate=%v", ge.IsAuth(), ge.IsRateLimit())
	}
	if ge.UserMessage() != domain.ErrReauthorize.Error() {
		t.Fatalf("message utilisateur = %q", ge.UserMessage())
	}
}

func TestGraphErrorPermissionIsAuth(t *testing.T) {
	g := newFakeGraph(t)
	g.fail("GET /me", "error_10.json", http.StatusForbidden, nil)

	_, err := g.newTestClient().Me(t.Context(), "TOKEN")
	ge, ok := domain.AsGraphError(err)
	if !ok {
		t.Fatalf("erreur non typée: %v", err)
	}
	if !ge.IsAuth() {
		t.Fatalf("le code 10 doit remonter comme une autorisation manquante: %+v", ge)
	}
}

func TestRateLimitRetriesExactlyOnce(t *testing.T) {
	g := newFakeGraph(t)
	attempts := 0
	g.handle("GET /me", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(g.fixture("error_4.json")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(g.fixture("me.json")))
	})

	user, err := g.newTestClient().Me(t.Context(), "TOKEN")
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if user.ID != "9876543210" {
		t.Fatalf("profil = %+v", user)
	}
	if attempts != 2 {
		t.Fatalf("%d tentatives, attendu 2", attempts)
	}
}

func TestRateLimitGivesUpAfterOneRetry(t *testing.T) {
	g := newFakeGraph(t)
	attempts := 0
	g.handle("GET /me", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(g.fixture("error_4.json")))
	})

	_, err := g.newTestClient().Me(t.Context(), "TOKEN")
	if err == nil {
		t.Fatal("aucune erreur après deux quotas")
	}
	if attempts != 2 {
		t.Fatalf("%d tentatives, attendu 2 (une seule relance)", attempts)
	}
	ge, ok := domain.AsGraphError(err)
	if !ok || !ge.IsRateLimit() {
		t.Fatalf("erreur = %v", err)
	}
	if !strings.Contains(ge.UserMessage(), "quota") {
		t.Fatalf("message utilisateur = %q", ge.UserMessage())
	}
}

func TestNonJSONErrorIsWrapped(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	})
	_, err := g.newTestClient().Me(t.Context(), "TOKEN")
	var ge *domain.GraphError
	if !errors.As(err, &ge) {
		t.Fatalf("erreur = %v", err)
	}
	if ge.HTTPStatus != http.StatusBadGateway || !strings.Contains(ge.Message, "502") {
		t.Fatalf("erreur = %+v", ge)
	}
}

func TestDebugToken(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /debug_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"app_id":"app-id","type":"USER","is_valid":true,
			"expires_at":1793318400,"data_access_expires_at":1801094400,
			"scopes":["pages_show_list","instagram_basic"],"user_id":"9876543210"}}`))
	})

	status, err := g.newTestClient().DebugToken(t.Context(), "USER-TOKEN")
	if err != nil {
		t.Fatalf("DebugToken: %v", err)
	}
	if !status.Valid || len(status.Scopes) != 2 {
		t.Fatalf("statut = %+v", status)
	}
	if status.ExpiresAt.Unix() != 1793318400 || status.DataAccessExpiresAt.Unix() != 1801094400 {
		t.Fatalf("échéances = %v / %v", status.ExpiresAt, status.DataAccessExpiresAt)
	}

	// The call authenticates with the app token, not with the token under
	// inspection, so the inspected one must travel as input_token only.
	q := g.calls("/debug_token")[0].Query
	if q.Get("input_token") != "USER-TOKEN" {
		t.Fatalf("input_token = %q", q.Get("input_token"))
	}
	if q.Get("access_token") != "app-id|app-secret" {
		t.Fatalf("access_token = %q", q.Get("access_token"))
	}
}

func TestDebugTokenReportsInvalid(t *testing.T) {
	g := newFakeGraph(t)
	g.handle("GET /debug_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"is_valid":false,"error":{"code":190,"message":"Session revoked"}}}`))
	})

	status, err := g.newTestClient().DebugToken(t.Context(), "REVOKED")
	if err != nil {
		t.Fatalf("DebugToken: %v", err)
	}
	if status.Valid || !strings.Contains(status.Reason, "revoked") {
		t.Fatalf("statut = %+v", status)
	}
	if !status.ExpiresAt.IsZero() {
		t.Fatalf("expiration = %v, attendue absente", status.ExpiresAt)
	}
}
