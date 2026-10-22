package llmservice

import (
	"encoding/json"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// normalizeProviderUpstreamOverrides canonicalizes extra_body, its priority,
// and drop_fields. An invalid value is returned to the caller.
func normalizeProviderUpstreamOverrides(provider *llmpool.ProviderConfig) error {
	if provider == nil {
		return nil
	}
	extra, priority, drop, err := llmpool.NormalizeMemberUpstreamOverrides(provider.ExtraBody, provider.ExtraBodyPriority, provider.DropFields)
	if err != nil {
		return err
	}
	provider.ExtraBody = extra
	provider.ExtraBodyPriority = priority
	provider.DropFields = drop
	return nil
}

// canonicalizeProviderUpstreamOverrides keeps a loadable registry when a stored
// value is already invalid. Apply still skips protected keys.
func canonicalizeProviderUpstreamOverrides(provider *llmpool.ProviderConfig) {
	_ = normalizeProviderUpstreamOverrides(provider)
}

// WithMemberUpstreamOverrides copies the saved member override onto a request
// config. Vendor configs replace the request config, so callers attach this
// after that replacement.
func WithMemberUpstreamOverrides(cfg corelib.MaclawLLMConfig, provider *llmpool.ProviderConfig) corelib.MaclawLLMConfig {
	if provider == nil {
		return cfg
	}
	return corelib.MemberUpstreamOverrides(cfg, provider.ExtraBody, provider.ExtraBodyPriority, provider.DropFields)
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
