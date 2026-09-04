package meta

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edouard/metasocial-mcp/internal/adapters/crypto"
	"github.com/edouard/metasocial-mcp/internal/adapters/sqlite"
	"github.com/edouard/metasocial-mcp/internal/app"
	"github.com/edouard/metasocial-mcp/internal/domain"
)

const (
	testPublicURL   = "https://mcp.example.re"
	testCallbackURI = testPublicURL + "/meta/callback"
	testAppSecret   = "app-secret"
)

// stubOAuth is a domain.MetaOAuthClient that never touches the network.
type stubOAuth struct {
	user  domain.MetaUser
	pages []domain.Page
	err   error
}

func (s *stubOAuth) AuthorizeURL(redirectURI, state string) string {
	return "https://facebook.test/v26.0/dialog/oauth?redirect_uri=" +
		url.QueryEscape(redirectURI) + "&state=" + url.QueryEscape(state)
}

func (s *stubOAuth) ExchangeCode(context.Context, string, string) (string, error) {
	return "SHORT", s.err
}

func (s *stubOAuth) ExchangeLongLivedToken(context.Context, string) (string, error) {
	return "LONG", nil
}

func (s *stubOAuth) Me(context.Context, string) (domain.MetaUser, error) { return s.user, nil }

func (s *stubOAuth) Accounts(context.Context, string) ([]domain.Page, error) { return s.pages, nil }

// stubIssuer records what the authorization server would have been asked.
type stubIssuer struct {
	seenTenantID string
	seenRequest  domain.OAuthRequest
	err          error
}

func (s *stubIssuer) IssueAuthCode(_ *http.Request, req domain.OAuthRequest, tenantID string) (string, error) {
	s.seenTenantID, s.seenRequest = tenantID, req
	if s.err != nil {
		return "", s.err
	}
	return req.RedirectURI + "?code=THE-CODE&state=" + url.QueryEscape(req.ClientState), nil
}

// systemClock is the trivial clock the handler tests need.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type handlerHarness struct {
	handlers *Handlers
	store    *sqlite.Store
	oauth    *stubOAuth
	issuer   *stubIssuer
}

func newHandlerHarness(t *testing.T, allow func(string) bool) *handlerHarness {
	t.Helper()
	cipher, err := crypto.New(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	store, err := sqlite.New(t.Context(), filepath.Join(t.TempDir(), "meta.db"), cipher)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	oauth := &stubOAuth{
		user: domain.MetaUser{ID: "meta-1", Name: "Édouard"},
		pages: []domain.Page{
			{PageID: "page-1", Name: "Page 1", PageToken: "PT1", SyncedAt: time.Now().UTC()},
		},
	}
	issuer := &stubIssuer{}
	login := app.NewLoginService(store, oauth, systemClock{}, allow)
	handlers := NewHandlers(login, issuer, HandlerOptions{
		PublicURL:   testPublicURL,
		RedirectURI: testCallbackURI,
		AppSecret:   testAppSecret,
	}, slog.New(slog.DiscardHandler))

	return &handlerHarness{handlers: handlers, store: store, oauth: oauth, issuer: issuer}
}

// parkState stores a pending MCP authorization request and returns its state.
func (h *handlerHarness) parkState(t *testing.T, state string, ttl time.Duration) domain.OAuthRequest {
	t.Helper()
	req := domain.OAuthRequest{
		ClientID:      "cid",
		RedirectURI:   "https://claude.ai/api/mcp/auth_callback",
		CodeChallenge: "challenge",
		ClientState:   "client-state",
		Resource:      testPublicURL + "/mcp",
	}
	if err := h.store.CreateLoginState(t.Context(), &domain.LoginState{
		State: state, Request: req, ExpiresAt: time.Now().UTC().Add(ttl),
	}); err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginPageShowsFacebookButton(t *testing.T) {
	h := newHandlerHarness(t, nil)
	rec := serve(h.handlers.LoginHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/login?state=abc", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Continuer avec Facebook") {
		t.Fatalf("bouton absent: %s", body)
	}
	if !strings.Contains(body, "facebook.test") || !strings.Contains(body, "state=abc") {
		t.Fatalf("lien de dialogue absent: %s", body)
	}
	if !strings.Contains(body, testPublicURL+"/privacy") {
		t.Fatal("lien vers la politique de confidentialité absent")
	}
}

func TestLoginPageRequiresState(t *testing.T) {
	h := newHandlerHarness(t, nil)
	rec := serve(h.handlers.LoginHandler(), httptest.NewRequest(http.MethodGet, "/meta/login", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, attendu 400", rec.Code)
	}
}

func TestCallbackCompletesTheFlow(t *testing.T) {
	h := newHandlerHarness(t, nil)
	req := h.parkState(t, "state-1", 10*time.Minute)

	rec := serve(h.handlers.CallbackHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/callback?state=state-1&code=fb-code", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d, corps %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, req.RedirectURI) || !strings.Contains(loc, "code=THE-CODE") {
		t.Fatalf("redirection = %s", loc)
	}
	if h.issuer.seenTenantID == "" || h.issuer.seenRequest.ClientID != "cid" {
		t.Fatalf("l'émetteur n'a pas reçu la demande: %+v", h.issuer)
	}

	// The tenant and its pages are persisted.
	tenant, err := h.store.TenantByMetaUserID(t.Context(), "meta-1")
	if err != nil {
		t.Fatalf("TenantByMetaUserID: %v", err)
	}
	if tenant.UserToken != "LONG" {
		t.Fatalf("jeton stocké = %q", tenant.UserToken)
	}
	pages, err := h.store.ListPages(t.Context(), tenant.ID)
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %+v (err %v)", pages, err)
	}
}

func TestCallbackRejectsUnknownOrExpiredState(t *testing.T) {
	h := newHandlerHarness(t, nil)
	h.parkState(t, "state-expiré", -time.Minute)

	for _, state := range []string{"state-inconnu", "state-expiré"} {
		rec := serve(h.handlers.CallbackHandler(),
			httptest.NewRequest(http.MethodGet, "/meta/callback?state="+state+"&code=c", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("state %q: status %d, attendu 400", state, rec.Code)
		}
	}
}

func TestCallbackRedirectsUserDenial(t *testing.T) {
	h := newHandlerHarness(t, nil)
	req := h.parkState(t, "state-1", 10*time.Minute)

	rec := serve(h.handlers.CallbackHandler(), httptest.NewRequest(http.MethodGet,
		"/meta/callback?state=state-1&error=access_denied&error_reason=user_denied", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if !strings.HasPrefix(loc.String(), req.RedirectURI) {
		t.Fatalf("redirection hors du client MCP: %s", loc)
	}
	if loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "client-state" {
		t.Fatalf("paramètres = %v", loc.Query())
	}
}

func TestCallbackRejectsForbiddenUser(t *testing.T) {
	h := newHandlerHarness(t, func(id string) bool { return id == "quelqu-un-d-autre" })
	h.parkState(t, "state-1", 10*time.Minute)

	rec := serve(h.handlers.CallbackHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/callback?state=state-1&code=fb-code", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, attendu 403", rec.Code)
	}
	if _, err := h.store.TenantByMetaUserID(t.Context(), "meta-1"); err == nil {
		t.Fatal("un tenant a été créé pour un compte non autorisé")
	}
}

func TestCallbackSurfacesGraphError(t *testing.T) {
	h := newHandlerHarness(t, nil)
	h.parkState(t, "state-1", 10*time.Minute)
	h.oauth.err = &domain.GraphError{HTTPStatus: 400, Code: 190, Message: "expired"}

	rec := serve(h.handlers.CallbackHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/callback?state=state-1&code=fb-code", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, attendu 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "reconnect_url") {
		t.Fatalf("le message de reconnexion est absent: %s", rec.Body.String())
	}
}

// signedRequest builds a Meta signed_request the way Facebook does.
func signedRequest(t *testing.T, secret string, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + encoded
}

func TestDataDeletionRemovesTenant(t *testing.T) {
	h := newHandlerHarness(t, nil)
	h.parkState(t, "state-1", 10*time.Minute)
	serve(h.handlers.CallbackHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/callback?state=state-1&code=fb-code", nil))
	if _, err := h.store.TenantByMetaUserID(t.Context(), "meta-1"); err != nil {
		t.Fatalf("le tenant devrait exister: %v", err)
	}

	form := url.Values{"signed_request": {signedRequest(t, testAppSecret, map[string]any{
		"algorithm": "HMAC-SHA256", "issued_at": time.Now().Unix(), "user_id": "meta-1",
	})}}
	req := httptest.NewRequest(http.MethodPost, "/meta/data-deletion", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := serve(h.handlers.DataDeletionHandler(), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, corps %s", rec.Code, rec.Body.String())
	}
	var resp deletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("décodage: %v", err)
	}
	if resp.ConfirmationCode == "" || !strings.HasPrefix(resp.URL, testPublicURL+"/meta/deauthorize") {
		t.Fatalf("réponse = %+v", resp)
	}
	if _, err := h.store.TenantByMetaUserID(t.Context(), "meta-1"); err == nil {
		t.Fatal("le tenant n'a pas été supprimé")
	}
}

func TestDataDeletionRejectsBadSignature(t *testing.T) {
	h := newHandlerHarness(t, nil)
	cases := map[string]string{
		"vide":                "",
		"sans point":          "abcdef",
		"signature étrangère": signedRequest(t, "mauvais-secret", map[string]any{"user_id": "meta-1"}),
		"payload illisible":   "AAAA.@@@@",
	}
	for name, signed := range cases {
		t.Run(name, func(t *testing.T) {
			form := url.Values{"signed_request": {signed}}
			req := httptest.NewRequest(http.MethodPost, "/meta/data-deletion", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := serve(h.handlers.DataDeletionHandler(), req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, attendu 400", rec.Code)
			}
		})
	}
}

func TestDataDeletionOnUnknownUserSucceeds(t *testing.T) {
	h := newHandlerHarness(t, nil)
	form := url.Values{"signed_request": {signedRequest(t, testAppSecret, map[string]any{
		"algorithm": "HMAC-SHA256", "user_id": "jamais-vu",
	})}}
	req := httptest.NewRequest(http.MethodPost, "/meta/data-deletion", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := serve(h.handlers.DataDeletionHandler(), req); rec.Code != http.StatusOK {
		t.Fatalf("status %d, attendu 200", rec.Code)
	}
}

func TestStaticPages(t *testing.T) {
	h := newHandlerHarness(t, nil)

	rec := serve(h.handlers.PrivacyHandler(), httptest.NewRequest(http.MethodGet, "/privacy", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Politique de confidentialité") {
		t.Fatalf("page de confidentialité: %d / %s", rec.Code, rec.Body.String())
	}

	rec = serve(h.handlers.DeauthorizeHandler(),
		httptest.NewRequest(http.MethodGet, "/meta/deauthorize?code=abc123", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "abc123") {
		t.Fatalf("page de suppression: %d / %s", rec.Code, rec.Body.String())
	}
}
