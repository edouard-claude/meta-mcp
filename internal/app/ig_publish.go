package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/edouard-claude/meta-mcp/internal/domain"
)

// Carousel bounds imposed by Instagram.
const (
	minCarouselItems = 2
	maxCarouselItems = 10
)

// IGPublishInput are the parameters of the ig_publish tool.
type IGPublishInput struct {
	PageID    string
	MediaType string
	ImageURL  string
	VideoURL  string
	Caption   string
	Children  []string
	Confirm   bool
}

// IGPublishOutput is both the preview and the result of an Instagram
// publication.
type IGPublishOutput struct {
	Preview    bool     `json:"preview"`
	MediaID    string   `json:"media_id,omitempty"`
	PageID     string   `json:"page_id"`
	IGUsername string   `json:"ig_username,omitempty"`
	MediaType  string   `json:"media_type"`
	ImageURL   string   `json:"image_url,omitempty"`
	VideoURL   string   `json:"video_url,omitempty"`
	Children   []string `json:"children,omitempty"`
	Caption    string   `json:"caption,omitempty"`
	Notice     string   `json:"notice,omitempty"`
}

// IGPublish publishes on Instagram, or returns a preview when the caller has
// not confirmed.
func (s *Service) IGPublish(ctx context.Context, tenantID string, in IGPublishInput) (*IGPublishOutput, error) {
	page, err := s.igPage(ctx, tenantID, in.PageID)
	if err != nil {
		return nil, err
	}
	req, err := buildIGPublishRequest(in)
	if err != nil {
		return nil, err
	}

	out := &IGPublishOutput{
		PageID:     page.PageID,
		IGUsername: page.IGUsername,
		MediaType:  req.MediaType,
		ImageURL:   req.ImageURL,
		VideoURL:   req.VideoURL,
		Children:   req.Children,
		Caption:    req.Caption,
	}
	if !in.Confirm {
		out.Preview = true
		out.Notice = previewNotice
		return out, nil
	}

	mediaID, err := s.graph.IGPublish(ctx, page.PageToken, page.IGUserID, req)
	if err != nil {
		return nil, err
	}
	out.MediaID = mediaID
	return out, nil
}

// buildIGPublishRequest validates the parameters for the requested media type.
func buildIGPublishRequest(in IGPublishInput) (domain.IGPublishRequest, error) {
	req := domain.IGPublishRequest{
		MediaType: strings.ToUpper(strings.TrimSpace(in.MediaType)),
		ImageURL:  strings.TrimSpace(in.ImageURL),
		VideoURL:  strings.TrimSpace(in.VideoURL),
		Caption:   strings.TrimSpace(in.Caption),
	}
	if req.MediaType == "" {
		req.MediaType = domain.IGMediaTypeImage
	}

	switch req.MediaType {
	case domain.IGMediaTypeStories:
		// A story takes one image or one video, never both, and no caption.
		if req.ImageURL == "" && req.VideoURL == "" {
			return req, errors.New("image_url ou video_url est obligatoire pour media_type=STORIES")
		}
		if req.ImageURL != "" && req.VideoURL != "" {
			return req, errors.New("une story porte soit image_url, soit video_url, pas les deux")
		}
		field, value := "image_url", req.ImageURL
		if req.VideoURL != "" {
			field, value = "video_url", req.VideoURL
		}
		if err := validateHTTPSURL(value, field); err != nil {
			return req, err
		}
		req.Caption = ""
	case domain.IGMediaTypeImage:
		if req.ImageURL == "" {
			return req, errors.New("image_url est obligatoire pour media_type=IMAGE")
		}
		if err := validateHTTPSURL(req.ImageURL, "image_url"); err != nil {
			return req, err
		}
	case domain.IGMediaTypeReels:
		if req.VideoURL == "" {
			return req, errors.New("video_url est obligatoire pour media_type=REELS")
		}
		if err := validateHTTPSURL(req.VideoURL, "video_url"); err != nil {
			return req, err
		}
	case domain.IGMediaTypeCarousel:
		children := make([]string, 0, len(in.Children))
		for i, raw := range in.Children {
			child := strings.TrimSpace(raw)
			if child == "" {
				continue
			}
			if err := validateHTTPSURL(child, fmt.Sprintf("children[%d]", i)); err != nil {
				return req, err
			}
			children = append(children, child)
		}
		if len(children) < minCarouselItems || len(children) > maxCarouselItems {
			return req, fmt.Errorf("un carrousel demande entre %d et %d URLs dans children",
				minCarouselItems, maxCarouselItems)
		}
		req.Children = children
	default:
		return req, fmt.Errorf("media_type doit valoir %s, %s, %s ou %s",
			domain.IGMediaTypeImage, domain.IGMediaTypeReels,
			domain.IGMediaTypeCarousel, domain.IGMediaTypeStories)
	}
	return req, nil
}
