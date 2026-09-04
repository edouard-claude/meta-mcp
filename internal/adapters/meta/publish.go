package meta

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/edouard/metasocial-mcp/internal/domain"
)

// Instagram container polling: Meta processes a media asynchronously, so the
// container has to be polled until it reports FINISHED.
const (
	defaultPollDelay = 2 * time.Second
	maxPollAttempts  = 30
)

// idResponse is the answer of every write endpoint.
type idResponse struct {
	ID     string `json:"id"`
	PostID string `json:"post_id"`
}

// PublishPost publishes on a Facebook Page, either a link/text post on /feed
// or a photo on /photos. A non-zero ScheduledAt hands the post to Meta's own
// scheduler instead of publishing right away.
func (c *Client) PublishPost(ctx context.Context, pageToken, pageID string, req domain.PublishPostRequest) (string, error) {
	params := url.Values{}
	path := pageID + "/feed"

	if req.IsPhoto() {
		path = pageID + "/photos"
		params.Set("url", req.PhotoURL)
		if req.Message != "" {
			params.Set("caption", req.Message)
		}
	} else {
		params.Set("message", req.Message)
		if req.Link != "" {
			params.Set("link", req.Link)
		}
	}

	if req.IsScheduled() {
		params.Set("published", "false")
		params.Set("scheduled_publish_time", strconv.FormatInt(req.ScheduledAt.Unix(), 10))
	}

	var resp idResponse
	if err := c.post(ctx, pageToken, path, params, &resp); err != nil {
		return "", fmt.Errorf("publication sur la page: %w", err)
	}
	if id := firstNonEmpty(resp.PostID, resp.ID); id != "" {
		return id, nil
	}
	return "", errors.New("publication sur la page: Meta n'a renvoyé aucun identifiant")
}

// ReplyToComment answers a comment on a Facebook Page post.
func (c *Client) ReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error) {
	var resp idResponse
	params := url.Values{"message": {message}}
	if err := c.post(ctx, pageToken, commentID+"/comments", params, &resp); err != nil {
		return "", fmt.Errorf("réponse au commentaire: %w", err)
	}
	if resp.ID == "" {
		return "", errors.New("réponse au commentaire: Meta n'a renvoyé aucun identifiant")
	}
	return resp.ID, nil
}

// IGReplyToComment answers a comment on an Instagram media.
func (c *Client) IGReplyToComment(ctx context.Context, pageToken, commentID, message string) (string, error) {
	var resp idResponse
	params := url.Values{"message": {message}}
	if err := c.post(ctx, pageToken, commentID+"/replies", params, &resp); err != nil {
		return "", fmt.Errorf("réponse au commentaire Instagram: %w", err)
	}
	if resp.ID == "" {
		return "", errors.New("réponse au commentaire Instagram: Meta n'a renvoyé aucun identifiant")
	}
	return resp.ID, nil
}

// IGPublish runs Meta's two step Instagram publication: build a container,
// wait for it to be processed, then publish it.
func (c *Client) IGPublish(ctx context.Context, pageToken, igUserID string, req domain.IGPublishRequest) (string, error) {
	containerID, err := c.createIGContainer(ctx, pageToken, igUserID, req)
	if err != nil {
		return "", err
	}
	if err := c.waitForIGContainer(ctx, pageToken, containerID); err != nil {
		return "", err
	}

	var resp idResponse
	params := url.Values{"creation_id": {containerID}}
	if err := c.post(ctx, pageToken, igUserID+"/media_publish", params, &resp); err != nil {
		return "", fmt.Errorf("publication Instagram: %w", err)
	}
	if resp.ID == "" {
		return "", errors.New("publication Instagram: Meta n'a renvoyé aucun identifiant")
	}
	return resp.ID, nil
}

// createIGContainer builds the media container, creating the carousel item
// containers first when needed.
func (c *Client) createIGContainer(ctx context.Context, pageToken, igUserID string, req domain.IGPublishRequest) (string, error) {
	params := url.Values{}
	if req.Caption != "" {
		params.Set("caption", req.Caption)
	}

	switch req.MediaType {
	case domain.IGMediaTypeReels:
		params.Set("media_type", domain.IGMediaTypeReels)
		params.Set("video_url", req.VideoURL)
	case domain.IGMediaTypeCarousel:
		children, err := c.createCarouselItems(ctx, pageToken, igUserID, req.Children)
		if err != nil {
			return "", err
		}
		params.Set("media_type", domain.IGMediaTypeCarousel)
		params.Set("children", strings.Join(children, ","))
	default:
		params.Set("image_url", req.ImageURL)
	}

	var resp idResponse
	if err := c.post(ctx, pageToken, igUserID+"/media", params, &resp); err != nil {
		return "", fmt.Errorf("création du conteneur Instagram: %w", err)
	}
	if resp.ID == "" {
		return "", errors.New("création du conteneur Instagram: aucun identifiant renvoyé")
	}
	return resp.ID, nil
}

// createCarouselItems uploads each carousel slide as its own container.
func (c *Client) createCarouselItems(ctx context.Context, pageToken, igUserID string, urls []string) ([]string, error) {
	children := make([]string, 0, len(urls))
	for i, mediaURL := range urls {
		params := url.Values{
			"image_url":        {mediaURL},
			"is_carousel_item": {"true"},
		}
		var resp idResponse
		if err := c.post(ctx, pageToken, igUserID+"/media", params, &resp); err != nil {
			return nil, fmt.Errorf("création de l'élément %d du carrousel: %w", i+1, err)
		}
		if resp.ID == "" {
			return nil, fmt.Errorf("création de l'élément %d du carrousel: aucun identifiant renvoyé", i+1)
		}
		children = append(children, resp.ID)
	}
	return children, nil
}

// containerStatus is the processing state of an Instagram container.
type containerStatus struct {
	StatusCode string `json:"status_code"`
	Status     string `json:"status"`
}

// waitForIGContainer polls the container until Meta reports it FINISHED.
func (c *Client) waitForIGContainer(ctx context.Context, pageToken, containerID string) error {
	delay := c.pollDelay()
	for attempt := range maxPollAttempts {
		var status containerStatus
		params := url.Values{"fields": {"status_code,status"}}
		if err := c.get(ctx, pageToken, containerID, params, &status); err != nil {
			return fmt.Errorf("suivi du conteneur Instagram: %w", err)
		}
		switch status.StatusCode {
		case "FINISHED":
			return nil
		case "ERROR":
			return fmt.Errorf("Meta a rejeté le média Instagram: %s", status.Status)
		case "EXPIRED":
			return errors.New("le conteneur Instagram a expiré avant d'être publié")
		}

		if attempt == maxPollAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return errors.New("le média Instagram n'a pas fini d'être traité par Meta, réessayez dans quelques minutes")
}

func (c *Client) pollDelay() time.Duration {
	if c.retryDelay > 0 && c.retryDelay < defaultPollDelay {
		// Tests shorten RetryDelay; reuse it so they do not sleep for
		// minutes waiting on a container.
		return c.retryDelay
	}
	return defaultPollDelay
}
