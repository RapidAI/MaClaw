package llmservice

import (
	"encoding/json"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ProbeOutputField is one output budget on the request that left this process.
// Value is the integer on the wire. Clipped is true when that integer is lower
// than the number the probe meant to send before the member cap and the Google
// REST hop ceiling. The receipt never includes the API key or the request body.
type ProbeOutputField struct {
	Value   int64 `json:"value"`
	Clipped bool  `json:"clipped"`
}

// ProbeOutputKind is the wire shape a member probe uses for its output budget.
func ProbeOutputKind(cfg corelib.MaclawLLMConfig) string {
	switch {
	case cfg.IsGoogleProtocol():
		return "google"
	case cfg.Protocol == "anthropic":
		return "anthropic"
	case cfg.IsResponsesAPI():
		return "responses"
	default:
		return "openai"
	}
}

// ProbeOutputBaseline is the output budget the probe meant to send before the
// member cap and the Google REST hop ceiling. Keys are the wire names that
// protocol puts on the body: max_tokens, max_completion_tokens,
// max_output_tokens, or maxOutputTokens. Magnitudes keep a fractional part so
// 16384.9 still counts as above a sent 16384.
func ProbeOutputBaseline(kind string, extra map[string]any, priority string, drop []string, maxTokens, maxCompletion *int64, tools bool) map[string]float64 {
	fallback := int64(64)
	if tools {
		fallback = 256
	}
	raw := map[string]any{}
	if maxTokens != nil {
		raw["max_tokens"] = *maxTokens
	}
	if maxCompletion != nil {
		raw["max_completion_tokens"] = *maxCompletion
	}
	if len(raw) == 0 {
		raw["max_tokens"] = fallback
	}
	var body map[string]any
	switch kind {
	case "google":
		n := int64(MemberProbeFieldBudget(raw, int(fallback), "max_completion_tokens", "max_tokens"))
		body = map[string]any{"generationConfig": map[string]any{"maxOutputTokens": n}}
	case "anthropic":
		n := int64(MemberProbeFieldBudget(raw, int(fallback), "max_tokens", "max_completion_tokens"))
		body = map[string]any{"max_tokens": n}
	case "responses":
		n := int64(MemberProbeFieldBudget(raw, int(fallback), "max_completion_tokens", "max_tokens"))
		body = map[string]any{"max_output_tokens": n}
	default:
		body = raw
	}
	corelib.ApplyMemberUpstreamBody(body, extra, priority, drop)
	return probeOutputMagnitudes(body)
}

// ProjectProbeOutput reads the output budgets from a built upstream body.
// Only those numbers are returned. baseline comes from ProbeOutputBaseline;
// a sent magnitude lower than that baseline is clipped.
func ProjectProbeOutput(body []byte, baseline map[string]float64) map[string]ProbeOutputField {
	if len(body) == 0 {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil || doc == nil {
		return nil
	}
	sent := map[string]any{}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if raw, ok := doc[key]; ok {
			sent[key] = raw
		}
	}
	if gen := probeObject(doc["generationConfig"]); gen != nil {
		if raw, ok := gen["maxOutputTokens"]; ok {
			sent["maxOutputTokens"] = raw
		}
	}
	if len(sent) == 0 {
		return nil
	}
	out := make(map[string]ProbeOutputField, len(sent))
	for key, raw := range sent {
		n, ok := corelib.OutputBudgetInt(raw)
		if !ok || n < 0 {
			continue
		}
		field := ProbeOutputField{Value: n}
		if asked, ok := baseline[key]; ok {
			if mag, ok := probeOutputMagnitude(raw); ok && mag < asked {
				field.Clipped = true
			}
		}
		out[key] = field
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func probeOutputMagnitudes(body map[string]any) map[string]float64 {
	if body == nil {
		return nil
	}
	out := map[string]float64{}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if mag, ok := probeOutputMagnitude(body[key]); ok {
			out[key] = mag
		}
	}
	if gen := probeObject(body["generationConfig"]); gen != nil {
		if mag, ok := probeOutputMagnitude(gen["maxOutputTokens"]); ok {
			out["maxOutputTokens"] = mag
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func probeOutputMagnitude(raw any) (float64, bool) {
	n, ok := corelib.OutputBudgetInt(raw)
	if !ok || n < 0 {
		return 0, false
	}
	switch v := raw.(type) {
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return float64(n), true
		}
		return f, true
	default:
		return float64(n), true
	}
}

func probeObject(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}
