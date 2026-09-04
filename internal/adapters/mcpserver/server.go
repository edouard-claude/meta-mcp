// Package mcpserver exposes the use cases as MCP tools over Streamable HTTP.
//
// Every tool reads its tenant from the verified bearer token carried by the
// request, never from a parameter, and every page identifier a client sends
// is checked against that tenant before any Graph call is made.
package mcpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/edouard-claude/meta-mcp/internal/app"
	"github.com/edouard-claude/meta-mcp/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "metasocial-mcp"
	serverVersion = "v1.0.0"
	serverTitle   = "Facebook & Instagram organique"
)

// deps carries what the tool handlers need.
type deps struct {
	svc    *app.Service
	logger *slog.Logger
}

// New builds the MCP server with every tool registered.
func New(svc *app.Service, logger *slog.Logger) *mcp.Server {
	d := &deps{svc: svc, logger: logger}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
		Title:   serverTitle,
	}, &mcp.ServerOptions{
		Instructions: instructions,
	})

	d.registerReadTools(srv)
	d.registerWriteTools(srv)
	return srv
}

const instructions = `Serveur MCP pour les Pages Facebook et les comptes Instagram professionnels
de l'utilisateur connecté.

Commencez par list_pages pour obtenir les page_id disponibles ; tous les
autres outils en ont besoin. sync_pages rafraîchit cette liste depuis Meta.

Les outils de lecture couvrent les statistiques de page (page_insights,
page_insights_metadata), les publications (page_posts, page_post_comments) et
Instagram (ig_account_insights, ig_follower_demographics, ig_media,
ig_media_comments). Les dates since/until sont au format AAAA-MM-JJ et
couvrent les 28 derniers jours par défaut.

Les outils d'écriture (page_publish_post, page_reply_comment, ig_publish,
ig_reply_comment) exigent confirm=true. Sans ce paramètre ils renvoient un
aperçu et n'écrivent rien chez Meta : montrez cet aperçu à l'utilisateur et
attendez son accord explicite avant de rappeler l'outil avec confirm=true.

Si un outil répond « autorisation expirée », appelez reconnect_url et donnez
l'adresse obtenue à l'utilisateur.`

// tenantID extracts the tenant from the verified bearer token. The token
// verifier puts it in TokenInfo.UserID; a request without one never reaches a
// handler, so this failing means a wiring bug.
func tenantID(req *mcp.CallToolRequest) (string, error) {
	if req == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return "", errors.New("session non authentifiée")
	}
	return req.Extra.TokenInfo.UserID, nil
}

// jsonResult packs a value as the single compact JSON text block every tool
// returns.
func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("sérialisation du résultat: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
	}, nil, nil
}

// toolError turns an internal error into the sentence the user reads. Graph
// failures are translated; anything unexpected is logged and replaced by a
// neutral message so no internal detail leaks to the client.
func (d *deps) toolError(tool string, err error) error {
	if ge, ok := domain.AsGraphError(err); ok {
		d.logger.Warn("erreur Meta", "tool", tool, "code", ge.Code, "subcode", ge.Subcode)
		return errors.New(ge.UserMessage())
	}
	switch {
	case errors.Is(err, domain.ErrUnknownPage),
		errors.Is(err, domain.ErrNoInstagram),
		errors.Is(err, domain.ErrReauthorize):
		return err
	case errors.Is(err, domain.ErrNotFound):
		return domain.ErrUnknownPage
	}
	d.logger.Error("erreur d'outil", "tool", tool, "error", err)
	return err
}

// writing annotates a tool that can create content at Meta.
func writing() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: ptr(false),
		IdempotentHint:  false,
		OpenWorldHint:   ptr(true),
	}
}

// readOnly annotates a tool that never writes anything at Meta.
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: ptr(false),
		OpenWorldHint:   ptr(true),
	}
}

func ptr[T any](v T) *T { return &v }
