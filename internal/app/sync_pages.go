package app

import "context"

// SyncPages re-reads the pages from Meta and replaces the stored ones, so a
// page the user just created, or just lost access to, is reflected.
func (s *Service) SyncPages(ctx context.Context, tenantID string) ([]PageView, error) {
	tenant, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	pages, err := syncPages(ctx, s.store, s.graph, tenant.ID, tenant.UserToken)
	if err != nil {
		return nil, err
	}
	return toPageViews(pages), nil
}
