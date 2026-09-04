package app

import (
	"context"
	"fmt"
)

// ListPages returns the pages already known for a tenant, straight from the
// store: no Graph call, so it answers instantly.
func (s *Service) ListPages(ctx context.Context, tenantID string) ([]PageView, error) {
	pages, err := s.store.ListPages(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("liste des pages: %w", err)
	}
	return toPageViews(pages), nil
}
