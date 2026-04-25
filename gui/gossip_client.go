package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type GossipPost struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Score     int    `json:"score"`
	Votes     int    `json:"votes"`
	Locked    bool   `json:"locked"`
	CreatedAt string `json:"created_at"`
}

type GossipComment struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	Content   string `json:"content"`
	Rating    int    `json:"rating"`
	CreatedAt string `json:"created_at"`
}

type GossipPublishResult struct {
	OK   bool       `json:"ok"`
	Post GossipPost `json:"post"`
}

type GossipBrowseResult struct {
	OK    bool         `json:"ok"`
	Posts []GossipPost `json:"posts"`
	Total int          `json:"total"`
	Page  int          `json:"page"`
}

type GossipCommentResult struct {
	OK      bool          `json:"ok"`
	Comment GossipComment `json:"comment"`
}

type GossipCommentsResult struct {
	OK       bool            `json:"ok"`
	Comments []GossipComment `json:"comments"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
}

type GossipSnapshotResult struct {
	Changed bool         `json:"changed"`
	Posts   []GossipPost `json:"posts,omitempty"`
	Total   int          `json:"total,omitempty"`
	ETag    string       `json:"etag,omitempty"`
}

type GossipClient struct {
	app    *App
	client *http.Client
}

func NewGossipClient(app *App) *GossipClient {
	return &GossipClient{
		app:    app,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *GossipClient) machineID() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return cfg.RemoteMachineID
}

func (c *GossipClient) userEmail() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return cfg.RemoteEmail
}

func readErrorBody(body io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(body, 4096))
	return string(b)
}

func (c *GossipClient) requireBase(ctx context.Context) (string, []string, error) {
	base, discovered, err := c.app.resolveHubCenterBaseURLCached(ctx, c.client)
	if err != nil {
		return "", nil, err
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return base, discovered, nil
}

func (c *GossipClient) requireWrite(ctx context.Context) (base, mid string, discovered []string, err error) {
	base, discovered, err = c.requireBase(ctx)
	if err != nil {
		return "", "", nil, err
	}
	mid = c.machineID()
	if mid == "" {
		return "", "", nil, fmt.Errorf("machine_id not configured")
	}
	return base, mid, discovered, nil
}

var errGossipForbidden = fmt.Errorf("gossip is disabled by hub policy")

func (c *GossipClient) checkGossipPermission() error {
	if c.app != nil && !c.app.isGossipAllowed() {
		return errGossipForbidden
	}
	return nil
}

func (c *GossipClient) PublishPost(ctx context.Context, content, category string) (*GossipPublishResult, error) {
	if err := c.checkGossipPermission(); err != nil {
		return nil, err
	}
	base, mid, discovered, err := c.requireWrite(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{
		"machine_id": mid,
		"user_email": c.userEmail(),
		"content":    content,
		"category":   category,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/publish", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var result GossipPublishResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return &result, nil
}

func (c *GossipClient) BrowsePosts(ctx context.Context, page int) (*GossipBrowseResult, error) {
	var result GossipBrowseResult
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, fmt.Sprintf("/api/gossip/browse?page=%d", page), 0, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *GossipClient) AddComment(ctx context.Context, postID, content string, rating int) (*GossipCommentResult, error) {
	if err := c.checkGossipPermission(); err != nil {
		return nil, err
	}
	base, mid, discovered, err := c.requireWrite(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"machine_id": mid,
		"user_email": c.userEmail(),
		"post_id":    postID,
		"content":    content,
		"rating":     rating,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/comment", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	var result GossipCommentResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return &result, nil
}

func (c *GossipClient) RatePost(ctx context.Context, postID string, rating int) error {
	if err := c.checkGossipPermission(); err != nil {
		return err
	}
	base, mid, discovered, err := c.requireWrite(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"machine_id": mid,
		"user_email": c.userEmail(),
		"post_id":    postID,
		"rating":     rating,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/rate", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	c.app.rememberHubCenterSelection(base, discovered)
	return nil
}

func (c *GossipClient) GetComments(ctx context.Context, postID string, page int) (*GossipCommentsResult, error) {
	var result GossipCommentsResult
	path := fmt.Sprintf("/api/gossip/comments?post_id=%s&page=%d", url.QueryEscape(postID), page)
	if _, _, err := c.app.getHubCenterJSON(ctx, c.client, path, 0, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *GossipClient) GetSnapshot(ctx context.Context, etag string) (*GossipSnapshotResult, error) {
	bases, err := c.app.resolveHubCenterCandidates(ctx, c.client)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/gossip/snapshot", nil)
		if err != nil {
			return nil, err
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotModified {
				lastErr = nil
				return
			}
			if resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
				return
			}
			var result GossipSnapshotResult
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				lastErr = fmt.Errorf("decode response: %w", err)
				return
			}
			result.Changed = true
			if newETag := resp.Header.Get("ETag"); newETag != "" {
				result.ETag = newETag
			}
			c.app.rememberHubCenterSelection(base, bases)
			lastErr = snapshotResultError{result: &result}
		}()
		if lastErr == nil {
			c.app.rememberHubCenterSelection(base, bases)
			return &GossipSnapshotResult{Changed: false}, nil
		}
		if wrapped, ok := lastErr.(snapshotResultError); ok {
			return wrapped.result, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no reachable hubcenter")
}

type snapshotResultError struct {
	result *GossipSnapshotResult
}

func (e snapshotResultError) Error() string {
	return "snapshot result"
}
