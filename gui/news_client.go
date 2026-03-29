package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NewsArticle is a single announcement from Hub Center.
type NewsArticle struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Category  string `json:"category"`
	Pinned    bool   `json:"pinned"`
	CreatedAt string `json:"created_at"`
}

// FetchNews retrieves the latest news articles from Hub Center.
// It is exposed as a Wails binding so the frontend can call it.
func (a *App) FetchNews() ([]NewsArticle, error) {
	base := a.newsBaseURL()
	if base == "" {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(base + "/api/news?limit=2")
	if err != nil {
		return nil, fmt.Errorf("fetch news: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch news: status %d", resp.StatusCode)
	}
	// Limit response body to 512KB to prevent abuse
	limited := io.LimitReader(resp.Body, 512*1024)
	var result struct {
		Articles []NewsArticle `json:"articles"`
	}
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode news: %w", err)
	}
	return result.Articles, nil
}

func (a *App) newsBaseURL() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if url == "" {
		url = defaultRemoteHubCenterURL
	}
	return strings.TrimRight(url, "/")
}
