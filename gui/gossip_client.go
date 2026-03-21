package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ── Data structures ─────────────────────────────────────────────────────

// GossipPost 帖子数据（从 HubCenter API 返回）。
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

// GossipComment 评论数据。
type GossipComment struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	Content   string `json:"content"`
	Rating    int    `json:"rating"`
	CreatedAt string `json:"created_at"`
}

// GossipPublishResult 发布帖子的响应。
type GossipPublishResult struct {
	OK   bool       `json:"ok"`
	Post GossipPost `json:"post"`
}

// GossipBrowseResult 浏览帖子列表的响应。
type GossipBrowseResult struct {
	OK    bool         `json:"ok"`
	Posts []GossipPost `json:"posts"`
	Total int          `json:"total"`
	Page  int          `json:"page"`
}

// GossipCommentResult 提交评论的响应。
type GossipCommentResult struct {
	OK      bool          `json:"ok"`
	Comment GossipComment `json:"comment"`
}

// GossipCommentsResult 获取评论列表的响应。
type GossipCommentsResult struct {
	OK       bool            `json:"ok"`
	Comments []GossipComment `json:"comments"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
}

// GossipSnapshotResult 快照轮询的响应。
type GossipSnapshotResult struct {
	Changed bool         `json:"changed"`
	Posts   []GossipPost `json:"posts,omitempty"`
	Total   int          `json:"total,omitempty"`
	ETag    string       `json:"etag,omitempty"`
}

// ── GossipClient ────────────────────────────────────────────────────────

// GossipClient 与 HubCenter Gossip API 交互。
type GossipClient struct {
	app    *App
	client *http.Client
}

// NewGossipClient 创建 GossipClient。
func NewGossipClient(app *App) *GossipClient {
	return &GossipClient{
		app:    app,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *GossipClient) baseURL() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if url == "" {
		url = defaultRemoteHubCenterURL
	}
	return strings.TrimRight(url, "/")
}

func (c *GossipClient) machineID() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.RemoteMachineID)
}

func (c *GossipClient) userEmail() string {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.RemoteEmail)
}

// ── API methods ─────────────────────────────────────────────────────────

// readErrorBody reads at most 4KB from the response body for error messages.
func readErrorBody(body io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(body, 4096))
	return string(b)
}

// requireBase returns the HubCenter base URL or an error if not configured.
func (c *GossipClient) requireBase() (string, error) {
	base := c.baseURL()
	if base == "" {
		return "", fmt.Errorf("hubcenter URL not configured")
	}
	return base, nil
}

// requireWrite returns (base, machineID) or an error if either is missing.
// Write operations (publish/comment/rate) need both.
func (c *GossipClient) requireWrite() (base, mid string, err error) {
	base, err = c.requireBase()
	if err != nil {
		return "", "", err
	}
	mid = c.machineID()
	if mid == "" {
		return "", "", fmt.Errorf("machine_id not configured")
	}
	return base, mid, nil
}

// PublishPost 发布帖子。POST /api/gossip/publish
func (c *GossipClient) PublishPost(ctx context.Context, content, category string) (*GossipPublishResult, error) {
	base, mid, err := c.requireWrite()
	if err != nil {
		return nil, err
	}

	payload, _ := json.Marshal(map[string]string{
		"machine_id": mid,
		"user_email": c.userEmail(),
		"content":    content,
		"category":   category,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/gossip/publish", bytes.NewReader(payload))
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
	return &result, nil
}

// BrowsePosts 浏览帖子列表。GET /api/gossip/browse?page=N
func (c *GossipClient) BrowsePosts(ctx context.Context, page int) (*GossipBrowseResult, error) {
	base, err := c.requireBase()
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/api/gossip/browse?page=%d", base, page)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var result GossipBrowseResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// AddComment 提交评论。POST /api/gossip/comment
func (c *GossipClient) AddComment(ctx context.Context, postID, content string, rating int) (*GossipCommentResult, error) {
	base, mid, err := c.requireWrite()
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

	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/gossip/comment", bytes.NewReader(payload))
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
	return &result, nil
}

// RatePost 评分帖子。POST /api/gossip/rate
func (c *GossipClient) RatePost(ctx context.Context, postID string, rating int) error {
	base, mid, err := c.requireWrite()
	if err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"machine_id": mid,
		"user_email": c.userEmail(),
		"post_id":    postID,
		"rating":     rating,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/gossip/rate", bytes.NewReader(payload))
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
	return nil
}

// GetComments 获取评论列表。GET /api/gossip/comments?post_id=X&page=N
func (c *GossipClient) GetComments(ctx context.Context, postID string, page int) (*GossipCommentsResult, error) {
	base, err := c.requireBase()
	if err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/api/gossip/comments?post_id=%s&page=%d", base, url.QueryEscape(postID), page)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var result GossipCommentsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetSnapshot 获取快照数据，支持 ETag 条件请求。GET /api/gossip/snapshot
// 当服务端返回 304 Not Modified 时，返回 Changed=false。
func (c *GossipClient) GetSnapshot(ctx context.Context, etag string) (*GossipSnapshotResult, error) {
	base, err := c.requireBase()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/gossip/snapshot", nil)
	if err != nil {
		return nil, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 304 Not Modified — data unchanged
	if resp.StatusCode == http.StatusNotModified {
		return &GossipSnapshotResult{Changed: false}, nil
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var result GossipSnapshotResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	result.Changed = true
	// Capture ETag from response header if present
	if newETag := resp.Header.Get("ETag"); newETag != "" {
		result.ETag = newETag
	}
	return &result, nil
}
