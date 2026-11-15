package botmgmt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

const (
	// Bots use the grant-required official group, so usage is deducted through
	// the owner's redeemed MaClaw service instead of the free system-free route.
	botLLMServiceGroupID = llmservice.MaClawOfficialServiceGroupID
	botLLMProviderName   = "hub-llm"
	botLLMModel          = "auto"
	// botLLMViewerReuse is how long a stored viewer token is reused. Hub
	// expires an unused token after 30 days and slides it while it is called.
	botLLMViewerReuse = 20 * 24 * time.Hour
)

// OwnerLLMIssuer mints the viewer token whose Hub usage is attributed to the
// bot's owner, and names the LLM endpoint that token calls.
type OwnerLLMIssuer interface {
	IssueViewerTokenForUser(ctx context.Context, userID string) (string, error)
	PublicLLMBaseURL(ctx context.Context) string
}

// ensureBotOwnerLLM writes the owner's Hub viewer token into that MaClawSrv
// user's model config. The agent then calls Hub as that user on the MaClaw
// official service group, unless an administrator selected another provider.
// A nil issuer leaves the call unchanged so tests without Hub identity still
// exercise the desktop path.
func (s *Service) ensureBotOwnerLLM(ctx context.Context, tenantID string, rec record, hubUserID string) error {
	return s.writeBotOwnerLLM(ctx, tenantID, rec, hubUserID, false)
}

func (s *Service) writeBotOwnerLLM(ctx context.Context, tenantID string, rec record, hubUserID string, force bool) error {
	if s == nil || s.OwnerLLM == nil || strings.TrimSpace(hubUserID) == "" {
		return nil
	}
	principal := findPrincipal(rec.Principals, hubUserID)
	if principal == nil || strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.TenantID) == "" {
		return fmt.Errorf("%w: bot owner is not provisioned", ErrSrv)
	}
	endpoint := strings.TrimSpace(s.OwnerLLM.PublicLLMBaseURL(ctx))
	if endpoint == "" {
		return fmt.Errorf("%w: hub public url is not configured", ErrSrv)
	}
	choice, _, err := s.persistedOwnerBotLLM(ctx, tenantID, hubUserID)
	if err != nil {
		return err
	}
	settings := s.llmSettingsSnapshot(ctx, tenantID)
	// Cloud and a local desktop run different configs, so each destination
	// keeps its own stamp. A local-stage edit does not rewrite the cloud srv,
	// and a cloud default/custom edit does not rewrite a node that already
	// has these stages.
	nodeID := strings.TrimSpace(rec.DialNode)
	stamp := ownerLLMStamp(settings, choice)
	if nodeID != "" {
		stamp = localDesktopLLMStamp(choice)
	}
	token, reused, err := s.ownerLLMViewerToken(ctx, tenantID, hubUserID, nodeID)
	if err != nil {
		return err
	}
	// A repeated command must not rewrite the MaClaw user. That write clears
	// dynamic capability contracts and round-trips the sanitized config.
	// force is the one rewrite after MaClaw reports the config is gone; the
	// endpoint marker stays in place until this write itself succeeds, so a
	// failed attempt does not make every later command rewrite too.
	if !force && reused && s.ownerLLMConfigCurrent(ctx, tenantID, hubUserID, nodeID, token, endpoint, stamp) {
		return nil
	}
	if err := s.pushBotOwnerLLM(ctx, rec, *principal, endpoint, token, settings, choice); err != nil {
		return err
	}
	if nodeID != "" {
		return s.markNodeLLMPushed(ctx, tenantID, hubUserID, nodeID, token, endpoint, stamp)
	}
	return s.markOwnerLLMPushed(ctx, tenantID, hubUserID, token, endpoint, stamp)
}

// llmConfigIncomplete is MaClaw refusing the turn before the agent starts.
// Readiness uses one fixed sentence. Matching only that sentence keeps a
// later failure, even one that mentions the same words, from sending the
// command again.
func llmConfigIncomplete(err error) bool {
	return err != nil && strings.Contains(err.Error(), "instance is not ready: user LLM configuration is incomplete")
}

// ownerLLMRepairGate is the viewer token observed before a rewrite. A command
// that succeeds in between advances the epoch, and this rewrite must not
// block that token afterwards.
type ownerLLMRepairGate struct {
	epoch uint64
	token string
}

// repairIncompleteOwnerLLM pushes the owner config again after MaClawSrv
// reports that it is missing. The stored endpoint normally suppresses that
// write; a wipe would otherwise wait out the viewer-token reuse window.
// A token that already failed one repair is left alone so a config that
// cannot be made valid is not rewritten on every message. A later command
// that does run clears that block, so a second wipe can be repaired too.
func (s *Service) repairIncompleteOwnerLLM(ctx context.Context, tenantID string, rec record, hubUserID string) (ownerLLMRepairGate, bool, error) {
	if s == nil || s.OwnerLLM == nil || strings.TrimSpace(hubUserID) == "" {
		return ownerLLMRepairGate{}, false, nil
	}
	gate, blocked := s.ownerLLMRepairSnapshot(ctx, tenantID, hubUserID)
	if blocked {
		return gate, false, nil
	}
	if err := s.writeBotOwnerLLM(ctx, tenantID, rec, hubUserID, true); err != nil {
		return gate, false, err
	}
	return gate, true, nil
}

func (s *Service) ownerLLMRepairSnapshot(ctx context.Context, tenantID, hubUserID string) (ownerLLMRepairGate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return ownerLLMRepairGate{}, true
	}
	principal := findPrincipal(fresh.Principals, hubUserID)
	if principal == nil {
		return ownerLLMRepairGate{}, true
	}
	token := strings.TrimSpace(principal.LLMViewerToken)
	if token == "" {
		return ownerLLMRepairGate{}, true
	}
	key := llmRepairKey(tenantID, hubUserID)
	gate := ownerLLMRepairGate{epoch: s.llmRepairEpoch[key], token: token}
	return gate, s.llmRepairTried[key] == token
}

func (s *Service) noteOwnerLLMRepairFailed(ctx context.Context, tenantID, hubUserID string, gate ownerLLMRepairGate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := llmRepairKey(tenantID, hubUserID)
	if s.llmRepairEpoch[key] != gate.epoch || strings.TrimSpace(gate.token) == "" {
		return
	}
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return
	}
	principal := findPrincipal(fresh.Principals, hubUserID)
	if principal == nil {
		return
	}
	token := strings.TrimSpace(principal.LLMViewerToken)
	if token == "" || token != gate.token {
		return
	}
	if s.llmRepairTried == nil {
		s.llmRepairTried = map[string]string{}
	}
	s.llmRepairTried[key] = token
}

// clearOwnerLLMRepairNote drops a failed rewrite after a command actually
// runs. The next wipe of this same viewer token can be written again.
func (s *Service) clearOwnerLLMRepairNote(_ context.Context, tenantID, hubUserID string) {
	if s == nil || strings.TrimSpace(hubUserID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := llmRepairKey(tenantID, hubUserID)
	if s.llmRepairEpoch == nil {
		s.llmRepairEpoch = map[string]uint64{}
	}
	s.llmRepairEpoch[key]++
	delete(s.llmRepairTried, key)
}

func llmRepairKey(tenantID, hubUserID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(hubUserID)
}

func (s *Service) ownerLLMViewerToken(ctx context.Context, tenantID, hubUserID, nodeID string) (string, bool, error) {
	if token, ok := s.reusableOwnerLLMToken(ctx, tenantID, hubUserID); ok {
		return token, true, nil
	}
	token, err := s.OwnerLLM.IssueViewerTokenForUser(ctx, hubUserID)
	if err != nil {
		return "", false, fmt.Errorf("%w: owner llm token: %s", ErrSrv, err.Error())
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false, fmt.Errorf("%w: owner llm token missing", ErrSrv)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return "", false, err
	}
	// The public principal keeps the token when this user has a cloud srv.
	// A node-only account stores it on that node, which is the srv that calls Hub.
	if !storeOwnerLLMToken(&fresh, hubUserID, nodeID, token, s.now().UTC().Format(time.RFC3339)) {
		return "", false, fmt.Errorf("%w: bot owner is not provisioned", ErrSrv)
	}
	if err := s.save(ctx, tenantID, fresh); err != nil {
		return "", false, err
	}
	return token, false, nil
}

func storeOwnerLLMToken(rec *record, hubUserID, nodeID, token, issued string) bool {
	if rec == nil {
		return false
	}
	set := func(principal *ownerPrincipal) {
		principal.LLMViewerToken = token
		principal.LLMViewerIssuedAt = issued
		principal.LLMEndpoint = ""
	}
	if idx := principalIndex(rec.Principals, hubUserID); idx >= 0 {
		set(&rec.Principals[idx])
		return true
	}
	if link := findNodeLink(rec.NodeLinks, nodeID); link != nil {
		if idx := principalIndex(link.Principals, hubUserID); idx >= 0 {
			set(&link.Principals[idx])
			return true
		}
	}
	for i := range rec.NodeLinks {
		if idx := principalIndex(rec.NodeLinks[i].Principals, hubUserID); idx >= 0 {
			set(&rec.NodeLinks[i].Principals[idx])
			return true
		}
	}
	return false
}

func viewerTokenHeld(rec record, hubUserID, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	holds := func(principal *ownerPrincipal) bool {
		return principal != nil && strings.TrimSpace(principal.LLMViewerToken) == token
	}
	if holds(findPrincipal(rec.Principals, hubUserID)) {
		return true
	}
	for i := range rec.NodeLinks {
		if holds(findPrincipal(rec.NodeLinks[i].Principals, hubUserID)) {
			return true
		}
	}
	return false
}

func (s *Service) ownerLLMConfigCurrent(ctx context.Context, tenantID, hubUserID, nodeID, token, endpoint, stamp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return false
	}
	if !viewerTokenHeld(fresh, hubUserID, token) {
		return false
	}
	principal := findPrincipal(fresh.Principals, hubUserID)
	if strings.TrimSpace(nodeID) != "" {
		link := findNodeLink(fresh.NodeLinks, nodeID)
		if link == nil {
			return false
		}
		principal = findPrincipal(link.Principals, hubUserID)
		if principal == nil {
			return false
		}
	}
	if principal == nil {
		return false
	}
	return strings.TrimSpace(principal.LLMEndpoint) == strings.TrimSpace(endpoint) &&
		strings.TrimSpace(endpoint) != "" &&
		principal.LLMConfigStamp == stamp
}

func (s *Service) markOwnerLLMPushed(ctx context.Context, tenantID, hubUserID, token, endpoint, stamp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	idx := principalIndex(fresh.Principals, hubUserID)
	if idx < 0 {
		return nil
	}
	// A newer token was issued while this write was in flight. Leave the
	// marker alone so that token is pushed on its own command.
	if strings.TrimSpace(fresh.Principals[idx].LLMViewerToken) != strings.TrimSpace(token) {
		return nil
	}
	fresh.Principals[idx].LLMEndpoint = strings.TrimSpace(endpoint)
	fresh.Principals[idx].LLMConfigStamp = stamp
	return s.save(ctx, tenantID, fresh)
}

// markNodeLLMPushed records the four-stage config written to one local
// desktop. The stamp is per node. The viewer token may live on the public
// principal or, for a node-only account, on this node.
func (s *Service) markNodeLLMPushed(ctx context.Context, tenantID, hubUserID, nodeID, token, endpoint, stamp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if !viewerTokenHeld(fresh, hubUserID, token) {
		return nil
	}
	link := findNodeLink(fresh.NodeLinks, nodeID)
	if link == nil {
		return nil
	}
	idx := principalIndex(link.Principals, hubUserID)
	if idx < 0 {
		return nil
	}
	link.Principals[idx].LLMEndpoint = strings.TrimSpace(endpoint)
	link.Principals[idx].LLMConfigStamp = stamp
	return s.save(ctx, tenantID, fresh)
}

func (s *Service) reusableOwnerLLMToken(ctx context.Context, tenantID, hubUserID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return "", false
	}
	// A cloud user keeps its own token. An old token left on a node must not
	// be reused for the cloud srv, or the cloud stamp never matches that write.
	if principal := findPrincipal(fresh.Principals, hubUserID); principal != nil {
		return reusablePrincipalToken(s, principal)
	}
	for i := range fresh.NodeLinks {
		if token, ok := reusablePrincipalToken(s, findPrincipal(fresh.NodeLinks[i].Principals, hubUserID)); ok {
			return token, true
		}
	}
	return "", false
}

func reusablePrincipalToken(s *Service, principal *ownerPrincipal) (string, bool) {
	if s == nil || principal == nil {
		return "", false
	}
	token := strings.TrimSpace(principal.LLMViewerToken)
	issued, err := time.Parse(time.RFC3339, strings.TrimSpace(principal.LLMViewerIssuedAt))
	if token == "" || err != nil || s.now().Sub(issued) >= botLLMViewerReuse {
		return "", false
	}
	return token, true
}

func (s *Service) pushBotOwnerLLM(ctx context.Context, rec record, principal ownerPrincipal, endpoint, viewerToken string, settings llmSettings, choice ownerBotLLM) error {
	path := "/api/v1/admin/tenants/" + url.PathEscape(principal.TenantID) + "/users/" + url.PathEscape(principal.UserID) + "/config"
	var current struct {
		AppConfig map[string]any `json:"app_config"`
	}
	if err := s.doAdmin(ctx, rec, http.MethodGet, path, nil, &current); err != nil {
		return fmt.Errorf("%w: read bot llm config: %s", ErrSrv, err.Error())
	}
	if current.AppConfig == nil {
		current.AppConfig = map[string]any{}
	}
	hubProvider := map[string]any{
		"name":           botLLMProviderName,
		"url":            endpoint,
		"key":            viewerToken,
		"model":          botLLMModel,
		"is_hub_service": true,
	}
	// A local MaClawSrv runs the four-stage choice. The cloud default/custom
	// switch does not apply here, and the tenant provider catalog (its URLs
	// and keys) is not copied onto that machine.
	if strings.TrimSpace(rec.DialNode) != "" {
		localChoice := localRuntimeChoice(choice)
		providers := []map[string]any{hubProvider}
		providers = appendCustomRoleProviders(providers, localChoice)
		applyCustomFlat(current.AppConfig, localChoice, endpoint, viewerToken)
		current.AppConfig["maclaw_llm_providers"] = providers
		current.AppConfig["maclaw_llm_profiles"] = dropUnresolvedProfiles(customRoleProfiles(current.AppConfig["maclaw_llm_profiles"], localChoice), providers)
		current.AppConfig["maclaw_bot_llm_mode"] = corelib.MaclawBotLLMModeCustom
		delete(current.AppConfig, "maclaw_hub_bot_roles")
		if err := s.doAdmin(ctx, rec, http.MethodPut, path, map[string]any{"app_config": current.AppConfig}, nil); err != nil {
			return fmt.Errorf("%w: write bot llm config: %s", ErrSrv, err.Error())
		}
		return nil
	}
	providers := make([]map[string]any, 0, 2+len(settings.Providers))
	providers = append(providers, hubProvider)
	for _, item := range settings.Providers {
		providers = append(providers, llmProviderWire(item))
	}
	if choice.Mode == corelib.MaclawBotLLMModeCustom {
		// Explicit rows are profile pins. Rows left unset follow the service,
		// so the flat provider and the administrator role pins stay in place.
		providers = appendCustomRoleProviders(providers, choice)
	}
	applyServiceBotLLMFlat(current.AppConfig, settings, endpoint, viewerToken)
	current.AppConfig["maclaw_llm_providers"] = providers
	if choice.Mode == corelib.MaclawBotLLMModeCustom {
		current.AppConfig["maclaw_llm_profiles"] = customRoleProfiles(current.AppConfig["maclaw_llm_profiles"], choice)
		current.AppConfig["maclaw_bot_llm_mode"] = corelib.MaclawBotLLMModeCustom
	} else {
		current.AppConfig["maclaw_bot_llm_mode"] = corelib.MaclawBotLLMModeDefault
		clearRoleProfiles(current.AppConfig)
	}
	if roles := hubBotRolesAppConfig(settings); roles != nil {
		current.AppConfig["maclaw_hub_bot_roles"] = roles
	} else {
		delete(current.AppConfig, "maclaw_hub_bot_roles")
	}
	if err := s.doAdmin(ctx, rec, http.MethodPut, path, map[string]any{"app_config": current.AppConfig}, nil); err != nil {
		return fmt.Errorf("%w: write bot llm config: %s", ErrSrv, err.Error())
	}
	return nil
}
