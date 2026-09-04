package app

import (
	"context"
	"slices"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// KnownMetrics is the catalogue page_insights_metadata returns.
//
// It is maintained here rather than read from Meta: the /insights/metadata
// endpoint no longer answers, and there is no other way to ask the Graph API
// what a given object supports. Treat it as a starting point, not as truth.
// page_insights is the real check, and reports whatever it could not read in
// its "rejected" list.
var KnownMetrics = []domain.InsightMeta{
	// --- Facebook Page, daily series ---
	{Name: "page_post_engagements", Period: "day", Surface: "page",
		Description: "Interactions avec les publications de la Page."},
	{Name: "page_daily_follows", Period: "day", Surface: "page",
		Description: "Nouveaux abonnés dans la journée."},
	{Name: "page_daily_unfollows", Period: "day", Surface: "page",
		Description: "Désabonnements dans la journée."},
	{Name: "page_follows", Period: "day", Surface: "page",
		Description: "Nombre total d'abonnés."},
	{Name: "page_views_total", Period: "day", Surface: "page",
		Description: "Vues de la Page."},
	{Name: "page_fans", Period: "day", Surface: "page",
		Description: "Nombre total de mentions J'aime de la Page."},
	{Name: "page_impressions", Period: "day", Surface: "page",
		Description: "Impressions, toutes sources confondues."},
	{Name: "page_video_views", Period: "day", Surface: "page",
		Description: "Vues des vidéos de la Page."},

	// --- Instagram, total_value ---
	{Name: "reach", Period: "day", Surface: "instagram",
		Description: "Comptes uniques ayant vu le contenu."},
	{Name: "views", Period: "day", Surface: "instagram",
		Description: "Vues du contenu."},
	{Name: "profile_views", Period: "day", Surface: "instagram",
		Description: "Visites du profil."},
	{Name: "accounts_engaged", Period: "day", Surface: "instagram",
		Description: "Comptes ayant interagi."},
	{Name: "total_interactions", Period: "day", Surface: "instagram",
		Description: "J'aime, commentaires, enregistrements et partages."},
	{Name: "likes", Period: "day", Surface: "instagram", Description: "Mentions J'aime."},
	{Name: "comments", Period: "day", Surface: "instagram", Description: "Commentaires."},
	{Name: "saves", Period: "day", Surface: "instagram", Description: "Enregistrements."},
	{Name: "shares", Period: "day", Surface: "instagram", Description: "Partages."},

	// --- Instagram, plain daily series ---
	{Name: "follower_count", Period: "day", Surface: "instagram",
		Description: "Nouveaux abonnés par jour. Refusée avec metric_type=total_value, le serveur la demande donc à part."},
}

// PageInsightsMetadata returns the metrics this server knows how to request.
// The page is still resolved first, so the tool stays tenant-scoped and a
// page_id from another account is refused exactly like everywhere else.
func (s *Service) PageInsightsMetadata(ctx context.Context, tenantID, pageID string) ([]domain.InsightMeta, error) {
	page, err := s.page(ctx, tenantID, pageID)
	if err != nil {
		return nil, err
	}

	out := make([]domain.InsightMeta, 0, len(KnownMetrics))
	for _, m := range KnownMetrics {
		// A page with no Instagram account cannot be asked for Instagram
		// metrics, so listing them would only mislead the caller.
		if m.Surface == "instagram" && !page.HasInstagram() {
			continue
		}
		out = append(out, m)
	}
	return slices.Clip(out), nil
}
