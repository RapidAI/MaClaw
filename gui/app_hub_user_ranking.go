package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const hubInvitationStatusRouteFallbackTTL = 5 * time.Minute

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

// HubUserInvitation is the desktop-facing projection of the tenant referral
// endpoint. Keeping it local prevents the React layer from handling bearer
// tokens or Hub URLs directly.
type HubUserInvitation struct {
	Enabled        bool             `json:"enabled"`
	InviteURL      string           `json:"invite_url,omitempty"`
	InviterCredits float64          `json:"inviter_credits,omitempty"`
	InviteeCredits float64          `json:"invitee_credits,omitempty"`
	DurationDays   int              `json:"duration_days,omitempty"`
	Invitees       []HubUserInvitee `json:"invitees,omitempty"`
	Total          int              `json:"total,omitempty"`
	Page           int              `json:"page,omitempty"`
	Error          string           `json:"error,omitempty"`
}

type HubUserInvitee struct {
	UserID       string `json:"user_id,omitempty"`
	Contact      string `json:"contact,omitempty"`
	RegisteredAt string `json:"registered_at,omitempty"`
	Status       string `json:"status,omitempty"`
}

// GetHubUserRanking queries the Hub for the current user's usage ranking.
func (a *App) GetHubUserRanking() HubUserRanking {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubUserRanking{Error: "config load failed"}
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return HubUserRanking{Error: "hub not configured"}
	}

	path := "/api/my-ranking"
	if tenantID := strings.TrimSpace(cfg.RemoteTenantID); tenantID != "" {
		path += "?tenant_id=" + url.QueryEscape(tenantID)
	}
	data, err := a.getHubJSON(hubURL, viewerToken, path)
	if err != nil {
		return HubUserRanking{Error: fmt.Sprintf("hub request failed: %v", err)}
	}

	var resp HubUserRanking
	if err := json.Unmarshal(data, &resp); err != nil {
		return HubUserRanking{Error: "invalid response from hub"}
	}
	return resp
}

// GetHubUserInvitations returns the invitation state for the current viewer.
// A disabled tenant is a normal successful response; the UI must then omit its
// invitation entry completely.
func (a *App) GetHubUserInvitations() HubUserInvitation {
	return a.getHubUserInvitations(1)
}

// GetHubUserInvitationStatus returns only whether invitations are enabled for
// the active tenant and viewer. The navigation rail polls this lightweight
// endpoint so checking the switch never creates a referral code or loads an
// invitation-history page.
func (a *App) GetHubUserInvitationStatus() HubUserInvitation {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubUserInvitation{Error: "config load failed"}
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return HubUserInvitation{Error: "hub not configured"}
	}
	if fallbackUntil, ok := a.hubInvitationStatusRouteFallback.Load(hubURL); ok {
		if until, valid := fallbackUntil.(time.Time); valid && time.Now().Before(until) {
			return a.getHubUserInvitations(1)
		}
		a.hubInvitationStatusRouteFallback.Delete(hubURL)
	}
	data, err := a.getHubJSON(hubURL, viewerToken, "/api/me/invitations/status")
	if err != nil {
		// Preserve the visibility contract for a rolling deployment where the
		// desktop app reaches an older Hub before its lightweight status route is
		// available. The existing endpoint remains authoritative; this fallback
		// can be removed once mixed-version Hub deployments are no longer served.
		if statusErr, ok := err.(veHubStatusError); ok && statusErr.statusCode == http.StatusNotFound {
			a.hubInvitationStatusRouteFallback.Store(hubURL, time.Now().Add(hubInvitationStatusRouteFallbackTTL))
			return a.getHubUserInvitations(1)
		}
		return HubUserInvitation{Error: fmt.Sprintf("hub request failed: %v", err)}
	}
	var resp HubUserInvitation
	if err := json.Unmarshal(data, &resp); err != nil {
		return HubUserInvitation{Error: "invalid response from hub"}
	}
	return resp
}

// GetHubUserInvitationsPage returns one 20-item page of the current viewer's
// invited users. The backend owns the viewer token and tenant scope.
func (a *App) GetHubUserInvitationsPage(page int) HubUserInvitation {
	return a.getHubUserInvitations(page)
}

func (a *App) getHubUserInvitations(page int) HubUserInvitation {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubUserInvitation{Error: "config load failed"}
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return HubUserInvitation{Error: "hub not configured"}
	}
	if page < 1 {
		page = 1
	}
	path := "/api/me/invitations?page=" + url.QueryEscape(fmt.Sprintf("%d", page))
	data, err := a.getHubJSON(hubURL, viewerToken, path)
	if err != nil {
		return HubUserInvitation{Error: fmt.Sprintf("hub request failed: %v", err)}
	}
	var resp HubUserInvitation
	if err := json.Unmarshal(data, &resp); err != nil {
		return HubUserInvitation{Error: "invalid response from hub"}
	}
	return resp
}

// RotateHubUserInvitation invalidates the current viewer's old referral link
// and returns the new opaque invitation URL. The frontend never sees Hub
// credentials and the service never persists the raw referral code locally.
func (a *App) RotateHubUserInvitation() HubUserInvitation {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubUserInvitation{Error: "config load failed"}
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return HubUserInvitation{Error: "hub not configured"}
	}
	data, err := a.postHubJSON(hubURL, viewerToken, "/api/me/invitations/rotate", map[string]any{})
	if err != nil {
		return HubUserInvitation{Error: fmt.Sprintf("hub request failed: %v", err)}
	}
	var resp HubUserInvitation
	if err := json.Unmarshal(data, &resp); err != nil {
		return HubUserInvitation{Error: "invalid response from hub"}
	}
	return resp
}
