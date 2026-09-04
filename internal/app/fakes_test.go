package app

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// fakeClock is a controllable domain.Clock.
type fakeClock struct{ now time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeStore is an in-memory domain.TenantStore. It enforces the same tenant
// scoping as the SQLite implementation, so a use case that leaks across
// tenants fails here too.
type fakeStore struct {
	mu sync.Mutex

	tenants map[string]*domain.Tenant          // by tenant id
	pages   map[string]map[string]*domain.Page // tenant id -> page id
	clients map[string]*domain.OAuthClient
	codes   map[string]*domain.AuthCode
	refresh map[string]*domain.RefreshToken
	states  map[string]*domain.LoginState

	// failures lets a test force an error from a given method.
	failures map[string]error
}

var _ domain.TenantStore = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants:  map[string]*domain.Tenant{},
		pages:    map[string]map[string]*domain.Page{},
		clients:  map[string]*domain.OAuthClient{},
		codes:    map[string]*domain.AuthCode{},
		refresh:  map[string]*domain.RefreshToken{},
		states:   map[string]*domain.LoginState{},
		failures: map[string]error{},
	}
}

func (s *fakeStore) failOn(method string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[method] = err
}

func (s *fakeStore) failure(method string) error { return s.failures[method] }

func (s *fakeStore) UpsertTenant(_ context.Context, t *domain.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("UpsertTenant"); err != nil {
		return err
	}
	for _, existing := range s.tenants {
		if existing.MetaUserID == t.MetaUserID && existing.ID != t.ID {
			return domain.ErrNotFound
		}
	}
	clone := *t
	s.tenants[t.ID] = &clone
	return nil
}

func (s *fakeStore) TenantByID(_ context.Context, id string) (*domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tenants[id]; ok {
		clone := *t
		return &clone, nil
	}
	return nil, domain.ErrNotFound
}

func (s *fakeStore) TenantByMetaUserID(_ context.Context, metaUserID string) (*domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tenants {
		if t.MetaUserID == metaUserID {
			clone := *t
			return &clone, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *fakeStore) DeleteTenant(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tenants, id)
	delete(s.pages, id)
	for hash, rt := range s.refresh {
		if rt.TenantID == id {
			delete(s.refresh, hash)
		}
	}
	return nil
}

func (s *fakeStore) ReplacePages(_ context.Context, tenantID string, pages []domain.Page) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("ReplacePages"); err != nil {
		return err
	}
	byID := map[string]*domain.Page{}
	for _, p := range pages {
		clone := p
		clone.TenantID = tenantID
		byID[p.PageID] = &clone
	}
	s.pages[tenantID] = byID
	return nil
}

func (s *fakeStore) ListPages(_ context.Context, tenantID string) ([]domain.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure("ListPages"); err != nil {
		return nil, err
	}
	out := []domain.Page{}
	for _, id := range slices.Sorted(maps.Keys(s.pages[tenantID])) {
		out = append(out, *s.pages[tenantID][id])
	}
	return out, nil
}

func (s *fakeStore) PageByID(_ context.Context, tenantID, pageID string) (*domain.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.pages[tenantID][pageID]; ok {
		clone := *p
		return &clone, nil
	}
	return nil, domain.ErrNotFound
}

func (s *fakeStore) RegisterClient(_ context.Context, c *domain.OAuthClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *c
	s.clients[c.ClientID] = &clone
	return nil
}

func (s *fakeStore) ClientByID(_ context.Context, clientID string) (*domain.OAuthClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[clientID]; ok {
		clone := *c
		return &clone, nil
	}
	return nil, domain.ErrNotFound
}

func (s *fakeStore) CreateAuthCode(_ context.Context, c *domain.AuthCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *c
	s.codes[c.Code] = &clone
	return nil
}

func (s *fakeStore) ConsumeAuthCode(_ context.Context, code string) (*domain.AuthCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[code]
	if !ok {
		return nil, domain.ErrNotFound
	}
	delete(s.codes, code)
	return c, nil
}

func (s *fakeStore) CreateRefreshToken(_ context.Context, rt *domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *rt
	s.refresh[rt.TokenHash] = &clone
	return nil
}

func (s *fakeStore) RotateRefreshToken(_ context.Context, hash string, now time.Time) (*domain.RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.refresh[hash]
	if !ok || rt.Revoked || !now.Before(rt.ExpiresAt) {
		return nil, domain.ErrNotFound
	}
	rt.Revoked = true
	clone := *rt
	return &clone, nil
}

func (s *fakeStore) RevokeTenantRefreshTokens(_ context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rt := range s.refresh {
		if rt.TenantID == tenantID {
			rt.Revoked = true
		}
	}
	return nil
}

func (s *fakeStore) CreateLoginState(_ context.Context, st *domain.LoginState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *st
	s.states[st.State] = &clone
	return nil
}

func (s *fakeStore) ConsumeLoginState(_ context.Context, state string) (*domain.LoginState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[state]
	if !ok {
		return nil, domain.ErrNotFound
	}
	delete(s.states, state)
	return st, nil
}

func (s *fakeStore) PurgeExpired(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, c := range s.codes {
		if !now.Before(c.ExpiresAt) {
			delete(s.codes, k)
		}
	}
	for k, st := range s.states {
		if !now.Before(st.ExpiresAt) {
			delete(s.states, k)
		}
	}
	return nil
}

func (s *fakeStore) Ping(context.Context) error { return nil }

func (s *fakeStore) Close() error { return nil }

// seedTenant registers a tenant with its pages in one call.
func (s *fakeStore) seedTenant(id, metaUserID, token string, pages ...domain.Page) {
	ctx := context.Background()
	_ = s.UpsertTenant(ctx, &domain.Tenant{
		ID: id, MetaUserID: metaUserID, DisplayName: "Tenant " + id, UserToken: token,
	})
	if len(pages) > 0 {
		_ = s.ReplacePages(ctx, id, pages)
	}
}
