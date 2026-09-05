package mcpserver

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDetailTools adds the tools that look at one object, plus the
// connection diagnostic.
func (d *deps) registerDetailTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "connection_status",
		Title:       "État de la connexion",
		Description: "Vérifie auprès de Meta que l'autorisation Facebook est toujours valide : validité du jeton, date d'expiration, permissions réellement accordées, pages synchronisées. À appeler quand un outil échoue, ou pour savoir combien de temps la connexion tiendra encore.",
		Annotations: readOnly(),
	}, d.toolConnectionStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_post_insights",
		Title:       "Statistiques d'une publication",
		Description: "Statistiques détaillées d'une publication de Page Facebook : impressions, impressions uniques, clics, réactions par type, vues vidéo. Plus complet que les trois chiffres agrégés de page_posts.",
		Annotations: readOnly(),
	}, d.toolPagePostInsights)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_media_insights",
		Title:       "Statistiques d'une publication Instagram",
		Description: "Statistiques détaillées d'une publication Instagram : couverture, vues, J'aime, commentaires, enregistrements, partages, interactions.",
		Annotations: readOnly(),
	}, d.toolIGMediaInsights)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_stories",
		Title:       "Stories en ligne",
		Description: "Stories Instagram actuellement visibles. Meta les retire de cette liste au bout de 24 h : une liste vide signifie qu'aucune story n'est en ligne, pas qu'il y a une erreur.",
		Annotations: readOnly(),
	}, d.toolIGStories)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_scheduled_posts",
		Title:       "Publications programmées",
		Description: "Liste les publications que Meta garde en attente pour une date future, avec leur date de publication prévue. C'est le pendant de scheduled_at dans page_publish_post.",
		Annotations: readOnly(),
	}, d.toolPageScheduledPosts)
}

// ----- connection_status -----

func (d *deps) toolConnectionStatus(ctx context.Context, req *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	status, err := d.svc.ConnectionStatus(ctx, tenant)
	if err != nil {
		return nil, nil, d.toolError("connection_status", err)
	}
	return jsonResult(status)
}

// ----- page_post_insights -----

// PostInsightsArgs are the arguments of page_post_insights.
type PostInsightsArgs struct {
	PostID  string   `json:"post_id" jsonschema:"Identifiant de la publication, tel que renvoyé par page_posts."`
	PageID  string   `json:"page_id,omitempty" jsonschema:"Page propriétaire. Facultatif : déduit de post_id, ou de la seule page connectée."`
	Metrics []string `json:"metrics,omitempty" jsonschema:"Métriques à lire. Par défaut: post_impressions, post_impressions_unique, post_clicks, post_reactions_by_type_total, post_video_views."`
}

func (d *deps) toolPagePostInsights(ctx context.Context, req *mcp.CallToolRequest, args PostInsightsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	set, err := d.svc.PagePostInsights(ctx, tenant, app.ObjectInsightsInput{
		ObjectID: args.PostID,
		PageID:   args.PageID,
		Metrics:  args.Metrics,
	})
	if err != nil {
		return nil, nil, d.toolError("page_post_insights", err)
	}
	return jsonResult(set)
}

// ----- ig_media_insights -----

// MediaInsightsArgs are the arguments of ig_media_insights.
type MediaInsightsArgs struct {
	MediaID string   `json:"media_id" jsonschema:"Identifiant de la publication Instagram, tel que renvoyé par ig_media."`
	PageID  string   `json:"page_id,omitempty" jsonschema:"Page dont dépend le compte Instagram. Facultatif quand un seul compte est connecté."`
	Metrics []string `json:"metrics,omitempty" jsonschema:"Métriques à lire. Par défaut: reach, views, likes, comments, saved, shares, total_interactions."`
}

func (d *deps) toolIGMediaInsights(ctx context.Context, req *mcp.CallToolRequest, args MediaInsightsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	set, err := d.svc.IGMediaInsights(ctx, tenant, app.ObjectInsightsInput{
		ObjectID: args.MediaID,
		PageID:   args.PageID,
		Metrics:  args.Metrics,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_media_insights", err)
	}
	return jsonResult(set)
}

// ----- ig_stories -----

func (d *deps) toolIGStories(ctx context.Context, req *mcp.CallToolRequest, args PageArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	stories, err := d.svc.IGStories(ctx, tenant, args.PageID)
	if err != nil {
		return nil, nil, d.toolError("ig_stories", err)
	}
	return jsonResult(stories)
}

// ----- page_scheduled_posts -----

// ScheduledPostsArgs are the arguments of page_scheduled_posts.
type ScheduledPostsArgs struct {
	PageID string `json:"page_id" jsonschema:"Identifiant de la Page Facebook, obtenu via list_pages."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Nombre maximum de publications, 25 par défaut, 100 au maximum."`
}

func (d *deps) toolPageScheduledPosts(ctx context.Context, req *mcp.CallToolRequest, args ScheduledPostsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	posts, err := d.svc.PageScheduledPosts(ctx, tenant, app.ScheduledPostsInput{
		PageID: args.PageID,
		Limit:  args.Limit,
	})
	if err != nil {
		return nil, nil, d.toolError("page_scheduled_posts", err)
	}
	return jsonResult(posts)
}
