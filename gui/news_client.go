package main

import (
	"context"
	"fmt"
	"net/http"
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
	client := &http.Client{Timeout: 8 * time.Second}
	var result struct {
		Articles []NewsArticle `json:"articles"`
	}
	if _, _, err := a.getHubCenterJSON(context.Background(), client, "/api/news?limit=2", 512*1024, &result); err != nil {
		return nil, fmt.Errorf("fetch news: %w", err)
	}
	return result.Articles, nil
}
