package app

import (
	"context"
	"fmt"
	"net/url"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// ReconnectStateTTL bounds how long a reconnection link stays valid.
const ReconnectStateTTL = LoginStateTTL

// ReconnectURL builds a single use link the user opens to renew their
// Facebook authorization, typically after Meta revoked the stored token.
//
// It parks a login state exactly like /oauth/authorize does, but with an
// empty OAuth request: the callback recognizes that and shows a confirmation
// page instead of redirecting to an MCP client.
func (s *Service) ReconnectURL(ctx context.Context, tenantID string) (string, error) {
	state, err := newSecret()
	if err != nil {
		return "", fmt.Errorf("génération du state de reconnexion: %w", err)
	}
	login := &domain.LoginState{
		State:     state,
		Request:   domain.OAuthRequest{},
		ExpiresAt: s.clock.Now().Add(ReconnectStateTTL),
	}
	if err := s.store.CreateLoginState(ctx, login); err != nil {
		return "", fmt.Errorf("enregistrement du state de reconnexion: %w", err)
	}
	return s.publicURL + "/meta/login?state=" + url.QueryEscape(state), nil
}
