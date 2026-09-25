package workbuddy

import "strings"

// ModelSpec is one built-in model. The upstream config endpoint omits models
// that are still callable, so this list stays authoritative.
type ModelSpec struct {
	ID            string
	Name          string
	ContextLength int64
	MaxOutput     int64
}

// Allowlist returns the built-in models for a profile id.
func Allowlist(profileID string) []ModelSpec {
	switch profileID {
	case "workbuddy":
		return globalModels
	case "codebuddy":
		return chinaModels
	default:
		return nil
	}
}

// ModelsForURL returns the built-in catalog for a chat or API URL.
func ModelsForURL(rawURL string) []ModelSpec {
	profile, ok := ProfileByURL(rawURL)
	if !ok {
		return nil
	}
	return Allowlist(profile.ID)
}

// UpstreamModel is one entry from the live /v3/config catalog.
type UpstreamModel struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MaxInputTokens  int64  `json:"maxInputTokens"`
	MaxOutputTokens int64  `json:"maxOutputTokens"`
}

// MergeCatalog keeps the built-in allowlist and appends models that only the
// live catalog knows about. Upstream names and limits fill gaps in the allowlist.
func MergeCatalog(profileID string, upstream []UpstreamModel) []ModelSpec {
	base := Allowlist(profileID)
	up := make(map[string]UpstreamModel, len(upstream))
	for _, model := range upstream {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		up[id] = model
	}
	out := make([]ModelSpec, 0, len(base)+len(upstream))
	seen := make(map[string]bool, len(base)+len(upstream))
	for _, spec := range base {
		if extra, ok := up[spec.ID]; ok {
			if extra.Name != "" {
				spec.Name = extra.Name
			}
			if extra.MaxInputTokens > 0 {
				spec.ContextLength = extra.MaxInputTokens
			}
			if extra.MaxOutputTokens > 0 {
				spec.MaxOutput = extra.MaxOutputTokens
			}
		}
		seen[spec.ID] = true
		out = append(out, spec)
	}
	for _, extra := range upstream {
		id := strings.TrimSpace(extra.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(extra.Name)
		if name == "" {
			name = id
		}
		maxOut := extra.MaxOutputTokens
		if maxOut <= 0 {
			maxOut = 8192
		}
		out = append(out, ModelSpec{ID: id, Name: name, ContextLength: extra.MaxInputTokens, MaxOutput: maxOut})
	}
	return out
}

var globalModels = []ModelSpec{
	{"default-model", "Auto", 176000, 24000},
	{"fast-model", "Fast", 200000, 32000},
	{"balanced-model", "Balanced", 256000, 32000},
	{"primary-model", "Primary", 272000, 72000},
	{"deep-model", "Deep", 176000, 24000},
	{"gpt-5.6-terra", "GPT-5.6-Terra", 1000000, 128000},
	{"gpt-5.6-luna", "GPT-5.6-Luna", 1000000, 128000},
	{"gpt-5.5", "GPT-5.5", 1000000, 72000},
	{"gpt-5.4", "GPT-5.4", 272000, 128000},
	{"gpt-5.3-codex", "GPT-5.3-Codex", 272000, 128000},
	{"gemini-3.1-pro", "Gemini-3.1-Pro", 400000, 64000},
	{"gemini-3.5-flash", "Gemini-3.5-Flash", 1000000, 65536},
	{"deepseek-v4.1-flash", "DeepSeek-V4.1-Flash", 1000000, 128000},
	{"glm-5.3", "GLM-5.3", 1000000, 48000},
	{"glm-5.2", "GLM-5.2", 1000000, 48000},
	{"hy3", "Hy3", 192000, 64000},
	{"hy4-preview", "Hy4-Preview", 192000, 64000},
	{"hy4-preview-x", "Hy4-Preview-X", 192000, 64000},
	{"kimi-k3", "Kimi-K3", 1000000, 32000},
	{"kimi-k2.7", "Kimi-K2.7", 256000, 32000},
	{"kimi-k2.6", "Kimi-K2.6", 256000, 32000},
	{"kimi-k2.5", "Kimi-K2.5", 164000, 32000},
	{"minimax-m3", "MiniMax-M3", 512000, 128000},
}

var chinaModels = []ModelSpec{
	{"default", "Default", 200000, 24000},
	{"deepseek-v4-pro", "DeepSeek-V4-Pro", 1000000, 50000},
	{"deepseek-v4-flash", "DeepSeek-V4-Flash", 1000000, 50000},
	{"deepseek-v4.1-flash", "DeepSeek-V4.1-Flash", 1000000, 128000},
	{"deepseek-v3-2-volc", "DeepSeek-V3.2", 96000, 32000},
	{"minimax-m3", "MiniMax-M3", 512000, 128000},
	{"minimax-m2.7", "MiniMax-M2.7", 200000, 48000},
	{"minimax-m2.5", "MiniMax-M2.5", 200000, 48000},
	{"glm-5.3", "GLM-5.3", 1000000, 48000},
	{"glm-5.3-flash", "GLM-5.3-Flash", 1000000, 32000},
	{"glm-5.2", "GLM-5.2", 1000000, 48000},
	{"glm-5.1", "GLM-5.1", 200000, 48000},
	{"glm-5.0", "GLM-5.0", 200000, 48000},
	{"glm-5.0-turbo", "GLM-5.0-Turbo", 200000, 48000},
	{"glm-5v-turbo", "GLM-5v-Turbo", 200000, 64000},
	{"glm-4.7", "GLM-4.7", 200000, 48000},
	{"glm-4.6", "GLM-4.6", 168000, 32000},
	{"glm-4.6v", "GLM-4.6V", 128000, 32000},
	{"kimi-k3-1", "Kimi-K3", 1000000, 32000},
	{"kimi-k2.7", "Kimi-K2.7-Code", 256000, 32000},
	{"kimi-k2.6", "Kimi-K2.6", 256000, 32000},
	{"kimi-k2.5", "Kimi-K2.5", 164000, 32000},
	{"kimi-k2-thinking", "Kimi-K2-Thinking", 164000, 32000},
	{"hy3", "Hy3", 192000, 64000},
	{"hy3-x", "Hy3", 192000, 64000},
	{"hy4-preview", "Hy4 preview", 1000000, 64000},
	{"hy4-preview-x", "Hy4 preview", 1000000, 64000},
	{"hunyuan-chat", "Hunyuan-Turbos", 200000, 8192},
}
