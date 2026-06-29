package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HubUserRanking is returned by GetHubUserRanking to the frontend.
type HubUserRanking struct {
	TotalTokens     int64  `json:"total_tokens"`
	DurationSeconds int64  `json:"duration_seconds"`
	TokenRank       int    `json:"token_rank"`
	DurationRank    int    `json:"duration_rank"`
	TotalUsers      int    `json:"total_users"`
	Period          string `json:"period"`
	Error           string `json:"error,omitempty"`
}

// GetHubUserRanking queries the Hub for the current user's usage ranking.
func (a *App) GetHubUserRanking() HubUserRanking {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubUserRanking{Error: "config load failed"}
	}
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return HubUserRanking{Error: "hub not configured"}
	}

	data, err := a.getHubJSON(hubURL, viewerToken, "/api/my-ranking")
	if err != nil {
		return HubUserRanking{Error: fmt.Sprintf("hub request failed: %v", err)}
	}

	var resp HubUserRanking
	if err := json.Unmarshal(data, &resp); err != nil {
		return HubUserRanking{Error: "invalid response from hub"}
	}
	return resp
}
