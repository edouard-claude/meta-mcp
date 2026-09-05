// Command metasocial-mcp is a multi-tenant remote MCP server for organic
// Facebook Page and Instagram Business content.
//
// It exposes an MCP endpoint over Streamable HTTP at /mcp, protected by its
// own OAuth 2.1 authorization server, and federates the end user login to
// Facebook Login for Business.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/adapters/authserver"
	"github.com/edouard-claude/meta-mcp/internal/adapters/clock"
	"github.com/edouard-claude/meta-mcp/internal/adapters/crypto"
	"github.com/edouard-claude/meta-mcp/internal/adapters/httpserver"
	"github.com/edouard-claude/meta-mcp/internal/adapters/mcpserver"
	"github.com/edouard-claude/meta-mcp/internal/adapters/meta"
	"github.com/edouard-claude/meta-mcp/internal/adapters/sqlite"
	"github.com/edouard-claude/meta-mcp/internal/app"
	"github.com/edouard-claude/meta-mcp/internal/config"
)

const (
	// shutdownGrace is how long in-flight requests get on SIGTERM.
	shutdownGrace = 10 * time.Second
	// purgeInterval is how often expired codes, states and refresh tokens are
	// swept out of the database.
	purgeInterval = 10 * time.Minute
	// tokenRefreshInterval is how often Meta user tokens close to expiry are
	// renewed. Twice a day is plenty for a two week renewal window, and keeps
	// the load on the Graph API negligible.
	tokenRefreshInterval = 12 * time.Hour
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "metasocial-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogFormat)

	cipher, err := crypto.New(cfg.TokenCipherKey)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := sqlite.New(ctx, cfg.DBPath, cipher)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.PurgeExpired(ctx, time.Now()); err != nil {
		return fmt.Errorf("purge au démarrage: %w", err)
	}
	go purgeLoop(ctx, store, logger)

	auth := authserver.New(store, clock.System{}, authserver.Options{
		Issuer:          cfg.PublicURL,
		Resource:        cfg.MCPResourceURL(),
		SigningKey:      cfg.JWTSigningKey,
		LoginPath:       "/meta/login",
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
	}, logger)

	graph := meta.NewClient(meta.Options{
		AppID:       cfg.MetaAppID,
		AppSecret:   cfg.MetaAppSecret,
		Version:     cfg.GraphVersion,
		GraphBase:   cfg.GraphBaseURL,
		DialogBase:  cfg.DialogBaseURL,
		RedirectURI: cfg.MetaRedirectURI(),
		Scopes:      cfg.MetaScopes,
	})

	login := app.NewLoginService(store, graph, clock.System{}, cfg.IsMetaUserAllowed)
	go refreshLoop(ctx, login, logger)

	metaHandlers := meta.NewHandlers(login, auth, meta.HandlerOptions{
		PublicURL:   cfg.PublicURL,
		RedirectURI: cfg.MetaRedirectURI(),
		AppSecret:   cfg.MetaAppSecret,
	}, logger)

	svc := app.NewService(store, graph, clock.System{}, cfg.PublicURL, cfg.ScopeList())
	mcpHandler := mcpserver.Handler(
		mcpserver.New(svc, logger),
		func(token string) (string, time.Time, error) {
			claims, err := auth.VerifyAccessToken(token)
			if err != nil {
				return "", time.Time{}, err
			}
			return claims.TenantID(), claims.Expiry(), nil
		},
		mcpserver.HandlerOptions{ResourceMetadataURL: cfg.ResourceMetadataURL()},
		logger,
	)

	handler := httpserver.New(httpserver.Handlers{
		ProtectedResourceMetadata: auth.ProtectedResourceMetadataHandler(),
		AuthServerMetadata:        auth.AuthServerMetadataHandler(),
		Register:                  auth.RegisterHandler(),
		Authorize:                 auth.AuthorizeHandler(),
		Token:                     auth.TokenHandler(),
		MetaLogin:                 metaHandlers.LoginHandler(),
		MetaCallback:              metaHandlers.CallbackHandler(),
		MetaDataDeletion:          metaHandlers.DataDeletionHandler(),
		MetaDeauthorize:           metaHandlers.DeauthorizeHandler(),
		Privacy:                   metaHandlers.PrivacyHandler(),
		MCP:                       mcpHandler,
		Health:                    store.Ping,
	}, logger)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("serveur démarré", "addr", cfg.ListenAddr, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("écoute HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("arrêt demandé, fermeture en cours")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("arrêt du serveur: %w", err)
	}
	return nil
}

// purgeLoop sweeps expired short-lived rows until the context is cancelled.
func purgeLoop(ctx context.Context, store *sqlite.Store, logger *slog.Logger) {
	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.PurgeExpired(ctx, time.Now()); err != nil {
				logger.Error("purge périodique", "error", err)
			}
		}
	}
}

// refreshLoop renews the Meta user tokens that are about to expire, at
// startup and then on a ticker. Without it every tenant silently loses access
// about sixty days after connecting.
func refreshLoop(ctx context.Context, login *app.LoginService, logger *slog.Logger) {
	sweep := func() {
		report, err := login.RefreshExpiringTokens(ctx, app.DefaultRefreshWindow)
		if err != nil {
			if ctx.Err() == nil {
				logger.Error("renouvellement des jetons Meta", "error", err)
			}
			return
		}
		if report.Checked == 0 {
			return
		}
		logger.Info("jetons Meta renouvelés",
			"examines", report.Checked,
			"renouveles", report.Refreshed,
			"a_reconnecter", len(report.Expired))
		for _, tenantID := range report.Expired {
			logger.Warn("jeton Meta non renouvelable, reconnexion requise", "tenant_id", tenantID)
		}
	}

	sweep()
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// newLogger builds the structured logger: JSON in production, text when
// LOG_FORMAT=text makes local output readable.
func newLogger(format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
