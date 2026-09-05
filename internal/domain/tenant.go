// Package domain holds the entities and the ports (interfaces) of the
// application. It depends on nothing but the standard library: no HTTP, no
// SQL, no Meta Graph API.
package domain

import "time"

// Tenant is one end user of the server. Every piece of data in the system is
// owned by exactly one tenant and is never visible to another.
type Tenant struct {
	ID          string    `json:"id"`
	MetaUserID  string    `json:"meta_user_id"`
	DisplayName string    `json:"display_name"`
	UserToken   string    `json:"-"` // long-lived Meta user token, never serialized
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// UserTokenExpiresAt is when Meta says the user token dies. The zero
	// value means unknown, which the refresh sweep treats as "renew now".
	UserTokenExpiresAt time.Time `json:"user_token_expires_at,omitzero"`
}

// TokenExpiresWithin reports whether the user token is due for renewal.
func (t *Tenant) TokenExpiresWithin(now time.Time, window time.Duration) bool {
	if t.UserTokenExpiresAt.IsZero() {
		return true
	}
	return t.UserTokenExpiresAt.Before(now.Add(window))
}

// Page is a Facebook Page owned by a tenant, together with the Instagram
// Business/Creator account linked to it, when there is one.
type Page struct {
	TenantID   string    `json:"-"`
	PageID     string    `json:"page_id"`
	Name       string    `json:"name"`
	IGUserID   string    `json:"ig_user_id,omitempty"`
	IGUsername string    `json:"ig_username,omitempty"`
	PageToken  string    `json:"-"` // page access token, never serialized
	SyncedAt   time.Time `json:"synced_at"`
}

// HasInstagram reports whether an Instagram account is linked to the page.
func (p *Page) HasInstagram() bool { return p.IGUserID != "" }

// MetaUser is the identity returned by GET /me.
type MetaUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OAuthClient is an MCP client registered through dynamic client registration.
type OAuthClient struct {
	ClientID     string
	ClientName   string
	RedirectURIs []string
	CreatedAt    time.Time
}

// AllowsRedirectURI reports whether uri is one of the exact URIs registered by
// the client. Comparison is exact, as required by OAuth 2.1.
func (c *OAuthClient) AllowsRedirectURI(uri string) bool {
	for _, u := range c.RedirectURIs {
		if u == uri {
			return true
		}
	}
	return false
}

// AuthCode is a single-use OAuth 2.1 authorization code bound to a PKCE
// challenge and to the tenant that authenticated at Meta.
type AuthCode struct {
	Code          string
	ClientID      string
	TenantID      string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	ExpiresAt     time.Time
}

// RefreshToken is stored hashed; the plaintext only ever exists in the token
// response sent to the client.
type RefreshToken struct {
	TokenHash string
	ClientID  string
	TenantID  string
	ExpiresAt time.Time
	Revoked   bool
}

// LoginState carries the pending MCP authorization request across the Meta
// login round trip. It doubles as the CSRF token of the Meta OAuth flow.
type LoginState struct {
	State     string
	Request   OAuthRequest
	ExpiresAt time.Time
}

// OAuthRequest is the MCP client's /oauth/authorize request, parked while the
// user authenticates against Facebook.
type OAuthRequest struct {
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
	ClientState   string `json:"client_state"`
	Resource      string `json:"resource"`
}
