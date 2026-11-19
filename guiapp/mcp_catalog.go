package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// mcpProviderInfo is the MCP-visible provider row. Secrets stay on the host.
type mcpProviderInfo struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	DefaultModel     string   `json:"default_model,omitempty"`
	Models           []string `json:"models"`
	ConnectionTested bool     `json:"connection_tested"`
}

// mcpModelCatalog is the list_models payload.
type mcpModelCatalog struct {
	Current   string            `json:"current,omitempty"`
	Providers []mcpProviderInfo `json:"providers"`
}

func mcpProviderModels(p corelib.MaclawLLMProvider) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(p.Models))
	add := func(model string) {
		model = strings.TrimSpace(corelib.MigrateZhipuCodingModel(p.Name, model))
		if model == "" {
			return
		}
		if _, ok := seen[model]; ok {
			return
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	add(p.Model)
	for _, model := range p.Models {
		add(model)
	}
	return out
}

func mcpProviderVisible(p corelib.MaclawLLMProvider, current string, models []string) bool {
	if strings.TrimSpace(p.Name) == "" || len(models) == 0 {
		return false
	}
	if p.ConnectionTestPassed {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(current))
}

func mcpModelCatalogFrom(providers []corelib.MaclawLLMProvider, current string) mcpModelCatalog {
	catalog := mcpModelCatalog{Current: strings.TrimSpace(current), Providers: []mcpProviderInfo{}}
	for _, provider := range providers {
		models := mcpProviderModels(provider)
		if !mcpProviderVisible(provider, current, models) {
			continue
		}
		catalog.Providers = append(catalog.Providers, mcpProviderInfo{
			ID:               maclawLLMProviderIDForRead(provider),
			Name:             strings.TrimSpace(provider.Name),
			DefaultModel:     models[0],
			Models:           models,
			ConnectionTested: provider.ConnectionTestPassed,
		})
	}
	return catalog
}

// mcpResolveProviderModel matches a provider by id or name against the
// visible catalog. An empty model uses that provider's default. A model
// without a provider is rejected. The returned name is the provider label
// MaterializeProviderByName understands.
func mcpResolveProviderModel(providers []corelib.MaclawLLMProvider, current, provider, model string) (string, string, error) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" && model == "" {
		return "", "", nil
	}
	if provider == "" {
		return "", "", fmt.Errorf("指定模型时必须同时指定服务商")
	}
	catalog := mcpModelCatalogFrom(providers, current)
	var match *mcpProviderInfo
	for i := range catalog.Providers {
		item := &catalog.Providers[i]
		if strings.EqualFold(item.ID, provider) || strings.EqualFold(item.Name, provider) {
			match = item
			break
		}
	}
	if match == nil {
		return "", "", fmt.Errorf("没有找到可用的服务商")
	}
	chosen := model
	if chosen == "" {
		chosen = match.DefaultModel
	}
	found := false
	for _, candidate := range match.Models {
		if strings.EqualFold(candidate, chosen) {
			chosen = candidate
			found = true
			break
		}
	}
	if !found {
		return "", "", fmt.Errorf("服务商没有这个模型")
	}
	return match.Name, chosen, nil
}
