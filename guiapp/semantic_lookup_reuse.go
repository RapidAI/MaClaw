package guiapp

import (
	"context"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Conversation lookup facts satisfy the generate after-edge. The planner
// lookup-to-generate Requires means generate needs facts, not that this
// turn must call search again.
type semanticConversationHistoryKey struct{}

func withSemanticConversationHistory(ctx context.Context, history []agent.ConversationEntry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(history) == 0 {
		return ctx
	}
	return context.WithValue(ctx, semanticConversationHistoryKey{}, history)
}

func semanticConversationHistory(ctx context.Context) []agent.ConversationEntry {
	if ctx == nil {
		return nil
	}
	history, _ := ctx.Value(semanticConversationHistoryKey{}).([]agent.ConversationEntry)
	return history
}

func semanticNeedIsLookup(need tool.CapabilityNeed) bool {
	return tool.IsLookupCapability(need.Capability)
}

func semanticNeedIsWebLookup(need tool.CapabilityNeed) bool {
	return tool.IsWebLookupCapability(need.Capability)
}

func semanticNeedsHaveLookup(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if semanticNeedIsLookup(need) {
			return true
		}
	}
	return false
}

func semanticNeedsHaveWebLookup(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if semanticNeedIsWebLookup(need) {
			return true
		}
	}
	return false
}

func semanticNeedsHaveGenerate(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if strings.TrimSpace(string(need.Capability)) == "document.generate.file" {
			return true
		}
	}
	return false
}

// A live-data image renderer consumes only evidence recorded by this turn's
// host-owned lookup. Unlike PDF generation, it has no safe assistant-text
// fallback: treating conversation prose as fresh renderer input would turn
// unproven history into a visual fact. Keep the lookup whenever this producer
// is present until a typed, provenance-preserving history handoff exists.
func semanticNeedsHaveLiveDataVisual(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if strings.TrimSpace(string(need.Capability)) == "visual.render.live_data" {
			return true
		}
	}
	return false
}

func semanticNeedsForReusableConversationLookup(needs []tool.CapabilityNeed, ctx context.Context, userText string) []tool.CapabilityNeed {
	kept, _ := semanticNeedsForReusableConversationLookupReport(needs, ctx, userText)
	return kept
}

// semanticNeedsForReusableConversationLookupReport additionally reports
// whether the conversation-evidence drop fired. A petition expansion re-plans
// without the turn's user text, so it cannot re-derive this decision; the
// parent surface records it in its replan input instead.
type semanticReusableLookupFactsKey struct{}
type semanticPetitionExpansionKey struct{}

func withSemanticPetitionExpansion(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticPetitionExpansionKey{}, true)
}

func semanticPetitionExpansion(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, _ := ctx.Value(semanticPetitionExpansionKey{}).(bool)
	return flag
}

type semanticPetitionBaselineKey struct{}

func withSemanticPetitionBaseline(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticPetitionBaselineKey{}, enabled)
}

func semanticPetitionBaseline(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, _ := ctx.Value(semanticPetitionBaselineKey{}).(bool)
	return flag
}

func withSemanticReusableLookupFacts(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticReusableLookupFactsKey{}, true)
}

// semanticReuseStoredLookupFacts reports whether this sentence may consume a
// web lookup the session already recorded. A classified lookup or a new deck
// searches again. A later generate, or a short follow-up, may reuse.
func semanticReuseStoredLookupFacts(text string, current intent.ClassificationResult) bool {
	if current.Degraded || lexicalFreshLookupRequest(text) || lexicalWebSearchRequest(text) {
		return false
	}
	if semanticUtteranceIsTaskFollowUp(text) {
		return true
	}
	switch current.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelOffice:
		// A classified lookup or a new deck is a new subject. The old 0.85
		// cliff reused 崇州's page for "重庆天气，生成格式化pdf" at 0.839.
		// Generating from the open facts stays on a document label and still
		// falls through below.
		return false
	}
	return true
}

func semanticReusableLookupFacts(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, _ := ctx.Value(semanticReusableLookupFactsKey{}).(bool)
	return flag
}

func semanticNeedsForReusableConversationLookupReport(needs []tool.CapabilityNeed, ctx context.Context, userText string) ([]tool.CapabilityNeed, bool) {
	if !semanticNeedsHaveWebLookup(needs) || !semanticNeedsHaveGenerate(needs) || semanticNeedsHaveLiveDataVisual(needs) {
		return needs, false
	}
	// Topic-string alignment is not a fact. Only a host residue that recorded
	// a successful web lookup for this task, and only when the user did not
	// ask for a fresh one.
	if lexicalWebSearchRequest(userText) || lexicalFreshLookupRequest(userText) || !semanticReusableLookupFacts(ctx) {
		return needs, false
	}
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if semanticNeedIsWebLookup(need) {
			continue
		}
		kept = append(kept, need)
	}
	if len(kept) == len(needs) || len(kept) == 0 {
		return needs, false
	}
	log.Printf("[semantic] omit this-turn lookup; session residue already has web facts")
	return kept, true
}

// semanticPetitionKeptLookupKey carries the petitioned label into a petition
// expansion re-plan. Host-set only; never derived from model output.
type semanticPetitionKeptLookupKey struct{}

func withSemanticPetitionKeptLookup(ctx context.Context, label intent.IntentLabel) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticPetitionKeptLookupKey{}, label)
}

func semanticPetitionKeptLookup(ctx context.Context) (intent.IntentLabel, bool) {
	if ctx == nil {
		return "", false
	}
	label, ok := ctx.Value(semanticPetitionKeptLookupKey{}).(intent.IntentLabel)
	return label, ok
}

// semanticNeedsForPetitionExpansionLookup mirrors the parent plan's
// conversation-reuse drop inside a petition expansion re-plan. The expansion
// deliberately plans without the turn's user text (a re-plan must not let
// prose steer authority), so the reuse heuristic above cannot fire and every
// lookup leg the parent dropped would resurrect — including legs of
// non-petitioned labels, which the strict-superset validator then rightly
// rejects (2026-08-28 重庆 turn: petitioning web_search on a
// live_data+document_generate composite revived live_data's freshness=current
// leg and killed the whole expansion). When the parent recorded the drop,
// apply it here to every lookup need except the petitioned label's own rule
// templates: the model explicitly asked for that leg, which is the same
// "explicit refresh beats reuse" judgment the heuristic already encodes.
func semanticNeedsForPetitionExpansionLookup(needs []tool.CapabilityNeed, ctx context.Context) []tool.CapabilityNeed {
	label, ok := semanticPetitionKeptLookup(ctx)
	if !ok {
		return needs
	}
	templates := imSemanticIntentRuleSet[label]
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	dropped := false
	for _, need := range needs {
		if !semanticNeedIsWebLookup(need) {
			kept = append(kept, need)
			continue
		}
		var qualifiers map[string]string
		found := false
		for _, template := range templates {
			if need.Capability == template.Capability {
				qualifiers = template.Qualifiers
				found = true
				break
			}
		}
		if !found {
			// A lookup leg from another label (live_data's freshness=current
			// search, when the petition is web_fetch) stays dropped. The
			// parent already omitted it on conversation reuse.
			dropped = true
			continue
		}
		// The composite resolver emits this capability under whichever label
		// it saw first. live_data's freshness=current does not match a search
		// petition's reference template, so the old qualifier check dropped
		// the leg the model just asked for and the expansion added nothing.
		// Align the qualifier to the petitioned template; the foreign leg is
		// not kept as its own authority.
		if !sameSemanticQualifiers(need.Qualifiers, qualifiers) {
			need.Qualifiers = cloneQualifierMap(qualifiers)
			dropped = true
		}
		kept = append(kept, need)
	}
	if !dropped {
		return needs
	}
	return kept
}

func cloneQualifierMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func conversationHasReusableLookupFacts(history []agent.ConversationEntry, userText string) bool {
	// Conversation prose and topic-key overlap are not lookup facts. A later
	// generate turn drops web lookup only when the host residue recorded a
	// successful web lookup for this task.
	_ = history
	_ = userText
	return false
}

func conversationEntryIsLookupResult(entry agent.ConversationEntry) bool {
	name := strings.ToLower(strings.TrimSpace(entry.ToolName))
	if name == "" {
		return false
	}
	switch name {
	case "web_search", semanticTrustedWebSearchAdapter, "web_fetch", semanticTrustedWebFetchAdapter:
		return true
	}
	return false
}

func lexicalFreshLookupRequest(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}
	for _, marker := range lexicalFreshLookupMarkers() {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func lookupTopicKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(hostOwnedPDFReportTitle(text)))
	if text == "" {
		return ""
	}
	for _, noise := range lookupTopicNoise() {
		text = strings.ReplaceAll(text, noise, " ")
	}
	text = strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t,.;:!?，。、；：！？", r) {
			return ' '
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), "")
}

func lookupTopicsAlign(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return left == right || strings.Contains(left, right) || strings.Contains(right, left)
}

func lexicalFreshLookupMarkers() []string {
	return []string{
		"refresh", "latest", "look up again", "search again",
		"\u91cd\u65b0\u67e5", "\u518d\u67e5\u4e00\u904d", "\u518d\u67e5\u4e00\u6b21", "\u518d\u67e5\u4e00\u4e0b", "\u518d\u67e5",
		"\u91cd\u65b0\u641c\u7d22", "\u518d\u641c", "\u5237\u65b0", "\u6700\u65b0", "\u5b9e\u65f6",
	}
}

func lookupTopicNoise() []string {
	return []string{
		"\u67e5\u8be2", "\u8bf7\u5e2e\u6211", "\u5e2e\u6211", "\u8bf7", "\u4e00\u4e0b",
		"\u751f\u6210", "generate", "\u4e00\u4efd", "\u7248\u672c",
		"pdf", "\u62a5\u544a", "report",
		"\u5929\u6c14", "weather",
		"\u80a1\u4ef7", "\u6c47\u7387", "stock price", "exchange rate", "\u822a\u73ed",
	}
}
