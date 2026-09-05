package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Resource URIs. They are tenant-scoped like everything else: the same URI
// gives each user their own data, resolved from their bearer token.
const (
	uriPages      = "metasocial://pages"
	uriConnection = "metasocial://connection"
)

func (d *deps) registerResources(srv *mcp.Server) {
	srv.AddResource(&mcp.Resource{
		URI:         uriPages,
		Name:        "pages",
		Title:       "Pages connectées",
		Description: "Pages Facebook du compte connecté et comptes Instagram professionnels liés. Même contenu que list_pages, sans appel à Meta.",
		MIMEType:    "application/json",
	}, d.readPages)

	srv.AddResource(&mcp.Resource{
		URI:         uriConnection,
		Name:        "connection",
		Title:       "État de la connexion",
		Description: "Validité du jeton Meta, expiration, permissions accordées et pages synchronisées.",
		MIMEType:    "application/json",
	}, d.readConnection)
}

func (d *deps) readPages(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	tenant, err := resourceTenantID(req)
	if err != nil {
		return nil, err
	}
	pages, err := d.svc.ListPages(ctx, tenant)
	if err != nil {
		return nil, d.toolError("resource:pages", err)
	}
	return jsonResource(uriPages, pages)
}

func (d *deps) readConnection(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	tenant, err := resourceTenantID(req)
	if err != nil {
		return nil, err
	}
	status, err := d.svc.ConnectionStatus(ctx, tenant)
	if err != nil {
		return nil, d.toolError("resource:connection", err)
	}
	return jsonResource(uriConnection, status)
}

// resourceTenantID reads the tenant from the verified bearer token, exactly
// as the tools do.
func resourceTenantID(req *mcp.ReadResourceRequest) (string, error) {
	if req == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return "", errSessionUnauthenticated
	}
	return req.Extra.TokenInfo.UserID, nil
}

func jsonResource(uri string, v any) (*mcp.ReadResourceResult, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("sérialisation de la ressource: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(payload),
		}},
	}, nil
}
