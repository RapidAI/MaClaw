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
	llmCurrentSystemFree = botLLMServiceGroupID
	llmProviderLimit     = 8
)

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
// Current is system-free, or the id of one provider in Providers.
type llmSettings struct {
	Current   string        `json:"current,omitempty"`
	Providers []llmProvider `json:"providers,omitempty"`
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

// LLMSettingsView is the admin LLM card. DefaultServiceGroup is the built-in
// system-free route. Current is that id, or one provider id.
type LLMSettingsView struct {
	DefaultServiceGroup string            `json:"default_service_group"`
	Current             string            `json:"current"`
	Providers           []LLMProviderView `json:"providers"`
}

// SaveLLMSettings replaces the extra providers MaClawSrv may call.
// system-free stays available. A blank key on a known id keeps the stored key.
func (s *Service) SaveLLMSettings(ctx context.Context, tenantID, current string, inputs []LLMProviderInput) (SettingsView, error) {
	if s == nil || s.System == nil {
		return SettingsView{}, ErrSettingsUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	next, err := normalizeLLMSettings(rec.LLM, current, inputs)
	if err != nil {
		return SettingsView{}, err
	}
	rec.LLM = next
	if err := s.save(ctx, tenantID, rec); err != nil {
		return SettingsView{}, err
	}
	return viewOf(rec), nil
}

func llmView(settings llmSettings) LLMSettingsView {
	current := strings.TrimSpace(settings.Current)
	if current == "" {
		current = llmCurrentSystemFree
	}
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
		DefaultServiceGroup: llmCurrentSystemFree,
		Current:             current,
		Providers:           providers,
	}
}

func llmIsDefault(settings llmSettings) bool {
	if len(settings.Providers) > 0 {
		return false
	}
	current := strings.TrimSpace(settings.Current)
	return current == "" || strings.EqualFold(current, llmCurrentSystemFree)
}

// llmConfigStamp names the provider list last written to MaClawSrv.
// The default system-free route has an empty stamp, so a deployment that
// has never saved an extra provider does not rewrite on the next command.
// The stamp is a hash. It does not contain the provider key.
func llmConfigStamp(settings llmSettings) string {
	if llmIsDefault(settings) {
		return ""
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, strings.TrimSpace(settings.Current))
	_, _ = io.WriteString(hash, "\n")
	for _, item := range settings.Providers {
		_, _ = io.WriteString(hash, item.ID+"\n"+item.Name+"\n"+item.Protocol+"\n"+item.URL+"\n"+item.Model+"\n")
		sum := sha256.Sum256([]byte(item.Key))
		_, _ = io.WriteString(hash, hex.EncodeToString(sum[:])+"\n")
	}
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
	if current == "" || strings.EqualFold(current, llmCurrentSystemFree) {
		return llmProvider{}, false
	}
	for _, item := range settings.Providers {
		if item.ID == current && strings.TrimSpace(item.Key) != "" {
			return item, true
		}
	}
	return llmProvider{}, false
}

func normalizeLLMSettings(prev llmSettings, current string, inputs []LLMProviderInput) (llmSettings, error) {
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
	current = strings.TrimSpace(current)
	switch {
	case current == "" || strings.EqualFold(current, llmCurrentSystemFree):
		current = llmCurrentSystemFree
	default:
		if _, ok := seenID[current]; !ok {
			return llmSettings{}, fmt.Errorf("%w: llm current provider was not found", ErrInvalidInput)
		}
	}
	return llmSettings{Current: current, Providers: out}, nil
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
	if strings.EqualFold(name, botLLMProviderName) || strings.EqualFold(name, llmCurrentSystemFree) {
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
