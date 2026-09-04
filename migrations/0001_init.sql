-- Tenants: one row per end user who logged in with Facebook.
CREATE TABLE tenants (
  id             TEXT PRIMARY KEY,           -- uuid v4
  meta_user_id   TEXT NOT NULL UNIQUE,
  display_name   TEXT NOT NULL,
  user_token_enc BLOB NOT NULL,              -- long-lived user token, AES-256-GCM
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
);

-- Pages: the Facebook Pages of a tenant, with the linked Instagram account.
CREATE TABLE pages (
  tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  page_id        TEXT NOT NULL,
  name           TEXT NOT NULL,
  ig_user_id     TEXT,                       -- NULL when no Instagram account is linked
  ig_username    TEXT,
  page_token_enc BLOB NOT NULL,
  synced_at      INTEGER NOT NULL,
  PRIMARY KEY (tenant_id, page_id)
);

-- MCP clients registered through dynamic client registration (RFC 7591).
CREATE TABLE oauth_clients (
  client_id     TEXT PRIMARY KEY,
  client_name   TEXT,
  redirect_uris TEXT NOT NULL,               -- JSON array
  created_at    INTEGER NOT NULL
);

-- Authorization codes, single use, 5 minute TTL.
CREATE TABLE oauth_codes (
  code           TEXT PRIMARY KEY,
  client_id      TEXT NOT NULL,
  tenant_id      TEXT NOT NULL,
  redirect_uri   TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  resource       TEXT,
  expires_at     INTEGER NOT NULL
);
CREATE INDEX idx_oauth_codes_expires ON oauth_codes(expires_at);

-- Refresh tokens, stored hashed, rotated on every use.
CREATE TABLE oauth_refresh_tokens (
  token_hash TEXT PRIMARY KEY,               -- sha256 of the token
  client_id  TEXT NOT NULL,
  tenant_id  TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  revoked    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_oauth_refresh_tenant ON oauth_refresh_tokens(tenant_id);
CREATE INDEX idx_oauth_refresh_expires ON oauth_refresh_tokens(expires_at);

-- CSRF state of the Meta login round trip, 10 minute TTL. Carries the pending
-- MCP authorization request as JSON.
CREATE TABLE login_states (
  state         TEXT PRIMARY KEY,
  oauth_request TEXT NOT NULL,
  expires_at    INTEGER NOT NULL
);
CREATE INDEX idx_login_states_expires ON login_states(expires_at);
