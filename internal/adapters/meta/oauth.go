package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

var _ domain.MetaOAuthClient = (*Client)(nil)

// AuthorizeURL builds the Facebook login dialog URL the user is sent to.
func (c *Client) AuthorizeURL(redirectURI, state string) string {
	q := url.Values{
		"client_id":     {c.appID},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"scope":         {c.scopes},
		"response_type": {"code"},
	}
	return c.dialogBase + "/" + c.version + "/dialog/oauth?" + q.Encode()
}

// tokenResponse is the shape of both /oauth/access_token answers.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// ExchangeCode trades the authorization code Facebook redirected with for a
// short-lived user access token.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	params := url.Values{
		"client_id":     {c.appID},
		"client_secret": {c.appSecret},
		"redirect_uri":  {redirectURI},
		"code":          {code},
	}
	var resp tokenResponse
	// No access token yet, so no bearer and no appsecret_proof here.
	if err := c.get(ctx, "", "oauth/access_token", params, &resp); err != nil {
		return "", fmt.Errorf("échange du code Meta: %w", err)
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("échange du code Meta: aucun access_token dans la réponse")
	}
	return resp.AccessToken, nil
}

// ExchangeLongLivedToken upgrades a short-lived user token to the 60 day one
// we actually store.
func (c *Client) ExchangeLongLivedToken(ctx context.Context, shortToken string) (string, error) {
	params := url.Values{
		"grant_type":        {"fb_exchange_token"},
		"client_id":         {c.appID},
		"client_secret":     {c.appSecret},
		"fb_exchange_token": {shortToken},
	}
	var resp tokenResponse
	if err := c.get(ctx, "", "oauth/access_token", params, &resp); err != nil {
		return "", fmt.Errorf("obtention du jeton longue durée: %w", err)
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("obtention du jeton longue durée: aucun access_token dans la réponse")
	}
	return resp.AccessToken, nil
}

// Me returns the Facebook identity behind a user token.
func (c *Client) Me(ctx context.Context, userToken string) (domain.MetaUser, error) {
	var user domain.MetaUser
	params := url.Values{"fields": {"id,name"}}
	if err := c.get(ctx, userToken, "me", params, &user); err != nil {
		return domain.MetaUser{}, fmt.Errorf("lecture du profil Meta: %w", err)
	}
	if user.ID == "" {
		return domain.MetaUser{}, fmt.Errorf("lecture du profil Meta: identifiant absent")
	}
	return user, nil
}

// accountItem is one entry of /me/accounts.
type accountItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
	Instagram   struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"instagram_business_account"`
}

// accountFields is the field set requested from /me/accounts: everything the
// server needs about a page in a single round trip.
const accountFields = "id,name,access_token,instagram_business_account{id,username}"

// Accounts lists every page the user administers, following pagination to the
// end. Pages without a page token are skipped: nothing can be done with them.
func (c *Client) Accounts(ctx context.Context, userToken string) ([]domain.Page, error) {
	params := url.Values{
		"fields": {accountFields},
		"limit":  {"100"},
	}
	items, err := c.collect(ctx, userToken, "me/accounts", params, 0)
	if err != nil {
		return nil, fmt.Errorf("lecture des pages Meta: %w", err)
	}

	now := time.Now().UTC()
	pages := make([]domain.Page, 0, len(items))
	for _, raw := range items {
		var item accountItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'une page Meta: %w", err)
		}
		if item.ID == "" || item.AccessToken == "" {
			continue
		}
		pages = append(pages, domain.Page{
			PageID:     item.ID,
			Name:       item.Name,
			IGUserID:   item.Instagram.ID,
			IGUsername: item.Instagram.Username,
			PageToken:  item.AccessToken,
			SyncedAt:   now,
		})
	}
	return pages, nil
}
