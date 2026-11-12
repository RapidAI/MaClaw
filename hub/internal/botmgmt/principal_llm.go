package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ownerLLMOverride is one owner's choice of provider for their own bots. It
// sits above the two defaults the service can derive on its own: the Hub owner
// route written with that owner's viewer token, and the provider an
// administrator selected for the tenant. Clearing the override lets whichever
// of those is currently configured take effect again.
//
// The concrete credential is never part of a view. Nothing here snapshots what
// the default resolved to at the time of the change, because that value
// rotates: the viewer token is re-issued and an administrator may pick a
// different tenant provider while the override is active. Both are recomputed
// from stored state on the next write instead.
type ownerLLMOverride struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	// WireAPI is carried so the bot talks to the provider the same way the
	// desktop does. It has no flat field of its own and only exists inside the
	// provider entry, so a provider that needs the responses API cannot work
	// without it.
	WireAPI string `json:"wire_api,omitempty"`
	// Official means this role uses the MaClaw Hub route already written for
	// the owner (hub-llm) and only overrides the model. No credential is stored.
	Official bool `json:"official,omitempty"`
}

// PrincipalLLMInput is one owner LLM save. Mode "default" returns every role
// to the service. Mode "custom" with Roles sets each role. Mode "custom"
// without Roles is the older single-provider body: foreground, background and
// computer use all take that provider, and the grounder stays unused. A blank
// key on a role whose provider name did not change keeps the stored key.
type PrincipalLLMInput struct {
	Mode     string                  `json:"mode"`
	Name     string                  `json:"name"`
	URL      string                  `json:"url"`
	Key      string                  `json:"key"`
	Model    string                  `json:"model"`
	Protocol string                  `json:"protocol"`
	WireAPI  string                  `json:"wire_api"`
	Roles    *PrincipalLLMRolesInput `json:"roles,omitempty"`
}

// PrincipalLLMRoleInput is one role in a custom save. Unused is grounder-only.
// Official selects the Hub route and a model, without a provider credential.
type PrincipalLLMRoleInput struct {
	Unused   bool   `json:"unused"`
	Official bool   `json:"official"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	WireAPI  string `json:"wire_api"`
}

// PrincipalLLMRolesInput is the four bot roles. A nil Roles pointer on the
// parent input means the caller is an older GUI that sent one provider.
type PrincipalLLMRolesInput struct {
	Foreground  PrincipalLLMRoleInput `json:"foreground"`
	Background  PrincipalLLMRoleInput `json:"background"`
	ComputerUse PrincipalLLMRoleInput `json:"computer_use"`
	Grounder    PrincipalLLMRoleInput `json:"grounder"`
}

// PrincipalLLMView is what the settings UI reads back. The key stays in the
// settings record; callers only learn whether one is set.
//
// Name, URL and Model mirror the foreground role so an older GUI, which only
// knows one provider, still has something to show. Saving from that GUI
// collapses the three main roles onto that one provider and clears the grounder.
type PrincipalLLMView struct {
	Mode   string                 `json:"mode"`
	Name   string                 `json:"name,omitempty"`
	URL    string                 `json:"url,omitempty"`
	Model  string                 `json:"model,omitempty"`
	KeySet bool                   `json:"key_set"`
	Roles  *PrincipalLLMRolesView `json:"roles,omitempty"`
	// Warning reports that the choice was stored but could not be written to
	// MaClawSrv yet, so it still applies on the owner's next bot open.
	Warning string `json:"warning,omitempty"`
}

// PrincipalLLMRoleView is one role with the key removed.
type PrincipalLLMRoleView struct {
	Unused   bool   `json:"unused,omitempty"`
	Official bool   `json:"official,omitempty"`
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Model    string `json:"model,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	WireAPI  string `json:"wire_api,omitempty"`
	KeySet   bool   `json:"key_set,omitempty"`
}

// PrincipalLLMRolesView is the custom-mode role list. It is omitted in default mode.
type PrincipalLLMRolesView struct {
	Foreground  PrincipalLLMRoleView `json:"foreground"`
	Background  PrincipalLLMRoleView `json:"background"`
	ComputerUse PrincipalLLMRoleView `json:"computer_use"`
	Grounder    PrincipalLLMRoleView `json:"grounder"`
}

// ownerLLMOverrideOf returns the override stored for an owner, or nil when
// that owner runs on the configured default.
func (s *Service) ownerLLMOverrideOf(ctx context.Context, tenantID, hubUserID string) *ownerLLMOverride {
	if s == nil || strings.TrimSpace(hubUserID) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return nil
	}
	return effectiveOwnerLLMOverride(rec, hubUserID)
}

func effectiveOwnerLLMOverride(rec record, hubUserID string) *ownerLLMOverride {
	override := rec.OwnerLLMOverrides[strings.TrimSpace(hubUserID)]
	if override == nil {
		return nil
	}
	// A stored row can predate a value being required, or a later edit can
	// clear one. Treat an incomplete row as no override rather than pushing a
	// config MaClawSrv would reject.
	if strings.TrimSpace(override.Name) == "" || strings.TrimSpace(override.URL) == "" ||
		strings.TrimSpace(override.Key) == "" || strings.TrimSpace(override.Model) == "" {
		return nil
	}
	if _, err := resolveOwnerLLMProtocol(override.Protocol); err != nil {
		return nil
	}
	return override
}

// GetPrincipalLLM is the read side of the owner LLM choice. Mode reports what
// the owner's bots actually run: their own roles, or the service default.
// A legacy single-provider override is migrated into those roles on this read.
func (s *Service) GetPrincipalLLM(ctx context.Context, tenantID, hubUserID string) (PrincipalLLMView, error) {
	choice, err := s.persistedOwnerBotLLM(ctx, tenantID, hubUserID)
	if err != nil {
		return PrincipalLLMView{}, err
	}
	return choice.view(), nil
}

// SavePrincipalLLM stores an owner's role choice and writes it to that
// owner's MaClawSrv user config. Mode "default" clears the roles, which
// restores whichever service default is configured now.
//
// The stored value survives a failed push: it is applied again on the next
// bot open. Callers receive the new view either way, with Warning set when
// only the push failed.
func (s *Service) SavePrincipalLLM(ctx context.Context, tenantID, hubUserID string, in PrincipalLLMInput) (PrincipalLLMView, error) {
	if s == nil || s.System == nil {
		return PrincipalLLMView{}, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return PrincipalLLMView{}, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		s.mu.Unlock()
		return PrincipalLLMView{}, err
	}
	migrateOwnerBotLLM(&rec, hubUserID)
	next, err := normalizeOwnerBotLLM(in, ownerBotLLMOf(rec, hubUserID), rec.LLM.Providers)
	if err != nil {
		s.mu.Unlock()
		return PrincipalLLMView{}, err
	}
	storeOwnerBotLLM(&rec, hubUserID, next)
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return PrincipalLLMView{}, err
	}
	s.mu.Unlock()

	view := next.view()
	if pushErr := s.pushPrincipalLLM(ctx, tenantID, hubUserID); pushErr != nil {
		view.Warning = pushErr.Error()
	}
	return view, nil
}

// pushPrincipalLLM writes the stored choice now, instead of waiting for the
// next bot open to notice the stamp moved.
func (s *Service) pushPrincipalLLM(ctx context.Context, tenantID, hubUserID string) error {
	if s == nil || s.OwnerLLM == nil {
		return nil
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	if findPrincipal(rec.Principals, hubUserID) == nil {
		// Nothing has ever been written for this owner, so there is no MaClaw
		// config to restore or replace. Their first bot open applies it.
		return nil
	}
	return s.writeBotOwnerLLM(ctx, tenantID, rec, hubUserID, true)
}

// normalizeCustomProvider validates one provider role against the tenant list.
// A blank key keeps the previous key only when the provider name is unchanged.
func normalizeCustomProvider(in PrincipalLLMInput, prev *ownerLLMOverride, providers []llmProvider) (*ownerLLMOverride, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || name != in.Name {
		return nil, fmt.Errorf("%w: llm provider name is required", ErrInvalidInput)
	}
	if len(name) > 64 || strings.ContainsAny(name, "\r\n") {
		return nil, fmt.Errorf("%w: llm provider name is invalid", ErrInvalidInput)
	}
	// Selections resolve by name inside one app_config. A name already carrying
	// meaning there would silently shadow it: MaClawSrv returns the first
	// match, and MaclawLLMProviderNameEqual folds some vendor aliases together.
	if reservedBotLLMName(name) {
		return nil, fmt.Errorf("%w: llm provider name is reserved", ErrInvalidInput)
	}
	for _, item := range providers {
		if corelib.MaclawLLMProviderNameEqual(item.Name, name) {
			return nil, fmt.Errorf("%w: llm provider name conflicts with a tenant provider", ErrInvalidInput)
		}
	}

	rawURL := strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if err := validateBaseURL(rawURL); err != nil {
		return nil, fmt.Errorf("%w: llm provider url is invalid", ErrInvalidInput)
	}
	model := strings.TrimSpace(in.Model)
	if model == "" || len(model) > 128 || strings.ContainsAny(model, "\r\n") {
		return nil, fmt.Errorf("%w: llm provider model is required", ErrInvalidInput)
	}
	wireAPI, err := resolveOwnerLLMWireAPI(in.WireAPI)
	if err != nil {
		return nil, err
	}
	protocol, err := resolveOwnerLLMProtocol(in.Protocol)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.Key)
	if strings.ContainsAny(key, "\r\n") || len(key) > 4096 {
		return nil, fmt.Errorf("%w: llm provider key is invalid", ErrInvalidInput)
	}
	if key == "" {
		if prev == nil || prev.Official || !corelib.MaclawLLMProviderNameEqual(prev.Name, name) || strings.TrimSpace(prev.Key) == "" {
			return nil, fmt.Errorf("%w: llm provider key is required", ErrInvalidInput)
		}
		key = prev.Key
	}
	return &ownerLLMOverride{
		Name: name, URL: rawURL, Key: key, Model: model, Protocol: protocol, WireAPI: wireAPI,
	}, nil
}

func resolveOwnerLLMProtocol(raw string) (string, error) {
	protocol := strings.ToLower(strings.TrimSpace(raw))
	switch protocol {
	case "":
		return "openai", nil
	case "openai", "anthropic":
		return protocol, nil
	}
	return "", fmt.Errorf("%w: llm provider protocol must be openai or anthropic", ErrInvalidInput)
}

// resolveOwnerLLMWireAPI mirrors the values corelib accepts on a provider
// entry, including the Codex websocket variant. Empty means the chat wire,
// which is what omitting the field also means downstream.
func resolveOwnerLLMWireAPI(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "chat":
		return "", nil
	case "responses", "responses-ws":
		return strings.ToLower(strings.TrimSpace(raw)), nil
	}
	return "", fmt.Errorf("%w: llm provider wire_api must be chat, responses or responses-ws", ErrInvalidInput)
}

// principalLLMStamp names the override. The key contributes only its hash, so
// the stamp never carries a credential.
func principalLLMStamp(override *ownerLLMOverride) string {
	if override == nil {
		return ""
	}
	hash := sha256.New()
	if override.Official {
		_, _ = io.WriteString(hash, "official\n")
		_, _ = io.WriteString(hash, strings.TrimSpace(override.Model)+"\n")
		return hex.EncodeToString(hash.Sum(nil))
	}
	_, _ = io.WriteString(hash, strings.TrimSpace(override.Name)+"\n")
	_, _ = io.WriteString(hash, strings.TrimSpace(override.URL)+"\n")
	_, _ = io.WriteString(hash, strings.TrimSpace(override.Model)+"\n")
	_, _ = io.WriteString(hash, strings.TrimSpace(override.Protocol)+"\n")
	_, _ = io.WriteString(hash, strings.TrimSpace(override.WireAPI)+"\n")
	sum := sha256.Sum256([]byte(override.Key))
	_, _ = io.WriteString(hash, hex.EncodeToString(sum[:])+"\n")
	return hex.EncodeToString(hash.Sum(nil))
}

// ownerLLMStamp names the whole config written for one owner: the tenant
// provider list plus that owner's role choice. Either half moving makes a
// rewrite happen, which is how leaving custom roles restores the service,
// and how a new administrator default reaches a bot that had none.
func ownerLLMStamp(settings llmSettings, choice ownerBotLLM) string {
	base := llmConfigStamp(settings)
	own := ownerBotLLMStamp(choice)
	if base == "" && own == "" {
		return ""
	}
	return base + "/" + own
}
