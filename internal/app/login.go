package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// LoginService turns a successful Facebook login into a tenant with its pages
// synchronized. It is the only use case that writes a tenant.
type LoginService struct {
	store domain.TenantStore
	meta  domain.MetaOAuthClient
	clock domain.Clock
	// allow reports whether a Facebook user id may create a tenant. It backs
	// the ALLOWED_META_USER_IDS whitelist.
	allow func(metaUserID string) bool
}

// NewLoginService wires the login use case. A nil allow function lets
// everyone in.
func NewLoginService(store domain.TenantStore, meta domain.MetaOAuthClient, clk domain.Clock, allow func(string) bool) *LoginService {
	if allow == nil {
		allow = func(string) bool { return true }
	}
	return &LoginService{store: store, meta: meta, clock: clk, allow: allow}
}

// AuthorizeURL is the Facebook dialog the user must visit.
func (s *LoginService) AuthorizeURL(redirectURI, state string) string {
	return s.meta.AuthorizeURL(redirectURI, state)
}

// LoginResult describes the tenant behind a completed login.
type LoginResult struct {
	TenantID    string
	DisplayName string
	Pages       int
}

// Complete runs the whole callback: it exchanges the code for a long-lived
// user token, identifies the user, creates or refreshes their tenant, and
// synchronizes their pages.
//
// A Facebook account that is not on the whitelist is rejected before any
// tenant row is written.
func (s *LoginService) Complete(ctx context.Context, code, redirectURI string) (*LoginResult, error) {
	shortToken, err := s.meta.ExchangeCode(ctx, code, redirectURI)
	if err != nil {
		return nil, err
	}
	userToken, err := s.meta.ExchangeLongLivedToken(ctx, shortToken)
	if err != nil {
		return nil, err
	}
	user, err := s.meta.Me(ctx, userToken)
	if err != nil {
		return nil, err
	}
	if !s.allow(user.ID) {
		return nil, domain.ErrForbiddenUser
	}

	now := s.clock.Now()
	tenant := &domain.Tenant{
		ID:          newTenantID(),
		MetaUserID:  user.ID,
		DisplayName: user.Name,
		UserToken:   userToken,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	// A returning user keeps the tenant id they already had, so their MCP
	// clients stay authorized against the same subject.
	existing, err := s.store.TenantByMetaUserID(ctx, user.ID)
	switch {
	case err == nil:
		tenant.ID = existing.ID
		tenant.CreatedAt = existing.CreatedAt
	case errors.Is(err, domain.ErrNotFound):
	default:
		return nil, fmt.Errorf("recherche du tenant: %w", err)
	}

	if err := s.store.UpsertTenant(ctx, tenant); err != nil {
		return nil, fmt.Errorf("enregistrement du tenant: %w", err)
	}

	pages, err := syncPages(ctx, s.store, s.meta, tenant.ID, userToken)
	if err != nil {
		return nil, err
	}
	return &LoginResult{TenantID: tenant.ID, DisplayName: tenant.DisplayName, Pages: len(pages)}, nil
}

// SyncPages refreshes the pages of an existing tenant from the Graph API.
func (s *LoginService) SyncPages(ctx context.Context, tenantID string) ([]domain.Page, error) {
	tenant, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return syncPages(ctx, s.store, s.meta, tenant.ID, tenant.UserToken)
}

// DeleteByMetaUserID removes a tenant and everything attached to it. It backs
// the Meta data deletion callback, and is a no-op when the account is
// unknown: Meta must get a success either way.
func (s *LoginService) DeleteByMetaUserID(ctx context.Context, metaUserID string) error {
	tenant, err := s.store.TenantByMetaUserID(ctx, metaUserID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recherche du tenant à supprimer: %w", err)
	}
	if err := s.store.DeleteTenant(ctx, tenant.ID); err != nil {
		return fmt.Errorf("suppression du tenant: %w", err)
	}
	return nil
}

// ConsumeState retrieves and burns the login state parked by the
// authorization endpoint, checking that it has not expired.
func (s *LoginService) ConsumeState(ctx context.Context, state string) (*domain.LoginState, error) {
	login, err := s.store.ConsumeLoginState(ctx, state)
	if err != nil {
		return nil, err
	}
	if !s.clock.Now().Before(login.ExpiresAt) {
		return nil, domain.ErrNotFound
	}
	return login, nil
}
