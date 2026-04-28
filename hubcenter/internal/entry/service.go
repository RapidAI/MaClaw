package entry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"golang.org/x/text/width"
)

var ErrIPBlocked = errors.New("ip blocked")

type HubAccessView struct {
	HubID                  string `json:"hub_id"`
	Name                   string `json:"name"`
	BaseURL                string `json:"base_url"`
	PWAURL                 string `json:"pwa_url"`
	Visibility             string `json:"visibility"`
	EnrollmentMode         string `json:"enrollment_mode"`
	CorporateEmailDomain   string `json:"corporate_email_domain,omitempty"`
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

type RouteSnapshotStats struct {
	BlockedEmails int `json:"blocked_emails"`
	BlockedIPs    int `json:"blocked_ips"`
	DefaultHubIDs int `json:"default_hub_ids"`
	EmailRoutes   int `json:"email_routes"`
	DomainRoutes  int `json:"domain_routes"`
	PublicHubs    int `json:"public_hubs"`
}

type RoutingHubStats struct {
	Total               int `json:"total"`
	Online              int `json:"online"`
	Pending             int `json:"pending"`
	Disabled            int `json:"disabled"`
	PublicSignup        int `json:"public_signup"`
	LegacyDomainHubs    int `json:"legacy_domain_hubs"`
	EnabledDomainRoutes int `json:"enabled_domain_routes"`
}

type RoutingMigrationStats struct {
	LegacyDomainBackfillPending int `json:"legacy_domain_backfill_pending"`
}

type RoutingDiagnostics struct {
	Snapshot  RouteSnapshotStats    `json:"snapshot"`
	Hubs      RoutingHubStats       `json:"hubs"`
	Migration RoutingMigrationStats `json:"migration"`
}

type Service struct {
	hubs          store.HubRepository
	links         store.HubUserLinkRepository
	routes        store.HubDomainRouteRepository
	blockedEmails store.BlockedEmailRepository
	blockedIPs    store.BlockedIPRepository
	snapshot      atomic.Pointer[routeSnapshot]
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, routes store.HubDomainRouteRepository, blockedEmails store.BlockedEmailRepository, blockedIPs store.BlockedIPRepository) *Service {
	return &Service{hubs: hubs, links: links, routes: routes, blockedEmails: blockedEmails, blockedIPs: blockedIPs}
}

func (s *Service) Rebuild(ctx context.Context) error {
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, false)
	if err != nil {
		return err
	}
	s.snapshot.Store(snap)
	return nil
}

func (s *Service) SnapshotStats() RouteSnapshotStats {
	snap := s.snapshot.Load()
	if snap == nil {
		return RouteSnapshotStats{}
	}
	return snap.stats()
}

func (s *Service) RoutingDiagnostics(ctx context.Context) (RoutingDiagnostics, error) {
	if err := s.Rebuild(ctx); err != nil {
		return RoutingDiagnostics{}, err
	}
	diagnostics := RoutingDiagnostics{Snapshot: s.SnapshotStats()}

	hubItems, err := s.hubs.ListAll(ctx)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	routeItems, err := s.routes.ListAll(ctx)
	if err != nil {
		return RoutingDiagnostics{}, err
	}

	enabledRoutesByHubDomain := make(map[string]struct{}, len(routeItems))
	for _, route := range routeItems {
		if route == nil || !route.Enabled {
			continue
		}
		domain := normalizeCorporateEmailDomain(route.Domain)
		if domain == "" {
			continue
		}
		enabledRoutesByHubDomain[route.HubID+"|"+domain] = struct{}{}
		diagnostics.Hubs.EnabledDomainRoutes++
	}

	for _, hub := range hubItems {
		if hub == nil {
			continue
		}
		diagnostics.Hubs.Total++
		if hub.IsDisabled {
			diagnostics.Hubs.Disabled++
		}
		switch strings.ToLower(strings.TrimSpace(hub.Status)) {
		case "online":
			diagnostics.Hubs.Online++
		case "pending_confirmation":
			diagnostics.Hubs.Pending++
		}
		if hub.AcceptPublicSignup {
			diagnostics.Hubs.PublicSignup++
		}
		legacyDomain := normalizeCorporateEmailDomain(hub.CorporateEmailDomain)
		if legacyDomain == "" {
			continue
		}
		diagnostics.Hubs.LegacyDomainHubs++
		if _, ok := enabledRoutesByHubDomain[hub.ID+"|"+legacyDomain]; !ok {
			diagnostics.Migration.LegacyDomainBackfillPending++
		}
	}

	return diagnostics, nil
}

func (s *Service) ResolveByEmail(ctx context.Context, email string) (*ResolveResult, error) {
	return s.ResolveByEmailFromIP(ctx, email, "")
}

func (s *Service) ResolveByEmailFromIP(ctx context.Context, email string, clientIP string) (*ResolveResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is required"}, nil
	}
	if s.snapshot.Load() == nil {
		if err := s.Rebuild(ctx); err != nil {
			return nil, err
		}
	}
	snap := s.snapshot.Load()
	if snap == nil {
		return &ResolveResult{Email: email, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolve(email, clientIP)
}

func (s *Service) ResolveAdminByEmail(ctx context.Context, email string) (*ResolveResult, error) {
	email = normalizeEmailPattern(email)
	if email == "" {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is required"}, nil
	}
	if isEmailPattern(email) {
		return s.ResolveAdminByEmailPattern(ctx, email)
	}
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, true)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return &ResolveResult{Email: email, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolveAdminEmail(email), nil
}

func (s *Service) ResolveAdminByEmailPattern(ctx context.Context, pattern string) (*ResolveResult, error) {
	pattern = normalizeEmailPattern(pattern)
	if pattern == "" {
		return &ResolveResult{Email: pattern, Mode: "none", Message: "Email pattern is required"}, nil
	}
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, true)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return &ResolveResult{Email: pattern, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolveAdminEmailPattern(pattern), nil
}

func (s *Service) ResolveByDomain(ctx context.Context, domain string) (*ResolveResult, error) {
	domain = normalizeCorporateEmailDomain(domain)
	if domain == "" {
		return &ResolveResult{Email: domain, Mode: "none", Message: "Domain is required"}, nil
	}
	if s.snapshot.Load() == nil {
		if err := s.Rebuild(ctx); err != nil {
			return nil, err
		}
	}
	snap := s.snapshot.Load()
	if snap == nil {
		return &ResolveResult{Email: domain, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolveDomain(domain), nil
}

func BuildPWAURL(baseURL, email string) string {
	return fmt.Sprintf(
		"%s/app?email=%s&entry=app&autologin=1",
		strings.TrimRight(baseURL, "/"),
		url.QueryEscape(email),
	)
}

func hubToAccessView(hub *store.HubInstance, email, routeDomain string) HubAccessView {
	corporateDomain := normalizeCorporateEmailDomain(routeDomain)
	if corporateDomain == "" {
		corporateDomain = normalizeCorporateEmailDomain(hub.CorporateEmailDomain)
	}
	pwaURL := ""
	if strings.TrimSpace(email) != "" {
		pwaURL = BuildPWAURL(hub.BaseURL, email)
	}
	return HubAccessView{
		HubID:                  hub.ID,
		Name:                   hub.Name,
		BaseURL:                hub.BaseURL,
		PWAURL:                 pwaURL,
		Visibility:             hub.Visibility,
		EnrollmentMode:         hub.EnrollmentMode,
		CorporateEmailDomain:   corporateDomain,
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

func compareHubPriority(a, b resolvedCandidate) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.routePriority != b.routePriority {
		return a.routePriority < b.routePriority
	}
	va := visibilityPriority(a.view.Visibility)
	vb := visibilityPriority(b.view.Visibility)
	if va != vb {
		return va < vb
	}
	if a.view.Name != b.view.Name {
		return a.view.Name < b.view.Name
	}
	return a.view.HubID < b.view.HubID
}

func visibilityPriority(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "shared":
		return 0
	case "public":
		return 1
	default:
		return 2
	}
}

func extractEmailDomain(email string) string {
	_, domain, ok := strings.Cut(normalizeEmailPattern(email), "@")
	if !ok {
		return ""
	}
	return normalizeCorporateEmailDomain(domain)
}

func normalizeEmailPattern(pattern string) string {
	pattern = strings.TrimSpace(strings.ToLower(width.Narrow.String(pattern)))
	pattern = strings.ReplaceAll(pattern, "＊", "*")
	if strings.HasPrefix(pattern, "@") {
		pattern = "*" + pattern
	}
	return pattern
}

func isEmailPattern(email string) bool {
	return strings.Contains(normalizeEmailPattern(email), "*")
}

func wildcardEmailMatch(pattern, email string) bool {
	pattern = normalizeEmailPattern(pattern)
	email = normalizeEmailPattern(email)
	if pattern == "" || email == "" {
		return false
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == email
	}
	if parts[0] != "" && !strings.HasPrefix(email, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		idx := strings.Index(email[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(email[pos:], last)
}

func normalizeCorporateEmailDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	domain = strings.TrimPrefix(domain, "@")
	domain = strings.TrimPrefix(domain, ".")
	return strings.TrimSpace(domain)
}

func sortCandidates(items []resolvedCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		return compareHubPriority(items[i], items[j])
	})
}
