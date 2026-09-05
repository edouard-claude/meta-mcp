package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (d *deps) registerPrompts(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "bilan_mensuel",
		Title:       "Bilan mensuel d'une page",
		Description: "Produit un bilan de la performance organique d'une Page Facebook et de son compte Instagram sur un mois : évolution des statistiques, meilleures publications, points d'attention.",
		Arguments: []*mcp.PromptArgument{
			{Name: "page_id", Description: "Page à analyser. Si absent, commencer par list_pages et demander laquelle.", Required: false},
			{Name: "mois", Description: "Mois au format AAAA-MM. Par défaut le mois écoulé.", Required: false},
		},
	}, d.promptMonthlyReport)

	srv.AddPrompt(&mcp.Prompt{
		Name:        "revue_commentaires",
		Title:       "Revue des commentaires",
		Description: "Passe en revue les commentaires récents d'une page et de son compte Instagram, propose des réponses, et signale ce qui mériterait une modération.",
		Arguments: []*mcp.PromptArgument{
			{Name: "page_id", Description: "Page à passer en revue. Si absent, commencer par list_pages.", Required: false},
		},
	}, d.promptCommentReview)
}

func (d *deps) promptMonthlyReport(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	page := argOr(req, "page_id", "à déterminer avec list_pages")
	month := argOr(req, "mois", "le mois écoulé")

	body := fmt.Sprintf(`Tu prépares le bilan organique de la page %s pour %s.

Marche à suivre :
1. list_pages pour confirmer le page_id et savoir si un compte Instagram est lié.
2. page_insights sur le mois demandé, puis sur le mois précédent, pour pouvoir comparer.
   Si le champ "rejected" est non vide, dis-le : ces métriques n'ont pas pu être lues.
3. page_posts sur la période, puis page_post_insights sur les trois publications
   les plus vues pour expliquer ce qui a marché.
4. Si un compte Instagram est lié : ig_account_insights, ig_media, et
   ig_follower_demographics avec breakdown=city pour situer l'audience.
5. Rédige le bilan en français : évolution chiffrée par rapport au mois précédent,
   ce qui a le mieux fonctionné et pourquoi, ce qui a décroché, et deux ou trois
   recommandations concrètes.

Ne publie rien. Ce bilan est une lecture seule : n'appelle aucun outil d'écriture.
Ne compare que des périodes de même longueur, et dis-le si les données manquent
pour un mois.`, page, month)

	return promptResult("Bilan mensuel", body), nil
}

func (d *deps) promptCommentReview(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	page := argOr(req, "page_id", "à déterminer avec list_pages")

	body := fmt.Sprintf(`Tu passes en revue les commentaires récents de la page %s.

Marche à suivre :
1. list_pages, puis page_posts pour les publications récentes.
2. page_post_comments sur les publications qui en ont, et si un compte Instagram
   est lié, ig_media puis ig_media_comments.
3. Classe les commentaires : ceux qui appellent une réponse, ceux qui n'en
   demandent pas, et ceux qui posent un problème (insultes, spam, hors sujet).
4. Pour chaque commentaire à traiter, propose le texte de la réponse à l'utilisateur
   et attends son accord.

Règles :
- N'écris rien sans confirmation. page_reply_comment, ig_reply_comment,
  page_moderate_comment et ig_moderate_comment renvoient un aperçu tant que
  confirm=true est absent : montre cet aperçu, obtiens un accord explicite,
  puis seulement rappelle l'outil avec confirm=true.
- Ne propose jamais action=delete de toi-même. Masquer est réversible, supprimer
  ne l'est pas : suggère hide, et laisse l'utilisateur demander la suppression.
- Réponds dans la langue du commentaire.`, page)

	return promptResult("Revue des commentaires", body), nil
}

// argOr reads a prompt argument, falling back when the client omitted it.
func argOr(req *mcp.GetPromptRequest, name, fallback string) string {
	if req == nil || req.Params == nil {
		return fallback
	}
	if v := req.Params.Arguments[name]; v != "" {
		return v
	}
	return fallback
}

func promptResult(description, body string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: body},
		}},
	}
}
