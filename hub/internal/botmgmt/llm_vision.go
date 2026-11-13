package botmgmt

import (
	"context"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type visionProbeResult struct {
	ID         string
	URL        string
	Model      string
	Supported  bool
	Known      bool
	DetectedAt string
}

func mergeVisionOverrides(prev []llmModelVision, keepDetection bool, inputs []LLMModelVisionInput) []llmModelVision {
	out := []llmModelVision{}
	if keepDetection {
		out = append(out, prev...)
	}
	for _, in := range inputs {
		model := strings.TrimSpace(in.Model)
		if model == "" || len(model) > 128 || strings.ContainsAny(model, "\r\n") {
			continue
		}
		override := visionOverride(in.Override)
		found := false
		for i := range out {
			if strings.EqualFold(out[i].Model, model) {
				out[i].Model = model
				out[i].Override = override
				if !keepDetection {
					out[i].Known = false
					out[i].Supported = false
					out[i].DetectedAt = ""
				}
				found = true
				break
			}
		}
		if !found {
			out = append(out, llmModelVision{Model: model, Override: override})
		}
	}
	return out
}

func visionOverride(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "yes", "1":
		return "on"
	case "off", "false", "no", "0":
		return "off"
	default:
		return "auto"
	}
}

func (s *Service) probeProviderInputs(ctx context.Context, tenantID string, inputs []LLMProviderInput, roles *LLMRoles) []visionProbeResult {
	if s == nil || s.ProbeVision == nil || len(inputs) == 0 {
		return nil
	}
	stored := map[string]llmProvider{}
	storedRoles := LLMRoles{}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	s.mu.Unlock()
	if err == nil {
		storedRoles = rec.LLM.Roles
		for _, item := range rec.LLM.Providers {
			stored[item.ID] = item
		}
	}
	if roles == nil {
		roles = &storedRoles
	}
	out := make([]visionProbeResult, 0, len(inputs))
	for _, in := range inputs {
		key := strings.TrimSpace(in.Key)
		id := strings.TrimSpace(in.ID)
		if key == "" {
			key = stored[id].Key
		}
		cfg := corelib.MaclawLLMConfig{
			URL:          strings.TrimRight(strings.TrimSpace(in.URL), "/"),
			Key:          key,
			Protocol:     strings.ToLower(strings.TrimSpace(in.Protocol)),
			ProviderName: strings.TrimSpace(in.Name),
		}
		if cfg.URL == "" || cfg.Key == "" {
			continue
		}
		for _, model := range visionModelsFor(id, strings.TrimSpace(in.Model), *roles) {
			cfg.Model = model
			supported, known := s.ProbeVision(ctx, cfg)
			out = append(out, visionProbeResult{
				ID: id, URL: cfg.URL, Model: model, Supported: supported, Known: known,
				DetectedAt: s.now().UTC().Format(time.RFC3339),
			})
		}
	}
	return out
}

// visionModelsFor is the provider default plus every role pin that names this
// provider. An empty pin model uses the provider default, so it is not a
// second probe.
func visionModelsFor(providerID, providerModel string, roles LLMRoles) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	add(providerModel)
	for _, role := range []LLMRoleAssignment{roles.Foreground, roles.Background, roles.ComputerUse, roles.Grounder} {
		if strings.ToLower(strings.TrimSpace(role.Mode)) != corelib.MaclawHubBotRolePin {
			continue
		}
		if strings.TrimSpace(role.Provider) != providerID || isBotDefaultServiceGroup(role.Provider) {
			continue
		}
		if model := strings.TrimSpace(role.Model); model != "" {
			add(model)
		}
	}
	return out
}

func applyVisionProbes(providers []llmProvider, probed []visionProbeResult) {
	for i := range providers {
		for _, result := range probed {
			if !result.Known {
				continue
			}
			if providers[i].ID != result.ID && providers[i].URL != result.URL {
				continue
			}
			upsertVision(&providers[i].Vision, result)
		}
	}
}

func upsertVision(list *[]llmModelVision, result visionProbeResult) {
	for i := range *list {
		if strings.EqualFold((*list)[i].Model, result.Model) {
			(*list)[i].Model = result.Model
			(*list)[i].Known = true
			(*list)[i].Supported = result.Supported
			(*list)[i].DetectedAt = result.DetectedAt
			if (*list)[i].Override == "" {
				(*list)[i].Override = "auto"
			}
			return
		}
	}
	*list = append(*list, llmModelVision{
		Model: result.Model, Override: "auto", Known: true, Supported: result.Supported, DetectedAt: result.DetectedAt,
	})
}

func (s *Service) probeStoredProviders(ctx context.Context, tenantID string) {
	if s == nil || s.ProbeVision == nil {
		return
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	s.mu.Unlock()
	if err != nil || len(rec.LLM.Providers) == 0 {
		return
	}
	inputs := make([]LLMProviderInput, 0, len(rec.LLM.Providers))
	for _, item := range rec.LLM.Providers {
		inputs = append(inputs, LLMProviderInput{
			ID: item.ID, Name: item.Name, Protocol: item.Protocol, URL: item.URL, Model: item.Model, Key: item.Key,
		})
	}
	roles := rec.LLM.Roles
	probed := s.probeProviderInputs(ctx, tenantID, inputs, &roles)
	if len(probed) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return
	}
	applyVisionProbes(fresh.LLM.Providers, probed)
	_ = s.save(ctx, tenantID, fresh)
}

// ProbeOneLLMProvider re-checks one saved provider model and stores the result.
func (s *Service) ProbeOneLLMProvider(ctx context.Context, tenantID, id, model string) (SettingsView, error) {
	if s == nil || s.System == nil {
		return SettingsView{}, ErrSettingsUnavailable
	}
	id = strings.TrimSpace(id)
	model = strings.TrimSpace(model)
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	s.mu.Unlock()
	if err != nil {
		return SettingsView{}, err
	}
	var found *llmProvider
	for i := range rec.LLM.Providers {
		if rec.LLM.Providers[i].ID == id {
			found = &rec.LLM.Providers[i]
			break
		}
	}
	if found == nil {
		return SettingsView{}, ErrNotFound
	}
	if model == "" {
		model = found.Model
	}
	if s.ProbeVision == nil {
		return viewOf(rec), nil
	}
	supported, known := s.ProbeVision(ctx, corelib.MaclawLLMConfig{
		URL: found.URL, Key: found.Key, Model: model, Protocol: found.Protocol, ProviderName: found.Name,
	})
	if !known {
		return viewOf(rec), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	applyVisionProbes(fresh.LLM.Providers, []visionProbeResult{{
		ID: id, URL: found.URL, Model: model, Supported: supported, Known: true,
		DetectedAt: s.now().UTC().Format(time.RFC3339),
	}})
	if err := s.save(ctx, tenantID, fresh); err != nil {
		return SettingsView{}, err
	}
	return viewOf(fresh), nil
}

// llmProviderWire is one provider pushed to MaClawSrv. Each probed or
// overridden model is listed on its own; an unprobed auto model is not
// written as supports_vision false.
func llmProviderWire(item llmProvider) map[string]any {
	wire := map[string]any{
		"id":       item.ID,
		"name":     item.Name,
		"url":      item.URL,
		"key":      item.Key,
		"model":    item.Model,
		"protocol": item.Protocol,
	}
	if len(item.Vision) == 0 {
		return wire
	}
	rows := make([]map[string]any, 0, len(item.Vision))
	var tested, supported []string
	defaultSupported := false
	defaultKnown := false
	for _, row := range item.Vision {
		model := strings.TrimSpace(row.Model)
		if model == "" {
			continue
		}
		entry := map[string]any{"model": model, "override": visionOverride(row.Override)}
		if row.DetectedAt != "" {
			entry["detected_at"] = row.DetectedAt
		}
		rows = append(rows, entry)
		if !row.Known {
			continue
		}
		tested = append(tested, model)
		if row.Supported {
			supported = append(supported, model)
		}
		if strings.EqualFold(model, item.Model) {
			defaultKnown = true
			defaultSupported = row.Supported
		}
	}
	wire["vision_by_model"] = rows
	if len(tested) > 0 {
		wire["vision_tested_models"] = tested
	}
	if len(supported) > 0 {
		wire["vision_models"] = supported
	}
	if defaultKnown {
		wire["supports_vision"] = defaultSupported
	}
	return wire
}
