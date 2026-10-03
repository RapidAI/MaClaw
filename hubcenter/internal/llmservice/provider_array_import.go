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
	ID                       string                           `json:"id"`
	Name                     string                           `json:"name"`
	Timezone                 string                           `json:"timezone,omitempty"`
	CreditMultiplier         float64                          `json:"credit_multiplier,omitempty"`
	CreditMultiplierSchedule []llmpool.CreditMultiplierWindow `json:"credit_multiplier_schedule,omitempty"`
	TokenPricing             llmpool.TokenPricing             `json:"token_pricing,omitempty"`
	Providers                []llmpool.ProviderConfig         `json:"providers"`
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
// paused flag and, when the payload omits api_key, its stored key. A new
// member is created in service; pause and resume use PATCH enabled.
// PreviewProviderArrays validates a batch and reports creates and updates
// without writing.
func (s *Service) PreviewProviderArrays(ctx context.Context, batch []ProviderArrayImport) (*ProviderArrayImportResult, error) {
	return s.importProviderArrays(ctx, batch, true)
}

func (s *Service) ImportProviderArrays(ctx context.Context, batch []ProviderArrayImport) (*ProviderArrayImportResult, error) {
	return s.importProviderArrays(ctx, batch, false)
}

func (s *Service) importProviderArrays(ctx context.Context, batch []ProviderArrayImport, dryRun bool) (*ProviderArrayImportResult, error) {
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
		if strings.Contains(arrayID, "#") {
			return nil, fmt.Errorf("arrays[%d] id %q cannot contain #", i, arrayID)
		}
		if err := validateProviderDefaultBilling(llmpool.ProviderConfig{TokenPricing: item.TokenPricing}); err != nil {
			return nil, fmt.Errorf("arrays[%d]: %w", i, err)
		}
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
			switch strings.ToLower(strings.TrimSpace(provider.Protocol)) {
			case "", "openai":
				provider.Protocol = "openai"
			case "anthropic":
				provider.Protocol = "anthropic"
			default:
				return nil, fmt.Errorf("arrays[%d].providers[%d] protocol must be openai or anthropic", i, pi)
			}
			if strings.Contains(provider.ID, "#") {
				return nil, fmt.Errorf("arrays[%d].providers[%d] id %q cannot contain #", i, pi, provider.ID)
			}
			tagsSet := provider.CapabilityTags != nil
			if tagsSet {
				tags, err := NormalizeCapabilityTags(provider.CapabilityTags)
				if err != nil {
					return nil, fmt.Errorf("arrays[%d].providers[%d]: %w", i, pi, err)
				}
				provider.CapabilityTags = tags
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
				if !tagsSet {
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
				if err := validateProviderDefaultBilling(provider); err != nil {
					return nil, fmt.Errorf("arrays[%d].providers[%d]: %w", i, pi, err)
				}
				if err := validateMemberPolicy(&provider); err != nil {
					return nil, fmt.Errorf("arrays[%d].providers[%d]: %w", i, pi, err)
				}
				next.Providers[idx] = provider
				row.Updated = append(row.Updated, provider.ID)
				continue
			}
			if hasBilling {
				applyImportedArrayBilling(&provider, item)
			}
			provider.NormalizeBilling()
			if err := validateProviderDefaultBilling(provider); err != nil {
				return nil, fmt.Errorf("arrays[%d].providers[%d]: %w", i, pi, err)
			}
			if err := validateMemberPolicy(&provider); err != nil {
				return nil, fmt.Errorf("arrays[%d].providers[%d]: %w", i, pi, err)
			}
			// Pause and resume go through PATCH enabled. A create must not
			// arrive already paused because the body included "paused".
			provider.Paused = false
			next.Providers = append(next.Providers, provider)
			row.Created = append(row.Created, provider.ID)
		}
		if err := applyImportedArrayRecord(next, arrayID, name, item, hasBilling); err != nil {
			return nil, err
		}
		if arr := findProviderArray(next, arrayID); arr != nil {
			if stored := strings.TrimSpace(arr.Name); stored != "" {
				row.Name = stored
			}
		}
		result.Arrays = append(result.Arrays, row)
	}
	if err := rejectImportedArrayIDCollisions(next, seenArrays); err != nil {
		return nil, err
	}
	if dryRun {
		return result, nil
	}
	if err := s.persistRegistry(ctx, next); err != nil {
		return nil, err
	}
	return result, nil
}

func rejectImportedArrayIDCollisions(reg *Registry, arrayIDs map[string]struct{}) error {
	if reg == nil {
		return nil
	}
	for i := range reg.Providers {
		provider := reg.Providers[i]
		providerID := strings.TrimSpace(provider.ID)
		if providerID == "" {
			continue
		}
		if _, imported := arrayIDs[strings.ToLower(providerID)]; !imported {
			continue
		}
		owner := strings.TrimSpace(provider.ArrayID)
		if strings.EqualFold(owner, providerID) {
			continue
		}
		if owner == "" {
			owner = providerID
		}
		return fmt.Errorf("array id %s matches provider %s in array %s; include that provider in this array or choose another id", providerID, providerID, owner)
	}
	return nil
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
		createdName := name
		if canonical, ok := tokenBankArrayName(arrayID); ok {
			createdName = canonical
		}
		reg.ProviderArrays = append(reg.ProviderArrays, llmpool.ProviderArray{ID: arrayID, Name: createdName})
		arr = &reg.ProviderArrays[len(reg.ProviderArrays)-1]
	}
	// item.Name is what the request submitted. The caller substitutes the first
	// provider's name when that field is empty, so a new ordinary array still
	// has a label. An omitted name is not a rename of a platform array.
	if err := rejectProtectedArrayRename(arr, item.Name); err != nil {
		return err
	}
	if !providerArrayProtected(arr) && strings.TrimSpace(name) != "" {
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
