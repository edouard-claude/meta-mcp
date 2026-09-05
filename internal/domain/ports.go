package domain

import (
	"context"
	"time"
)

// Clock abstracts time so that expiry logic is testable.
type Clock interface {
	Now() time.Time
}

// TokenCipher seals and opens the Meta tokens stored in the database.
type TokenCipher interface {
	Encrypt(plaintext string) ([]byte, error)
	Decrypt(ciphertext []byte) (string, error)
}

// TenantStore persists tenants, their pages and the whole OAuth state.
//
// Every page-scoped method takes a tenantID: there is deliberately no method
// that resolves a page_id on its own, so a handler cannot accidentally reach
// across tenants.
type TenantStore interface {
	UpsertTenant(ctx context.Context, t *Tenant) error
	TenantByID(ctx context.Context, id string) (*Tenant, error)
	TenantByMetaUserID(ctx context.Context, metaUserID string) (*Tenant, error)
	DeleteTenant(ctx context.Context, id string) error
	// TenantsDueForTokenRefresh lists the tenants whose Meta user token dies
	// before deadline, plus those whose deadline is unknown.
	TenantsDueForTokenRefresh(ctx context.Context, deadline time.Time) ([]Tenant, error)

	ReplacePages(ctx context.Context, tenantID string, pages []Page) error
	ListPages(ctx context.Context, tenantID string) ([]Page, error)
	PageByID(ctx context.Context, tenantID, pageID string) (*Page, error)

	RegisterClient(ctx context.Context, c *OAuthClient) error
	ClientByID(ctx context.Context, clientID string) (*OAuthClient, error)

	CreateAuthCode(ctx context.Context, c *AuthCode) error
	// ConsumeAuthCode atomically deletes and returns the code. A second call
	// with the same code returns ErrNotFound, which is what makes codes
	// single-use.
	ConsumeAuthCode(ctx context.Context, code string) (*AuthCode, error)

	CreateRefreshToken(ctx context.Context, rt *RefreshToken) error
	// RotateRefreshToken atomically revokes the presented token and returns
	// it. An already revoked or expired token yields ErrNotFound.
	RotateRefreshToken(ctx context.Context, tokenHash string, now time.Time) (*RefreshToken, error)
	RevokeTenantRefreshTokens(ctx context.Context, tenantID string) error

	CreateLoginState(ctx context.Context, s *LoginState) error
	ConsumeLoginState(ctx context.Context, state string) (*LoginState, error)

	// PurgeExpired removes every expired code, login state and refresh token.
	PurgeExpired(ctx context.Context, now time.Time) error

	Ping(ctx context.Context) error
	Close() error
}

// LongLivedToken is what Meta hands back for a long-lived user token.
type LongLivedToken struct {
	Token string
	// ExpiresIn is how long Meta says the token lasts. Zero means Meta did
	// not say, which happens for tokens it considers non-expiring.
	ExpiresIn time.Duration
}

// TokenStatus is what /debug_token says about a stored token.
type TokenStatus struct {
	Valid               bool
	ExpiresAt           time.Time
	DataAccessExpiresAt time.Time
	Scopes              []string
	// Reason is Meta's explanation when the token is not valid.
	Reason string
}

// MetaOAuthClient is the slice of the Graph API the login flow needs. It is a
// port of its own so the login use case, and its tests, never have to know
// about insights or publishing.
type MetaOAuthClient interface {
	// AuthorizeURL builds the Facebook login dialog URL.
	AuthorizeURL(redirectURI, state string) string
	// ExchangeCode trades an authorization code for a short-lived user token.
	ExchangeCode(ctx context.Context, code, redirectURI string) (string, error)
	// ExchangeLongLivedToken trades a token for a long-lived one. It accepts
	// a short-lived token after login, and a still valid long-lived token to
	// push the deadline back, which is how a session survives past 60 days.
	ExchangeLongLivedToken(ctx context.Context, token string) (LongLivedToken, error)
	// Me returns the identity behind a user token.
	Me(ctx context.Context, userToken string) (MetaUser, error)
	// Accounts lists the pages the user administers, with their page tokens
	// and linked Instagram accounts, following pagination to the end.
	Accounts(ctx context.Context, userToken string) ([]Page, error)
	// DebugToken reports whether a token is still usable, when it expires,
	// and which permissions it actually carries.
	DebugToken(ctx context.Context, token string) (TokenStatus, error)
}

// GraphClient is everything the application needs from the Meta Graph API.
// Implementations translate transport failures into *GraphError.
type GraphClient interface {
	MetaOAuthClient

	// --- Facebook Page, read ---

	PageInsights(ctx context.Context, pageToken, pageID string, metrics []string, since, until time.Time) (InsightSet, error)
	PagePosts(ctx context.Context, pageToken, pageID string, since time.Time, limit int) ([]Post, error)
	PostComments(ctx context.Context, pageToken, postID string, limit int) ([]Comment, error)
	PostInsights(ctx context.Context, pageToken, postID string, metrics []string) (InsightSet, error)
	ScheduledPosts(ctx context.Context, pageToken, pageID string, limit int) ([]ScheduledPost, error)

	// --- Facebook Page, write ---

	PublishPost(ctx context.Context, pageToken, pageID string, req PublishPostRequest) (string, error)
	ReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error)
	// SetCommentHidden hides or unhides a comment. Facebook and Instagram
	// spell the parameter differently, which implementations absorb.
	SetCommentHidden(ctx context.Context, pageToken, commentID string, hidden, instagram bool) error
	DeleteObject(ctx context.Context, pageToken, objectID string) error

	// --- Instagram, read ---

	IGAccountInsights(ctx context.Context, pageToken, igUserID string, metrics []string, since, until time.Time) (InsightSet, error)
	IGFollowerDemographics(ctx context.Context, pageToken, igUserID, breakdown string) ([]Breakdown, error)
	IGMedia(ctx context.Context, pageToken, igUserID string, since time.Time, limit int) ([]Media, error)
	IGMediaComments(ctx context.Context, pageToken, mediaID string, limit int) ([]Comment, error)
	IGMediaInsights(ctx context.Context, pageToken, mediaID string, metrics []string) (InsightSet, error)
	// IGStories lists the stories still live on the account; Meta drops them
	// from this edge once they are 24 hours old.
	IGStories(ctx context.Context, pageToken, igUserID string) ([]Media, error)

	// --- Instagram, write ---

	IGPublish(ctx context.Context, pageToken, igUserID string, req IGPublishRequest) (string, error)
	IGReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error)
}
