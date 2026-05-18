package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Types — Wails binding layer (mirrors hub types but separate for decoupling)
// ---------------------------------------------------------------------------

// DirectoryResponse is the paginated response returned by GetWorkflowDirectory.
type DirectoryResponse struct {
	Items    []DirectoryItem `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// DirectoryItem represents a single item in a workflow directory view.
type DirectoryItem struct {
	InstanceID    string  `json:"instance_id"`
	WorkflowName  string  `json:"workflow_name"`
	Status        string  `json:"status"`
	CurrentNode   string  `json:"current_node,omitempty"`
	InitiatorName string  `json:"initiator_name,omitempty"`
	InitiatedAt   string  `json:"initiated_at"`
	CompletedAt   *string `json:"completed_at,omitempty"`
	Result        string  `json:"result,omitempty"`
	UserRole      string  `json:"user_role,omitempty"`
	Urgency       string  `json:"urgency,omitempty"`
	TimeRemaining *int    `json:"time_remaining_hours,omitempty"`
	ConfirmType   string  `json:"confirm_type,omitempty"`
}

// DirectoryFilter contains filter parameters for directory queries.
type DirectoryFilter struct {
	Status       string  `json:"status,omitempty"`
	DateFrom     *string `json:"date_from,omitempty"`
	DateTo       *string `json:"date_to,omitempty"`
	WorkflowType string  `json:"workflow_type,omitempty"`
	Role         string  `json:"role,omitempty"`
	Result       string  `json:"result,omitempty"`
	Page         int     `json:"page"`
	PageSize     int     `json:"page_size"`
}

// ---------------------------------------------------------------------------
// View → API path mapping
// ---------------------------------------------------------------------------

var directoryViewPaths = map[string]string{
	"initiated":            "/api/v1/directory/initiated",
	"pending_action":       "/api/v1/directory/pending-action",
	"pending_confirmation": "/api/v1/directory/pending-confirmation",
	"completed":            "/api/v1/directory/completed",
}

// ---------------------------------------------------------------------------
// Wails binding
// ---------------------------------------------------------------------------

// GetWorkflowDirectory returns directory items for the specified view.
// view: "initiated" | "pending_action" | "pending_confirmation" | "completed"
// filter: JSON-encoded DirectoryFilter (may be empty string for defaults)
func (a *App) GetWorkflowDirectory(view string, filter string) (*DirectoryResponse, error) {
	apiPath, ok := directoryViewPaths[view]
	if !ok {
		return nil, fmt.Errorf("invalid directory view: %q (valid: initiated, pending_action, pending_confirmation, completed)", view)
	}

	// Parse filter JSON.
	var f DirectoryFilter
	if strings.TrimSpace(filter) != "" {
		if err := json.Unmarshal([]byte(filter), &f); err != nil {
			return nil, fmt.Errorf("invalid filter JSON: %w", err)
		}
	}

	// Apply defaults.
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}

	// Build query parameters.
	params := url.Values{}
	params.Set("page", fmt.Sprint(f.Page))
	params.Set("page_size", fmt.Sprint(f.PageSize))
	if f.Status != "" {
		params.Set("status", f.Status)
	}
	if f.DateFrom != nil && *f.DateFrom != "" {
		params.Set("date_from", *f.DateFrom)
	}
	if f.DateTo != nil && *f.DateTo != "" {
		params.Set("date_to", *f.DateTo)
	}
	if f.WorkflowType != "" {
		params.Set("workflow_type", f.WorkflowType)
	}
	if f.Role != "" {
		params.Set("role", f.Role)
	}
	if f.Result != "" {
		params.Set("result", f.Result)
	}

	fullPath := apiPath + "?" + params.Encode()

	// Call Hub Directory API via HubCenter client helper.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp DirectoryResponse
	_, _, err := a.getHubCenterJSON(ctx, &http.Client{Timeout: 15 * time.Second}, fullPath, 1<<20, &resp)
	if err != nil {
		return nil, fmt.Errorf("hub directory API call failed: %w", err)
	}

	return &resp, nil
}
