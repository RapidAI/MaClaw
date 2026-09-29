package llmservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

const (
	maxProviderArrayImportArrays    = 50
	maxProviderArrayImportProviders = 200
)

// ProviderArrayImport is one logical provider and the upstreams that belong to it.
// Members share the array multiplier and token price.
type ProviderArrayImport struct {
	ID                       string                            `json:"id"`
	Name                     string                            `json:"name"`
	Timezone                 string                            `json:"timezone,omitempty"`
	CreditMultiplier         float64                           `json:"credit_multiplier,omitempty"`
	CreditMultiplierSchedule []llmpool.CreditMultiplierWindow  `json:"credit_multiplier_schedule,omitempty"`
	TokenPricing             llmpool.TokenPricing              `json:"token_pricing,omitempty"`
	Providers                []llmpool.ProviderConfig          `json:"providers"`
}

// ProviderArrayImportItemResult reports which members were created or updated.
type ProviderArrayImportItemResult struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Created []string `json:"created,omitempty"`
	Updated []string `json:"updated,omitempty"`
}

// ProviderArrayImportResult is the outcome of one all-or-nothing batch.
type ProviderArrayImportResult struct {
	Arrays []ProviderArrayImportItemResult `json:"arrays"`
}

// ImportProviderArrays creates or updates providers and places them in the
// named arrays. The write is all or nothing. An existing provider keeps its
// paused flag and, when the payload omits api_key, its stored key.
func (s *Service) ImportProviderArrays(ctx context.Context, batch []ProviderArrayImport) (*ProviderArrayImportResult, error) {
	if s == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	if len(batch) == 0 {
		return nil, fmt.Errorf("arrays is required")
	}
	if len(batch) > maxProviderArrayImportArrays {
		return nil, fmt.Errorf("at most %d arrays per request", maxProviderArrayImportArrays)
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	next := cloneRegistry(reg)
	seenProviders := map[string]string{}
	seenArrays := map[string]struct{}{}
	result := &ProviderArrayImportResult{Arrays: make([]ProviderArrayImportItemResult, 0, len(batch))}
	totalProviders := 0
	for i := range batch {
		item := batch[i]
		if len(item.Providers) == 0 {
			return nil, fmt.Errorf("arrays[%d] needs at least one provider", i)
		}
		totalProviders += len(item.Providers)
		if totalProviders > maxProviderArrayImportProviders {
			return nil, fmt.Errorf("at most %d providers per request", maxProviderArrayImportProviders)
		}
		arrayID := strings.TrimSpace(item.ID)
		if arrayID == "" {
			arrayID = strings.TrimSpace(item.Providers[0].ID)
		}
		if arrayID == "" {
			return nil, fmt.Errorf("arrays[%d] needs an id or a provider id", i)
		}
		if _, dup := seenArrays[strings.ToLower(arrayID)]; dup {
			return nil, fmt.Errorf("duplicate array id %s", arrayID)
		}
		seenArrays[strings.ToLower(arrayID)] = struct{}{}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.Providers[0].Name)
		}
		if name == "" {
			name = arrayID
		}
		hasBilling := providerArrayImportHasBilling(item)
		row := ProviderArrayImportItemResult{ID: arrayID, Name: name}
		for pi := range item.Providers {
			provider := item.Providers[pi]
			provider.ID = strings.TrimSpace(provider.ID)
			provider.Name = strings.TrimSpace(provider.Name)
			provider.APIURL = strings.TrimSpace(provider.APIURL)
			if provider.ID == "" || provider.Name == "" || provider.APIURL == "" {
				return nil, fmt.Errorf("arrays[%d].providers[%d] needs id, name, and api_url", i, pi)
			}
			if !strings.HasPrefix(provider.APIURL, "https://") && !strings.HasPrefix(provider.APIURL, "http://") {
				return nil, fmt.Errorf("arrays[%d].providers[%d] api_url must start with http:// or https://", i, pi)
			}
			if prev, ok := seenProviders[strings.ToLower(provider.ID)]; ok {
				return nil, fmt.Errorf("provider %s is listed more than once (arrays %s and %s)", provider.ID, prev, arrayID)
			}
			seenProviders[strings.ToLower(provider.ID)] = arrayID
			if strings.TrimSpace(provider.Protocol) == "" {
				provider.Protocol = "openai"
			}
			provider.ArrayID = arrayID
			provider.ArrayIndependent = false
			if pi == 0 {
				provider.ArrayName = name
			} else {
				provider.ArrayName = ""
			}
			idx := providerIndex(next, provider.ID)
			if idx >= 0 {
				existing := next.Providers[idx]
				if strings.TrimSpace(provider.APIKey) == "" {
					provider.APIKey = existing.APIKey
				}
				if provider.Models == nil {
					provider.Models = append([]string(nil), existing.Models...)
				}
				if provider.CapabilityTags == nil {
					provider.CapabilityTags = append([]string(nil), existing.CapabilityTags...)
				}
				provider = mergeUnspecifiedProviderFields(existing, provider)
				provider.ArrayID = arrayID
				if pi == 0 {
					provider.ArrayName = name
				}
				if hasBilling {
					applyImportedArrayBilling(&provider, item)
				}
				provider.NormalizeBilling()
				next.Providers[idx] = provider
				row.Updated = append(row.Updated, provider.ID)
				continue
			}
			if hasBilling {
				applyImportedArrayBilling(&provider, item)
			}
			provider.NormalizeBilling()
			next.Providers = append(next.Providers, provider)
			row.Created = append(row.Created, provider.ID)
		}
		if err := applyImportedArrayRecord(next, arrayID, name, item, hasBilling); err != nil {
			return nil, err
		}
		result.Arrays = append(result.Arrays, row)
	}
	if err := s.persistRegistry(ctx, next); err != nil {
		return nil, err
	}
	return result, nil
}

func providerArrayImportHasBilling(item ProviderArrayImport) bool {
	if item.CreditMultiplier > 0 || strings.TrimSpace(item.Timezone) != "" || len(item.CreditMultiplierSchedule) > 0 {
		return true
	}
	return item.TokenPricing.HasCreditPricing() || len(item.TokenPricing.PriceSchedule) > 0
}

func applyImportedArrayBilling(provider *llmpool.ProviderConfig, item ProviderArrayImport) {
	if provider == nil {
		return
	}
	provider.Timezone = item.Timezone
	provider.CreditMultiplier = item.CreditMultiplier
	provider.CreditMultiplierSchedule = append([]llmpool.CreditMultiplierWindow(nil), item.CreditMultiplierSchedule...)
	provider.TokenPricing = item.TokenPricing.Clone()
}

func applyImportedArrayRecord(reg *Registry, arrayID, name string, item ProviderArrayImport, hasBilling bool) error {
	arr := findProviderArray(reg, arrayID)
	if arr == nil {
		reg.ProviderArrays = append(reg.ProviderArrays, llmpool.ProviderArray{ID: arrayID, Name: name})
		arr = &reg.ProviderArrays[len(reg.ProviderArrays)-1]
	}
	if strings.TrimSpace(name) != "" {
		arr.Name = name
	}
	if !hasBilling {
		return nil
	}
	arr.Timezone = item.Timezone
	arr.CreditMultiplier = item.CreditMultiplier
	arr.CreditMultiplierSchedule = append([]llmpool.CreditMultiplierWindow(nil), item.CreditMultiplierSchedule...)
	arr.TokenPricing = item.TokenPricing.Clone()
	return nil
}
