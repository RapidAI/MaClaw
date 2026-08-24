package entry

import (
	"context"
	"encoding/json"
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
	TenantID               string `json:"tenant_id,omitempty"`
	TenantName             string `json:"tenant_name,omitempty"`
	Name                   string `json:"name"`
	BaseURL                string `json:"base_url"`
	PWAURL                 string `json:"pwa_url"`
	Visibility             string `json:"visibility"`
	EnrollmentMode         string `json:"enrollment_mode"`
	CorporateEmailDomain   string `json:"corporate_email_domain,omitempty"`
	Status                 string `json:"status"`
	InvitationCodeRequired bool   `json:"invitation_code_required"`
	RouteKind              string `json:"route_kind,omitempty"`
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
	hubs                 store.HubRepository
	links                store.HubUserLinkRepository
	routes               store.HubDomainRouteRepository
	blockedEmails        store.BlockedEmailRepository
	blockedIPs           store.BlockedIPRepository
	invitationCodeRoutes store.InvitationCodeRouteRepository
	settings             store.SystemSettingsRepository
	snapshot             atomic.Pointer[routeSnapshot]
}

func NewService(hubs store.HubRepository, links store.HubUserLinkRepository, routes store.HubDomainRouteRepository, blockedEmails store.BlockedEmailRepository, blockedIPs store.BlockedIPRepository, settingsOpt ...store.SystemSettingsRepository) *Service {
	var settings store.SystemSettingsRepository
	if len(settingsOpt) > 0 {
		settings = settingsOpt[0]
	}
	return &Service{hubs: hubs, links: links, routes: routes, blockedEmails: blockedEmails, blockedIPs: blockedIPs, settings: settings}
}

func (s *Service) SetInvitationCodeRoutes(repo store.InvitationCodeRouteRepository) {
	s.invitationCodeRoutes = repo
}

// EmailHasHubTenantLink reports whether an email has an explicit normal-user
// link to the requested hub and tenant. Hub-owner and tenant-administrator
// inventory links deliberately do not satisfy this check: callers use it to
// authorize product-user operations, not administrator identities.
func (s *Service) EmailHasHubTenantLink(ctx context.Context, email, hubID, tenantID string) (bool, error) {
	if s == nil || s.links == nil {
		return false, nil
	}
	email = strings.TrimSpace(strings.ToLower(email))
	hubID = strings.TrimSpace(hubID)
	rawTenantID := strings.TrimSpace(tenantID)
	tenantID = normalizeCapabilityTenantID(rawTenantID)
	if email == "" || hubID == "" || rawTenantID == "" {
		return false, nil
	}
	links, err := s.links.ListByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link == nil || isOwnerLink(link) || isHubTenantAdminLink(link) {
			continue
		}
		if strings.TrimSpace(link.HubID) == hubID && normalizeCapabilityTenantID(link.TenantID) == tenantID {
			return true, nil
		}
	}
	return false, nil
}

// EmailHasHubTenantAdministratorLink reports whether an email belongs to a
// tenant administrator for the requested Hub and tenant. Administrator
// inventory is intentionally distinct from ordinary-user routing: an email
// may have both roles, but a normal-user link alone must never authorize an
// administrator-only operation.
func (s *Service) EmailHasHubTenantAdministratorLink(ctx context.Context, email, hubID, tenantID string) (bool, error) {
	if s == nil || s.links == nil {
		return false, nil
	}
	email = strings.TrimSpace(strings.ToLower(email))
	hubID = strings.TrimSpace(hubID)
	rawTenantID := strings.TrimSpace(tenantID)
	tenantID = normalizeCapabilityTenantID(rawTenantID)
	if email == "" || hubID == "" || rawTenantID == "" {
		return false, nil
	}
	links, err := s.links.ListByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link == nil || !isHubTenantAdminLink(link) {
			continue
		}
		if strings.TrimSpace(link.HubID) == hubID && normalizeCapabilityTenantID(link.TenantID) == tenantID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) Rebuild(ctx context.Context) error {
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, s.settings, false)
	if err != nil {
		return err
	}
	// Load invitation code → (hub_id, tenant_id) routes into the snapshot for code-based routing.
	if s.invitationCodeRoutes != nil {
		codeRoutes, err := s.invitationCodeRoutes.ListAll(ctx)
		if err != nil {
			return err
		}
		snap.invitationCodeRoutes = make(map[string]invitationCodeTarget, len(codeRoutes))
		for _, route := range codeRoutes {
			if route == nil {
				continue
			}
			snap.invitationCodeRoutes[strings.ToUpper(strings.TrimSpace(route.Code))] = invitationCodeTarget{
				HubID:       route.HubID,
				TenantID:    route.TenantID,
				UsedByEmail: route.UsedByEmail,
			}
		}
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

// InvitationCodeRouteResult contains the lookup result for an invitation code.
type InvitationCodeRouteResult struct {
	Found       bool   `json:"found"`
	Code        string `json:"code"`
	HubID       string `json:"hub_id,omitempty"`
	HubName     string `json:"hub_name,omitempty"`
	HubURL      string `json:"hub_url,omitempty"`
	HubStatus   string `json:"hub_status,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
	TenantName  string `json:"tenant_name,omitempty"`
	UsedByEmail string `json:"used_by_email,omitempty"`
	Message     string `json:"message,omitempty"`
}

// LookupInvitationCodeRoute queries the invitation code routing table and returns
// the hub and tenant information associated with the code.
func (s *Service) LookupInvitationCodeRoute(ctx context.Context, code string) (*InvitationCodeRouteResult, error) {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return &InvitationCodeRouteResult{Code: code, Message: "Invitation code is required"}, nil
	}

	// First check the in-memory snapshot (fast path).
	snap := s.snapshot.Load()
	if snap != nil && snap.invitationCodeRoutes != nil {
		if target, ok := snap.invitationCodeRoutes[code]; ok {
			if hub, exists := snap.activeHubsByID[target.HubID]; exists {
				return &InvitationCodeRouteResult{
					Found:       true,
					Code:        code,
					HubID:       hub.ID,
					HubName:     hub.Name,
					HubURL:      hub.BaseURL,
					HubStatus:   hub.Status,
					TenantID:    target.TenantID,
					TenantName:  tenantNameForHub(hub, target.TenantID),
					UsedByEmail: target.UsedByEmail,
				}, nil
			}
			// Hub exists in route table but not in active hubs (maybe disabled/offline).
			return &InvitationCodeRouteResult{
				Found:       true,
				Code:        code,
				HubID:       target.HubID,
				TenantID:    target.TenantID,
				UsedByEmail: target.UsedByEmail,
				Message:     "Hub is registered but currently not active",
			}, nil
		}
	}

	// Fallback: query the repository directly (in case snapshot is stale).
	if s.invitationCodeRoutes != nil {
		route, err := s.invitationCodeRoutes.GetByCode(ctx, code)
		if err != nil {
			return nil, err
		}
		if route != nil {
			result := &InvitationCodeRouteResult{Found: true, Code: code, HubID: route.HubID, TenantID: route.TenantID, UsedByEmail: route.UsedByEmail}
			// Try to get hub details.
			hub, _ := s.hubs.GetByID(ctx, route.HubID)
			if hub != nil {
				result.HubName = hub.Name
				result.HubURL = hub.BaseURL
				result.HubStatus = hub.Status
				result.TenantName = tenantNameForHub(hub, route.TenantID)
			}
			return result, nil
		}
	}

	return &InvitationCodeRouteResult{Found: false, Code: code, Message: "Invitation code not found in routing table"}, nil
}

// tenantNameForHub extracts the tenant display name from a hub's capabilities JSON.
func tenantNameForHub(hub *store.HubInstance, tenantID string) string {
	if hub == nil || tenantID == "" || tenantID == "tenant_default" {
		return ""
	}
	if strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return ""
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return ""
	}
	names, ok := caps["tenant_names"].(map[string]any)
	if !ok {
		return ""
	}
	name, ok := names[tenantID]
	if !ok || name == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(name))
}

func (s *Service) RoutingDiagnostics(ctx context.Context) (RoutingDiagnostics, error) {
	hubItems, err := listSnapshotHubs(ctx, s.hubs)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	linkItems, err := listSnapshotUserLinks(ctx, s.links)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	routeItems, err := listSnapshotDomainRoutes(ctx, s.routes)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	blockedEmailItems, err := listSnapshotBlockedEmails(ctx, s.blockedEmails)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	blockedIPItems, err := listSnapshotBlockedIPs(ctx, s.blockedIPs)
	if err != nil {
		return RoutingDiagnostics{}, err
	}
	policies := loadSnapshotRegistrationPolicies(ctx, s.settings)
	snap := buildRouteSnapshotFromItems(hubItems, linkItems, routeItems, blockedEmailItems, blockedIPItems, policies, false)
	s.snapshot.Store(snap)
	diagnostics := RoutingDiagnostics{Snapshot: snap.stats()}

	enabledRoutesByHubDomain := make(map[string]struct{}, len(routeItems))
	for _, route := range routeItems {
		if route == nil || !route.Enabled {
			continue
		}
		domain := normalizeCorporateEmailDomain(route.Domain)
		if domain == "" {
			continue
		}
		enabledRoutesByHubDomain[route.HubID+"|"+normalizeViewTenantID(route.TenantID)+"|"+domain] = struct{}{}
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
		if _, ok := enabledRoutesByHubDomain[hub.ID+"||"+legacyDomain]; !ok {
			diagnostics.Migration.LegacyDomainBackfillPending++
		}
	}

	return diagnostics, nil
}

func (s *Service) ResolveByEmail(ctx context.Context, email string) (*ResolveResult, error) {
	return s.ResolveByEmailFromIP(ctx, email, "")
}

func (s *Service) ResolveByEmailFromIP(ctx context.Context, email string, clientIP string, invitationCode ...string) (*ResolveResult, error) {
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
	code := ""
	if len(invitationCode) > 0 {
		code = strings.TrimSpace(invitationCode[0])
	}
	return snap.resolve(email, clientIP, code)
}

func (s *Service) ResolveAdminByEmail(ctx context.Context, email string) (*ResolveResult, error) {
	email = normalizeEmailPattern(email)
	if email == "" {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is required"}, nil
	}
	if isEmailPattern(email) {
		return s.ResolveAdminByEmailPattern(ctx, email)
	}
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, s.settings, true)
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
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, s.settings, true)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return &ResolveResult{Email: pattern, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolveAdminEmailPattern(pattern), nil
}

func (s *Service) ResolveAdminByDomain(ctx context.Context, domain string) (*ResolveResult, error) {
	domain = normalizeCorporateEmailDomain(domain)
	if domain == "" {
		return &ResolveResult{Email: domain, Mode: "none", Message: "Domain is required"}, nil
	}
	snap, err := buildRouteSnapshot(ctx, s.hubs, s.links, s.routes, s.blockedEmails, s.blockedIPs, s.settings, true)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return &ResolveResult{Email: domain, Mode: "none", Message: "No available hubs found"}, nil
	}
	return snap.resolveAdminDomain(domain), nil
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

func BuildPWAURL(baseURL, email string, tenantIDOpt ...string) string {
	query := fmt.Sprintf("email=%s&entry=app&autologin=1", url.QueryEscape(email))
	if len(tenantIDOpt) > 0 && normalizeViewTenantID(tenantIDOpt[0]) != "" {
		query += "&tenant_id=" + url.QueryEscape(normalizeViewTenantID(tenantIDOpt[0]))
	}
	return fmt.Sprintf("%s/app?%s", strings.TrimRight(baseURL, "/"), query)
}

func normalizeViewTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func hubToAccessView(hub *store.HubInstance, email, routeDomain string, tenantIDOpt ...string) HubAccessView {
	return hubToAccessViewWithRouteKind(hub, email, routeDomain, "", tenantIDOpt...)
}

func hubToAccessViewWithRouteKind(hub *store.HubInstance, email, routeDomain, routeKind string, tenantIDOpt ...string) HubAccessView {
	corporateDomain := normalizeCorporateEmailDomain(routeDomain)
	if corporateDomain == "" {
		corporateDomain = normalizeCorporateEmailDomain(hub.CorporateEmailDomain)
	}
	tenantID := ""
	if len(tenantIDOpt) > 0 {
		tenantID = normalizeViewTenantID(tenantIDOpt[0])
	}
	pwaURL := ""
	if strings.TrimSpace(email) != "" || tenantID != "" {
		pwaURL = BuildPWAURL(hub.BaseURL, email, tenantID)
	}
	return HubAccessView{
		HubID:                  hub.ID,
		TenantID:               tenantID,
		TenantName:             tenantNameForHubTenant(hub, tenantID),
		Name:                   hub.Name,
		BaseURL:                hub.BaseURL,
		PWAURL:                 pwaURL,
		Visibility:             hub.Visibility,
		EnrollmentMode:         hub.EnrollmentMode,
		CorporateEmailDomain:   corporateDomain,
		Status:                 hub.Status,
		InvitationCodeRequired: hub.InvitationCodeRequired,
		RouteKind:              routeKind,
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
