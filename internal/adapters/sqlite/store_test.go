package sqlite

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/adapters/crypto"
	"github.com/edouard-claude/meta-mcp/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	cipher, err := crypto.New(bytes.Repeat([]byte{0x2a}, 32))
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	store, err := New(t.Context(), filepath.Join(t.TempDir(), "test.db"), cipher)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func seedTenant(t *testing.T, s *Store, id, metaUserID, token string) *domain.Tenant {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	tenant := &domain.Tenant{
		ID:          id,
		MetaUserID:  metaUserID,
		DisplayName: "Tenant " + id,
		UserToken:   token,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.UpsertTenant(t.Context(), tenant); err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}
	return tenant
}

func TestTenantRoundTripAndTokenIsEncryptedAtRest(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	const token = "EAAG-long-lived-user-token"
	seedTenant(t, s, "tenant-a", "meta-1", token)

	got, err := s.TenantByMetaUserID(ctx, "meta-1")
	if err != nil {
		t.Fatalf("TenantByMetaUserID: %v", err)
	}
	if got.UserToken != token {
		t.Fatalf("UserToken = %q, attendu %q", got.UserToken, token)
	}
	if got.ID != "tenant-a" || got.DisplayName != "Tenant tenant-a" {
		t.Fatalf("tenant inattendu: %+v", got)
	}

	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT user_token_enc FROM tenants WHERE id = ?`, "tenant-a").
		Scan(&stored); err != nil {
		t.Fatalf("lecture brute: %v", err)
	}
	if bytes.Contains(stored, []byte(token)) {
		t.Fatal("le jeton utilisateur est stocké en clair")
	}
}

func TestUpsertTenantRefreshesToken(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	seedTenant(t, s, "tenant-a", "meta-1", "old-token")
	// A second login reuses the same Meta user id and must not create a
	// second tenant, only refresh the token.
	seedTenant(t, s, "tenant-other-uuid", "meta-1", "new-token")

	got, err := s.TenantByMetaUserID(ctx, "meta-1")
	if err != nil {
		t.Fatalf("TenantByMetaUserID: %v", err)
	}
	if got.ID != "tenant-a" {
		t.Fatalf("l'id du tenant a changé: %s", got.ID)
	}
	if got.UserToken != "new-token" {
		t.Fatalf("UserToken = %q, attendu new-token", got.UserToken)
	}
}

func TestTenantNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.TenantByID(t.Context(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("TenantByID inconnu = %v, attendu ErrNotFound", err)
	}
}

func TestPagesAreIsolatedPerTenant(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	seedTenant(t, s, "tenant-a", "meta-1", "tok-a")
	seedTenant(t, s, "tenant-b", "meta-2", "tok-b")

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.ReplacePages(ctx, "tenant-a", []domain.Page{
		{PageID: "page-a", Name: "Page A", PageToken: "page-token-a", SyncedAt: now},
	}); err != nil {
		t.Fatalf("ReplacePages A: %v", err)
	}
	if err := s.ReplacePages(ctx, "tenant-b", []domain.Page{
		{PageID: "page-b", Name: "Page B", IGUserID: "ig-b", IGUsername: "bee", PageToken: "page-token-b", SyncedAt: now},
	}); err != nil {
		t.Fatalf("ReplacePages B: %v", err)
	}

	// Tenant A must not reach tenant B's page, even knowing its id.
	if _, err := s.PageByID(ctx, "tenant-a", "page-b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("fuite entre tenants: PageByID = %v, attendu ErrNotFound", err)
	}

	pages, err := s.ListPages(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 1 || pages[0].PageID != "page-a" {
		t.Fatalf("ListPages(tenant-a) = %+v", pages)
	}
	if pages[0].PageToken != "page-token-a" {
		t.Fatalf("PageToken = %q", pages[0].PageToken)
	}

	pageB, err := s.PageByID(ctx, "tenant-b", "page-b")
	if err != nil {
		t.Fatalf("PageByID B: %v", err)
	}
	if pageB.IGUserID != "ig-b" || pageB.IGUsername != "bee" || !pageB.HasInstagram() {
		t.Fatalf("compte Instagram non restitué: %+v", pageB)
	}
}

func TestReplacePagesDropsStaleOnes(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	seedTenant(t, s, "tenant-a", "meta-1", "tok")
	now := time.Now().UTC()

	if err := s.ReplacePages(ctx, "tenant-a", []domain.Page{
		{PageID: "p1", Name: "One", PageToken: "t1", SyncedAt: now},
		{PageID: "p2", Name: "Two", PageToken: "t2", SyncedAt: now},
	}); err != nil {
		t.Fatalf("ReplacePages: %v", err)
	}
	if err := s.ReplacePages(ctx, "tenant-a", []domain.Page{
		{PageID: "p2", Name: "Two", PageToken: "t2", SyncedAt: now},
	}); err != nil {
		t.Fatalf("ReplacePages 2: %v", err)
	}
	pages, err := s.ListPages(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 1 || pages[0].PageID != "p2" {
		t.Fatalf("les pages périmées n'ont pas disparu: %+v", pages)
	}
}

func TestDeleteTenantCascades(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	seedTenant(t, s, "tenant-a", "meta-1", "tok")
	if err := s.ReplacePages(ctx, "tenant-a", []domain.Page{
		{PageID: "p1", Name: "One", PageToken: "t1", SyncedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("ReplacePages: %v", err)
	}
	if err := s.CreateRefreshToken(ctx, &domain.RefreshToken{
		TokenHash: "hash-1", ClientID: "c1", TenantID: "tenant-a",
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	if err := s.DeleteTenant(ctx, "tenant-a"); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	if _, err := s.TenantByID(ctx, "tenant-a"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("le tenant existe encore: %v", err)
	}
	pages, err := s.ListPages(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 0 {
		t.Fatalf("pages orphelines: %+v", pages)
	}
	if _, err := s.RotateRefreshToken(ctx, "hash-1", time.Now()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("refresh token survivant: %v", err)
	}
}

func TestOAuthClientRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	client := &domain.OAuthClient{
		ClientID:     "cid",
		ClientName:   "Claude",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback", "http://localhost:6274/cb"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
	}
	if err := s.RegisterClient(ctx, client); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	got, err := s.ClientByID(ctx, "cid")
	if err != nil {
		t.Fatalf("ClientByID: %v", err)
	}
	if len(got.RedirectURIs) != 2 || !got.AllowsRedirectURI("http://localhost:6274/cb") {
		t.Fatalf("redirect_uris = %+v", got.RedirectURIs)
	}
	if got.AllowsRedirectURI("https://evil.example/cb") {
		t.Fatal("AllowsRedirectURI accepte une URI non enregistrée")
	}
	if _, err := s.ClientByID(ctx, "unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ClientByID inconnu = %v", err)
	}
}

func TestAuthCodeIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	code := &domain.AuthCode{
		Code: "abc", ClientID: "cid", TenantID: "tenant-a",
		RedirectURI: "https://claude.ai/cb", CodeChallenge: "chal",
		Resource:  "https://mcp.example.re/mcp",
		ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second),
	}
	if err := s.CreateAuthCode(ctx, code); err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	got, err := s.ConsumeAuthCode(ctx, "abc")
	if err != nil {
		t.Fatalf("ConsumeAuthCode: %v", err)
	}
	if got.TenantID != "tenant-a" || got.CodeChallenge != "chal" || got.Resource != code.Resource {
		t.Fatalf("code inattendu: %+v", got)
	}
	if _, err := s.ConsumeAuthCode(ctx, "abc"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rejeu du code accepté: %v", err)
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	rt := &domain.RefreshToken{
		TokenHash: "hash", ClientID: "cid", TenantID: "tenant-a",
		ExpiresAt: now.Add(time.Hour),
	}
	if err := s.CreateRefreshToken(ctx, rt); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	got, err := s.RotateRefreshToken(ctx, "hash", now)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if got.TenantID != "tenant-a" || got.ClientID != "cid" {
		t.Fatalf("refresh token inattendu: %+v", got)
	}
	if _, err := s.RotateRefreshToken(ctx, "hash", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rejeu du refresh token accepté: %v", err)
	}
}

func TestRefreshTokenExpiryAndRevocation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()

	if err := s.CreateRefreshToken(ctx, &domain.RefreshToken{
		TokenHash: "expired", ClientID: "cid", TenantID: "tenant-a", ExpiresAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	if _, err := s.RotateRefreshToken(ctx, "expired", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("refresh token expiré accepté: %v", err)
	}

	if err := s.CreateRefreshToken(ctx, &domain.RefreshToken{
		TokenHash: "live", ClientID: "cid", TenantID: "tenant-a", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	if err := s.RevokeTenantRefreshTokens(ctx, "tenant-a"); err != nil {
		t.Fatalf("RevokeTenantRefreshTokens: %v", err)
	}
	if _, err := s.RotateRefreshToken(ctx, "live", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("refresh token révoqué accepté: %v", err)
	}
}

func TestLoginStateIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	st := &domain.LoginState{
		State: "st",
		Request: domain.OAuthRequest{
			ClientID: "cid", RedirectURI: "https://claude.ai/cb",
			CodeChallenge: "chal", ClientState: "client-state", Resource: "https://mcp.example.re/mcp",
		},
		ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second),
	}
	if err := s.CreateLoginState(ctx, st); err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	got, err := s.ConsumeLoginState(ctx, "st")
	if err != nil {
		t.Fatalf("ConsumeLoginState: %v", err)
	}
	if got.Request != st.Request {
		t.Fatalf("requête OAuth altérée: %+v", got.Request)
	}
	if _, err := s.ConsumeLoginState(ctx, "st"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("rejeu du state accepté: %v", err)
	}
}

func TestPurgeExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	past := now.Add(-time.Hour)

	if err := s.CreateAuthCode(ctx, &domain.AuthCode{
		Code: "old", ClientID: "c", TenantID: "t", RedirectURI: "u", CodeChallenge: "x", ExpiresAt: past,
	}); err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}
	if err := s.CreateLoginState(ctx, &domain.LoginState{State: "old", ExpiresAt: past}); err != nil {
		t.Fatalf("CreateLoginState: %v", err)
	}
	if err := s.CreateAuthCode(ctx, &domain.AuthCode{
		Code: "live", ClientID: "c", TenantID: "t", RedirectURI: "u", CodeChallenge: "x",
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateAuthCode: %v", err)
	}

	if err := s.PurgeExpired(ctx, now); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if _, err := s.ConsumeAuthCode(ctx, "old"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("code expiré non purgé: %v", err)
	}
	if _, err := s.ConsumeLoginState(ctx, "old"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("state expiré non purgé: %v", err)
	}
	if _, err := s.ConsumeAuthCode(ctx, "live"); err != nil {
		t.Fatalf("code valide purgé à tort: %v", err)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	cipher, err := crypto.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("crypto.New: %v", err)
	}
	for i := range 2 {
		store, err := New(context.Background(), path, cipher)
		if err != nil {
			t.Fatalf("ouverture %d: %v", i, err)
		}
		if err := store.Ping(t.Context()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
		store.Close()
	}
}

func TestTokenExpiryRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(60 * 24 * time.Hour)

	tenant := seedTenant(t, s, "tenant-a", "meta-1", "tok")
	tenant.UserTokenExpiresAt = expiry
	if err := s.UpsertTenant(ctx, tenant); err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}

	got, err := s.TenantByID(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("TenantByID: %v", err)
	}
	if !got.UserTokenExpiresAt.Equal(expiry) {
		t.Fatalf("expiration = %v, attendu %v", got.UserTokenExpiresAt, expiry)
	}

	// An unknown deadline stays unknown rather than becoming the epoch.
	tenant.UserTokenExpiresAt = time.Time{}
	if err := s.UpsertTenant(ctx, tenant); err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}
	if got, err = s.TenantByID(ctx, "tenant-a"); err != nil {
		t.Fatalf("TenantByID: %v", err)
	}
	if !got.UserTokenExpiresAt.IsZero() {
		t.Fatalf("expiration = %v, attendue inconnue", got.UserTokenExpiresAt)
	}
}

func TestTenantsDueForTokenRefresh(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)

	withExpiry := func(id, metaUserID string, expiry, updatedAt time.Time) {
		t.Helper()
		tenant := seedTenant(t, s, id, metaUserID, "tok-"+id)
		tenant.UserTokenExpiresAt = expiry
		tenant.UpdatedAt = updatedAt
		if err := s.UpsertTenant(ctx, tenant); err != nil {
			t.Fatalf("UpsertTenant: %v", err)
		}
	}
	// No known deadline and not looked at for a month.
	withExpiry("inconnu", "meta-0", time.Time{}, now.Add(-30*24*time.Hour))
	withExpiry("bientot", "meta-1", now.Add(3*24*time.Hour), now)
	withExpiry("lointain", "meta-2", now.Add(50*24*time.Hour), now)
	// No known deadline either, but checked an hour ago.
	withExpiry("recent", "meta-3", time.Time{}, now.Add(-time.Hour))

	due, err := s.TenantsDueForTokenRefresh(ctx, now.Add(14*24*time.Hour), now.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("TenantsDueForTokenRefresh: %v", err)
	}
	ids := map[string]bool{}
	for _, tenant := range due {
		ids[tenant.ID] = true
		if tenant.UserToken != "tok-"+tenant.ID {
			t.Fatalf("jeton non déchiffré: %+v", tenant)
		}
	}
	if len(due) != 2 || !ids["inconnu"] || !ids["bientot"] {
		t.Fatalf("tenants à renouveler = %v", ids)
	}

	// The one checked an hour ago is left alone: otherwise a non-expiring
	// token would be re-exchanged on every single sweep.
	if ids["recent"] {
		t.Fatal("un tenant sans échéance vérifié récemment est renouvelé à nouveau")
	}
}
