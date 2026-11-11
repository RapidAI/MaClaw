package botmgmt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	llmDefaultServiceGroup  = botLLMServiceGroupID
	legacyBotServiceGroupID = "system-free"
	llmProviderLimit        = 8
)

func reservedBotLLMName(name string) bool {
	return strings.EqualFold(name, botLLMProviderName) ||
		strings.EqualFold(name, llmDefaultServiceGroup) ||
		strings.EqualFold(name, legacyBotServiceGroupID)
}

var llmProviderIDPattern = regexp.MustCompile(`^lp_[a-z0-9]{8,32}$`)

// llmProvider is one upstream an administrator added for MaClawSrv.
// The key stays in the settings record and is never copied into a view.
type llmProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
	Key      string `json:"key,omitempty"`
	Model    string `json:"model"`
}

// llmSettings is the tenant LLM choice for MaClawSrv bot turns.
// Current is the MaClaw official service group, or the id of one provider.
type llmSettings struct {
	Current   string        `json:"current,omitempty"`
	Providers []llmProvider `json:"providers,omitempty"`
	// SystemFreeCreditsLimit is how many credits one bot owner may spend on
	// system-free. Zero means that consumption is not limited. Bots do not
	// draw this from the new-user benefit allowance.
	SystemFreeCreditsLimit float64 `json:"system_free_credits_limit,omitempty"`
	// Roles is the optional per-role provider choice. Empty roles mean every
	// loop follows Current, and the grounder is unused.
	Roles LLMRoles `json:"roles,omitempty"`
}

// LLMProviderInput is one provider in an admin save. An empty key keeps the
// key already stored for that id.
type LLMProviderInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
}

// LLMProviderView is a provider with the key removed.
type LLMProviderView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	URL      string `json:"url"`
	Model    string `json:"model"`
	KeySet   bool   `json:"key_set"`
}

// LLMRoleAssignment is one Bot-management role choice. Mode is inherit, pin,
// or unused (grounder only). Provider is a provider id or the official group
// id when Mode is pin.
type LLMRoleAssignment struct {
	Mode     string `json:"mode,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// LLMRoles is the optional per-role provider choice for bot turns.
type LLMRoles struct {
	Foreground  LLMRoleAssignment `json:"foreground,omitempty"`
	Background  LLMRoleAssignment `json:"background,omitempty"`
	ComputerUse LLMRoleAssignment `json:"computer_use,omitempty"`
	Grounder    LLMRoleAssignment `json:"grounder,omitempty"`
}

// LLMSettingsView is the admin LLM card. DefaultServiceGroup is the MaClaw
// official service group. Current is that id, or one provider id.
type LLMSettingsView struct {
	DefaultServiceGroup string            `json:"default_service_group"`
	Current             string            `json:"current"`
	Providers           []LLMProviderView `json:"providers"`
	// SystemFreeCreditsLimit is the credits cap for bot system-free usage.
	// Zero means unlimited.
	SystemFreeCreditsLimit float64  `json:"system_free_credits_limit"`
	Roles                  LLMRoles `json:"roles"`
}

// SaveLLMSettings replaces the extra providers MaClawSrv may call.
// The official group stays available. A blank key on a known id keeps the stored key.
// A nil credits limit keeps the limit already stored.
// A nil roles value keeps the roles already stored (and coerces pins whose
// provider was removed in this save). A non-nil value, including an empty
// object, replaces the stored roles.
func (s *Service) SaveLLMSettings(ctx context.Context, tenantID, current string, inputs []LLMProviderInput, creditsLimit *float64, roles *LLMRoles) (SettingsView, error) {
	if s == nil || s.System == nil {
		return SettingsView{}, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	next, err := normalizeLLMSettings(rec.LLM, current, inputs, roles)
	if err != nil {
		return SettingsView{}, err
	}
	limit := rec.LLM.SystemFreeCreditsLimit
	if creditsLimit != nil {
		limit, err = normalizeSystemFreeCreditsLimit(*creditsLimit)
		if err != nil {
			return SettingsView{}, err
		}
	}
	next.SystemFreeCreditsLimit = limit
	rec.LLM = next
	if err := s.save(ctx, tenantID, rec); err != nil {
		return SettingsView{}, err
	}
	return viewOf(rec), nil
}

func llmView(settings llmSettings) LLMSettingsView {
	current := canonicalBotServiceGroup(settings.Current)
	providers := make([]LLMProviderView, 0, len(settings.Providers))
	for _, item := range settings.Providers {
		providers = append(providers, LLMProviderView{
			ID:       item.ID,
			Name:     item.Name,
			Protocol: item.Protocol,
			URL:      item.URL,
			Model:    item.Model,
			KeySet:   strings.TrimSpace(item.Key) != "",
		})
	}
	return LLMSettingsView{
		DefaultServiceGroup:    llmDefaultServiceGroup,
		Current:                current,
		Providers:              providers,
		SystemFreeCreditsLimit: settings.SystemFreeCreditsLimit,
		Roles:                  llmRolesView(settings.Roles),
	}
}

func llmIsDefault(settings llmSettings) bool {
	if len(settings.Providers) > 0 {
		return false
	}
	if !llmRolesAreDefault(settings.Roles) {
		return false
	}
	return isBotDefaultServiceGroup(settings.Current)
}

// isBotDefaultServiceGroup reports the built-in bot route. Empty and the
// older system-free id are the same choice: the MaClaw official group.
func isBotDefaultServiceGroup(current string) bool {
	current = strings.TrimSpace(current)
	return current == "" || strings.EqualFold(current, legacyBotServiceGroupID) || strings.EqualFold(current, llmDefaultServiceGroup)
}

func canonicalBotServiceGroup(current string) string {
	if isBotDefaultServiceGroup(current) {
		return llmDefaultServiceGroup
	}
	return strings.TrimSpace(current)
}

// llmConfigStamp names the provider list last written to MaClawSrv.
// The default official route has an empty stamp, so a deployment that
// has never saved an extra provider does not rewrite on the next command.
// The stamp is a hash. It does not contain the provider key.
func llmConfigStamp(settings llmSettings) string {
	if llmIsDefault(settings) {
		return ""
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, canonicalBotServiceGroup(settings.Current))
	_, _ = io.WriteString(hash, "\n")
	for _, item := range settings.Providers {
		_, _ = io.WriteString(hash, item.ID+"\n"+item.Name+"\n"+item.Protocol+"\n"+item.URL+"\n"+item.Model+"\n")
		sum := sha256.Sum256([]byte(item.Key))
		_, _ = io.WriteString(hash, hex.EncodeToString(sum[:])+"\n")
	}
	_, _ = io.WriteString(hash, llmRolesStamp(settings.Roles))
	return hex.EncodeToString(hash.Sum(nil))
}

func (s *Service) llmSettingsSnapshot(ctx context.Context, tenantID string) llmSettings {
	if s == nil {
		return llmSettings{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return llmSettings{}
	}
	out := rec.LLM
	if len(out.Providers) == 0 {
		return out
	}
	out.Providers = append([]llmProvider(nil), out.Providers...)
	return out
}

func selectedBotLLMProvider(settings llmSettings) (llmProvider, bool) {
	current := strings.TrimSpace(settings.Current)
	if isBotDefaultServiceGroup(current) {
		return llmProvider{}, false
	}
	for _, item := range settings.Providers {
		if item.ID == current && strings.TrimSpace(item.Key) != "" {
			return item, true
		}
	}
	return llmProvider{}, false
}

func normalizeLLMSettings(prev llmSettings, current string, inputs []LLMProviderInput, roles *LLMRoles) (llmSettings, error) {
	if inputs == nil {
		inputs = []LLMProviderInput{}
	}
	if len(inputs) > llmProviderLimit {
		return llmSettings{}, fmt.Errorf("%w: llm provider limit is %d", ErrInvalidInput, llmProviderLimit)
	}
	prevByID := make(map[string]llmProvider, len(prev.Providers))
	for _, item := range prev.Providers {
		prevByID[item.ID] = item
	}
	seenID := map[string]struct{}{}
	seenNames := make([]string, 0, len(inputs))
	out := make([]llmProvider, 0, len(inputs))
	for _, in := range inputs {
		item, err := normalizeLLMProvider(in, prevByID)
		if err != nil {
			return llmSettings{}, err
		}
		if _, ok := seenID[item.ID]; ok {
			return llmSettings{}, fmt.Errorf("%w: llm provider id is duplicated", ErrInvalidInput)
		}
		for _, seen := range seenNames {
			// MaClaw resolves the selected provider by name, and treats the
			// Zhipu coding aliases as one name. Two such rows would run as the
			// first one.
			if corelib.MaclawLLMProviderNameEqual(seen, item.Name) {
				return llmSettings{}, fmt.Errorf("%w: llm provider name is duplicated", ErrInvalidInput)
			}
		}
		seenID[item.ID] = struct{}{}
		seenNames = append(seenNames, item.Name)
		out = append(out, item)
	}
	current = canonicalBotServiceGroup(current)
	if !isBotDefaultServiceGroup(current) {
		if _, ok := seenID[current]; !ok {
			return llmSettings{}, fmt.Errorf("%w: llm current provider was not found", ErrInvalidInput)
		}
	}
	normalized, err := normalizeLLMRoles(prev.Roles, roles, out)
	if err != nil {
		return llmSettings{}, err
	}
	return llmSettings{Current: current, Providers: out, Roles: normalized}, nil
}

func normalizeLLMProvider(in LLMProviderInput, prev map[string]llmProvider) (llmProvider, error) {
	id := strings.TrimSpace(in.ID)
	existing, known := prev[id]
	if !llmProviderIDPattern.MatchString(id) {
		id = newLLMProviderID()
		known = false
		existing = llmProvider{}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "\r\n") {
		return llmProvider{}, fmt.Errorf("%w: llm provider name is required", ErrInvalidInput)
	}
	if reservedBotLLMName(name) {
		return llmProvider{}, fmt.Errorf("%w: llm provider name is reserved", ErrInvalidInput)
	}
	protocol := strings.ToLower(strings.TrimSpace(in.Protocol))
	if protocol != "openai" && protocol != "anthropic" {
		return llmProvider{}, fmt.Errorf("%w: llm provider protocol must be openai or anthropic", ErrInvalidInput)
	}
	rawURL := strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if err := validateBaseURL(rawURL); err != nil {
		return llmProvider{}, fmt.Errorf("%w: llm provider url is invalid", ErrInvalidInput)
	}
	model := strings.TrimSpace(in.Model)
	if model == "" || utf8.RuneCountInString(model) > 128 || strings.ContainsAny(model, "\r\n") {
		return llmProvider{}, fmt.Errorf("%w: llm provider model is required", ErrInvalidInput)
	}
	key := strings.TrimSpace(in.Key)
	if strings.ContainsAny(key, "\r\n") || len(key) > 4096 {
		return llmProvider{}, fmt.Errorf("%w: llm provider key is invalid", ErrInvalidInput)
	}
	if key == "" {
		if !known || strings.TrimSpace(existing.Key) == "" {
			return llmProvider{}, fmt.Errorf("%w: llm provider key is required", ErrInvalidInput)
		}
		key = existing.Key
	}
	return llmProvider{ID: id, Name: name, Protocol: protocol, URL: rawURL, Key: key, Model: model}, nil
}

func newLLMProviderID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprint(buf)))
		return "lp_" + hex.EncodeToString(sum[:8])
	}
	return "lp_" + hex.EncodeToString(buf[:])
}

func llmRolesView(roles LLMRoles) LLMRoles {
	return LLMRoles{
		Foreground:  llmRoleView(roles.Foreground, false),
		Background:  llmRoleView(roles.Background, false),
		ComputerUse: llmRoleView(roles.ComputerUse, false),
		Grounder:    llmRoleView(roles.Grounder, true),
	}
}

func llmRoleView(role LLMRoleAssignment, grounder bool) LLMRoleAssignment {
	mode := strings.ToLower(strings.TrimSpace(role.Mode))
	provider := strings.TrimSpace(role.Provider)
	switch mode {
	case corelib.MaclawHubBotRolePin:
		if isBotDefaultServiceGroup(provider) {
			provider = llmDefaultServiceGroup
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRolePin, Provider: provider}
	case corelib.MaclawHubBotRoleInherit:
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}
	case corelib.MaclawHubBotRoleUnused:
		if grounder {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleUnused}
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}
	default:
		if grounder {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleUnused}
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}
	}
}

func llmRolesAreDefault(roles LLMRoles) bool {
	return llmRoleIsDefault(roles.Foreground, false) &&
		llmRoleIsDefault(roles.Background, false) &&
		llmRoleIsDefault(roles.ComputerUse, false) &&
		llmRoleIsDefault(roles.Grounder, true)
}

func llmRoleIsDefault(role LLMRoleAssignment, grounder bool) bool {
	mode := strings.ToLower(strings.TrimSpace(role.Mode))
	if grounder {
		return mode == "" || mode == corelib.MaclawHubBotRoleUnused
	}
	return mode == "" || mode == corelib.MaclawHubBotRoleInherit
}

func llmRolesStamp(roles LLMRoles) string {
	roles = llmRolesView(roles)
	return "fg:" + roles.Foreground.Mode + ":" + roles.Foreground.Provider +
		"\nbg:" + roles.Background.Mode + ":" + roles.Background.Provider +
		"\ncu:" + roles.ComputerUse.Mode + ":" + roles.ComputerUse.Provider +
		"\ngr:" + roles.Grounder.Mode + ":" + roles.Grounder.Provider + "\n"
}

func normalizeLLMRoles(prev LLMRoles, in *LLMRoles, providers []llmProvider) (LLMRoles, error) {
	src := prev
	if in != nil {
		src = *in
	}
	fg, err := normalizeLLMRole(src.Foreground, providers, false)
	if err != nil {
		return LLMRoles{}, err
	}
	bg, err := normalizeLLMRole(src.Background, providers, false)
	if err != nil {
		return LLMRoles{}, err
	}
	cu, err := normalizeLLMRole(src.ComputerUse, providers, false)
	if err != nil {
		return LLMRoles{}, err
	}
	gr, err := normalizeLLMRole(src.Grounder, providers, true)
	if err != nil {
		return LLMRoles{}, err
	}
	return LLMRoles{Foreground: fg, Background: bg, ComputerUse: cu, Grounder: gr}, nil
}

func normalizeLLMRole(in LLMRoleAssignment, providers []llmProvider, grounder bool) (LLMRoleAssignment, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	provider := strings.TrimSpace(in.Provider)
	switch mode {
	case "", corelib.MaclawHubBotRoleInherit:
		if grounder && mode == "" {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleUnused}, nil
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}, nil
	case corelib.MaclawHubBotRoleUnused:
		if !grounder {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}, nil
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleUnused}, nil
	case corelib.MaclawHubBotRolePin:
		if isBotDefaultServiceGroup(provider) {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRolePin, Provider: llmDefaultServiceGroup}, nil
		}
		for _, item := range providers {
			if item.ID == provider {
				return LLMRoleAssignment{Mode: corelib.MaclawHubBotRolePin, Provider: item.ID}, nil
			}
		}
		// The pinned provider was removed in this save: fall back rather than
		// reject the whole settings write.
		if grounder {
			return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleUnused}, nil
		}
		return LLMRoleAssignment{Mode: corelib.MaclawHubBotRoleInherit}, nil
	default:
		return LLMRoleAssignment{}, fmt.Errorf("%w: llm role mode is invalid", ErrInvalidInput)
	}
}

// hubBotRolesAppConfig is the MaClawSrv payload for maclaw_hub_bot_roles.
// It names providers (never keys). A default assignment is omitted so the
// writer can delete the config key.
func hubBotRolesAppConfig(settings llmSettings) map[string]any {
	if llmRolesAreDefault(settings.Roles) {
		return nil
	}
	roles := llmRolesView(settings.Roles)
	return map[string]any{
		"foreground":   hubBotRoleAppConfig(settings, roles.Foreground),
		"background":   hubBotRoleAppConfig(settings, roles.Background),
		"computer_use": hubBotRoleAppConfig(settings, roles.ComputerUse),
		"grounder":     hubBotRoleAppConfig(settings, roles.Grounder),
	}
}

func hubBotRoleAppConfig(settings llmSettings, role LLMRoleAssignment) map[string]any {
	out := map[string]any{"mode": role.Mode}
	if role.Mode == corelib.MaclawHubBotRolePin {
		out["provider"] = roleProviderName(settings, role.Provider)
	}
	return out
}

func roleProviderName(settings llmSettings, id string) string {
	if isBotDefaultServiceGroup(id) {
		return botLLMProviderName
	}
	for _, item := range settings.Providers {
		if item.ID == id {
			return item.Name
		}
	}
	return strings.TrimSpace(id)
}
