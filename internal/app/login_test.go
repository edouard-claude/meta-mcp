package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// fakeMetaOAuth is a scriptable domain.MetaOAuthClient.
type fakeMetaOAuth struct {
	shortToken string
	longToken  string
	longTTL    time.Duration
	user       domain.MetaUser
	pages      []domain.Page
	status     domain.TokenStatus

	exchangeErr error
	longErr     error
	meErr       error
	accountsErr error
	statusErr   error

	seenCode        string
	seenRedirectURI string
	seenUserToken   string
}

var _ domain.MetaOAuthClient = (*fakeMetaOAuth)(nil)

func newFakeMeta() *fakeMetaOAuth {
	return &fakeMetaOAuth{
		shortToken: "SHORT",
		longToken:  "LONG",
		longTTL:    60 * 24 * time.Hour,
		status:     domain.TokenStatus{Valid: true, Scopes: []string{"pages_show_list"}},
		user:       domain.MetaUser{ID: "meta-1", Name: "Édouard"},
		pages: []domain.Page{
			{PageID: "page-1", Name: "Page 1", PageToken: "PT1", IGUserID: "ig-1", IGUsername: "un"},
			{PageID: "page-2", Name: "Page 2", PageToken: "PT2"},
		},
	}
}

func (f *fakeMetaOAuth) AuthorizeURL(redirectURI, state string) string {
	return "https://facebook.test/dialog?redirect_uri=" + redirectURI + "&state=" + state
}

func (f *fakeMetaOAuth) ExchangeCode(_ context.Context, code, redirectURI string) (string, error) {
	f.seenCode, f.seenRedirectURI = code, redirectURI
	return f.shortToken, f.exchangeErr
}

func (f *fakeMetaOAuth) ExchangeLongLivedToken(_ context.Context, token string) (domain.LongLivedToken, error) {
	if f.longErr != nil {
		return domain.LongLivedToken{}, f.longErr
	}
	// Meta accepts both a short-lived token and a still valid long-lived one.
	if token != f.shortToken && token != f.longToken {
		return domain.LongLivedToken{}, errors.New("jeton inattendu")
	}
	return domain.LongLivedToken{Token: f.longToken, ExpiresIn: f.longTTL}, nil
}

func (f *fakeMetaOAuth) DebugToken(context.Context, string) (domain.TokenStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeMetaOAuth) Me(_ context.Context, userToken string) (domain.MetaUser, error) {
	f.seenUserToken = userToken
	return f.user, f.meErr
}

func (f *fakeMetaOAuth) Accounts(_ context.Context, userToken string) ([]domain.Page, error) {
	f.seenUserToken = userToken
	return f.pages, f.accountsErr
}

func newLoginHarness(t *testing.T, allow func(string) bool) (*LoginService, *fakeStore, *fakeMetaOAuth, *fakeClock) {
	t.Helper()
	store, meta, clk := newFakeStore(), newFakeMeta(), newFakeClock()
	return NewLoginService(store, meta, clk, allow), store, meta, clk
}

func TestCompleteCreatesTenantAndPages(t *testing.T) {
	svc, store, meta, _ := newLoginHarness(t, nil)

	result, err := svc.Complete(t.Context(), "the-code", "https://mcp.example.re/meta/callback")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.DisplayName != "Édouard" || result.Pages != 2 || result.TenantID == "" {
		t.Fatalf("résultat = %+v", result)
	}
	if meta.seenCode != "the-code" || meta.seenRedirectURI != "https://mcp.example.re/meta/callback" {
		t.Fatalf("paramètres transmis à Meta: %q / %q", meta.seenCode, meta.seenRedirectURI)
	}
	// Only the long-lived token is ever stored.
	tenant, err := store.TenantByID(t.Context(), result.TenantID)
	if err != nil {
		t.Fatalf("TenantByID: %v", err)
	}
	if tenant.UserToken != "LONG" || tenant.MetaUserID != "meta-1" {
		t.Fatalf("tenant = %+v", tenant)
	}
	pages, err := store.ListPages(t.Context(), result.TenantID)
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 2 || pages[0].PageToken != "PT1" {
		t.Fatalf("pages = %+v", pages)
	}
}

func TestCompleteReusesExistingTenantID(t *testing.T) {
	svc, store, _, _ := newLoginHarness(t, nil)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.seedTenant("tenant-existant", "meta-1", "VIEUX-JETON")
	store.tenants["tenant-existant"].CreatedAt = created

	result, err := svc.Complete(t.Context(), "code", "uri")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if result.TenantID != "tenant-existant" {
		t.Fatalf("l'identifiant du tenant a changé: %s", result.TenantID)
	}
	tenant, _ := store.TenantByID(t.Context(), "tenant-existant")
	if tenant.UserToken != "LONG" {
		t.Fatalf("le jeton n'a pas été renouvelé: %q", tenant.UserToken)
	}
	if !tenant.CreatedAt.Equal(created) {
		t.Fatalf("created_at écrasé: %v", tenant.CreatedAt)
	}
}

func TestCompleteRejectsUserOutsideWhitelist(t *testing.T) {
	allow := func(id string) bool { return id == "meta-autorisé" }
	svc, store, _, _ := newLoginHarness(t, allow)

	_, err := svc.Complete(t.Context(), "code", "uri")
	if !errors.Is(err, domain.ErrForbiddenUser) {
		t.Fatalf("erreur = %v, attendu ErrForbiddenUser", err)
	}
	if len(store.tenants) != 0 {
		t.Fatal("un tenant a été créé pour un compte non autorisé")
	}
}

func TestCompletePropagatesMetaFailures(t *testing.T) {
	graphErr := &domain.GraphError{HTTPStatus: 400, Code: 190, Message: "expired"}
	cases := map[string]func(*fakeMetaOAuth){
		"échange du code":    func(m *fakeMetaOAuth) { m.exchangeErr = graphErr },
		"jeton longue durée": func(m *fakeMetaOAuth) { m.longErr = graphErr },
		"profil":             func(m *fakeMetaOAuth) { m.meErr = graphErr },
		"pages":              func(m *fakeMetaOAuth) { m.accountsErr = graphErr },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			svc, _, meta, _ := newLoginHarness(t, nil)
			breakIt(meta)
			if _, err := svc.Complete(t.Context(), "code", "uri"); !errors.Is(err, graphErr) {
				t.Fatalf("erreur = %v", err)
			}
		})
	}
}

func TestSyncPages(t *testing.T) {
	svc, store, meta, _ := newLoginHarness(t, nil)
	store.seedTenant("tenant-a", "meta-1", "LONG",
		domain.Page{PageID: "obsolete", Name: "Obsolète", PageToken: "old"})

	pages, err := svc.SyncPages(t.Context(), "tenant-a")
	if err != nil {
		t.Fatalf("SyncPages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("%d pages", len(pages))
	}
	if meta.seenUserToken != "LONG" {
		t.Fatalf("jeton utilisé = %q", meta.seenUserToken)
	}
	stored, _ := store.ListPages(t.Context(), "tenant-a")
	if len(stored) != 2 {
		t.Fatalf("les pages obsolètes n'ont pas été remplacées: %+v", stored)
	}
}

func TestSyncPagesUnknownTenant(t *testing.T) {
	svc, _, _, _ := newLoginHarness(t, nil)
	if _, err := svc.SyncPages(t.Context(), "inconnu"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erreur = %v", err)
	}
}

func TestConsumeState(t *testing.T) {
	svc, store, _, clk := newLoginHarness(t, nil)
	req := domain.OAuthRequest{ClientID: "cid", RedirectURI: "https://claude.ai/cb", ClientState: "st"}
	_ = store.CreateLoginState(t.Context(), &domain.LoginState{
		State: "abc", Request: req, ExpiresAt: clk.Now().Add(10 * time.Minute),
	})

	got, err := svc.ConsumeState(t.Context(), "abc")
	if err != nil {
		t.Fatalf("ConsumeState: %v", err)
	}
	if got.Request != req {
		t.Fatalf("requête = %+v", got.Request)
	}
	if _, err := svc.ConsumeState(t.Context(), "abc"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rejeu accepté: %v", err)
	}
}

func TestConsumeStateRejectsExpired(t *testing.T) {
	svc, store, _, clk := newLoginHarness(t, nil)
	_ = store.CreateLoginState(t.Context(), &domain.LoginState{
		State: "abc", ExpiresAt: clk.Now().Add(time.Minute),
	})
	clk.advance(2 * time.Minute)

	if _, err := svc.ConsumeState(t.Context(), "abc"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("un state expiré a été accepté: %v", err)
	}
}

func TestDeleteByMetaUserID(t *testing.T) {
	svc, store, _, _ := newLoginHarness(t, nil)
	store.seedTenant("tenant-a", "meta-1", "LONG",
		domain.Page{PageID: "p1", Name: "P1", PageToken: "t"})

	if err := svc.DeleteByMetaUserID(t.Context(), "meta-1"); err != nil {
		t.Fatalf("DeleteByMetaUserID: %v", err)
	}
	if _, err := store.TenantByID(t.Context(), "tenant-a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("le tenant existe encore: %v", err)
	}
	// An unknown account must succeed silently: Meta expects a success.
	if err := svc.DeleteByMetaUserID(t.Context(), "meta-inconnu"); err != nil {
		t.Fatalf("compte inconnu: %v", err)
	}
}

func TestNewTenantIDLooksLikeUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := newTenantID()
		if len(id) != 36 || strings.Count(id, "-") != 4 {
			t.Fatalf("format inattendu: %q", id)
		}
		if id[14] != '4' {
			t.Fatalf("version != 4: %q", id)
		}
		if c := id[19]; c != '8' && c != '9' && c != 'a' && c != 'b' {
			t.Fatalf("variant RFC 4122 absent: %q", id)
		}
		if seen[id] {
			t.Fatalf("collision: %q", id)
		}
		seen[id] = true
	}
}
