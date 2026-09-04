package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/edouard/metasocial-mcp/internal/domain"
)

// defaultWindowDays is the reporting window used when the caller gives no
// since/until: the last four weeks.
const defaultWindowDays = 28

// dayLayout is the date format every tool accepts.
const dayLayout = "2006-01-02"

// LoginStateTTL bounds how long a parked login request stays valid. It
// mirrors the value the authorization server uses for its own states.
const LoginStateTTL = 10 * time.Minute

// Service implements the MCP tools. Every method takes the tenant id resolved
// from the bearer token, and no method can reach data outside that tenant.
type Service struct {
	store domain.TenantStore
	graph domain.GraphClient
	clock domain.Clock
	// publicURL is the base URL of this server, used to build the
	// reconnection link.
	publicURL string
}

// NewService wires the tool use cases.
func NewService(store domain.TenantStore, graph domain.GraphClient, clk domain.Clock, publicURL string) *Service {
	return &Service{store: store, graph: graph, clock: clk, publicURL: publicURL}
}

// PageView is what the tools return about a page. It deliberately carries no
// token.
type PageView struct {
	PageID     string `json:"page_id"`
	Name       string `json:"name"`
	IGUserID   string `json:"ig_user_id,omitempty"`
	IGUsername string `json:"ig_username,omitempty"`
	SyncedAt   string `json:"synced_at"`
}

func toPageViews(pages []domain.Page) []PageView {
	out := make([]PageView, 0, len(pages))
	for _, p := range pages {
		out = append(out, PageView{
			PageID:     p.PageID,
			Name:       p.Name,
			IGUserID:   p.IGUserID,
			IGUsername: p.IGUsername,
			SyncedAt:   p.SyncedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// page resolves a page_id given by the client, always scoped to the tenant.
// A page id the tenant does not own is reported as unknown, never as
// forbidden, so nothing can be learned about other tenants.
func (s *Service) page(ctx context.Context, tenantID, pageID string) (*domain.Page, error) {
	if strings.TrimSpace(pageID) == "" {
		return nil, errors.New("page_id est obligatoire")
	}
	page, err := s.store.PageByID(ctx, tenantID, pageID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrUnknownPage
	}
	if err != nil {
		return nil, fmt.Errorf("lecture de la page: %w", err)
	}
	return page, nil
}

// igPage resolves a page and checks it has a linked Instagram account.
func (s *Service) igPage(ctx context.Context, tenantID, pageID string) (*domain.Page, error) {
	page, err := s.page(ctx, tenantID, pageID)
	if err != nil {
		return nil, err
	}
	if !page.HasInstagram() {
		return nil, domain.ErrNoInstagram
	}
	return page, nil
}

// ownerPage finds which page of the tenant owns a Graph object.
//
// Meta object ids are not self-describing, so three strategies are tried in
// order: the page_id the caller supplied, the "{page-id}_{object-id}" prefix
// Facebook uses for posts and comments, and finally the single page of a
// tenant that only administers one. Otherwise the caller is asked to be
// explicit, rather than the server guessing across pages.
func (s *Service) ownerPage(ctx context.Context, tenantID, pageIDHint, objectID string) (*domain.Page, error) {
	if strings.TrimSpace(objectID) == "" {
		return nil, errors.New("identifiant de l'objet manquant")
	}
	if pageIDHint != "" {
		return s.page(ctx, tenantID, pageIDHint)
	}

	if prefix, _, ok := strings.Cut(objectID, "_"); ok && prefix != "" {
		page, err := s.store.PageByID(ctx, tenantID, prefix)
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("lecture de la page: %w", err)
		}
	}

	pages, err := s.store.ListPages(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("liste des pages: %w", err)
	}
	if len(pages) == 1 {
		return &pages[0], nil
	}
	if len(pages) == 0 {
		return nil, errors.New("aucune page connectée, utilisez sync_pages")
	}
	return nil, errors.New("impossible de déterminer la page concernée, précisez page_id")
}

// ownerIGPage is ownerPage restricted to pages with an Instagram account. An
// Instagram media or comment id carries no page prefix, so the caller usually
// has to pass page_id when several pages are connected.
func (s *Service) ownerIGPage(ctx context.Context, tenantID, pageIDHint, objectID string) (*domain.Page, error) {
	if pageIDHint != "" {
		return s.igPage(ctx, tenantID, pageIDHint)
	}
	pages, err := s.store.ListPages(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("liste des pages: %w", err)
	}
	var withIG []domain.Page
	for _, p := range pages {
		if p.HasInstagram() {
			withIG = append(withIG, p)
		}
	}
	switch len(withIG) {
	case 0:
		return nil, domain.ErrNoInstagram
	case 1:
		return &withIG[0], nil
	default:
		return nil, errors.New("plusieurs comptes Instagram sont connectés, précisez page_id")
	}
}

// dateWindow turns the optional since/until parameters into a time range,
// defaulting to the last 28 days.
func (s *Service) dateWindow(since, until string) (time.Time, time.Time, error) {
	now := s.clock.Now()

	end := now
	if until != "" {
		parsed, err := parseDay(until, "until")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		// Include the whole day the caller asked for.
		end = parsed.Add(24*time.Hour - time.Second)
	}

	start := end.AddDate(0, 0, -defaultWindowDays)
	if since != "" {
		parsed, err := parseDay(since, "since")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start = parsed
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, errors.New("since doit être antérieur à until")
	}
	return start, end, nil
}

func parseDay(value, name string) (time.Time, error) {
	day, err := time.ParseInLocation(dayLayout, value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s doit être au format AAAA-MM-JJ", name)
	}
	return day, nil
}

// metricsOrDefault falls back to the default metric set when the caller gave
// none.
func metricsOrDefault(requested, fallback []string) []string {
	cleaned := make([]string, 0, len(requested))
	for _, m := range requested {
		if m = strings.TrimSpace(m); m != "" {
			cleaned = append(cleaned, m)
		}
	}
	if len(cleaned) == 0 {
		return fallback
	}
	return cleaned
}

// clampLimit keeps a caller supplied limit inside the accepted range.
func clampLimit(limit, def, max int) int {
	switch {
	case limit <= 0:
		return def
	case limit > max:
		return max
	default:
		return limit
	}
}

// syncPages refreshes the pages of a tenant from the Graph API. It is shared
// by the login flow and by the sync_pages tool.
func syncPages(ctx context.Context, store domain.TenantStore, meta domain.MetaOAuthClient, tenantID, userToken string) ([]domain.Page, error) {
	pages, err := meta.Accounts(ctx, userToken)
	if err != nil {
		return nil, err
	}
	if err := store.ReplacePages(ctx, tenantID, pages); err != nil {
		return nil, fmt.Errorf("enregistrement des pages: %w", err)
	}
	return pages, nil
}
