package entry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const systemKeyHubRegistrationPolicies = "hub_registration_policies"

const (
	rankDefaultLink       = 0
	rankLinkedHub         = 1
	rankDomainRoute       = 2
	rankPublicHub         = 3
	rankPrivateLink       = 4
	routeSnapshotPageSize = 1000
)

type hubPageLister interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubInstance, error)
}

type hubUserLinkPageLister interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubUserLink, error)
}

type hubDomainRoutePageLister interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.HubDomainRoute, error)
}

type blockedEmailPageLister interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedEmail, error)
}

type blockedIPPageLister interface {
	ListPage(ctx context.Context, offset, limit int) ([]*store.BlockedIP, error)
}

type snapshotCandidate struct {
	hub                        *store.HubInstance
	tenantID                   string
	routeDomain                string
	routePriority              int
	rank                       int
	ownerLink                  bool
	registrationPolicyFallback bool
	legacyHubPublicSignup      bool
}

type resolvedCandidate struct {
	view          HubAccessView
	routePriority int
	rank          int
}

type routeSnapshot struct {
	blockedEmails        map[string]struct{}
	blockedIPs           map[string]struct{}
	defaultHubIDs        map[string]string
	adminUserRoutes      map[string]struct{}
	emailRoutes          map[string][]snapshotCandidate
	domainRoutes         map[string][]snapshotCandidate
	publicHubs           []snapshotCandidate
	activeHubsByID       map[string]*store.HubInstance   // all active hubs indexed by ID
	tenantDomainsByHub   map[string]map[string][]string  // hub_id -> tenant_id -> allowed email domains
	invitationCodeRoutes map[string]invitationCodeTarget // invitation_code → (hub_id, tenant_id)
}

// invitationCodeTarget holds the routing target for an invitation code.
type invitationCodeTarget struct {
	HubID       string
	TenantID    string
	UsedByEmail string
}

type registrationPolicyConfig struct {
	HubOrigin          string                                       `json:"hub_origin"`
	DefaultSignupScope string                                       `json:"default_signup_scope"`
	Tenants            map[string]store.HubTenantRegistrationPolicy `json:"tenants"`
}

type registrationPolicyStore struct {
	Hubs map[string]registrationPolicyConfig `json:"hubs"`
}

func buildRouteSnapshot(ctx context.Context, hubs store.HubRepository, links store.HubUserLinkRepository, routes store.HubDomainRouteRepository, blockedEmails store.BlockedEmailRepository, blockedIPs store.BlockedIPRepository, settings store.SystemSettingsRepository, includeOwnerLinks bool) (*routeSnapshot, error) {
	hubItems, err := listSnapshotHubs(ctx, hubs)
	if err != nil {
		return nil, err
	}
	linkItems, err := listSnapshotUserLinks(ctx, links)
	if err != nil {
		return nil, err
	}
	routeItems, err := listSnapshotDomainRoutes(ctx, routes)
	if err != nil {
		return nil, err
	}
	blockedEmailItems, err := listSnapshotBlockedEmails(ctx, blockedEmails)
	if err != nil {
		return nil, err
	}
	blockedIPItems, err := listSnapshotBlockedIPs(ctx, blockedIPs)
	if err != nil {
		return nil, err
	}
	policies := loadSnapshotRegistrationPolicies(ctx, settings)
	return buildRouteSnapshotFromItems(hubItems, linkItems, routeItems, blockedEmailItems, blockedIPItems, policies, includeOwnerLinks), nil
}

func loadSnapshotRegistrationPolicies(ctx context.Context, settings store.SystemSettingsRepository) map[string]registrationPolicyConfig {
	if settings == nil {
		return nil
	}
	raw, err := settings.Get(ctx, systemKeyHubRegistrationPolicies)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var state registrationPolicyStore
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil
	}
	if len(state.Hubs) == 0 {
		return nil
	}
	out := make(map[string]registrationPolicyConfig, len(state.Hubs))
	for hubID, cfg := range state.Hubs {
		out[strings.TrimSpace(hubID)] = normalizeRegistrationPolicyConfig(cfg)
	}
	return out
}

func listSnapshotHubs(ctx context.Context, repo store.HubRepository) ([]*store.HubInstance, error) {
	if lister, ok := repo.(hubPageLister); ok {
		return listSnapshotPages(ctx, lister.ListPage)
	}
	return repo.ListAll(ctx)
}

func listSnapshotUserLinks(ctx context.Context, repo store.HubUserLinkRepository) ([]*store.HubUserLink, error) {
	if lister, ok := repo.(hubUserLinkPageLister); ok {
		return listSnapshotPages(ctx, lister.ListPage)
	}
	return repo.ListAll(ctx)
}

func listSnapshotDomainRoutes(ctx context.Context, repo store.HubDomainRouteRepository) ([]*store.HubDomainRoute, error) {
	if lister, ok := repo.(hubDomainRoutePageLister); ok {
		return listSnapshotPages(ctx, lister.ListPage)
	}
	return repo.ListAll(ctx)
}

func listSnapshotBlockedEmails(ctx context.Context, repo store.BlockedEmailRepository) ([]*store.BlockedEmail, error) {
	if lister, ok := repo.(blockedEmailPageLister); ok {
		return listSnapshotPages(ctx, lister.ListPage)
	}
	return repo.List(ctx)
}

func listSnapshotBlockedIPs(ctx context.Context, repo store.BlockedIPRepository) ([]*store.BlockedIP, error) {
	if lister, ok := repo.(blockedIPPageLister); ok {
		return listSnapshotPages(ctx, lister.ListPage)
	}
	return repo.List(ctx)
}

func listSnapshotPages[T any](ctx context.Context, listPage func(context.Context, int, int) ([]T, error)) ([]T, error) {
	out := make([]T, 0)
	for offset := 0; ; offset += routeSnapshotPageSize {
		items, err := listPage(ctx, offset, routeSnapshotPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < routeSnapshotPageSize {
			return out, nil
		}
	}
}

func buildRouteSnapshotFromItems(hubItems []*store.HubInstance, linkItems []*store.HubUserLink, routeItems []*store.HubDomainRoute, blockedEmailItems []*store.BlockedEmail, blockedIPItems []*store.BlockedIP, policies map[string]registrationPolicyConfig, includeOwnerLinks bool) *routeSnapshot {
	activeHubs := make(map[string]*store.HubInstance, len(hubItems))
	tenantDomainsByHub := make(map[string]map[string][]string, len(hubItems))
	for _, hub := range hubItems {
		if hub == nil || hub.IsDisabled || hub.Status != "online" {
			continue
		}
		activeHubs[hub.ID] = hub
		if tenantDomains := tenantDomainCapabilityMap(hub); len(tenantDomains) > 0 {
			tenantDomainsByHub[hub.ID] = tenantDomains
		}
	}

	snap := &routeSnapshot{
		blockedEmails:        map[string]struct{}{},
		blockedIPs:           map[string]struct{}{},
		defaultHubIDs:        map[string]string{},
		adminUserRoutes:      map[string]struct{}{},
		emailRoutes:          map[string][]snapshotCandidate{},
		domainRoutes:         map[string][]snapshotCandidate{},
		publicHubs:           make([]snapshotCandidate, 0),
		activeHubsByID:       activeHubs,
		tenantDomainsByHub:   tenantDomainsByHub,
		invitationCodeRoutes: nil, // populated by entry.Service.Rebuild after snapshot is built
	}

	for _, item := range blockedEmailItems {
		if item == nil {
			continue
		}
		snap.blockedEmails[strings.TrimSpace(strings.ToLower(item.Email))] = struct{}{}
	}
	for _, item := range blockedIPItems {
		if item == nil {
			continue
		}
		snap.blockedIPs[strings.TrimSpace(item.IP)] = struct{}{}
	}

	adminUserLinks := snap.adminUserRoutes
	for _, link := range linkItems {
		if link == nil || !isAdminUserLink(link) {
			continue
		}
		if activeHubs[link.HubID] == nil {
			continue
		}
		email := strings.TrimSpace(strings.ToLower(link.Email))
		if email != "" {
			adminUserLinks[emailTenantRouteKey(email, strings.TrimSpace(link.TenantID))] = struct{}{}
		}
	}

	for _, link := range linkItems {
		if link == nil {
			continue
		}
		ownerLink := isOwnerLink(link)
		if !includeOwnerLinks && ownerLink {
			continue
		}
		hub := activeHubs[link.HubID]
		if hub == nil {
			continue
		}
		candidate := snapshotCandidate{hub: hub, tenantID: normalizeCapabilityTenantID(link.TenantID), rank: rankLinkedHub, routePriority: 0, ownerLink: ownerLink}
		email := strings.TrimSpace(strings.ToLower(link.Email))
		if candidate.tenantID == "" {
			candidate.tenantID = tenantIDForHubEmail(hub, email)
		}
		if _, adminManaged := adminUserLinks[emailTenantRouteKey(email, candidate.tenantID)]; adminManaged && !isAdminUserLink(link) {
			continue
		}
		snap.emailRoutes[email] = append(snap.emailRoutes[email], candidate)
		if link.IsDefault && normalizeCapabilityTenantID(link.TenantID) == "" {
			snap.defaultHubIDs[email] = link.HubID
		}
	}
	if includeOwnerLinks {
		for _, hub := range activeHubs {
			for _, email := range hubInventoryEmails(hub) {
				if email == "" {
					continue
				}
				snap.emailRoutes[email] = append(snap.emailRoutes[email], snapshotCandidate{hub: hub, tenantID: tenantIDForHubEmail(hub, email), rank: rankLinkedHub, routePriority: 0})
			}
		}
	}

	seenDomainRoute := map[string]struct{}{}
	adminDomainRoutes := map[string]struct{}{}
	for _, route := range routeItems {
		if route == nil || !route.Enabled {
			continue
		}
		hub := activeHubs[route.HubID]
		if hub == nil {
			continue
		}
		domain := normalizeCorporateEmailDomain(route.Domain)
		if domain == "" {
			continue
		}
		tenantID := normalizeCapabilityTenantID(route.TenantID)
		key := route.HubID + "|" + tenantID + "|" + domain
		seenDomainRoute[key] = struct{}{}
		if isAdminDomainRoute(route) {
			adminDomainRoutes[domainTenantRouteKey(domain, tenantID)] = struct{}{}
		}
		snap.domainRoutes[domain] = append(snap.domainRoutes[domain], snapshotCandidate{hub: hub, tenantID: tenantID, routeDomain: domain, routePriority: route.Priority, rank: rankDomainRoute})
	}

	for _, hub := range activeHubs {
		legacyDomain := normalizeCorporateEmailDomain(hub.CorporateEmailDomain)
		if legacyDomain != "" {
			key := hub.ID + "||" + legacyDomain
			_, adminManaged := adminDomainRoutes[domainTenantRouteKey(legacyDomain, "")]
			if _, ok := seenDomainRoute[key]; !ok && !adminManaged {
				snap.domainRoutes[legacyDomain] = append(snap.domainRoutes[legacyDomain], snapshotCandidate{hub: hub, routeDomain: legacyDomain, routePriority: 100, rank: rankDomainRoute})
			}
		}
		if cfg, configured := policies[hub.ID]; configured {
			appendPolicyPublicFallbacks(snap, hub, cfg)
			continue
		}
		if cfg, configured := registrationPolicyConfigFromHubRow(hub); configured {
			appendPolicyPublicFallbacks(snap, hub, cfg)
			continue
		}
		// Legacy Hub-wide settings are migrated to the legacy default tenant,
		// yielding one deterministic target instead of every Hub tenant.
		if hub.AcceptPublicSignup {
			snap.publicHubs = append(snap.publicHubs, snapshotCandidate{hub: hub, tenantID: "", rank: rankPublicHub, routePriority: 1000, registrationPolicyFallback: true, legacyHubPublicSignup: true})
		}
	}

	for email := range snap.emailRoutes {
		sort.SliceStable(snap.emailRoutes[email], func(i, j int) bool {
			return compareSnapshotCandidate(snap.emailRoutes[email][i], snap.emailRoutes[email][j])
		})
	}
	for domain := range snap.domainRoutes {
		sort.SliceStable(snap.domainRoutes[domain], func(i, j int) bool {
			return compareSnapshotCandidate(snap.domainRoutes[domain][i], snap.domainRoutes[domain][j])
		})
	}
	sort.SliceStable(snap.publicHubs, func(i, j int) bool {
		return compareSnapshotCandidate(snap.publicHubs[i], snap.publicHubs[j])
	})

	return snap
}

func registrationPolicyConfigFromHubRow(hub *store.HubInstance) (registrationPolicyConfig, bool) {
	if hub == nil {
		return registrationPolicyConfig{}, false
	}
	cfg := registrationPolicyConfig{
		HubOrigin:          strings.ToLower(strings.TrimSpace(hub.HubOrigin)),
		DefaultSignupScope: strings.ToLower(strings.TrimSpace(hub.DefaultSignupScope)),
		Tenants:            map[string]store.HubTenantRegistrationPolicy{},
	}
	if cfg.HubOrigin == "" {
		cfg.HubOrigin = "self_hosted"
	}
	if cfg.DefaultSignupScope == "" {
		cfg.DefaultSignupScope = "domain_restricted"
	}
	if strings.TrimSpace(hub.RegistrationPolicyJSON) != "" {
		var state store.HubRegistrationPolicyState
		if err := json.Unmarshal([]byte(hub.RegistrationPolicyJSON), &state); err == nil {
			for tenantID, policy := range state.Tenants {
				if strings.TrimSpace(policy.TenantID) == "" {
					policy.TenantID = tenantID
				}
				cfg.Tenants[normalizeCapabilityTenantID(policy.TenantID)] = policy
			}
		}
	}
	cfg = normalizeRegistrationPolicyConfig(cfg)
	configured := cfg.HubOrigin == "official" || cfg.DefaultSignupScope != "domain_restricted" || len(cfg.Tenants) > 0
	return cfg, configured
}

func normalizeRegistrationPolicyConfig(cfg registrationPolicyConfig) registrationPolicyConfig {
	cfg.HubOrigin = strings.ToLower(strings.TrimSpace(cfg.HubOrigin))
	if cfg.HubOrigin != "official" {
		cfg.HubOrigin = "self_hosted"
	}
	legacyPublicDefault := strings.EqualFold(strings.TrimSpace(cfg.DefaultSignupScope), "public")
	cfg.DefaultSignupScope = normalizeRegistrationDefaultSignupScope(cfg.DefaultSignupScope)
	normalizedTenants := make(map[string]store.HubTenantRegistrationPolicy, len(cfg.Tenants))
	hasPublicFallback := false
	for tenantID, policy := range cfg.Tenants {
		if strings.TrimSpace(policy.TenantID) == "" {
			policy.TenantID = tenantID
		}
		policy.TenantID = normalizeCapabilityTenantID(policy.TenantID)
		policy.SignupScope = normalizeRegistrationTenantSignupScope(policy.SignupScope)
		policy.Status = strings.ToLower(strings.TrimSpace(policy.Status))
		if policy.Status == "" {
			policy.Status = "active"
		}
		if legacyPublicDefault && policy.IsPublicFallback && policy.SignupScope == "inherit" {
			policy.SignupScope = "public"
		}
		if policy.IsPublicFallback && policy.SignupScope == "public" {
			hasPublicFallback = true
		}
		normalizedTenants[policy.TenantID] = policy
	}
	cfg.Tenants = normalizedTenants
	if legacyPublicDefault && hasPublicFallback {
		cfg.HubOrigin = "official"
	}
	return cfg
}

func normalizeRegistrationDefaultSignupScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "invite_only", "domain_restricted":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return "domain_restricted"
	}
}

func normalizeRegistrationTenantSignupScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "public", "invite_only", "domain_restricted":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return "inherit"
	}
}

func appendPolicyPublicFallbacks(snap *routeSnapshot, hub *store.HubInstance, cfg registrationPolicyConfig) bool {
	if snap == nil || hub == nil || len(cfg.Tenants) == 0 {
		return false
	}
	if strings.ToLower(strings.TrimSpace(cfg.HubOrigin)) != "official" {
		return false
	}
	added := false
	tenantIDs := make([]string, 0, len(cfg.Tenants))
	for tenantID := range cfg.Tenants {
		tenantIDs = append(tenantIDs, tenantID)
	}
	sort.Strings(tenantIDs)
	for _, tenantID := range tenantIDs {
		policy := cfg.Tenants[tenantID]
		if !policy.IsPublicFallback || strings.EqualFold(strings.TrimSpace(policy.Status), "disabled") {
			continue
		}
		if effectivePolicySignupScope(cfg, policy) != "public" {
			continue
		}
		snap.publicHubs = append(snap.publicHubs, snapshotCandidate{hub: hub, tenantID: normalizeCapabilityTenantID(tenantID), rank: rankPublicHub, routePriority: 1000, registrationPolicyFallback: true})
		added = true
	}
	return added
}

func effectivePolicySignupScope(cfg registrationPolicyConfig, policy store.HubTenantRegistrationPolicy) string {
	scope := strings.ToLower(strings.TrimSpace(policy.SignupScope))
	if scope == "inherit" || scope == "" {
		scope = strings.ToLower(strings.TrimSpace(cfg.DefaultSignupScope))
	}
	if scope == "" {
		return "domain_restricted"
	}
	return scope
}

func hubInventoryEmails(hub *store.HubInstance) []string {
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return nil
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	values, ok := caps["user_emails"].([]any)
	if ok {
		for _, value := range values {
			email := strings.TrimSpace(strings.ToLower(fmt.Sprint(value)))
			if email == "" {
				continue
			}
			if _, ok := seen[email]; ok {
				continue
			}
			seen[email] = struct{}{}
			out = append(out, email)
		}
	}
	if tenantEmails, ok := caps["tenant_user_emails"].(map[string]any); ok {
		for _, rawEmails := range tenantEmails {
			for _, rawEmail := range capabilityStringList(rawEmails) {
				email := strings.TrimSpace(strings.ToLower(rawEmail))
				if email == "" {
					continue
				}
				if _, ok := seen[email]; ok {
					continue
				}
				seen[email] = struct{}{}
				out = append(out, email)
			}
		}
	}
	return out
}

func tenantIDForHubEmail(hub *store.HubInstance, email string) string {
	email = strings.TrimSpace(strings.ToLower(email))
	if hub == nil || email == "" || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return ""
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return ""
	}
	tenantEmails, ok := caps["tenant_user_emails"].(map[string]any)
	if !ok {
		return ""
	}
	for rawTenantID, rawEmails := range tenantEmails {
		tenantID := normalizeCapabilityTenantID(rawTenantID)
		if tenantID == "" {
			continue
		}
		for _, rawEmail := range capabilityStringList(rawEmails) {
			if strings.TrimSpace(strings.ToLower(rawEmail)) == email {
				return tenantID
			}
		}
	}
	return ""
}

func normalizeCapabilityTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "tenant_default" {
		return ""
	}
	return tenantID
}

func tenantNameForHubTenant(hub *store.HubInstance, tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if hub == nil || tenantID == "" || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
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

func capabilityStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func isAdminDomainRoute(route *store.HubDomainRoute) bool {
	if route == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(route.ID), "hdr_admin_")
}

func isOwnerLink(link *store.HubUserLink) bool {
	if link == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(link.ID), "hul_owner_")
}

func isAdminUserLink(link *store.HubUserLink) bool {
	if link == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(link.ID), "hul_admin_")
}

func isPhoneRouteIdentity(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(value)), "phone:")
}

func (s *routeSnapshot) stats() RouteSnapshotStats {
	if s == nil {
		return RouteSnapshotStats{}
	}
	return RouteSnapshotStats{
		BlockedEmails: len(s.blockedEmails),
		BlockedIPs:    len(s.blockedIPs),
		DefaultHubIDs: len(s.defaultHubIDs),
		EmailRoutes:   len(s.emailRoutes),
		DomainRoutes:  len(s.domainRoutes),
		PublicHubs:    len(s.publicHubs),
	}
}

func compareSnapshotCandidate(a, b snapshotCandidate) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.routePriority != b.routePriority {
		return a.routePriority < b.routePriority
	}
	va := visibilityPriority(a.hub.Visibility)
	vb := visibilityPriority(b.hub.Visibility)
	if va != vb {
		return va < vb
	}
	if a.hub.Name != b.hub.Name {
		return a.hub.Name < b.hub.Name
	}
	return a.hub.ID < b.hub.ID
}

func (s *routeSnapshot) resolve(email string, clientIP string, invitationCode ...string) (*ResolveResult, error) {
	if _, blocked := s.blockedIPs[strings.TrimSpace(clientIP)]; blocked {
		return nil, ErrIPBlocked
	}
	if _, blocked := s.blockedEmails[email]; blocked {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is blocked"}, nil
	}

	// Invitation code routing takes HIGHEST priority — when a code is provided,
	// route directly to the Hub that issued it, regardless of any existing
	// emailRoutes, adminUserRoutes, or domainRoutes. This ensures re-invitation
	// to a different Hub works even when stale routes exist.
	code := ""
	if len(invitationCode) > 0 {
		code = strings.TrimSpace(strings.ToUpper(invitationCode[0]))
	}
	if code != "" {
		if s.invitationCodeRoutes == nil {
			// Invitation code routing table not yet initialized (HubCenter just
			// restarted and snapshot rebuild is pending). Return a transient error.
			return &ResolveResult{Email: email, Mode: "none", Message: "INVITATION_CODE_ROUTING_INITIALIZING"}, nil
		}
		target, ok := s.invitationCodeRoutes[code]
		if ok && strings.TrimSpace(target.UsedByEmail) == "" {
			hub, exists := s.activeHubsByID[target.HubID]
			if !exists {
				return &ResolveResult{Email: email, Mode: "none", Message: "INVITATION_CODE_HUB_OFFLINE"}, nil
			}
			codeResult := map[string]resolvedCandidate{}
			s.mergeCandidate(codeResult, email, snapshotCandidate{hub: hub, tenantID: target.TenantID, rank: rankDefaultLink, routePriority: 0})
			return buildResolveResult(email, codeResult, ""), nil
		}
		// A consumed code is retained for administration, but must not route a
		// different identity to its original tenant. The successful enrollment
		// has already created the historical user route, so normal routing below
		// handles that identity while new identities use the public fallback.
	}

	// No invitation code — fall through to normal email/domain/public routing.
	resultsByHub := map[string]resolvedCandidate{}
	merge := func(candidate snapshotCandidate) {
		s.mergeCandidate(resultsByHub, email, candidate)
	}

	if isPhoneRouteIdentity(email) {
		// Existing phone identities retain their historical Hub/tenant route.
		// New phone identities (with no route) use the explicit public fallback
		// tenant; a valid invitation code above can always select another target.
		for _, candidate := range s.resolveEmailRouteCandidates(email) {
			merge(candidate)
		}
		if len(resultsByHub) > 0 {
			return buildResolveResult(email, resultsByHub, "No phone route found"), nil
		}
		phoneFallbacks := map[string]resolvedCandidate{}
		for _, candidate := range s.publicHubs {
			if candidate.registrationPolicyFallback && !candidate.legacyHubPublicSignup {
				s.mergeCandidate(phoneFallbacks, email, candidate)
			}
		}
		return buildResolveResult(email, phoneFallbacks, "No phone fallback tenant configured"), nil
	}

	defaultHubID := strings.TrimSpace(s.defaultHubIDs[email])
	hasDirectNonPrivateUserRoute := false
	for _, candidate := range s.resolveEmailRouteCandidates(email) {
		if candidate.hub != nil && candidate.hub.ID == defaultHubID {
			candidate.rank = rankDefaultLink
		} else if candidate.hub != nil && strings.EqualFold(candidate.hub.Visibility, "private") {
			candidate.rank = rankPrivateLink
		}
		if candidate.hub != nil && !strings.EqualFold(candidate.hub.Visibility, "private") {
			hasDirectNonPrivateUserRoute = true
		}
		merge(candidate)
	}
	if s.hasAdminUserRoute(email) {
		return buildResolveResult(email, resultsByHub, "No available hubs found"), nil
	}
	if hasDirectNonPrivateUserRoute {
		return buildResolveResult(email, resultsByHub, "No available hubs found"), nil
	}
	domainCandidates := s.domainRoutes[extractEmailDomain(email)]
	domainCandidateMatched := false
	for _, candidate := range domainCandidates {
		if !s.candidateAllowsEmailDomain(candidate, email) {
			continue
		}
		domainCandidateMatched = true
		merge(candidate)
	}
	if !domainCandidateMatched {
		for _, candidate := range s.publicHubs {
			merge(candidate)
		}
	}

	return buildResolveResult(email, resultsByHub, "No available hubs found"), nil
}

func (s *routeSnapshot) resolveEmailRouteCandidates(email string) []snapshotCandidate {
	if s == nil {
		return nil
	}
	candidates := s.emailRoutes[email]
	if len(candidates) <= 1 {
		return candidates
	}
	allowed := make([]snapshotCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		configured, matches := s.candidateEmailDomainStatus(candidate, email)
		if !configured {
			allowed = append(allowed, candidate)
			continue
		}
		if matches {
			allowed = append(allowed, candidate)
		}
	}
	if len(allowed) > 0 {
		return allowed
	}
	return candidates
}

func (s *routeSnapshot) resolveAdminEmail(email string) *ResolveResult {
	if _, blocked := s.blockedEmails[email]; blocked {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is blocked"}
	}

	directCandidates := s.emailRoutes[email]
	selected := make([]snapshotCandidate, 0, len(directCandidates))
	for _, candidate := range directCandidates {
		if candidate.ownerLink {
			continue
		}
		selected = append(selected, candidate)
	}
	if len(selected) == 0 {
		selected = s.domainRoutes[extractEmailDomain(email)]
	}

	resultsByHub := map[string]resolvedCandidate{}
	defaultHubID := strings.TrimSpace(s.defaultHubIDs[email])
	for _, candidate := range selected {
		if candidate.hub != nil && candidate.hub.ID == defaultHubID {
			candidate.rank = rankDefaultLink
		} else if candidate.hub != nil && strings.EqualFold(candidate.hub.Visibility, "private") {
			candidate.rank = rankPrivateLink
		}
		s.mergeCandidate(resultsByHub, email, candidate)
	}

	return buildResolveResult(email, resultsByHub, "No available hubs found")
}

func (s *routeSnapshot) hasAdminUserRoute(email string) bool {
	if s == nil || len(s.adminUserRoutes) == 0 {
		return false
	}
	prefix := strings.TrimSpace(strings.ToLower(email)) + "\x00"
	for key := range s.adminUserRoutes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func emailTenantRouteKey(email, tenantID string) string {
	tenantID = normalizeCapabilityTenantID(tenantID)
	return strings.TrimSpace(strings.ToLower(email)) + "\x00" + tenantID
}

func domainTenantRouteKey(domain, tenantID string) string {
	tenantID = normalizeCapabilityTenantID(tenantID)
	return normalizeCorporateEmailDomain(domain) + "\x00" + tenantID
}

func (s *routeSnapshot) resolveAdminEmailPattern(pattern string) *ResolveResult {
	resultsByHub := map[string]resolvedCandidate{}
	for email, candidates := range s.emailRoutes {
		if !wildcardEmailMatch(pattern, email) {
			continue
		}
		defaultHubID := strings.TrimSpace(s.defaultHubIDs[email])
		for _, candidate := range candidates {
			if candidate.ownerLink {
				continue
			}
			if candidate.hub != nil && candidate.hub.ID == defaultHubID {
				candidate.rank = rankDefaultLink
			} else if candidate.hub != nil && strings.EqualFold(candidate.hub.Visibility, "private") {
				candidate.rank = rankPrivateLink
			}
			s.mergeCandidate(resultsByHub, email, candidate)
		}
	}
	return buildResolveResult(pattern, resultsByHub, "No matching user links found")
}

func (s *routeSnapshot) mergeCandidate(resultsByHub map[string]resolvedCandidate, email string, candidate snapshotCandidate) {
	if candidate.hub == nil {
		return
	}
	key := virtualHubCandidateKey(candidate)
	current, ok := resultsByHub[key]
	next := resolvedCandidate{
		view:          hubToAccessView(candidate.hub, email, candidate.routeDomain, candidate.tenantID),
		routePriority: candidate.routePriority,
		rank:          candidate.rank,
	}
	if !ok || compareHubPriority(next, current) {
		resultsByHub[key] = next
	}
}

func (s *routeSnapshot) candidateAllowsEmailDomain(candidate snapshotCandidate, email string) bool {
	configured, matches := s.candidateEmailDomainStatus(candidate, email)
	return !configured || matches
}

func (s *routeSnapshot) candidateEmailDomainStatus(candidate snapshotCandidate, email string) (configured bool, matches bool) {
	if s == nil || candidate.hub == nil {
		return false, false
	}
	if candidate.routeDomain == "" {
		return false, false
	}
	domain := extractEmailDomain(email)
	if domain == "" {
		return false, false
	}
	tenantID := normalizeCapabilityTenantID(candidate.tenantID)
	tenantDomains := s.tenantDomainsByHub[strings.TrimSpace(candidate.hub.ID)][tenantID]
	if len(tenantDomains) == 0 {
		return false, false
	}
	for _, allowed := range tenantDomains {
		if strings.EqualFold(allowed, domain) {
			return true, true
		}
	}
	return true, false
}

func tenantDomainCapabilityMap(hub *store.HubInstance) map[string][]string {
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return tenantDomainMapWithDefaultCorporateDomains(nil, hub, nil)
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return tenantDomainMapWithDefaultCorporateDomains(nil, hub, nil)
	}
	out := map[string][]string{}
	if strings.EqualFold(strings.TrimSpace(fmt.Sprint(caps["tenant_domain_source"])), "configured") {
		if raw, ok := caps["tenant_domains"].(map[string]any); ok {
			for rawTenantID, rawDomains := range raw {
				domains := normalizeTenantCapabilityDomains(capabilityStringList(rawDomains))
				if len(domains) == 0 {
					continue
				}
				out[normalizeCapabilityTenantID(rawTenantID)] = domains
			}
		}
	}
	return tenantDomainMapWithDefaultCorporateDomains(out, hub, caps)
}

func tenantDomainMapWithDefaultCorporateDomains(out map[string][]string, hub *store.HubInstance, caps map[string]any) map[string][]string {
	if len(out[""]) > 0 {
		return out
	}
	values := []string{}
	if hub != nil && caps != nil && strings.EqualFold(strings.TrimSpace(fmt.Sprint(caps["corporate_email_domain_source"])), "configured") {
		values = append(values, capabilityStringList(caps["corporate_email_domains"])...)
		if single := strings.TrimSpace(fmt.Sprint(caps["corporate_email_domain"])); single != "" {
			values = append(values, single)
		}
	}
	domains := normalizeTenantCapabilityDomains(values)
	if len(domains) == 0 {
		return out
	}
	if out == nil {
		out = map[string][]string{}
	}
	out[""] = domains
	return out
}

func normalizeTenantCapabilityDomains(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		domain := normalizeCorporateEmailDomain(value)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	return out
}

func virtualHubCandidateKey(candidate snapshotCandidate) string {
	if candidate.hub == nil {
		return ""
	}
	return candidate.hub.ID + "\x00" + strings.TrimSpace(candidate.tenantID)
}

func buildResolveResult(email string, resultsByHub map[string]resolvedCandidate, emptyMessage string) *ResolveResult {
	items := make([]resolvedCandidate, 0, len(resultsByHub))
	for _, item := range resultsByHub {
		items = append(items, item)
	}
	sortCandidates(items)
	if len(items) == 0 {
		return &ResolveResult{Email: email, Mode: "none", Message: emptyMessage}
	}

	views := make([]HubAccessView, 0, len(items))
	for _, item := range items {
		views = append(views, item.view)
	}
	mode := "multiple"
	if len(views) == 1 {
		mode = "single"
	}
	return &ResolveResult{
		Email:        email,
		Mode:         mode,
		DefaultHubID: views[0].HubID,
		DefaultPWA:   views[0].PWAURL,
		Hubs:         views,
	}
}

func (s *routeSnapshot) resolveAdminDomain(domain string) *ResolveResult {
	domain = normalizeCorporateEmailDomain(domain)
	resultsByHub := map[string]resolvedCandidate{}
	for _, candidate := range s.domainRoutes[domain] {
		s.mergeCandidate(resultsByHub, "", candidate)
	}
	pattern := "*@" + domain
	for email, candidates := range s.emailRoutes {
		if !wildcardEmailMatch(pattern, email) {
			continue
		}
		defaultHubID := strings.TrimSpace(s.defaultHubIDs[email])
		for _, candidate := range candidates {
			if candidate.ownerLink {
				continue
			}
			if candidate.hub != nil && candidate.hub.ID == defaultHubID {
				candidate.rank = rankDefaultLink
			} else if candidate.hub != nil && strings.EqualFold(candidate.hub.Visibility, "private") {
				candidate.rank = rankPrivateLink
			}
			s.mergeCandidate(resultsByHub, email, candidate)
		}
	}
	return buildResolveResult(domain, resultsByHub, "No domain route or matching user links found")
}

func (s *routeSnapshot) resolveDomain(domain string) *ResolveResult {
	domain = normalizeCorporateEmailDomain(domain)
	candidates := s.domainRoutes[domain]
	if len(candidates) == 0 {
		return &ResolveResult{Email: domain, Mode: "none", Message: "No domain route found"}
	}

	items := make([]resolvedCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.hub == nil {
			continue
		}
		key := virtualHubCandidateKey(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, resolvedCandidate{
			view:          hubToAccessView(candidate.hub, "", candidate.routeDomain, candidate.tenantID),
			routePriority: candidate.routePriority,
			rank:          candidate.rank,
		})
	}
	if len(items) == 0 {
		return &ResolveResult{Email: domain, Mode: "none", Message: "No domain route found"}
	}
	sortCandidates(items)
	views := make([]HubAccessView, 0, len(items))
	for _, item := range items {
		views = append(views, item.view)
	}
	mode := "multiple"
	if len(views) == 1 {
		mode = "single"
	}
	return &ResolveResult{
		Email:        domain,
		Mode:         mode,
		DefaultHubID: views[0].HubID,
		Hubs:         views,
	}
}
