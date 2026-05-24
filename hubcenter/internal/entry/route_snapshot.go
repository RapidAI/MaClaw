package entry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	rankDefaultLink = 0
	rankLinkedHub   = 1
	rankDomainRoute = 2
	rankPublicHub   = 3
	rankPrivateLink = 4
)

type snapshotCandidate struct {
	hub           *store.HubInstance
	tenantID      string
	routeDomain   string
	routePriority int
	rank          int
	ownerLink     bool
}

type resolvedCandidate struct {
	view          HubAccessView
	routePriority int
	rank          int
}

type routeSnapshot struct {
	blockedEmails   map[string]struct{}
	blockedIPs      map[string]struct{}
	defaultHubIDs   map[string]string
	adminUserRoutes map[string]struct{}
	emailRoutes     map[string][]snapshotCandidate
	domainRoutes    map[string][]snapshotCandidate
	publicHubs      []snapshotCandidate
}

func buildRouteSnapshot(ctx context.Context, hubs store.HubRepository, links store.HubUserLinkRepository, routes store.HubDomainRouteRepository, blockedEmails store.BlockedEmailRepository, blockedIPs store.BlockedIPRepository, includeOwnerLinks bool) (*routeSnapshot, error) {
	hubItems, err := hubs.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	linkItems, err := links.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	routeItems, err := routes.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	blockedEmailItems, err := blockedEmails.List(ctx)
	if err != nil {
		return nil, err
	}
	blockedIPItems, err := blockedIPs.List(ctx)
	if err != nil {
		return nil, err
	}

	activeHubs := make(map[string]*store.HubInstance, len(hubItems))
	for _, hub := range hubItems {
		if hub == nil || hub.IsDisabled || hub.Status != "online" {
			continue
		}
		activeHubs[hub.ID] = hub
	}

	snap := &routeSnapshot{
		blockedEmails:   map[string]struct{}{},
		blockedIPs:      map[string]struct{}{},
		defaultHubIDs:   map[string]string{},
		adminUserRoutes: map[string]struct{}{},
		emailRoutes:     map[string][]snapshotCandidate{},
		domainRoutes:    map[string][]snapshotCandidate{},
		publicHubs:      make([]snapshotCandidate, 0),
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
		candidate := snapshotCandidate{hub: hub, tenantID: strings.TrimSpace(link.TenantID), rank: rankLinkedHub, routePriority: 0, ownerLink: ownerLink}
		email := strings.TrimSpace(strings.ToLower(link.Email))
		if candidate.tenantID == "" {
			candidate.tenantID = tenantIDForHubEmail(hub, email)
		}
		if _, adminManaged := adminUserLinks[emailTenantRouteKey(email, candidate.tenantID)]; adminManaged && !isAdminUserLink(link) {
			continue
		}
		snap.emailRoutes[email] = append(snap.emailRoutes[email], candidate)
		if link.IsDefault && strings.TrimSpace(link.TenantID) == "" {
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
		tenantID := strings.TrimSpace(route.TenantID)
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
		if hub.AcceptPublicSignup || (legacyDomain == "" && isPubliclyDiscoverable(hub)) {
			snap.publicHubs = append(snap.publicHubs, snapshotCandidate{hub: hub, rank: rankPublicHub, routePriority: 1000})
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

	return snap, nil
}

func hubInventoryEmails(hub *store.HubInstance) []string {
	if hub == nil || strings.TrimSpace(hub.CapabilitiesJSON) == "" {
		return nil
	}
	var caps map[string]any
	if err := json.Unmarshal([]byte(hub.CapabilitiesJSON), &caps); err != nil {
		return nil
	}
	values, ok := caps["user_emails"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
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
		tenantID := strings.TrimSpace(rawTenantID)
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

func (s *routeSnapshot) resolve(email string, clientIP string) (*ResolveResult, error) {
	if _, blocked := s.blockedIPs[strings.TrimSpace(clientIP)]; blocked {
		return nil, ErrIPBlocked
	}
	if _, blocked := s.blockedEmails[email]; blocked {
		return &ResolveResult{Email: email, Mode: "none", Message: "Email is blocked"}, nil
	}

	resultsByHub := map[string]resolvedCandidate{}
	merge := func(candidate snapshotCandidate) {
		s.mergeCandidate(resultsByHub, email, candidate)
	}

	defaultHubID := strings.TrimSpace(s.defaultHubIDs[email])
	for _, candidate := range s.emailRoutes[email] {
		if candidate.hub != nil && candidate.hub.ID == defaultHubID {
			candidate.rank = rankDefaultLink
		} else if candidate.hub != nil && strings.EqualFold(candidate.hub.Visibility, "private") {
			candidate.rank = rankPrivateLink
		}
		merge(candidate)
	}
	if s.hasAdminUserRoute(email) {
		return buildResolveResult(email, resultsByHub, "No available hubs found"), nil
	}
	domainCandidates := s.domainRoutes[extractEmailDomain(email)]
	for _, candidate := range domainCandidates {
		merge(candidate)
	}
	if len(domainCandidates) == 0 {
		for _, candidate := range s.publicHubs {
			merge(candidate)
		}
	}

	return buildResolveResult(email, resultsByHub, "No available hubs found"), nil
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
	tenantID = strings.TrimSpace(tenantID)
	return strings.TrimSpace(strings.ToLower(email)) + "\x00" + tenantID
}

func domainTenantRouteKey(domain, tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
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
