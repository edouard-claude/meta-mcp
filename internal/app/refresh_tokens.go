package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// DefaultRefreshWindow is how long before expiry a Meta user token is renewed.
// Meta issues roughly 60 day tokens, so two weeks of margin leaves room for a
// server that was down for a while without letting anyone drop off.
const DefaultRefreshWindow = 14 * 24 * time.Hour

// RefreshReport summarizes one sweep.
type RefreshReport struct {
	Checked   int
	Refreshed int
	// Expired lists the tenants whose token Meta refused to renew. They have
	// to go through reconnect_url; nothing is deleted on their behalf.
	Expired []string
}

// RefreshExpiringTokens renews every Meta user token that dies within the
// window, and returns what it did.
//
// Meta accepts a still valid long-lived token in the fb_exchange_token flow
// and hands back a fresh 60 day one, so a tenant who keeps using the server
// never has to log in again. Without this sweep every tenant silently breaks
// about two months after connecting.
func (s *LoginService) RefreshExpiringTokens(ctx context.Context, window time.Duration) (RefreshReport, error) {
	if window <= 0 {
		window = DefaultRefreshWindow
	}
	now := s.clock.Now()

	tenants, err := s.store.TenantsDueForTokenRefresh(ctx, now.Add(window))
	if err != nil {
		return RefreshReport{}, fmt.Errorf("liste des jetons à renouveler: %w", err)
	}

	report := RefreshReport{Checked: len(tenants)}
	for _, tenant := range tenants {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		switch err := s.refreshOne(ctx, tenant); {
		case err == nil:
			report.Refreshed++
		case errors.Is(err, domain.ErrReauthorize):
			report.Expired = append(report.Expired, tenant.ID)
		default:
			// A transport hiccup or a quota must not abort the sweep: the
			// remaining tenants still deserve their renewal.
			report.Expired = append(report.Expired, tenant.ID)
		}
	}
	return report, nil
}

// refreshOne renews a single tenant's token in place.
func (s *LoginService) refreshOne(ctx context.Context, tenant domain.Tenant) error {
	longLived, err := s.meta.ExchangeLongLivedToken(ctx, tenant.UserToken)
	if err != nil {
		if ge, ok := domain.AsGraphError(err); ok && ge.IsAuth() {
			return domain.ErrReauthorize
		}
		return err
	}

	now := s.clock.Now()
	updated := tenant
	updated.UserToken = longLived.Token
	updated.UpdatedAt = now
	updated.UserTokenExpiresAt = expiryFrom(now, longLived.ExpiresIn)

	if err := s.store.UpsertTenant(ctx, &updated); err != nil {
		return fmt.Errorf("enregistrement du jeton renouvelé: %w", err)
	}
	return nil
}
