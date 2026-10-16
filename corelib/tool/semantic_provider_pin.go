package tool

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// UnmetReasonPinnedProviderUnavailable is the unmet reason recorded when a
// pinned provider is the only implementation the turn was allowed to use and
// it is not selectable. It is deliberately distinct from "no_feasible_provider"
// so a host can say "the implementation you named is unavailable" instead of
// "this capability is not covered" — the latter reads as a catalog gap and
// hides that a silent substitution is what actually happened.
const UnmetReasonPinnedProviderUnavailable = "pinned_provider_unavailable"

// ProviderPin binds one capability of a single route to one installed dynamic
// provider the user named in the turn text.
//
// A managed semantic surface never shows an implementation name to the model:
// a dynamic adapter renders as its capability's stable prompt spelling, or as
// a rotating grant token. That is deliberate — a vendor name must not become a
// routing primitive the model can mint, and the legacy gateways that used to
// hand the model an open provider selector (call_mcp_tool, manage_skill) are
// banned from a managed surface for the same reason.
//
// What that design left unrepresented is the intent "use the implementation I
// installed and named". Quality-ranked selection can answer it by accident —
// a higher-quality MCP provider wins under the shared prompt name — but it can
// never be requested, observed, or enforced. A pin is the missing address:
//
//   - it is derived by the host from the user text, never by the model;
//   - it names a binding that is already published and contract-verified, so
//     it cannot admit a provider the catalog would have rejected;
//   - it narrows selection for one capability and is inert when that
//     capability is not needed this turn;
//   - it relaxes nothing: a pinned provider that is not Ready or is lifecycle
//     ineligible is simply not selectable. The planner records
//     UnmetReasonPinnedProviderUnavailable rather than falling through to a
//     lower-quality builtin under the same prompt name.
type ProviderPin struct {
	Capability CapabilityID
	Kind       string // mcp, skill
	ProviderID string // MCP server id; skill stable id
	// ImplementationID further narrows the pin to one implementation inside
	// the provider (e.g. one tool on a server that publishes several). Empty
	// leaves the pin provider-wide; set, it excludes sibling implementations —
	// the user named that exact tool.
	ImplementationID string
}

// valid reports whether the pin carries enough identity to narrow selection.
// An empty Kind or ProviderID would match any provider and is therefore
// treated as no pin at all.
func (p ProviderPin) valid() bool {
	return strings.TrimSpace(string(p.Capability)) != "" &&
		strings.TrimSpace(p.Kind) != "" &&
		strings.TrimSpace(p.ProviderID) != ""
}

// Matches reports whether binding is the pinned implementation. Comparison is
// on the immutable provider identity only: a schema digest change republishes
// the same server under a new binding, and a pin is a statement about the
// installed product, not about one published revision of it.
func (p ProviderPin) Matches(binding ProviderBinding) bool {
	if !p.valid() {
		return false
	}
	if strings.TrimSpace(binding.Kind) != strings.TrimSpace(p.Kind) ||
		strings.TrimSpace(binding.ProviderID) != strings.TrimSpace(p.ProviderID) {
		return false
	}
	if impl := strings.TrimSpace(p.ImplementationID); impl != "" {
		return strings.TrimSpace(binding.ImplementationID) == impl
	}
	return true
}

// PinForCapability returns the pin that governs capability. The first pin wins
// so a host that derived two pins for one capability has a deterministic
// outcome instead of an order-dependent one.
func PinForCapability(pins []ProviderPin, capability CapabilityID) (ProviderPin, bool) {
	want := strings.TrimSpace(string(capability))
	if want == "" {
		return ProviderPin{}, false
	}
	for _, pin := range pins {
		if !pin.valid() {
			continue
		}
		if strings.TrimSpace(string(pin.Capability)) == want {
			return pin, true
		}
	}
	return ProviderPin{}, false
}

// CloneProviderPins copies a pin slice so a persisted or replayed route
// request cannot share a live slice with the planning call.
func CloneProviderPins(pins []ProviderPin) []ProviderPin {
	if len(pins) == 0 {
		return nil
	}
	out := make([]ProviderPin, 0, len(pins))
	for _, pin := range pins {
		out = append(out, ProviderPin{
			Capability:       CapabilityID(strings.TrimSpace(string(pin.Capability))),
			Kind:             strings.TrimSpace(pin.Kind),
			ProviderID:       strings.TrimSpace(pin.ProviderID),
			ImplementationID: strings.TrimSpace(pin.ImplementationID),
		})
	}
	return out
}

// normalizeProviderReference folds one reference onto its comparison form.
// Server ids, display names and vendor tool names are spelled with either "-"
// or "_" depending on where they came from, and a user typing either spelling
// means the same product.
func normalizeProviderReference(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "-", "_")
}

// namedInText reports whether normalized token appears in normalized text as a
// whole identifier: a hit inside a longer identifier is not a reference to
// this token. "web_search" must not match inside "web_search_prime", and
// "search" must not match inside "research".
func namedInText(normalizedText, normalizedToken string) bool {
	if normalizedText == "" || normalizedToken == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(normalizedText[start:], normalizedToken)
		if idx < 0 {
			return false
		}
		at := start + idx
		if identifierBoundary(normalizedText, at, at+len(normalizedToken)) {
			return true
		}
		start = at + 1
		if start >= len(normalizedText) {
			return false
		}
	}
}

// identifierBoundary reports that the match is not flanked by identifier
// characters. '_' is an identifier character after normalization: it is the
// folded form of '-' and the ordinary separator inside a tool name.
//
// Only ASCII letters, digits and '_' count as identifier bytes, so a CJK
// reference always sits on a boundary: CJK is written without separators, and
// demanding one would make every CJK reference unmatched rather than more
// precise. The cost is that a CJK reference also matches inside a longer CJK
// phrase; minCJKProviderReferenceRunes is what keeps that from reaching
// ordinary prose.
func identifierBoundary(text string, start, end int) bool {
	if start > 0 {
		if isIdentifierByte(text[start-1]) {
			return false
		}
	}
	if end < len(text) {
		if isIdentifierByte(text[end]) {
			return false
		}
	}
	return true
}

func isIdentifierByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// minProviderReferenceLength is the shortest ASCII reference a pin may be
// derived from. Anything shorter matches ordinary prose far too often: "ai",
// "db" and "io" are fragments of normal sentences.
const minProviderReferenceLength = 4

// minCJKProviderReferenceRunes is the same threshold for a reference that
// contains CJK. Two CJK characters are two syllables, not a distinctive
// product reference, and CJK text has no spaces to bound a match: a server
// whose display name is 「文档」 would otherwise pin itself on every turn that
// merely says "文档", and a false pin is not a no-op — it can divert the turn
// to that server or, when the server is down, turn the whole turn into a
// refusal. Missing a pin degrades to the previous quality-ranked behavior;
// inventing one overrides the user's actual words, so the threshold stays
// strict.
const minCJKProviderReferenceRunes = 3

// providerReferenceEligible reports whether a normalized reference is
// distinctive enough to carry a pin. Runes are counted, not bytes: a CJK
// character occupies three bytes, so a byte length would clear a four-byte
// floor with a single two-syllable word.
func providerReferenceEligible(reference string) bool {
	if reference == "" {
		return false
	}
	runes := utf8.RuneCountInString(reference)
	for _, r := range reference {
		if isCJKRune(r) {
			return runes >= minCJKProviderReferenceRunes
		}
	}
	return runes >= minProviderReferenceLength
}

func isCJKRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// ProviderReferenceMatcher decides whether a turn's text names an installed
// implementation. Normalizing the text is the expensive part — a lowercase and
// a separator fold over the whole message — and a host asks the same question
// about every installed server, so the normalized form is computed once and
// reused instead of once per (server, reference) pair.
type ProviderReferenceMatcher struct {
	text string
}

// NewProviderReferenceMatcher prepares matcher for one turn's user text.
func NewProviderReferenceMatcher(userText string) ProviderReferenceMatcher {
	return ProviderReferenceMatcher{text: normalizeProviderReference(userText)}
}

// ServerNames reports whether the turn names this server by its id or by one
// of its display names.
func (m ProviderReferenceMatcher) ServerNames(serverID string, displayNames ...string) bool {
	if m.text == "" {
		return false
	}
	// m.text is already normalized by NewProviderReferenceMatcher, so call the
	// pre-normalized variant. Re-normalizing here would undo the one-time
	// normalization the matcher exists to provide.
	if providerIDNamedIn(m.text, serverID) {
		return true
	}
	for _, name := range displayNames {
		if providerIDNamedIn(m.text, name) {
			return true
		}
	}
	return false
}

// ToolNames reports whether the turn names a vendor tool spelling this server
// publishes.
func (m ProviderReferenceMatcher) ToolNames(toolName string) bool {
	if m.text == "" {
		return false
	}
	// Same reasoning as ServerNames: m.text is pre-normalized, so use the
	// pre-normalized variant rather than re-normalizing the whole message.
	return providerToolNamedIn(m.text, toolName)
}

// ProviderIDNamed reports whether userText names the server-level reference
// serverIDOrName. A server id is the product the user installed, so any
// reference long enough to be distinctive is eligible.
//
// It normalizes userText once and delegates to providerIDNamedIn, which is the
// variant the matcher calls with text it has already normalized.
func ProviderIDNamed(userText, serverIDOrName string) bool {
	return providerIDNamedIn(normalizeProviderReference(userText), serverIDOrName)
}

// providerIDNamedIn is the matcher-facing variant: normalizedText has already
// been lowercased and folded (the expensive whole-message pass), so it only
// normalizes the short reference and runs the boundary-aware match.
func providerIDNamedIn(normalizedText, serverIDOrName string) bool {
	reference := normalizeProviderReference(serverIDOrName)
	if !providerReferenceEligible(reference) {
		return false
	}
	return namedInText(normalizedText, reference)
}

// ProviderToolNamed reports whether userText names the tool-level reference
// toolName. A bare lowercase word is not eligible: most vendor tool names are
// one common English word ("search", "fetch", "read") and pinning on those
// would hijack every ordinary turn that happens to use the word. Only a
// compound spelling — snake_case, kebab-case or camelCase, which is what makes
// a vendor tool name recognizable as a product's tool — can carry a pin.
func ProviderToolNamed(userText, toolName string) bool {
	return providerToolNamedIn(normalizeProviderReference(userText), toolName)
}

// providerToolNamedIn is the matcher-facing variant: normalizedText has already
// been lowercased and folded, so it only normalizes the short tool reference
// and runs the compound-eligibility and boundary-aware checks.
func providerToolNamedIn(normalizedText, toolName string) bool {
	reference := normalizeProviderReference(toolName)
	if !providerReferenceEligible(reference) || !compoundProviderReference(toolName) {
		return false
	}
	return namedInText(normalizedText, reference)
}

// compoundProviderReference reports that a tool name is spelled as a compound
// rather than as one bare word.
func compoundProviderReference(toolName string) bool {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "_-") {
		return true
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] >= 'A' && trimmed[i] <= 'Z' {
			return true
		}
	}
	return false
}
