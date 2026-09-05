package app

import (
	"context"
	"fmt"
	"slices"
	"time"
)

// ConnectionStatus is what the connection_status tool reports.
type ConnectionStatus struct {
	DisplayName string `json:"display_name"`
	Healthy     bool   `json:"healthy"`
	// Summary is a sentence the assistant can read out as is.
	Summary string `json:"summary"`

	TokenValid     bool   `json:"token_valid"`
	TokenExpiresAt string `json:"token_expires_at,omitempty"`
	DaysRemaining  int    `json:"days_remaining,omitempty"`
	TokenReason    string `json:"token_reason,omitempty"`

	Pages           int      `json:"pages"`
	PagesWithIG     int      `json:"pages_with_instagram"`
	MissingScopes   []string `json:"missing_scopes,omitempty"`
	ReconnectionURL string   `json:"reconnect_url,omitempty"`
}

// ConnectionStatus tells the user whether their Facebook authorization is
// still good, when it dies, and what is missing.
//
// It exists because the failure mode of this server is silent: a revoked or
// expired token only surfaces when a tool happens to be called. Asking Meta
// directly turns that into something a user can check and act on.
func (s *Service) ConnectionStatus(ctx context.Context, tenantID string) (*ConnectionStatus, error) {
	tenant, err := s.store.TenantByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	pages, err := s.store.ListPages(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("liste des pages: %w", err)
	}

	status := &ConnectionStatus{DisplayName: tenant.DisplayName, Pages: len(pages)}
	for _, p := range pages {
		if p.HasInstagram() {
			status.PagesWithIG++
		}
	}

	token, err := s.graph.DebugToken(ctx, tenant.UserToken)
	if err != nil {
		// Meta could not be reached. Report what the database knows rather
		// than failing: the stored deadline is still useful information.
		status.TokenValid = false
		status.TokenReason = "impossible de joindre Meta pour vérifier le jeton"
		status.fill(s.clock.Now(), tenant.UserTokenExpiresAt, nil, s.requiredScopes)
		s.attachReconnectLink(ctx, tenantID, status)
		return status, nil
	}

	status.TokenValid = token.Valid
	status.TokenReason = token.Reason
	expiry := token.ExpiresAt
	if expiry.IsZero() {
		expiry = tenant.UserTokenExpiresAt
	}
	status.fill(s.clock.Now(), expiry, token.Scopes, s.requiredScopes)
	s.attachReconnectLink(ctx, tenantID, status)
	return status, nil
}

// attachReconnectLink mints a usable single-use link, but only when something
// is actually wrong: minting one on every status check would litter the table
// with states nobody follows.
func (s *Service) attachReconnectLink(ctx context.Context, tenantID string, status *ConnectionStatus) {
	if status.Healthy {
		return
	}
	if link, err := s.ReconnectURL(ctx, tenantID); err == nil {
		status.ReconnectionURL = link
	}
}

// fill computes the derived fields shared by both paths.
func (c *ConnectionStatus) fill(now, expiry time.Time, granted, required []string) {
	if !expiry.IsZero() {
		c.TokenExpiresAt = expiry.UTC().Format(time.RFC3339)
		c.DaysRemaining = int(expiry.Sub(now).Hours() / 24)
	}
	for _, scope := range required {
		if len(granted) > 0 && !slices.Contains(granted, scope) {
			c.MissingScopes = append(c.MissingScopes, scope)
		}
	}

	c.Healthy = c.TokenValid && len(c.MissingScopes) == 0
	switch {
	case !c.TokenValid:
		c.Summary = "L'autorisation Facebook n'est plus valide, une reconnexion est nécessaire."
	case len(c.MissingScopes) > 0:
		c.Summary = fmt.Sprintf("Connexion active mais %d permission(s) manquante(s) : certaines fonctions échoueront.",
			len(c.MissingScopes))
	case c.Pages == 0:
		c.Summary = "Connexion valide, mais aucune page n'est synchronisée. Utilisez sync_pages."
	case c.TokenExpiresAt == "":
		c.Summary = fmt.Sprintf("Connexion valide, %d page(s), sans date d'expiration connue.", c.Pages)
	default:
		c.Summary = fmt.Sprintf("Connexion valide, %d page(s), jeton valable encore %d jour(s).",
			c.Pages, c.DaysRemaining)
	}
}
