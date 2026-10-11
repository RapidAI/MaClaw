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
}

// PrincipalLLMInput is one owner LLM save. Mode "default" drops any override
// and returns the owner to the configured default; "custom" installs one. A
// blank key on an existing override keeps the stored key.
type PrincipalLLMInput struct {
	Mode     string `json:"mode"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	WireAPI  string `json:"wire_api"`
}

// PrincipalLLMView is what the settings UI reads back. The key stays in the
// settings record; callers only learn whether one is set.
type PrincipalLLMView struct {
	Mode   string `json:"mode"`
	Name   string `json:"name,omitempty"`
	URL    string `json:"url,omitempty"`
	Model  string `json:"model,omitempty"`
	KeySet bool   `json:"key_set"`
	// Warning reports that the choice was stored but could not be written to
	// MaClawSrv yet, so it still applies on the owner's next bot open.
	Warning string `json:"warning,omitempty"`
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
// the owner's bots actually run: their own choice, or the configured default.
func (s *Service) GetPrincipalLLM(ctx context.Context, tenantID, hubUserID string) (PrincipalLLMView, error) {
	if s == nil || s.System == nil {
		return PrincipalLLMView{}, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return PrincipalLLMView{}, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return PrincipalLLMView{}, err
	}
	override := effectiveOwnerLLMOverride(rec, hubUserID)
	if override == nil {
		return PrincipalLLMView{Mode: "default"}, nil
	}
	return PrincipalLLMView{
		Mode:   "custom",
		Name:   override.Name,
		URL:    override.URL,
		Model:  override.Model,
		KeySet: strings.TrimSpace(override.Key) != "",
	}, nil
}

// SavePrincipalLLM stores an owner's provider choice and writes it to that
// owner's MaClawSrv user config. Mode "default" drops the override, which
// restores whichever default is configured now, system or administrator.
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
	override, err := normalizeOwnerLLMOverride(in, rec, hubUserID)
	if err != nil {
		s.mu.Unlock()
		return PrincipalLLMView{}, err
	}
	if rec.OwnerLLMOverrides == nil && override != nil {
		rec.OwnerLLMOverrides = map[string]*ownerLLMOverride{}
	}
	switch {
	case override == nil:
		delete(rec.OwnerLLMOverrides, hubUserID)
	default:
		rec.OwnerLLMOverrides[hubUserID] = override
	}
	if len(rec.OwnerLLMOverrides) == 0 {
		rec.OwnerLLMOverrides = nil
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return PrincipalLLMView{}, err
	}
	s.mu.Unlock()

	view, err := s.GetPrincipalLLM(ctx, tenantID, hubUserID)
	if err != nil {
		return view, err
	}
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

// normalizeOwnerLLMOverride validates one save against the tenant's provider
// list. A nil result means "return to the configured default".
func normalizeOwnerLLMOverride(in PrincipalLLMInput, rec record, hubUserID string) (*ownerLLMOverride, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	switch mode {
	case "", "default", "system":
		return nil, nil
	case "custom", "provider":
	default:
		return nil, fmt.Errorf("%w: llm mode must be default or custom", ErrInvalidInput)
	}
	prev := rec.OwnerLLMOverrides[hubUserID]

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
	if strings.EqualFold(name, botLLMProviderName) || strings.EqualFold(name, llmCurrentSystemFree) {
		return nil, fmt.Errorf("%w: llm provider name is reserved", ErrInvalidInput)
	}
	for _, item := range rec.LLM.Providers {
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
		if prev == nil || strings.TrimSpace(prev.Key) == "" {
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
// provider list plus that owner's override. Either half moving makes a rewrite
// happen, which is how leaving an override restores the default, and how a new
// administrator default reaches a bot that had none.
func ownerLLMStamp(settings llmSettings, override *ownerLLMOverride) string {
	base := llmConfigStamp(settings)
	own := principalLLMStamp(override)
	if base == "" && own == "" {
		return ""
	}
	return base + "/" + own
}
