// Package config reads and validates the runtime configuration from the
// environment. The binary refuses to start on an invalid configuration rather
// than failing later on the first request.
package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment variable names, in the order of the SPEC table.
const (
	EnvPublicURL          = "PUBLIC_URL"
	EnvListenAddr         = "LISTEN_ADDR"
	EnvDBPath             = "DB_PATH"
	EnvTokenCipherKey     = "TOKEN_CIPHER_KEY"
	EnvJWTSigningKey      = "JWT_SIGNING_KEY"
	EnvMetaAppID          = "META_APP_ID"
	EnvMetaAppSecret      = "META_APP_SECRET"
	EnvMetaGraphVersion   = "META_GRAPH_VERSION"
	EnvMetaScopes         = "META_SCOPES"
	EnvAccessTokenTTL     = "ACCESS_TOKEN_TTL"
	EnvRefreshTokenTTL    = "REFRESH_TOKEN_TTL"
	EnvLogFormat          = "LOG_FORMAT"
	EnvAllowedMetaUserIDs = "ALLOWED_META_USER_IDS"

	// EnvGraphBaseURL and EnvDialogBaseURL point the Graph client somewhere
	// else than Meta. They exist for the end to end test, which runs the
	// real binary against a fake Graph, and are not meant for production.
	EnvGraphBaseURL  = "META_GRAPH_BASE_URL"
	EnvDialogBaseURL = "META_DIALOG_BASE_URL"
)

const (
	defaultListenAddr      = ":8080"
	defaultDBPath          = "/data/metasocial.db"
	defaultGraphVersion    = "v26.0"
	defaultAccessTokenTTL  = time.Hour
	defaultRefreshTokenTTL = 720 * time.Hour
	defaultLogFormat       = "json"

	// DefaultMetaScopes is the permission set the Facebook login dialog asks
	// for. Reading insights and publishing organic content both need it.
	DefaultMetaScopes = "pages_show_list,pages_read_engagement,pages_manage_posts," +
		"pages_read_user_content,pages_manage_engagement,read_insights,instagram_basic," +
		"instagram_manage_insights,instagram_content_publish,instagram_manage_comments," +
		"business_management"

	// cipherKeyLen is the AES-256 key size.
	cipherKeyLen = 32
	// minSigningKeyLen is the minimum HMAC-SHA256 key size.
	minSigningKeyLen = 32
)

// Config is the validated configuration of the process.
type Config struct {
	PublicURL       string
	ListenAddr      string
	DBPath          string
	TokenCipherKey  []byte
	JWTSigningKey   []byte
	MetaAppID       string
	MetaAppSecret   string
	GraphVersion    string
	MetaScopes      string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	LogFormat       string

	// GraphBaseURL and DialogBaseURL override the Meta endpoints. Empty in
	// production, where the client uses graph.facebook.com and
	// www.facebook.com.
	GraphBaseURL  string
	DialogBaseURL string

	// AllowedMetaUserIDs, when non-empty, is the whitelist of Facebook user
	// ids allowed to create a tenant.
	AllowedMetaUserIDs map[string]struct{}
}

// MCPResourceURL is the canonical identifier of the protected resource, used
// as the JWT audience and as the RFC 8707 resource indicator.
func (c *Config) MCPResourceURL() string { return c.PublicURL + "/mcp" }

// MetaRedirectURI is the redirect registered in the Meta app.
func (c *Config) MetaRedirectURI() string { return c.PublicURL + "/meta/callback" }

// ResourceMetadataURL is advertised in the WWW-Authenticate header of a 401.
func (c *Config) ResourceMetadataURL() string {
	return c.PublicURL + "/.well-known/oauth-protected-resource"
}

// IsMetaUserAllowed reports whether a Facebook user id may create a tenant.
func (c *Config) IsMetaUserAllowed(metaUserID string) bool {
	if len(c.AllowedMetaUserIDs) == 0 {
		return true
	}
	_, ok := c.AllowedMetaUserIDs[metaUserID]
	return ok
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:         env(EnvListenAddr, defaultListenAddr),
		DBPath:             env(EnvDBPath, defaultDBPath),
		MetaAppID:          strings.TrimSpace(os.Getenv(EnvMetaAppID)),
		MetaAppSecret:      strings.TrimSpace(os.Getenv(EnvMetaAppSecret)),
		GraphVersion:       env(EnvMetaGraphVersion, defaultGraphVersion),
		MetaScopes:         env(EnvMetaScopes, DefaultMetaScopes),
		LogFormat:          env(EnvLogFormat, defaultLogFormat),
		GraphBaseURL:       strings.TrimRight(os.Getenv(EnvGraphBaseURL), "/"),
		DialogBaseURL:      strings.TrimRight(os.Getenv(EnvDialogBaseURL), "/"),
		AllowedMetaUserIDs: parseCSVSet(os.Getenv(EnvAllowedMetaUserIDs)),
	}

	publicURL, err := parsePublicURL(os.Getenv(EnvPublicURL))
	if err != nil {
		return nil, err
	}
	cfg.PublicURL = publicURL

	if cfg.MetaAppID == "" {
		return nil, fmt.Errorf("%s est obligatoire", EnvMetaAppID)
	}
	if cfg.MetaAppSecret == "" {
		return nil, fmt.Errorf("%s est obligatoire", EnvMetaAppSecret)
	}

	if cfg.TokenCipherKey, err = decodeKey(EnvTokenCipherKey, cipherKeyLen, true); err != nil {
		return nil, err
	}
	if cfg.JWTSigningKey, err = decodeKey(EnvJWTSigningKey, minSigningKeyLen, false); err != nil {
		return nil, err
	}

	if cfg.AccessTokenTTL, err = parseDuration(EnvAccessTokenTTL, defaultAccessTokenTTL); err != nil {
		return nil, err
	}
	if cfg.RefreshTokenTTL, err = parseDuration(EnvRefreshTokenTTL, defaultRefreshTokenTTL); err != nil {
		return nil, err
	}

	switch cfg.LogFormat {
	case "json", "text":
	default:
		return nil, fmt.Errorf("%s doit valoir json ou text, pas %q", EnvLogFormat, cfg.LogFormat)
	}

	if !strings.HasPrefix(cfg.GraphVersion, "v") {
		return nil, fmt.Errorf("%s doit ressembler à v26.0, pas %q", EnvMetaGraphVersion, cfg.GraphVersion)
	}

	return cfg, nil
}

// parsePublicURL enforces an absolute https URL without a trailing slash: the
// whole OAuth surface derives its URLs from it, and OAuth 2.1 forbids plain
// HTTP for a public issuer.
func parsePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%s est obligatoire", EnvPublicURL)
	}
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s invalide: %w", EnvPublicURL, err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("%s doit être en https://, pas %q", EnvPublicURL, u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s doit contenir un hôte", EnvPublicURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%s ne doit contenir ni query ni fragment", EnvPublicURL)
	}
	return raw, nil
}

// decodeKey decodes a base64 key and checks its size. When exact is true the
// length must match exactly, otherwise it is a minimum.
func decodeKey(name string, size int, exact bool) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("%s est obligatoire", name)
	}
	key, err := decodeBase64(raw)
	if err != nil {
		return nil, fmt.Errorf("%s doit être encodé en base64: %w", name, err)
	}
	if exact && len(key) != size {
		return nil, fmt.Errorf("%s doit faire exactement %d octets, pas %d", name, size, len(key))
	}
	if !exact && len(key) < size {
		return nil, fmt.Errorf("%s doit faire au moins %d octets, pas %d", name, size, len(key))
	}
	return key, nil
}

// decodeBase64 accepts both the standard and the URL-safe alphabet, padded or
// not, because key material gets copied around by hand.
func decodeBase64(raw string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	var err error
	for _, enc := range encodings {
		var out []byte
		if out, err = enc.DecodeString(raw); err == nil {
			return out, nil
		}
	}
	return nil, err
}

func parseDuration(name string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s invalide: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s doit être strictement positif", name)
	}
	return d, nil
}

func env(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func parseCSVSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out[p] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
