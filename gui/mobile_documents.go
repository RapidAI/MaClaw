package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MobileDocumentDraftSummary is a Hub-shared emergency draft visible on desktop.
type MobileDocumentDraftSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Template  string `json:"template"`
	UpdatedAt string `json:"updated_at"`
	RuneCount int    `json:"rune_count"`
	Preview   string `json:"preview"`
	Markdown  string `json:"markdown,omitempty"`
}

// ListMobileDocumentDrafts returns the viewer's mobile/Hub drafts (same library as phone).
// Requires remote Hub URL + viewer token (desktop login).
func (a *App) ListMobileDocumentDrafts(limit int, includeBody bool) ([]MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to list mobile documents")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if includeBody {
		q.Set("include_body", "1")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/drafts?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list mobile documents failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list mobile documents failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Drafts []MobileDocumentDraftSummary `json:"drafts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode mobile documents: %w", err)
	}
	if payload.Drafts == nil {
		return []MobileDocumentDraftSummary{}, nil
	}
	return payload.Drafts, nil
}

// GetMobileDocumentDraft fetches one draft (full markdown) from Hub.
func (a *App) GetMobileDocumentDraft(draftID string) (*MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(draftID)
	if id == "" {
		return nil, fmt.Errorf("draft id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to open mobile documents")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/drafts/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get mobile document failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get mobile document failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Draft *MobileDocumentDraftSummary `json:"draft"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode mobile document: %w", err)
	}
	if payload.Draft == nil || strings.TrimSpace(payload.Draft.ID) == "" {
		// Single-draft payload uses mobileDocumentDraftPayload shape (id/title/markdown).
		var alt map[string]any
		if err := json.Unmarshal(data, &alt); err == nil {
			if draftMap, ok := alt["draft"].(map[string]any); ok {
				out := &MobileDocumentDraftSummary{
					ID:        stringFromAny(draftMap["id"]),
					Title:     stringFromAny(draftMap["title"]),
					Template:  stringFromAny(draftMap["template"]),
					Markdown:  stringFromAny(draftMap["markdown"]),
					UpdatedAt: stringFromAny(draftMap["updated_at"]),
				}
				return out, nil
			}
		}
		return nil, fmt.Errorf("Hub did not return a draft")
	}
	return payload.Draft, nil
}
