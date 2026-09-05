package mcpserver

import (
	"context"

	"github.com/edouard-claude/meta-mcp/internal/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// confirmDoc is appended to every write tool description so the calling model
// cannot miss the rule.
const confirmDoc = " ÉCRITURE : sans confirm=true, l'outil renvoie seulement un aperçu et " +
	"n'écrit rien chez Meta. Montrez l'aperçu à l'utilisateur, obtenez son accord explicite, " +
	"puis rappelez l'outil avec confirm=true."

// registerWriteTools adds the tools that can create content at Meta. All of
// them are gated behind confirm=true.
func (d *deps) registerWriteTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_publish_post",
		Title:       "Publier sur une page",
		Description: "Publie un message, un lien ou une photo sur une Page Facebook, immédiatement ou à une date programmée." + confirmDoc,
		Annotations: writing(),
	}, d.toolPagePublishPost)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_reply_comment",
		Title:       "Répondre à un commentaire Facebook",
		Description: "Répond à un commentaire laissé sur une publication de Page Facebook." + confirmDoc,
		Annotations: writing(),
	}, d.toolPageReplyComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_publish",
		Title:       "Publier sur Instagram",
		Description: "Publie une image, un reel, un carrousel ou une story sur le compte Instagram professionnel lié à une page. Les médias doivent être accessibles publiquement en https. Une story ne porte pas de légende." + confirmDoc,
		Annotations: writing(),
	}, d.toolIGPublish)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_reply_comment",
		Title:       "Répondre à un commentaire Instagram",
		Description: "Répond à un commentaire laissé sur une publication Instagram." + confirmDoc,
		Annotations: writing(),
	}, d.toolIGReplyComment)
}

// ----- page_publish_post -----

// PublishPostArgs are the arguments of page_publish_post.
type PublishPostArgs struct {
	PageID      string `json:"page_id" jsonschema:"Identifiant de la Page Facebook, obtenu via list_pages."`
	Message     string `json:"message,omitempty" jsonschema:"Texte de la publication. Devient la légende quand photo_url est fournie."`
	Link        string `json:"link,omitempty" jsonschema:"URL https à partager. Incompatible avec photo_url."`
	PhotoURL    string `json:"photo_url,omitempty" jsonschema:"URL https d'une image publiquement accessible, publiée en tant que photo."`
	ScheduledAt string `json:"scheduled_at,omitempty" jsonschema:"Date de publication programmée, au format ISO 8601 (2026-09-10T09:00:00Z). Entre 10 minutes et 6 mois dans le futur. Absent = publication immédiate."`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"Doit valoir true pour publier réellement. false ou absent renvoie un aperçu sans rien écrire chez Meta."`
}

func (d *deps) toolPagePublishPost(ctx context.Context, req *mcp.CallToolRequest, args PublishPostArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.PublishPost(ctx, tenant, app.PublishPostInput{
		PageID:      args.PageID,
		Message:     args.Message,
		Link:        args.Link,
		PhotoURL:    args.PhotoURL,
		ScheduledAt: args.ScheduledAt,
		Confirm:     args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("page_publish_post", err)
	}
	if !out.Preview {
		d.logger.Info("publication créée", "tool", "page_publish_post", "page_id", out.PageID)
	}
	return jsonResult(out)
}

// ----- page_reply_comment -----

// ReplyCommentArgs are the arguments of page_reply_comment and
// ig_reply_comment.
type ReplyCommentArgs struct {
	CommentID string `json:"comment_id" jsonschema:"Identifiant du commentaire auquel répondre."`
	Message   string `json:"message" jsonschema:"Texte de la réponse."`
	PageID    string `json:"page_id,omitempty" jsonschema:"Page concernée. Facultatif : déduit de comment_id, ou de la seule page connectée."`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Doit valoir true pour publier réellement la réponse. false ou absent renvoie un aperçu."`
}

func (d *deps) toolPageReplyComment(ctx context.Context, req *mcp.CallToolRequest, args ReplyCommentArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.ReplyToComment(ctx, tenant, app.ReplyCommentInput{
		CommentID: args.CommentID,
		PageID:    args.PageID,
		Message:   args.Message,
		Confirm:   args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("page_reply_comment", err)
	}
	if !out.Preview {
		d.logger.Info("réponse publiée", "tool", "page_reply_comment", "page_id", out.PageID)
	}
	return jsonResult(out)
}

// ----- ig_publish -----

// IGPublishArgs are the arguments of ig_publish.
type IGPublishArgs struct {
	PageID    string   `json:"page_id" jsonschema:"Page Facebook dont le compte Instagram professionnel est lié."`
	MediaType string   `json:"media_type,omitempty" jsonschema:"IMAGE (défaut), REELS, CAROUSEL ou STORIES."`
	ImageURL  string   `json:"image_url,omitempty" jsonschema:"URL https de l'image, pour media_type=IMAGE ou STORIES."`
	VideoURL  string   `json:"video_url,omitempty" jsonschema:"URL https de la vidéo, pour media_type=REELS ou STORIES."`
	Children  []string `json:"children,omitempty" jsonschema:"URLs https des images du carrousel, de 2 à 10, pour media_type=CAROUSEL."`
	Caption   string   `json:"caption,omitempty" jsonschema:"Légende de la publication."`
	Confirm   bool     `json:"confirm,omitempty" jsonschema:"Doit valoir true pour publier réellement. false ou absent renvoie un aperçu sans rien écrire chez Meta."`
}

func (d *deps) toolIGPublish(ctx context.Context, req *mcp.CallToolRequest, args IGPublishArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.IGPublish(ctx, tenant, app.IGPublishInput{
		PageID:    args.PageID,
		MediaType: args.MediaType,
		ImageURL:  args.ImageURL,
		VideoURL:  args.VideoURL,
		Children:  args.Children,
		Caption:   args.Caption,
		Confirm:   args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_publish", err)
	}
	if !out.Preview {
		d.logger.Info("publication Instagram créée", "tool", "ig_publish", "page_id", out.PageID)
	}
	return jsonResult(out)
}

// ----- ig_reply_comment -----

func (d *deps) toolIGReplyComment(ctx context.Context, req *mcp.CallToolRequest, args ReplyCommentArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.IGReplyToComment(ctx, tenant, app.ReplyCommentInput{
		CommentID: args.CommentID,
		PageID:    args.PageID,
		Message:   args.Message,
		Confirm:   args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_reply_comment", err)
	}
	if !out.Preview {
		d.logger.Info("réponse Instagram publiée", "tool", "ig_reply_comment", "page_id", out.PageID)
	}
	return jsonResult(out)
}

// registerModerationTools adds the tools that hide, delete or cancel. They
// are the destructive end of the surface, so their previews say so.
func (d *deps) registerModerationTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_moderate_comment",
		Title:       "Modérer un commentaire Facebook",
		Description: "Masque, réaffiche ou supprime un commentaire sur une publication de Page." + confirmDoc,
		Annotations: moderating(),
	}, d.toolPageModerateComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ig_moderate_comment",
		Title:       "Modérer un commentaire Instagram",
		Description: "Masque, réaffiche ou supprime un commentaire sur une publication Instagram." + confirmDoc,
		Annotations: moderating(),
	}, d.toolIGModerateComment)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_delete_post",
		Title:       "Supprimer une publication",
		Description: "Supprime définitivement une publication déjà en ligne sur une Page. La publication, ses réactions et ses commentaires disparaissent, et rien ne les restaure. Pour une publication encore programmée, utilisez plutôt page_cancel_scheduled_post." + confirmDoc,
		Annotations: moderating(),
	}, d.toolPageDeletePost)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "page_cancel_scheduled_post",
		Title:       "Annuler une publication programmée",
		Description: "Supprime une publication que Meta gardait en attente. L'annulation est définitive : la publication est supprimée, pas mise en pause. Utilisez page_scheduled_posts pour obtenir post_id." + confirmDoc,
		Annotations: moderating(),
	}, d.toolPageCancelScheduledPost)
}

// ModerateCommentArgs are the arguments of the two moderation tools.
type ModerateCommentArgs struct {
	CommentID string `json:"comment_id" jsonschema:"Identifiant du commentaire."`
	Action    string `json:"action" jsonschema:"hide pour masquer, unhide pour réafficher, delete pour supprimer définitivement."`
	PageID    string `json:"page_id,omitempty" jsonschema:"Page concernée. Facultatif : déduit de comment_id, ou de la seule page connectée."`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Doit valoir true pour appliquer réellement. false ou absent renvoie un aperçu."`
}

func (d *deps) toolPageModerateComment(ctx context.Context, req *mcp.CallToolRequest, args ModerateCommentArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.ModerateComment(ctx, tenant, app.ModerateCommentInput{
		CommentID: args.CommentID,
		Action:    args.Action,
		PageID:    args.PageID,
		Confirm:   args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("page_moderate_comment", err)
	}
	if !out.Preview {
		d.logger.Info("commentaire modéré", "tool", "page_moderate_comment",
			"action", out.Action, "page_id", out.PageID)
	}
	return jsonResult(out)
}

func (d *deps) toolIGModerateComment(ctx context.Context, req *mcp.CallToolRequest, args ModerateCommentArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.IGModerateComment(ctx, tenant, app.ModerateCommentInput{
		CommentID: args.CommentID,
		Action:    args.Action,
		PageID:    args.PageID,
		Confirm:   args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("ig_moderate_comment", err)
	}
	if !out.Preview {
		d.logger.Info("commentaire Instagram modéré", "tool", "ig_moderate_comment",
			"action", out.Action, "page_id", out.PageID)
	}
	return jsonResult(out)
}

// CancelScheduledPostArgs are the arguments of page_cancel_scheduled_post.
type CancelScheduledPostArgs struct {
	PostID  string `json:"post_id" jsonschema:"Identifiant de la publication programmée, obtenu via page_scheduled_posts."`
	PageID  string `json:"page_id,omitempty" jsonschema:"Page concernée. Facultatif : déduit de post_id, ou de la seule page connectée."`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Doit valoir true pour annuler réellement. false ou absent renvoie un aperçu."`
}

func (d *deps) toolPageCancelScheduledPost(ctx context.Context, req *mcp.CallToolRequest, args CancelScheduledPostArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.PageCancelScheduledPost(ctx, tenant, app.CancelScheduledPostInput{
		PostID:  args.PostID,
		PageID:  args.PageID,
		Confirm: args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("page_cancel_scheduled_post", err)
	}
	if !out.Preview {
		d.logger.Info("publication programmée annulée", "page_id", out.PageID)
	}
	return jsonResult(out)
}

// DeletePostArgs are the arguments of page_delete_post.
type DeletePostArgs struct {
	PostID  string `json:"post_id" jsonschema:"Identifiant de la publication à supprimer, tel que renvoyé par page_posts."`
	PageID  string `json:"page_id,omitempty" jsonschema:"Page concernée. Facultatif : déduit de post_id, ou de la seule page connectée."`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Doit valoir true pour supprimer réellement. false ou absent renvoie un aperçu."`
}

func (d *deps) toolPageDeletePost(ctx context.Context, req *mcp.CallToolRequest, args DeletePostArgs) (*mcp.CallToolResult, any, error) {
	tenant, err := tenantID(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := d.svc.PageDeletePost(ctx, tenant, app.DeletePostInput{
		PostID:  args.PostID,
		PageID:  args.PageID,
		Confirm: args.Confirm,
	})
	if err != nil {
		return nil, nil, d.toolError("page_delete_post", err)
	}
	if !out.Preview {
		d.logger.Info("publication supprimée", "page_id", out.PageID, "post_id", out.PostID)
	}
	return jsonResult(out)
}
