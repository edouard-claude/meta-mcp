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

// GraphClient is everything the application needs from the Meta Graph API.
// Implementations translate transport failures into *GraphError.
type GraphClient interface {
	// --- OAuth ---

	// AuthorizeURL builds the Facebook login dialog URL.
	AuthorizeURL(redirectURI, state string) string
	// ExchangeCode trades an authorization code for a short-lived user token.
	ExchangeCode(ctx context.Context, code, redirectURI string) (string, error)
	// ExchangeLongLivedToken trades a short-lived user token for a 60 day one.
	ExchangeLongLivedToken(ctx context.Context, shortToken string) (string, error)
	// Me returns the identity behind a user token.
	Me(ctx context.Context, userToken string) (MetaUser, error)
	// Accounts lists the pages the user administers, with their page tokens
	// and linked Instagram accounts, following pagination to the end.
	Accounts(ctx context.Context, userToken string) ([]Page, error)

	// --- Facebook Page, read ---

	PageInsights(ctx context.Context, pageToken, pageID string, metrics []string, since, until time.Time) ([]Insight, error)
	PageInsightsMetadata(ctx context.Context, pageToken, pageID string) ([]InsightMeta, error)
	PagePosts(ctx context.Context, pageToken, pageID string, since time.Time, limit int) ([]Post, error)
	PostComments(ctx context.Context, pageToken, postID string, limit int) ([]Comment, error)

	// --- Facebook Page, write ---

	PublishPost(ctx context.Context, pageToken, pageID string, req PublishPostRequest) (string, error)
	ReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error)

	// --- Instagram, read ---

	IGAccountInsights(ctx context.Context, pageToken, igUserID string, metrics []string, since, until time.Time) ([]Insight, error)
	IGFollowerDemographics(ctx context.Context, pageToken, igUserID, breakdown string) ([]Breakdown, error)
	IGMedia(ctx context.Context, pageToken, igUserID string, since time.Time, limit int) ([]Media, error)
	IGMediaComments(ctx context.Context, pageToken, mediaID string, limit int) ([]Comment, error)

	// --- Instagram, write ---

	IGPublish(ctx context.Context, pageToken, igUserID string, req IGPublishRequest) (string, error)
	IGReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error)
}
