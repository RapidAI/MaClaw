package corelib

import (
	"net/url"
	"strings"
)

// kimiLockedSamplingKeys are fields the Kimi gateway fills itself.
// Thinking mode uses temperature 1 and some non-thinking modes use 0.6;
// top_p, n, and the penalties are fixed too. Any other explicit value is
// rejected, and the documented fix is to leave the fields out.
var kimiLockedSamplingKeys = []string{
	"temperature",
	"top_p",
	"n",
	"presence_penalty",
	"frequency_penalty",
}

// OmitKimiLockedSampling removes sampling fields Kimi owns.
// It reports whether this request must not carry those fields, including
// when they were already absent, so callers do not write them back.
func OmitKimiLockedSampling(cfg MaclawLLMConfig, body map[string]interface{}) bool {
	if body == nil || !KimiRequestOmitsSampling(cfg, kimiBodyModel(body)) {
		return false
	}
	for _, key := range kimiLockedSamplingKeys {
		delete(body, key)
	}
	return true
}

// KimiRequestOmitsSampling reports a Kimi Code or K2/K3 request. bodyModel is
// optional; the provider config is enough when the body has not been built.
func KimiRequestOmitsSampling(cfg MaclawLLMConfig, bodyModel string) bool {
	// Most forwards are not Kimi. Decide that before parsing the URL or
	// rewriting the model: UpstreamModel parses every URL to detect CodeGen.
	if !hasKimiHint(cfg.URL) && !hasKimiHint(cfg.Model) && !hasKimiHint(bodyModel) {
		return false
	}
	if hasKimiHint(cfg.URL) && kimiCodingEndpoint(cfg.URL) {
		return true
	}
	if kimiModelOmitsSampling(bodyModel) {
		return true
	}
	if bodyModel != cfg.Model && kimiModelOmitsSampling(cfg.Model) {
		return true
	}
	return false
}

func kimiBodyModel(body map[string]interface{}) string {
	if body == nil {
		return ""
	}
	model, _ := body["model"].(string)
	return model
}

// kimiModelOmitsSampling reports Kimi model ids whose sampling parameters are
// fixed. Vendor prefixes (moonshot:kimi-k2.6) and router suffixes
// (kimi-for-coding:latest) are both recognized. The K2/K3 check requires a
// version boundary so an unrelated id such as kimi-k20 is left alone.
func kimiModelOmitsSampling(model string) bool {
	if !hasKimiHint(model) {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(model))
	name = strings.ReplaceAll(name, "_", "-")
	for _, part := range strings.FieldsFunc(name, isKimiModelSeparator) {
		if kimiPartLocked(strings.Trim(part, "]")) {
			return true
		}
	}
	return false
}

func isKimiModelSeparator(r rune) bool {
	switch r {
	case '/', ':', '@', '[', ' ', '\t':
		return true
	default:
		return false
	}
}

func kimiPartLocked(name string) bool {
	switch {
	case name == "kimi-for-coding", name == "kimi-code", name == "kimi-coding":
		return true
	case kimiFamilyLocked(name, "kimi-k2"), kimiFamilyLocked(name, "kimi-k3"):
		return true
	default:
		return false
	}
}

func kimiFamilyLocked(name, family string) bool {
	if name == family {
		return true
	}
	if !strings.HasPrefix(name, family) || len(name) == len(family) {
		return false
	}
	switch name[len(family)] {
	case '.', '-':
		return true
	default:
		return false
	}
}

// kimiCodingEndpoint reports Kimi Code's coding API. Every model on that host
// locks sampling, including aliases that do not start with kimi-k2.
func kimiCodingEndpoint(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !hasKimiHint(raw) || !containsASCIIFold(raw, "/coding") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		parsed, err = url.Parse("https://" + strings.TrimLeft(raw, "/"))
		if err != nil || parsed.Host == "" {
			return false
		}
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "api.kimi.com" && host != "api.kimi.ai" {
		return false
	}
	return containsASCIIFold(parsed.EscapedPath(), "/coding")
}

// hasKimiHint reports whether s contains "kimi" in any ASCII case.
// It allocates nothing so the non-Kimi forward path stays cheap.
func hasKimiHint(s string) bool {
	const needle = "kimi"
	if len(s) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		if foldASCII(s[i]) == 'k' && foldASCII(s[i+1]) == 'i' && foldASCII(s[i+2]) == 'm' && foldASCII(s[i+3]) == 'i' {
			return true
		}
	}
	return false
}

func containsASCIIFold(s, lowerNeedle string) bool {
	n := len(lowerNeedle)
	if n == 0 || len(s) < n {
		return false
	}
	for i := 0; i+n <= len(s); i++ {
		matched := true
		for j := 0; j < n; j++ {
			if foldASCII(s[i+j]) != lowerNeedle[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func foldASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
