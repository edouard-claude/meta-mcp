package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

var _ domain.GraphClient = (*Client)(nil)

// Field sets requested from the Graph API, kept next to the call that uses
// them so a change to one never silently breaks the decoding of another.
const (
	// postFields inlines the post insights so a listing costs one request.
	// post_impressions_unique is gone from the set: Meta deprecated it and
	// rejects the whole expansion when it appears.
	postFields = "id,message,created_time,permalink_url," +
		"insights.metric(post_impressions,post_clicks,post_reactions_by_type_total)"
	// postFieldsPlain drops the expansion entirely. It is the fallback when
	// Meta refuses the inlined metrics, so a deprecated metric costs the
	// caller its numbers rather than the whole listing.
	postFieldsPlain = "id,message,created_time,permalink_url"
	commentFields   = "id,from{name},message,created_time"
	igCommentFields = "id,username,text,timestamp"
	igMediaFields   = "id,caption,media_type,media_product_type,timestamp,permalink," +
		"like_count,comments_count," +
		"insights.metric(reach,views,saved,shares,total_interactions)"
	// igMediaFieldsPlain is the same fallback as postFieldsPlain: keep the
	// media even when Meta refuses the inlined insights.
	igMediaFieldsPlain = "id,caption,media_type,media_product_type,timestamp,permalink," +
		"like_count,comments_count"
)

// insightItem is one entry of an /insights response. Facebook fills Values;
// Instagram, queried with metric_type=total_value, fills TotalValue instead.
type insightItem struct {
	Name   string `json:"name"`
	Period string `json:"period"`
	Title  string `json:"title"`
	Values []struct {
		Value   json.RawMessage `json:"value"`
		EndTime string          `json:"end_time"`
	} `json:"values"`
	TotalValue struct {
		Value      json.RawMessage `json:"value"`
		Breakdowns []struct {
			DimensionKeys []string `json:"dimension_keys"`
			Results       []struct {
				DimensionValues []string `json:"dimension_values"`
				Value           int64    `json:"value"`
			} `json:"results"`
		} `json:"breakdowns"`
	} `json:"total_value"`
}

// toInsight normalizes both shapes into a single domain.Insight.
func (i insightItem) toInsight() domain.Insight {
	out := domain.Insight{Metric: i.Name, Period: i.Period, Title: i.Title, Values: []domain.InsightValue{}}
	for _, v := range i.Values {
		out.Values = append(out.Values, domain.InsightValue{EndTime: v.EndTime, Value: v.Value})
	}
	if len(out.Values) == 0 && len(i.TotalValue.Value) > 0 {
		out.Values = append(out.Values, domain.InsightValue{Value: i.TotalValue.Value})
	}
	return out
}

// decodeInsights turns a paginated /insights payload into domain insights.
func decodeInsights(items []json.RawMessage) ([]domain.Insight, error) {
	out := make([]domain.Insight, 0, len(items))
	for _, raw := range items {
		var item insightItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'une métrique: %w", err)
		}
		out = append(out, item.toInsight())
	}
	return out, nil
}

// codeUnsupportedMetric is what Meta answers when one name in the metric list
// is unknown, deprecated, or not available on that object. It condemns the
// whole batch, which is why insightsWithFallback exists.
const codeUnsupportedMetric = 100

// PageInsights reads the organic metrics of a Facebook Page over a window.
func (c *Client) PageInsights(ctx context.Context, pageToken, pageID string, metrics []string, since, until time.Time) (domain.InsightSet, error) {
	base := unixParams(url.Values{"period": {"day"}}, since, until)
	set, err := c.insightsWithFallback(ctx, pageToken, pageID+"/insights", base, metrics)
	if err != nil {
		return domain.InsightSet{}, fmt.Errorf("statistiques de la page: %w", err)
	}
	return set, nil
}

// insightsWithFallback asks for every metric at once, and falls back to one
// request per metric when Meta rejects the batch over a single bad name. The
// metrics that still fail are reported as rejected rather than failing the
// whole call, so one deprecated name never costs the caller the rest.
func (c *Client) insightsWithFallback(ctx context.Context, token, path string, base url.Values, metrics []string) (domain.InsightSet, error) {
	if len(metrics) == 0 {
		return domain.InsightSet{Insights: []domain.Insight{}}, nil
	}

	set, err := c.insights(ctx, token, path, base, metrics)
	if err == nil {
		return set, nil
	}
	if ge, ok := domain.AsGraphError(err); !ok || ge.Code != codeUnsupportedMetric {
		return domain.InsightSet{}, err
	}

	out := domain.InsightSet{Insights: []domain.Insight{}}
	for _, metric := range metrics {
		one, err := c.insights(ctx, token, path, base, []string{metric})
		if err == nil {
			out.Insights = append(out.Insights, one.Insights...)
			continue
		}
		// Only an unsupported metric is skipped. An expired token or a
		// quota is a real failure and must reach the caller.
		if ge, ok := domain.AsGraphError(err); ok && ge.Code == codeUnsupportedMetric {
			out.Rejected = append(out.Rejected, metric)
			continue
		}
		return domain.InsightSet{}, err
	}
	return out, nil
}

// insights performs one /insights request for the given metric list.
func (c *Client) insights(ctx context.Context, token, path string, base url.Values, metrics []string) (domain.InsightSet, error) {
	params := url.Values{}
	for k, v := range base {
		params[k] = v
	}
	params.Set("metric", strings.Join(metrics, ","))

	items, err := c.collect(ctx, token, path, params, 0)
	if err != nil {
		return domain.InsightSet{}, err
	}
	insights, err := decodeInsights(items)
	if err != nil {
		return domain.InsightSet{}, err
	}
	return domain.InsightSet{Insights: insights}, nil
}

// postItem is one entry of /{page-id}/posts with its insights inlined.
type postItem struct {
	ID           string `json:"id"`
	Message      string `json:"message"`
	CreatedTime  string `json:"created_time"`
	PermalinkURL string `json:"permalink_url"`
	Insights     struct {
		Data []insightItem `json:"data"`
	} `json:"insights"`
}

// PagePosts lists the posts of a page with their organic performance.
func (c *Client) PagePosts(ctx context.Context, pageToken, pageID string, since time.Time, limit int) ([]domain.Post, error) {
	params := unixParams(url.Values{
		"fields": {postFields},
		"limit":  {strconv.Itoa(pageSize(limit))},
	}, since, time.Time{})

	items, err := c.collect(ctx, pageToken, pageID+"/posts", params, limit)
	if err != nil {
		// A metric Meta no longer serves condemns the whole expansion. The
		// posts themselves are still readable, and a listing without its
		// numbers beats no listing at all.
		if ge, ok := domain.AsGraphError(err); !ok || ge.Code != codeUnsupportedMetric {
			return nil, fmt.Errorf("publications de la page: %w", err)
		}
		params.Set("fields", postFieldsPlain)
		if items, err = c.collect(ctx, pageToken, pageID+"/posts", params, limit); err != nil {
			return nil, fmt.Errorf("publications de la page: %w", err)
		}
	}

	posts := make([]domain.Post, 0, len(items))
	for _, raw := range items {
		var item postItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'une publication: %w", err)
		}
		post := domain.Post{
			PostID:      item.ID,
			CreatedTime: item.CreatedTime,
			Message:     item.Message,
			Permalink:   item.PermalinkURL,
		}
		for _, in := range item.Insights.Data {
			switch in.Name {
			case "post_impressions_unique":
				post.ImpressionsUnique = firstMetricValue(in)
			case "post_clicks":
				post.Clicks = firstMetricValue(in)
			case "post_reactions_by_type_total":
				post.Reactions = firstMetricValue(in)
			}
		}
		posts = append(posts, post)
	}
	return posts, nil
}

// commentItem covers both the Facebook and the Instagram comment shapes.
type commentItem struct {
	ID   string `json:"id"`
	From struct {
		Name string `json:"name"`
	} `json:"from"`
	Username    string `json:"username"`
	Message     string `json:"message"`
	Text        string `json:"text"`
	CreatedTime string `json:"created_time"`
	Timestamp   string `json:"timestamp"`
}

func (i commentItem) toComment() domain.Comment {
	return domain.Comment{
		CommentID:   i.ID,
		From:        i.From.Name,
		Username:    i.Username,
		Message:     firstNonEmpty(i.Message, i.Text),
		CreatedTime: firstNonEmpty(i.CreatedTime, i.Timestamp),
	}
}

// PostComments lists the comments of a Facebook Page post.
func (c *Client) PostComments(ctx context.Context, pageToken, postID string, limit int) ([]domain.Comment, error) {
	return c.comments(ctx, pageToken, postID+"/comments", commentFields, limit, "commentaires de la publication")
}

// IGMediaComments lists the comments of an Instagram media.
func (c *Client) IGMediaComments(ctx context.Context, pageToken, mediaID string, limit int) ([]domain.Comment, error) {
	return c.comments(ctx, pageToken, mediaID+"/comments", igCommentFields, limit, "commentaires du média")
}

func (c *Client) comments(ctx context.Context, token, path, fields string, limit int, what string) ([]domain.Comment, error) {
	params := url.Values{
		"fields": {fields},
		"limit":  {strconv.Itoa(pageSize(limit))},
	}
	items, err := c.collect(ctx, token, path, params, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	out := make([]domain.Comment, 0, len(items))
	for _, raw := range items {
		var item commentItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'un commentaire: %w", err)
		}
		out = append(out, item.toComment())
	}
	return out, nil
}

// timeSeriesIGMetrics are the Instagram metrics that Meta refuses under
// metric_type=total_value and only serves as a plain daily series.
var timeSeriesIGMetrics = map[string]bool{
	"follower_count": true,
}

// IGAccountInsights reads the account level Instagram metrics.
//
// The metrics do not all live under the same query shape: most need
// metric_type=total_value, while follower_count is rejected with it. They are
// therefore split into two requests and merged back, so the caller never has
// to know about the distinction.
func (c *Client) IGAccountInsights(ctx context.Context, pageToken, igUserID string, metrics []string, since, until time.Time) (domain.InsightSet, error) {
	var totals, series []string
	for _, m := range metrics {
		if timeSeriesIGMetrics[m] {
			series = append(series, m)
		} else {
			totals = append(totals, m)
		}
	}

	path := igUserID + "/insights"
	out := domain.InsightSet{Insights: []domain.Insight{}}

	if len(totals) > 0 {
		base := unixParams(url.Values{
			"period":      {"day"},
			"metric_type": {"total_value"},
		}, since, until)
		set, err := c.insightsWithFallback(ctx, pageToken, path, base, totals)
		if err != nil {
			return domain.InsightSet{}, fmt.Errorf("statistiques Instagram: %w", err)
		}
		out.Insights = append(out.Insights, set.Insights...)
		out.Rejected = append(out.Rejected, set.Rejected...)
	}

	if len(series) > 0 {
		base := unixParams(url.Values{"period": {"day"}}, since, until)
		set, err := c.insightsWithFallback(ctx, pageToken, path, base, series)
		if err != nil {
			return domain.InsightSet{}, fmt.Errorf("statistiques Instagram: %w", err)
		}
		out.Insights = append(out.Insights, set.Insights...)
		out.Rejected = append(out.Rejected, set.Rejected...)
	}
	return out, nil
}

// IGFollowerDemographics reads one demographic breakdown of the followers.
func (c *Client) IGFollowerDemographics(ctx context.Context, pageToken, igUserID, breakdown string) ([]domain.Breakdown, error) {
	params := url.Values{
		"metric":      {"follower_demographics"},
		"period":      {"lifetime"},
		"metric_type": {"total_value"},
		"breakdown":   {breakdown},
	}
	items, err := c.collect(ctx, pageToken, igUserID+"/insights", params, 0)
	if err != nil {
		return nil, fmt.Errorf("démographie des abonnés: %w", err)
	}

	out := []domain.Breakdown{}
	for _, raw := range items {
		var item insightItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage de la démographie: %w", err)
		}
		for _, b := range item.TotalValue.Breakdowns {
			for _, res := range b.Results {
				out = append(out, domain.Breakdown{
					Key:   strings.Join(res.DimensionValues, " / "),
					Value: res.Value,
				})
			}
		}
	}
	return out, nil
}

// mediaItem is one entry of /{ig-user-id}/media with its insights inlined.
type mediaItem struct {
	ID            string `json:"id"`
	Caption       string `json:"caption"`
	MediaType     string `json:"media_type"`
	ProductType   string `json:"media_product_type"`
	Timestamp     string `json:"timestamp"`
	Permalink     string `json:"permalink"`
	LikeCount     int64  `json:"like_count"`
	CommentsCount int64  `json:"comments_count"`
	Insights      struct {
		Data []insightItem `json:"data"`
	} `json:"insights"`
}

// IGMedia lists the Instagram media of an account with their insights.
func (c *Client) IGMedia(ctx context.Context, pageToken, igUserID string, since time.Time, limit int) ([]domain.Media, error) {
	params := unixParams(url.Values{
		"fields": {igMediaFields},
		"limit":  {strconv.Itoa(pageSize(limit))},
	}, since, time.Time{})

	items, err := c.collect(ctx, pageToken, igUserID+"/media", params, limit)
	if err != nil {
		if ge, ok := domain.AsGraphError(err); !ok || ge.Code != codeUnsupportedMetric {
			return nil, fmt.Errorf("médias Instagram: %w", err)
		}
		params.Set("fields", igMediaFieldsPlain)
		if items, err = c.collect(ctx, pageToken, igUserID+"/media", params, limit); err != nil {
			return nil, fmt.Errorf("médias Instagram: %w", err)
		}
	}

	media := make([]domain.Media, 0, len(items))
	for _, raw := range items {
		var item mediaItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("décodage d'un média: %w", err)
		}
		m := domain.Media{
			MediaID:       item.ID,
			Type:          item.MediaType,
			ProductType:   item.ProductType,
			Timestamp:     item.Timestamp,
			Caption:       item.Caption,
			Permalink:     item.Permalink,
			LikeCount:     item.LikeCount,
			CommentsCount: item.CommentsCount,
		}
		for _, in := range item.Insights.Data {
			switch in.Name {
			case "reach":
				m.Reach = firstMetricValue(in)
			case "views":
				m.Views = firstMetricValue(in)
			case "saved":
				m.Saved = firstMetricValue(in)
			case "shares":
				m.Shares = firstMetricValue(in)
			case "total_interactions":
				m.TotalInteractions = firstMetricValue(in)
			}
		}
		media = append(media, m)
	}
	return media, nil
}

// firstMetricValue extracts the single number carried by a post or media
// insight. post_reactions_by_type_total returns an object keyed by reaction
// type, which is summed.
func firstMetricValue(in insightItem) int64 {
	var raw json.RawMessage
	switch {
	case len(in.Values) > 0:
		raw = in.Values[0].Value
	case len(in.TotalValue.Value) > 0:
		raw = in.TotalValue.Value
	default:
		return 0
	}

	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number
	}
	var byType map[string]int64
	if err := json.Unmarshal(raw, &byType); err == nil {
		var total int64
		for _, v := range byType {
			total += v
		}
		return total
	}
	return 0
}

// pageSize is the Graph page size to ask for: the caller's limit, capped at
// what Meta accepts on a single page.
func pageSize(limit int) int {
	const graphMaxPageSize = 100
	if limit <= 0 || limit > graphMaxPageSize {
		return graphMaxPageSize
	}
	return limit
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
