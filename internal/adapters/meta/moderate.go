package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// scheduledPostFields is what a held post is worth reading.
const scheduledPostFields = "id,message,created_time,scheduled_publish_time"

// ScheduledPosts lists the posts Meta is holding for a future publication.
func (c *Client) ScheduledPosts(ctx context.Context, pageToken, pageID string, limit int) ([]domain.ScheduledPost, error) {
	params := url.Values{
		"fields": {scheduledPostFields},
		"limit":  {strconv.Itoa(pageSize(limit))},
	}
	items, err := c.collect(ctx, pageToken, pageID+"/scheduled_posts", params, limit)
	if err != nil {
		return nil, fmt.Errorf("publications programmées: %w", err)
	}

	out := make([]domain.ScheduledPost, 0, len(items))
	for _, raw := range items {
		var item struct {
			ID                   string `json:"id"`
			Message              string `json:"message"`
			CreatedTime          string `json:"created_time"`
			ScheduledPublishTime any    `json:"scheduled_publish_time"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'une publication programmée: %w", err)
		}
		out = append(out, domain.ScheduledPost{
			PostID:      item.ID,
			Message:     item.Message,
			CreatedTime: item.CreatedTime,
			ScheduledAt: scheduledTime(item.ScheduledPublishTime),
		})
	}
	return out, nil
}

// scheduledTime normalizes scheduled_publish_time, which Meta returns as a
// unix timestamp on some edges and as an ISO string on others.
func scheduledTime(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
	default:
		return ""
	}
}

// SetCommentHidden hides or unhides a comment. Facebook spells the parameter
// is_hidden, Instagram spells it hide, and the caller should not have to know.
func (c *Client) SetCommentHidden(ctx context.Context, pageToken, commentID string, hidden, instagram bool) error {
	field := "is_hidden"
	if instagram {
		field = "hide"
	}
	params := url.Values{field: {strconv.FormatBool(hidden)}}

	var resp struct {
		Success bool `json:"success"`
	}
	if err := c.post(ctx, pageToken, commentID, params, &resp); err != nil {
		return fmt.Errorf("modération du commentaire: %w", err)
	}
	return nil
}

// DeleteObject removes a Graph object: a comment, or a post Meta was holding
// for a scheduled publication.
//
// It goes through POST with the method override rather than a DELETE verb,
// which the Graph API accepts everywhere and keeps the request builder to two
// shapes.
func (c *Client) DeleteObject(ctx context.Context, pageToken, objectID string) error {
	params := url.Values{"method": {"delete"}}

	var resp struct {
		Success bool `json:"success"`
	}
	if err := c.post(ctx, pageToken, objectID, params, &resp); err != nil {
		return fmt.Errorf("suppression: %w", err)
	}
	return nil
}

// PostInsights reads the metrics of a single Page post.
func (c *Client) PostInsights(ctx context.Context, pageToken, postID string, metrics []string) (domain.InsightSet, error) {
	set, err := c.insightsWithFallback(ctx, pageToken, postID+"/insights", url.Values{}, metrics)
	if err != nil {
		return domain.InsightSet{}, fmt.Errorf("statistiques de la publication: %w", err)
	}
	return set, nil
}

// IGMediaInsights reads the metrics of a single Instagram media.
func (c *Client) IGMediaInsights(ctx context.Context, pageToken, mediaID string, metrics []string) (domain.InsightSet, error) {
	set, err := c.insightsWithFallback(ctx, pageToken, mediaID+"/insights", url.Values{}, metrics)
	if err != nil {
		return domain.InsightSet{}, fmt.Errorf("statistiques du média: %w", err)
	}
	return set, nil
}

// IGStories lists the stories still visible on the account. Meta removes them
// from this edge once they are 24 hours old, so an empty list is normal.
func (c *Client) IGStories(ctx context.Context, pageToken, igUserID string) ([]domain.Media, error) {
	params := url.Values{"fields": {igStoryFields}}
	items, err := c.collect(ctx, pageToken, igUserID+"/stories", params, 0)
	if err != nil {
		return nil, fmt.Errorf("stories Instagram: %w", err)
	}

	out := make([]domain.Media, 0, len(items))
	for _, raw := range items {
		var item mediaItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'une story: %w", err)
		}
		out = append(out, domain.Media{
			MediaID:     item.ID,
			Type:        item.MediaType,
			ProductType: item.ProductType,
			Timestamp:   item.Timestamp,
			Caption:     item.Caption,
			Permalink:   item.Permalink,
		})
	}
	return out, nil
}

// igStoryFields is deliberately narrower than igMediaFields: a story carries
// no like or comment count, and asking for them fails the whole request.
const igStoryFields = "id,media_type,media_product_type,timestamp,caption,permalink"
