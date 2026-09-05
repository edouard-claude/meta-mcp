package app

import (
	"errors"
	"testing"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// seedTenantWithExpiry registers a tenant whose token dies at a given time.
func seedTenantWithExpiry(t *testing.T, store *fakeStore, id, metaUserID string, expiry time.Time) {
	t.Helper()
	store.seedTenant(id, metaUserID, "LONG")
	store.tenants[id].UserTokenExpiresAt = expiry
}

func TestRefreshRenewsTokensCloseToExpiry(t *testing.T) {
	svc, store, meta, clk := newLoginHarness(t, nil)
	now := clk.Now()

	seedTenantWithExpiry(t, store, "bientot", "meta-1", now.Add(3*24*time.Hour))
	seedTenantWithExpiry(t, store, "plus-tard", "meta-2", now.Add(50*24*time.Hour))

	report, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow)
	if err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Checked != 1 || report.Refreshed != 1 || len(report.Expired) != 0 {
		t.Fatalf("rapport = %+v", report)
	}

	renewed, err := store.TenantByID(t.Context(), "bientot")
	if err != nil {
		t.Fatalf("TenantByID: %v", err)
	}
	want := now.Add(meta.longTTL)
	if !renewed.UserTokenExpiresAt.Equal(want) {
		t.Fatalf("nouvelle expiration = %v, attendu %v", renewed.UserTokenExpiresAt, want)
	}
	if !renewed.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at = %v", renewed.UpdatedAt)
	}

	// The tenant that was nowhere near expiry must not have been touched.
	untouched, err := store.TenantByID(t.Context(), "plus-tard")
	if err != nil {
		t.Fatalf("TenantByID: %v", err)
	}
	if !untouched.UserTokenExpiresAt.Equal(now.Add(50 * 24 * time.Hour)) {
		t.Fatalf("le tenant lointain a été renouvelé: %v", untouched.UserTokenExpiresAt)
	}
}

// TestRefreshCoversTenantsWithUnknownExpiry pins the migration path: rows
// written before the expiry column existed carry a zero deadline and must be
// renewed once so they gain a real one.
func TestRefreshCoversTenantsWithUnknownExpiry(t *testing.T) {
	svc, store, _, clk := newLoginHarness(t, nil)
	seedTenantWithExpiry(t, store, "ancien", "meta-1", time.Time{})
	// Never checked, so well past the recheck interval.
	store.tenants["ancien"].UpdatedAt = clk.Now().Add(-30 * 24 * time.Hour)

	report, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow)
	if err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Refreshed != 1 {
		t.Fatalf("rapport = %+v", report)
	}
	renewed, _ := store.TenantByID(t.Context(), "ancien")
	if renewed.UserTokenExpiresAt.IsZero() || !renewed.UserTokenExpiresAt.After(clk.Now()) {
		t.Fatalf("expiration = %v", renewed.UserTokenExpiresAt)
	}
}

// TestRefreshReportsTokensMetaRefuses covers the case the sweep cannot fix:
// the user revoked the app, so nothing but a reconnection will help.
func TestRefreshReportsTokensMetaRefuses(t *testing.T) {
	svc, store, meta, clk := newLoginHarness(t, nil)
	meta.longErr = &domain.GraphError{HTTPStatus: 400, Code: 190, Message: "revoked"}
	seedTenantWithExpiry(t, store, "revoque", "meta-1", clk.Now().Add(time.Hour))

	report, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow)
	if err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Refreshed != 0 || len(report.Expired) != 1 || report.Expired[0] != "revoque" {
		t.Fatalf("rapport = %+v", report)
	}
	// Nothing is deleted: the tenant keeps its pages until it reconnects.
	if _, err := store.TenantByID(t.Context(), "revoque"); err != nil {
		t.Fatalf("le tenant a disparu: %v", err)
	}
}

// TestRefreshContinuesAfterOneFailure makes sure one broken tenant does not
// cost everyone else their renewal.
func TestRefreshContinuesAfterOneFailure(t *testing.T) {
	svc, store, meta, clk := newLoginHarness(t, nil)
	now := clk.Now()
	seedTenantWithExpiry(t, store, "a", "meta-1", now.Add(time.Hour))
	seedTenantWithExpiry(t, store, "b", "meta-2", now.Add(time.Hour))

	// The fake refuses any token that is neither the short nor the long one.
	store.tenants["a"].UserToken = "JETON-INCONNU"
	meta.longTTL = 30 * 24 * time.Hour

	report, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow)
	if err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Checked != 2 || report.Refreshed != 1 || len(report.Expired) != 1 {
		t.Fatalf("rapport = %+v", report)
	}
	renewed, _ := store.TenantByID(t.Context(), "b")
	if !renewed.UserTokenExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("le second tenant n'a pas été renouvelé: %v", renewed.UserTokenExpiresAt)
	}
}

func TestRefreshPropagatesStoreFailure(t *testing.T) {
	svc, store, _, _ := newLoginHarness(t, nil)
	boom := errors.New("base indisponible")
	store.failOn("TenantsDueForTokenRefresh", boom)

	if _, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow); !errors.Is(err, boom) {
		t.Fatalf("erreur = %v", err)
	}
}

func TestLoginStoresTokenDeadline(t *testing.T) {
	svc, store, meta, clk := newLoginHarness(t, nil)

	result, err := svc.Complete(t.Context(), "code", "uri")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	tenant, _ := store.TenantByID(t.Context(), result.TenantID)
	if !tenant.UserTokenExpiresAt.Equal(clk.Now().Add(meta.longTTL)) {
		t.Fatalf("expiration = %v", tenant.UserTokenExpiresAt)
	}

	// Meta omits expires_in for tokens it treats as non-expiring; the
	// deadline then stays unknown rather than landing in 1970.
	meta.longTTL = 0
	if _, err := svc.Complete(t.Context(), "code", "uri"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	tenant, _ = store.TenantByID(t.Context(), result.TenantID)
	if !tenant.UserTokenExpiresAt.IsZero() {
		t.Fatalf("expiration = %v, attendue inconnue", tenant.UserTokenExpiresAt)
	}
}

func TestTokenExpiresWithin(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		expiry time.Time
		want   bool
	}{
		{"inconnue", time.Time{}, true},
		{"dans une heure", now.Add(time.Hour), true},
		{"déjà passée", now.Add(-time.Hour), true},
		{"dans un mois", now.Add(30 * 24 * time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := &domain.Tenant{UserTokenExpiresAt: tc.expiry}
			if got := tenant.TokenExpiresWithin(now, DefaultRefreshWindow); got != tc.want {
				t.Fatalf("TokenExpiresWithin = %v, attendu %v", got, tc.want)
			}
		})
	}
}

// TestRefreshLeavesFreshlyCheckedUnknownExpiryAlone covers what the live API
// exposed: Meta issues non-expiring tokens, and treating an absent deadline as
// "renew now" re-exchanged them on every single sweep.
func TestRefreshLeavesFreshlyCheckedUnknownExpiryAlone(t *testing.T) {
	svc, store, _, clk := newLoginHarness(t, nil)
	seedTenantWithExpiry(t, store, "sans-echeance", "meta-1", time.Time{})
	store.tenants["sans-echeance"].UpdatedAt = clk.Now().Add(-time.Hour)

	report, err := svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow)
	if err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Checked != 0 {
		t.Fatalf("rapport = %+v, le jeton venait d'être vérifié", report)
	}

	// Once the recheck interval has passed, it is looked at again.
	store.tenants["sans-echeance"].UpdatedAt = clk.Now().Add(-8 * 24 * time.Hour)
	if report, err = svc.RefreshExpiringTokens(t.Context(), DefaultRefreshWindow); err != nil {
		t.Fatalf("RefreshExpiringTokens: %v", err)
	}
	if report.Refreshed != 1 {
		t.Fatalf("rapport = %+v", report)
	}
}
