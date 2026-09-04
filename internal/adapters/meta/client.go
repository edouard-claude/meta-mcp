// Package meta talks to the Meta Graph API and drives the Facebook Login for
// Business flow. It is the only place in the codebase that knows Graph URLs,
// field names and error codes.
package meta

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

const (
	defaultGraphBaseURL  = "https://graph.facebook.com"
	defaultDialogBaseURL = "https://www.facebook.com"
	defaultTimeout       = 20 * time.Second
	// defaultRetryDelay is how long we wait before the single retry allowed
	// after a rate limit, when Meta sends no Retry-After header.
	defaultRetryDelay = 60 * time.Second
	// maxPages bounds pagination so a runaway cursor cannot loop forever.
	maxPages = 50
	// maxErrorBody caps how much of an unparseable body ends up in an error.
	maxErrorBody = 300
)

// Options configures the Graph client. Only AppID and AppSecret are
// mandatory; the URLs are overridden by the tests.
type Options struct {
	AppID       string
	AppSecret   string
	Version     string
	GraphBase   string
	DialogBase  string
	RedirectURI string
	Scopes      string
	HTTPClient  *http.Client
	// RetryDelay overrides the wait before the single rate-limit retry.
	RetryDelay time.Duration
}

// Client implements domain.GraphClient over the Meta Graph API.
type Client struct {
	http        *http.Client
	graphBase   string
	dialogBase  string
	version     string
	appID       string
	appSecret   string
	redirectURI string
	scopes      string
	retryDelay  time.Duration
}

// NewClient builds a Graph client from the options.
func NewClient(opts Options) *Client {
	c := &Client{
		http:        opts.HTTPClient,
		graphBase:   strings.TrimRight(orDefault(opts.GraphBase, defaultGraphBaseURL), "/"),
		dialogBase:  strings.TrimRight(orDefault(opts.DialogBase, defaultDialogBaseURL), "/"),
		version:     orDefault(opts.Version, "v26.0"),
		appID:       opts.AppID,
		appSecret:   opts.AppSecret,
		redirectURI: opts.RedirectURI,
		scopes:      opts.Scopes,
		retryDelay:  opts.RetryDelay,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: defaultTimeout}
	}
	if c.retryDelay == 0 {
		c.retryDelay = defaultRetryDelay
	}
	return c
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// endpoint builds https://graph.facebook.com/{version}/{path}.
func (c *Client) endpoint(path string) string {
	return c.graphBase + "/" + c.version + "/" + strings.TrimLeft(path, "/")
}

// appSecretProof is the HMAC-SHA256 of an access token keyed with the app
// secret. Meta uses it to prove the call comes from the application that owns
// the token, and rejects a stolen token replayed from elsewhere.
func (c *Client) appSecretProof(token string) string {
	mac := hmac.New(sha256.New, []byte(c.appSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// graphResponse is the envelope of every list endpoint.
type graphResponse struct {
	Data   []json.RawMessage `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

// errorEnvelope is the shape Meta returns on failure.
type errorEnvelope struct {
	Error struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		Code      int    `json:"code"`
		Subcode   int    `json:"error_subcode"`
		FBTraceID string `json:"fbtrace_id"`
	} `json:"error"`
}

// get issues an authenticated GET and decodes the body into out.
func (c *Client) get(ctx context.Context, token, path string, params url.Values, out any) error {
	body, err := c.call(ctx, http.MethodGet, c.endpoint(path), token, params)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

// post issues an authenticated POST with form parameters.
func (c *Client) post(ctx context.Context, token, path string, params url.Values, out any) error {
	body, err := c.call(ctx, http.MethodPost, c.endpoint(path), token, params)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

func decodeInto(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("décodage de la réponse Meta: %w", err)
	}
	return nil
}

// call performs the request, retrying once on a rate limit as allowed by the
// SPEC, and turns any non-2xx answer into a *domain.GraphError.
func (c *Client) call(ctx context.Context, method, rawURL, token string, params url.Values) ([]byte, error) {
	body, err := c.callOnce(ctx, method, rawURL, token, params)
	if err == nil {
		return body, nil
	}

	var ge *domain.GraphError
	if !errors.As(err, &ge) || !ge.IsRateLimit() {
		return nil, err
	}
	// A single retry, after Retry-After when Meta bothered to send one.
	delay := ge.RetryAfter
	if delay <= 0 {
		delay = c.retryDelay
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
	}
	return c.callOnce(ctx, method, rawURL, token, params)
}

func (c *Client) callOnce(ctx context.Context, method, rawURL, token string, params url.Values) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	if token != "" {
		params.Set("access_token", token)
		if c.appSecret != "" {
			params.Set("appsecret_proof", c.appSecretProof(token))
		}
	}

	var (
		req *http.Request
		err error
	)
	if method == http.MethodGet {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		req, err = http.NewRequestWithContext(ctx, method, rawURL+sep+params.Encode(), nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(params.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("construction de la requête Meta: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("appel de l'API Meta: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("lecture de la réponse Meta: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, graphErrorFrom(resp, body)
	}
	return body, nil
}

// graphErrorFrom decodes Meta's error envelope, falling back to a truncated
// body when the answer is not the expected JSON.
func graphErrorFrom(resp *http.Response, body []byte) error {
	ge := &domain.GraphError{HTTPStatus: resp.StatusCode}

	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		ge.Code = env.Error.Code
		ge.Subcode = env.Error.Subcode
		ge.Type = env.Error.Type
		ge.Message = env.Error.Message
		ge.TraceID = env.Error.FBTraceID
	} else {
		ge.Message = truncate(string(body), maxErrorBody)
	}

	if after := resp.Header.Get("Retry-After"); after != "" {
		if secs, err := strconv.Atoi(after); err == nil && secs > 0 {
			ge.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return ge
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// collect walks a paginated endpoint, following paging.next until the limit
// is reached, the cursor runs out, or maxPages is hit.
func (c *Client) collect(ctx context.Context, token, path string, params url.Values, limit int) ([]json.RawMessage, error) {
	body, err := c.call(ctx, http.MethodGet, c.endpoint(path), token, params)
	if err != nil {
		return nil, err
	}

	items := []json.RawMessage{}
	for page := 0; page < maxPages; page++ {
		var resp graphResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("décodage de la page %d: %w", page, err)
		}
		items = append(items, resp.Data...)
		if limit > 0 && len(items) >= limit {
			return items[:limit], nil
		}
		if resp.Paging.Next == "" {
			return items, nil
		}
		// paging.next already carries every parameter, credentials included.
		if body, err = c.call(ctx, http.MethodGet, resp.Paging.Next, "", nil); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// unixParams adds a since/until window to a query when the bounds are set.
func unixParams(params url.Values, since, until time.Time) url.Values {
	if !since.IsZero() {
		params.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	if !until.IsZero() {
		params.Set("until", strconv.FormatInt(until.Unix(), 10))
	}
	return params
}
