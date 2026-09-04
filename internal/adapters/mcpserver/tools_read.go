package mcpserver

import (
	"context"

	"github.com/edouard/metasocial-mcp/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerReadTools adds every tool that only reads from Meta.
func (d *deps) registerReadTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pages",
		Title:       "Lister les pages",
		Description: "Liste les Pages Facebook du compte connecté, avec le compte Instagram professionnel lié quand il y en a un. Lecture locale, sans appel à Meta. À appeler en premier : les autres outils ont besoin de page_id.",
		Annotations: readOnly(),
	}, d.toolListPages)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "sync_pages",
		Title:       "Resynchroniser les pages",
		Description: "Relit la liste des pages depuis Meta et remplace celle stockée. À utiliser après la création d'une page, la liaison d'un compte Instagram, ou un changement de rôle.",
		Annotations: readOnly(),
	}, d.toolSyncPages)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_insights",
		Title:       "Statistiques d'une page",
		Description: "Statistiques organiques quotidiennes d'une Page Facebook sur une période. Par défaut : impressions uniques, engagements, nouveaux abonnés, abonnés et vues de la page sur les 28 derniers jours.",
		Annotations: readOnly(),
	}, d.toolPageInsights)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_insights_metadata",
		Title:       "Métriques disponibles",
		Description: "Liste les métriques que Meta expose actuellement pour cette page, avec leurs périodes. Utile quand page_insights refuse une métrique.",
		Annotations: readOnly(),
	}, d.toolPageInsightsMetadata)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_posts",
		Title:       "Publications d'une page",
		Description: "Publications récentes d'une Page Facebook avec leurs impressions uniques, clics et réactions.",
		Annotations: readOnly(),
	}, d.toolPagePosts)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_post_comments",
		Title:       "Commentaires d'une publication",
		Description: "Commentaires laissés sur une publication de Page Facebook.",
		Annotations: readOnly(),
	}, d.toolPagePostComments)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_account_insights",
		Title:       "Statistiques Instagram",
		Description: "Statistiques du compte Instagram professionnel lié à une page : couverture, vues, visites de profil, comptes engagés, interactions et abonnés.",
		Annotations: readOnly(),
	}, d.toolIGAccountInsights)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_follower_demographics",
		Title:       "Démographie des abonnés",
		Description: "Répartition des abonnés Instagram selon une dimension : city, country, age ou gender.",
		Annotations: readOnly(),
	}, d.toolIGFollowerDemographics)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_media",
		Title:       "Publications Instagram",
		Description: "Publications Instagram récentes avec leurs statistiques : couverture, vues, enregistrements, partages et interactions.",
		Annotations: readOnly(),
	}, d.toolIGMedia)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_media_comments",
		Title:       "Commentaires Instagram",
		Description: "Commentaires laissés sur une publication Instagram.",
		Annotations: readOnly(),
	}, d.toolIGMediaComments)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reconnect_url",
		Title:       "Lien de reconnexion",
		Description: "Adresse à ouvrir pour réautoriser l'accès à Facebook quand un outil répond « autorisation expirée ».",
		Annotations: readOnly(),
	}, d.toolReconnectURL)
}

// ----- list_pages -----

// NoArgs is the input of the tools that take no parameter.
type NoArgs struct{}

func (d *deps) toolListPages(ctx context.Context, req *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	pages, err := d.svc.ListPages(ctx, tenant)
	if err != nil {
		return nil, nil, d.toolError("list_pages", err)
	}
	return jsonResult(pages)
}

// ----- sync_pages -----

func (d *deps) toolSyncPages(ctx context.Context, req *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	pages, err := d.svc.SyncPages(ctx, tenant)
	if err != nil {
		return nil, nil, d.toolError("sync_pages", err)
	}
	return jsonResult(pages)
}

// ----- page_insights -----

// PageInsightsArgs are the arguments of page_insights.
type PageInsightsArgs struct {
	PageID  string   `json:"page_id" jsonschema:"Identifiant de la Page Facebook, obtenu via list_pages."`
	Since   string   `json:"since,omitempty" jsonschema:"Début de la période, au format AAAA-MM-JJ. Par défaut 28 jours avant until."`
	Until   string   `json:"until,omitempty" jsonschema:"Fin de la période, au format AAAA-MM-JJ. Par défaut aujourd'hui."`
	Metrics []string `json:"metrics,omitempty" jsonschema:"Métriques Meta à lire. Par défaut: page_impressions_unique, page_post_engagements, page_daily_follows, page_follows, page_views_total. Utilisez page_insights_metadata pour connaître les métriques disponibles."`
}

func (d *deps) toolPageInsights(ctx context.Context, req *mcp.CallToolRequest, args PageInsightsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	insights, err := d.svc.PageInsights(ctx, tenant, app.PageInsightsInput{
		PageID:  args.PageID,
		Since:   args.Since,
		Until:   args.Until,
		Metrics: args.Metrics,
	})
	if err != nil {
		return nil, nil, d.toolError("page_insights", err)
	}
	return jsonResult(insights)
}

// ----- page_insights_metadata -----

// PageArgs is the input of the tools that only need a page.
type PageArgs struct {
	PageID string `json:"page_id" jsonschema:"Identifiant de la Page Facebook, obtenu via list_pages."`
}

func (d *deps) toolPageInsightsMetadata(ctx context.Context, req *mcp.CallToolRequest, args PageArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	metrics, err := d.svc.PageInsightsMetadata(ctx, tenant, args.PageID)
	if err != nil {
		return nil, nil, d.toolError("page_insights_metadata", err)
	}
	return jsonResult(metrics)
}

// ----- page_posts -----

// PagePostsArgs are the arguments of page_posts.
type PagePostsArgs struct {
	PageID string `json:"page_id" jsonschema:"Identifiant de la Page Facebook, obtenu via list_pages."`
	Since  string `json:"since,omitempty" jsonschema:"Ne remonter que les publications postérieures à cette date, au format AAAA-MM-JJ. Par défaut les 28 derniers jours."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Nombre maximum de publications, 25 par défaut, 100 au maximum."`
}

func (d *deps) toolPagePosts(ctx context.Context, req *mcp.CallToolRequest, args PagePostsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	posts, err := d.svc.PagePosts(ctx, tenant, app.PagePostsInput{
		PageID: args.PageID,
		Since:  args.Since,
		Limit:  args.Limit,
	})
	if err != nil {
		return nil, nil, d.toolError("page_posts", err)
	}
	return jsonResult(posts)
}

// ----- page_post_comments -----

// PagePostCommentsArgs are the arguments of page_post_comments.
type PagePostCommentsArgs struct {
	PostID string `json:"post_id" jsonschema:"Identifiant de la publication, tel que renvoyé par page_posts."`
	PageID string `json:"page_id,omitempty" jsonschema:"Page propriétaire de la publication. Facultatif : déduit de post_id, ou de la seule page connectée."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Nombre maximum de commentaires, 50 par défaut, 100 au maximum."`
}

func (d *deps) toolPagePostComments(ctx context.Context, req *mcp.CallToolRequest, args PagePostCommentsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	comments, err := d.svc.PagePostComments(ctx, tenant, app.PageCommentsInput{
		PostID: args.PostID,
		PageID: args.PageID,
		Limit:  args.Limit,
	})
	if err != nil {
		return nil, nil, d.toolError("page_post_comments", err)
	}
	return jsonResult(comments)
}

// ----- ig_account_insights -----

// IGInsightsArgs are the arguments of ig_account_insights.
type IGInsightsArgs struct {
	PageID  string   `json:"page_id" jsonschema:"Page Facebook dont le compte Instagram professionnel est lié."`
	Since   string   `json:"since,omitempty" jsonschema:"Début de la période, au format AAAA-MM-JJ. Par défaut 28 jours avant until."`
	Until   string   `json:"until,omitempty" jsonschema:"Fin de la période, au format AAAA-MM-JJ. Par défaut aujourd'hui."`
	Metrics []string `json:"metrics,omitempty" jsonschema:"Métriques Instagram à lire. Par défaut: reach, views, profile_views, accounts_engaged, total_interactions, follower_count."`
}

func (d *deps) toolIGAccountInsights(ctx context.Context, req *mcp.CallToolRequest, args IGInsightsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	insights, err := d.svc.IGAccountInsights(ctx, tenant, app.IGInsightsInput{
		PageID:  args.PageID,
		Since:   args.Since,
		Until:   args.Until,
		Metrics: args.Metrics,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_account_insights", err)
	}
	return jsonResult(insights)
}

// ----- ig_follower_demographics -----

// IGDemographicsArgs are the arguments of ig_follower_demographics.
type IGDemographicsArgs struct {
	PageID    string `json:"page_id" jsonschema:"Page Facebook dont le compte Instagram professionnel est lié."`
	Breakdown string `json:"breakdown" jsonschema:"Dimension de la répartition: city, country, age ou gender."`
}

func (d *deps) toolIGFollowerDemographics(ctx context.Context, req *mcp.CallToolRequest, args IGDemographicsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	breakdowns, err := d.svc.IGFollowerDemographics(ctx, tenant, args.PageID, args.Breakdown)
	if err != nil {
		return nil, nil, d.toolError("ig_follower_demographics", err)
	}
	return jsonResult(breakdowns)
}

// ----- ig_media -----

// IGMediaArgs are the arguments of ig_media.
type IGMediaArgs struct {
	PageID string `json:"page_id" jsonschema:"Page Facebook dont le compte Instagram professionnel est lié."`
	Since  string `json:"since,omitempty" jsonschema:"Ne remonter que les publications postérieures à cette date, au format AAAA-MM-JJ. Par défaut les 28 derniers jours."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Nombre maximum de publications, 25 par défaut, 100 au maximum."`
}

func (d *deps) toolIGMedia(ctx context.Context, req *mcp.CallToolRequest, args IGMediaArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	media, err := d.svc.IGMedia(ctx, tenant, app.IGMediaInput{
		PageID: args.PageID,
		Since:  args.Since,
		Limit:  args.Limit,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_media", err)
	}
	return jsonResult(media)
}

// ----- ig_media_comments -----

// IGMediaCommentsArgs are the arguments of ig_media_comments.
type IGMediaCommentsArgs struct {
	MediaID string `json:"media_id" jsonschema:"Identifiant de la publication Instagram, tel que renvoyé par ig_media."`
	PageID  string `json:"page_id,omitempty" jsonschema:"Page dont dépend le compte Instagram. Facultatif quand un seul compte Instagram est connecté."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Nombre maximum de commentaires, 50 par défaut, 100 au maximum."`
}

func (d *deps) toolIGMediaComments(ctx context.Context, req *mcp.CallToolRequest, args IGMediaCommentsArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	comments, err := d.svc.IGMediaComments(ctx, tenant, app.IGCommentsInput{
		MediaID: args.MediaID,
		PageID:  args.PageID,
		Limit:   args.Limit,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_media_comments", err)
	}
	return jsonResult(comments)
}

// ----- reconnect_url -----

func (d *deps) toolReconnectURL(ctx context.Context, req *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	url, err := d.svc.ReconnectURL(ctx, tenant)
	if err != nil {
		return nil, nil, d.toolError("reconnect_url", err)
	}
	return jsonResult(map[string]string{"url": url})
}
