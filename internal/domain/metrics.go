package domain

import (
	"encoding/json"
	"time"
)

// Insight is one metric time series returned by the Graph API. The value of a
// data point is left as raw JSON because Meta returns integers, floats and
// objects under the same field.
type Insight struct {
	Metric string         `json:"metric"`
	Period string         `json:"period"`
	Title  string         `json:"title,omitempty"`
	Values []InsightValue `json:"values"`
}

// InsightValue is a single data point of an Insight.
type InsightValue struct {
	EndTime string          `json:"end_time,omitempty"`
	Value   json.RawMessage `json:"value"`
}

// InsightSet is the answer of an insights query. Meta rejects a whole batch
// when a single metric name is unsupported, so metrics that could not be read
// are reported in Rejected instead of failing the call.
type InsightSet struct {
	Insights []Insight `json:"insights"`
	Rejected []string  `json:"rejected,omitempty"`
}

// InsightMeta describes a metric this server knows how to request.
type InsightMeta struct {
	Name        string `json:"name"`
	Period      string `json:"period,omitempty"`
	Description string `json:"description,omitempty"`
	Surface     string `json:"surface,omitempty"`
}

// Post is a Facebook Page post with its organic insights flattened.
type Post struct {
	PostID            string `json:"post_id"`
	CreatedTime       string `json:"created_time"`
	Message           string `json:"message,omitempty"`
	Permalink         string `json:"permalink,omitempty"`
	ImpressionsUnique int64  `json:"impressions_unique"`
	Clicks            int64  `json:"clicks"`
	Reactions         int64  `json:"reactions"`
}

// Comment is a comment on a Page post or on an Instagram media.
type Comment struct {
	CommentID   string `json:"comment_id"`
	From        string `json:"from,omitempty"`
	Username    string `json:"username,omitempty"`
	Message     string `json:"message,omitempty"`
	CreatedTime string `json:"created_time,omitempty"`
}

// Media is an Instagram media with its insights flattened.
type Media struct {
	MediaID           string `json:"media_id"`
	Type              string `json:"type,omitempty"`
	ProductType       string `json:"product_type,omitempty"`
	Timestamp         string `json:"timestamp,omitempty"`
	Caption           string `json:"caption,omitempty"`
	Permalink         string `json:"permalink,omitempty"`
	LikeCount         int64  `json:"like_count"`
	CommentsCount     int64  `json:"comments_count"`
	Reach             int64  `json:"reach"`
	Views             int64  `json:"views"`
	Saved             int64  `json:"saved"`
	Shares            int64  `json:"shares"`
	TotalInteractions int64  `json:"total_interactions"`
}

// Breakdown is one bucket of a demographic breakdown (city, country, age,
// gender).
type Breakdown struct {
	Key   string `json:"key"`
	Value int64  `json:"value"`
}

// PublishPostRequest describes a Page feed or photo publication.
type PublishPostRequest struct {
	Message     string
	Link        string
	PhotoURL    string
	ScheduledAt time.Time // zero means publish now
}

// IsPhoto reports whether the request must go through /{page-id}/photos.
func (r PublishPostRequest) IsPhoto() bool { return r.PhotoURL != "" }

// IsScheduled reports whether Meta must hold the post until ScheduledAt.
func (r PublishPostRequest) IsScheduled() bool { return !r.ScheduledAt.IsZero() }

// Instagram media types accepted by ig_publish.
const (
	IGMediaTypeImage    = "IMAGE"
	IGMediaTypeReels    = "REELS"
	IGMediaTypeCarousel = "CAROUSEL"
	IGMediaTypeStories  = "STORIES"
)

// ScheduledPost is a Page post Meta is holding until its publication time.
type ScheduledPost struct {
	PostID      string `json:"post_id"`
	Message     string `json:"message,omitempty"`
	CreatedTime string `json:"created_time,omitempty"`
	ScheduledAt string `json:"scheduled_at,omitempty"`
}

// Comment moderation actions accepted by the moderation tools.
const (
	ModerationHide   = "hide"
	ModerationUnhide = "unhide"
	ModerationDelete = "delete"
)

// IGPublishRequest describes an Instagram publication. Meta requires a two
// step flow: create a container, then publish it once it is ready.
type IGPublishRequest struct {
	MediaType string
	ImageURL  string
	VideoURL  string
	Caption   string
	Children  []string // carousel item URLs
}
