package entry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

var ErrIPBlocked = errors.New("ip blocked")

type HubAccessView struct {
	HubID                  string `json:"hub_id"`
	Name                   string `json:"name"`
	BaseURL                string `json:"base_url"`
	PWAURL                 string `json:"pwa_url"`
	Visibility             string `json:"visibility"`
	EnrollmentMode         string `json:"enrollment_mode"`
	Status                 string `json:"status"`
	InvitationCodeRequired bool   `json:"invitation_code_required"`
}

type ResolveResult struct {
	Email        string          `json:"email"`
	Mode         string          `json:"mode"`
	DefaultHubID string          `json:"default_hub_id,omitempty"`
	DefaultPWA   string          `json:"default_pwa_url,omitempty"`
	Hubs         []HubAccessView `json:"hubs,omitempty"`
	Message      string          `json:"message,omitempty"`
}

type Service struct {
	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	blockedEmails store.BlockedEmailRepository
	blockedIPs    store.BlockedIPRepository
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, blockedEmails store.BlockedEmailRepository, blockedIPs store.BlockedIPRepository) *Service {
	return &Service{hubs: hubs, links: links, blockedEmails: blockedEmails, blockedIPs: blockedIPs}
}

func (s *Service) ResolveByEmail(ctx context.Context, email string) (*ResolveResult, error) {
	return s.ResolveByEmailFromIP(ctx, email, "")
}

func (s *Service) ResolveByEmailFromIP(ctx context.Context, email string, clientIP string) (*ResolveResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is required"}, nil
	}

	if s.blockedIPs != nil {
		blockedIP, err := s.blockedIPs.GetByIP(ctx, strings.TrimSpace(clientIP))
		if err != nil {
			return nil, err
		}
		if blockedIP != nil {
			return nil, ErrIPBlocked
		}
	}

	blocked, err := s.blockedEmails.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if blocked != nil {
		return &ResolveResult{
			Email:   email,
			Mode:    "none",
			Message: "Email is blocked",
		}, nil
	}

	_, err = s.hubs.ListByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	allHubs, err := s.hubs.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	defaultLinkHubID := ""
	if link, err := s.links.GetDefaultByEmail(ctx, email); err == nil && link != nil {
		defaultLinkHubID = link.HubID
	}

	byID := map[string]*store.HubInstance{}
	for _, hub := range allHubs {
		if hub == nil || byID[hub.ID] != nil {
			continue
		}
		if isPubliclyDiscoverable(hub) {
			byID[hub.ID] = hub
		}
	}

	items := make([]HubAccessView, 0, len(byID))
	for _, hub := range byID {
		if hub == nil || hub.IsDisabled || hub.Status != "online" {
			continue
		}
		items = append(items, hubToAccessView(hub, email))
	}
	sort.SliceStable(items, func(i, j int) bool {
		return compareHubPriority(items[i], items[j], email, defaultLinkHubID)
	})

	if len(items) == 0 {
		return &ResolveResult{Email: email, Mode: "none", Message: "No available hubs found"}, nil
	}
	if len(items) == 1 {
		return &ResolveResult{
			Email:        email,
			Mode:         "single",
			DefaultHubID: items[0].HubID,
			DefaultPWA:   items[0].PWAURL,
			Hubs:         items,
		}, nil
	}

	return &ResolveResult{
		Email:        email,
		Mode:         "multiple",
		DefaultHubID: items[0].HubID,
		DefaultPWA:   items[0].PWAURL,
		Hubs:         items,
	}, nil
}

func BuildPWAURL(baseURL, email string) string {
	return fmt.Sprintf(
		"%s/app?email=%s&entry=app&autologin=1",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(email),
	)
}

func hubToAccessView(hub *store.HubInstance, email string) HubAccessView {
	return HubAccessView{
		HubID:                  hub.ID,
		Name:                   hub.Name,
		BaseURL:                hub.BaseURL,
		PWAURL:                 BuildPWAURL(hub.BaseURL, email),
		Visibility:             hub.Visibility,
		EnrollmentMode:         hub.EnrollmentMode,
		Status:                 hub.Status,
		InvitationCodeRequired: hub.InvitationCodeRequired,
	}
}

func isPubliclyDiscoverable(hub *store.HubInstance) bool {
	switch strings.ToLower(strings.TrimSpace(hub.Visibility)) {
	case "public", "shared":
		return true
	default:
		return false
	}
}

func compareHubPriority(a, b HubAccessView, email, defaultLinkHubID string) bool {
	pa := hubPriority(a, email, defaultLinkHubID)
	pb := hubPriority(b, email, defaultLinkHubID)
	if pa != pb {
		return pa < pb
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.HubID < b.HubID
}

func hubPriority(item HubAccessView, email, defaultLinkHubID string) int {
	if item.HubID == defaultLinkHubID && !strings.EqualFold(item.Visibility, "private") {
		return 0
	}
	if strings.EqualFold(item.Visibility, "shared") {
		return 1
	}
	if strings.EqualFold(item.Visibility, "public") {
		return 2
	}
	return 3
}
