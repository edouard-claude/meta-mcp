package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Store is the SQLite implementation of domain.TenantStore. It owns the
// cipher used to seal Meta tokens, so the rest of the application only ever
// sees plaintext tokens in memory and ciphertext on disk.
type Store struct {
	db     *sql.DB
	cipher domain.TokenCipher
}

var _ domain.TenantStore = (*Store)(nil)

// New opens the database at path, creates the parent directory if needed,
// applies the migrations and returns a ready Store.
func New(ctx context.Context, path string, cipher domain.TokenCipher) (*Store, error) {
	if dir := DBPathDir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create db directory %s: %w", dir, err)
		}
	}
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, cipher: cipher}, nil
}

// Ping checks that the database is reachable; it backs GET /healthz.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// ----- tenants -----

// UpsertTenant creates the tenant or refreshes its name and user token,
// keying on the Meta user id so that a second login reuses the same tenant.
func (s *Store) UpsertTenant(ctx context.Context, t *domain.Tenant) error {
	enc, err := s.cipher.Encrypt(t.UserToken)
	if err != nil {
		return fmt.Errorf("encrypt user token: %w", err)
	}
	const q = `INSERT INTO tenants (id, meta_user_id, display_name, user_token_enc, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(meta_user_id) DO UPDATE SET
			display_name   = excluded.display_name,
			user_token_enc = excluded.user_token_enc,
			updated_at     = excluded.updated_at`
	if _, err := s.db.ExecContext(ctx, q,
		t.ID, t.MetaUserID, t.DisplayName, enc, t.CreatedAt.Unix(), t.UpdatedAt.Unix(),
	); err != nil {
		return fmt.Errorf("upsert tenant: %w", err)
	}
	return nil
}

// TenantByID loads a tenant by its internal uuid.
func (s *Store) TenantByID(ctx context.Context, id string) (*domain.Tenant, error) {
	return s.tenantBy(ctx, `SELECT id, meta_user_id, display_name, user_token_enc, created_at, updated_at
		FROM tenants WHERE id = ?`, id)
}

// TenantByMetaUserID loads a tenant by the Facebook user id behind it.
func (s *Store) TenantByMetaUserID(ctx context.Context, metaUserID string) (*domain.Tenant, error) {
	return s.tenantBy(ctx, `SELECT id, meta_user_id, display_name, user_token_enc, created_at, updated_at
		FROM tenants WHERE meta_user_id = ?`, metaUserID)
}

func (s *Store) tenantBy(ctx context.Context, query string, arg any) (*domain.Tenant, error) {
	var (
		t                    domain.Tenant
		enc                  []byte
		createdAt, updatedAt int64
	)
	err := s.db.QueryRowContext(ctx, query, arg).
		Scan(&t.ID, &t.MetaUserID, &t.DisplayName, &enc, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select tenant: %w", err)
	}
	if t.UserToken, err = s.cipher.Decrypt(enc); err != nil {
		return nil, fmt.Errorf("decrypt user token: %w", err)
	}
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	t.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &t, nil
}

// DeleteTenant removes a tenant and, through the foreign key, its pages. Its
// refresh tokens are revoked too so no client keeps a live session.
func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tenant: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_refresh_tokens WHERE tenant_id = ?`, id); err != nil {
		return fmt.Errorf("delete refresh tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE tenant_id = ?`, id); err != nil {
		return fmt.Errorf("delete auth codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE tenant_id = ?`, id); err != nil {
		return fmt.Errorf("delete pages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete tenant: %w", err)
	}
	return nil
}

// ----- pages -----

// ReplacePages makes the stored pages of a tenant match the given slice
// exactly: pages the user no longer administers disappear.
func (s *Store) ReplacePages(ctx context.Context, tenantID string, pages []domain.Page) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace pages: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE tenant_id = ?`, tenantID); err != nil {
		return fmt.Errorf("clear pages: %w", err)
	}
	const insert = `INSERT INTO pages
		(tenant_id, page_id, name, ig_user_id, ig_username, page_token_enc, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	for _, p := range pages {
		enc, err := s.cipher.Encrypt(p.PageToken)
		if err != nil {
			return fmt.Errorf("encrypt page token: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insert,
			tenantID, p.PageID, p.Name, nullable(p.IGUserID), nullable(p.IGUsername), enc, p.SyncedAt.Unix(),
		); err != nil {
			return fmt.Errorf("insert page: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace pages: %w", err)
	}
	return nil
}

// ListPages returns every page of a tenant, ordered by name.
func (s *Store) ListPages(ctx context.Context, tenantID string) ([]domain.Page, error) {
	const q = `SELECT page_id, name, ig_user_id, ig_username, page_token_enc, synced_at
		FROM pages WHERE tenant_id = ? ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("select pages: %w", err)
	}
	defer rows.Close()

	pages := []domain.Page{}
	for rows.Next() {
		p, err := s.scanPage(rows, tenantID)
		if err != nil {
			return nil, err
		}
		pages = append(pages, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pages: %w", err)
	}
	return pages, nil
}

// PageByID loads one page of one tenant. A page id belonging to another
// tenant yields ErrNotFound, which is the whole isolation guarantee.
func (s *Store) PageByID(ctx context.Context, tenantID, pageID string) (*domain.Page, error) {
	const q = `SELECT page_id, name, ig_user_id, ig_username, page_token_enc, synced_at
		FROM pages WHERE tenant_id = ? AND page_id = ?`
	row := s.db.QueryRowContext(ctx, q, tenantID, pageID)
	p, err := s.scanPage(row, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return p, err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func (s *Store) scanPage(sc scanner, tenantID string) (*domain.Page, error) {
	var (
		p        domain.Page
		igID     sql.NullString
		igName   sql.NullString
		enc      []byte
		syncedAt int64
	)
	if err := sc.Scan(&p.PageID, &p.Name, &igID, &igName, &enc, &syncedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan page: %w", err)
	}
	token, err := s.cipher.Decrypt(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt page token: %w", err)
	}
	p.TenantID = tenantID
	p.IGUserID = igID.String
	p.IGUsername = igName.String
	p.PageToken = token
	p.SyncedAt = time.Unix(syncedAt, 0).UTC()
	return &p, nil
}

// ----- oauth clients -----

// RegisterClient stores a dynamically registered MCP client.
func (s *Store) RegisterClient(ctx context.Context, c *domain.OAuthClient) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return fmt.Errorf("marshal redirect_uris: %w", err)
	}
	const q = `INSERT INTO oauth_clients (client_id, client_name, redirect_uris, created_at)
		VALUES (?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q, c.ClientID, c.ClientName, string(uris), c.CreatedAt.Unix()); err != nil {
		return fmt.Errorf("insert oauth client: %w", err)
	}
	return nil
}

// ClientByID loads a registered client.
func (s *Store) ClientByID(ctx context.Context, clientID string) (*domain.OAuthClient, error) {
	const q = `SELECT client_id, client_name, redirect_uris, created_at FROM oauth_clients WHERE client_id = ?`
	var (
		c         domain.OAuthClient
		name      sql.NullString
		uris      string
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx, q, clientID).Scan(&c.ClientID, &name, &uris, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select oauth client: %w", err)
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode redirect_uris: %w", err)
	}
	c.ClientName = name.String
	c.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &c, nil
}

// ----- authorization codes -----

// CreateAuthCode stores a freshly minted authorization code.
func (s *Store) CreateAuthCode(ctx context.Context, c *domain.AuthCode) error {
	const q = `INSERT INTO oauth_codes
		(code, client_id, tenant_id, redirect_uri, code_challenge, resource, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q,
		c.Code, c.ClientID, c.TenantID, c.RedirectURI, c.CodeChallenge, nullable(c.Resource), c.ExpiresAt.Unix(),
	); err != nil {
		return fmt.Errorf("insert auth code: %w", err)
	}
	return nil
}

// ConsumeAuthCode deletes the code and returns it in one statement, so a
// replay of the same code finds nothing. Expiry is checked by the caller
// against the returned deadline.
func (s *Store) ConsumeAuthCode(ctx context.Context, code string) (*domain.AuthCode, error) {
	const q = `DELETE FROM oauth_codes WHERE code = ?
		RETURNING code, client_id, tenant_id, redirect_uri, code_challenge, resource, expires_at`
	var (
		c         domain.AuthCode
		resource  sql.NullString
		expiresAt int64
	)
	err := s.db.QueryRowContext(ctx, q, code).
		Scan(&c.Code, &c.ClientID, &c.TenantID, &c.RedirectURI, &c.CodeChallenge, &resource, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume auth code: %w", err)
	}
	c.Resource = resource.String
	c.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return &c, nil
}

// ----- refresh tokens -----

// CreateRefreshToken stores the SHA-256 of a refresh token.
func (s *Store) CreateRefreshToken(ctx context.Context, rt *domain.RefreshToken) error {
	const q = `INSERT INTO oauth_refresh_tokens (token_hash, client_id, tenant_id, expires_at, revoked)
		VALUES (?, ?, ?, ?, 0)`
	if _, err := s.db.ExecContext(ctx, q, rt.TokenHash, rt.ClientID, rt.TenantID, rt.ExpiresAt.Unix()); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// RotateRefreshToken marks the presented token revoked and returns it. A
// token that is already revoked, expired or unknown yields ErrNotFound, so a
// replayed refresh token is simply rejected.
func (s *Store) RotateRefreshToken(ctx context.Context, tokenHash string, now time.Time) (*domain.RefreshToken, error) {
	const q = `UPDATE oauth_refresh_tokens SET revoked = 1
		WHERE token_hash = ? AND revoked = 0 AND expires_at > ?
		RETURNING token_hash, client_id, tenant_id, expires_at`
	var (
		rt        domain.RefreshToken
		expiresAt int64
	)
	err := s.db.QueryRowContext(ctx, q, tokenHash, now.Unix()).
		Scan(&rt.TokenHash, &rt.ClientID, &rt.TenantID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	rt.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	rt.Revoked = true
	return &rt, nil
}

// RevokeTenantRefreshTokens kills every live session of a tenant.
func (s *Store) RevokeTenantRefreshTokens(ctx context.Context, tenantID string) error {
	const q = `UPDATE oauth_refresh_tokens SET revoked = 1 WHERE tenant_id = ?`
	if _, err := s.db.ExecContext(ctx, q, tenantID); err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}

// ----- login states -----

// CreateLoginState parks an MCP authorization request for the duration of the
// Facebook login.
func (s *Store) CreateLoginState(ctx context.Context, st *domain.LoginState) error {
	payload, err := json.Marshal(st.Request)
	if err != nil {
		return fmt.Errorf("marshal oauth request: %w", err)
	}
	const q = `INSERT INTO login_states (state, oauth_request, expires_at) VALUES (?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q, st.State, string(payload), st.ExpiresAt.Unix()); err != nil {
		return fmt.Errorf("insert login state: %w", err)
	}
	return nil
}

// ConsumeLoginState deletes and returns a login state, making it single use.
func (s *Store) ConsumeLoginState(ctx context.Context, state string) (*domain.LoginState, error) {
	const q = `DELETE FROM login_states WHERE state = ? RETURNING state, oauth_request, expires_at`
	var (
		st        domain.LoginState
		payload   string
		expiresAt int64
	)
	err := s.db.QueryRowContext(ctx, q, state).Scan(&st.State, &payload, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume login state: %w", err)
	}
	if err := json.Unmarshal([]byte(payload), &st.Request); err != nil {
		return nil, fmt.Errorf("decode oauth request: %w", err)
	}
	st.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return &st, nil
}

// ----- housekeeping -----

// PurgeExpired drops every short-lived row that is past its deadline. It runs
// at startup and on a ticker.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) error {
	statements := []string{
		`DELETE FROM oauth_codes WHERE expires_at <= ?`,
		`DELETE FROM login_states WHERE expires_at <= ?`,
		`DELETE FROM oauth_refresh_tokens WHERE expires_at <= ?`,
	}
	for _, q := range statements {
		if _, err := s.db.ExecContext(ctx, q, now.Unix()); err != nil {
			return fmt.Errorf("purge expired: %w", err)
		}
	}
	return nil
}

// nullable turns an empty string into a SQL NULL, keeping optional columns
// genuinely empty rather than holding "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
