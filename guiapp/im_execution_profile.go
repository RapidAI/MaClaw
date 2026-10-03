package guiapp

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type executionLayer string

const (
	executionLayerDirect executionLayer = "direct"
	executionLayerFull   executionLayer = "full"
	executionLayerLight  executionLayer = "light"
)

type ExecutionProfile struct {
	Layer                string
	TaskType             string
	PromptProfile        string
	Confidence           float64
	Reason               string
	RequiredCapabilities []string
	DirectToolName       string
	ToolBudget           int
	// SchemaTokenBudget is the trusted host cap on CatalogRenderer schema
	// tokens for a managed plan. Zero means unlimited. It is independent of
	// ToolBudget and must never be inferred from the selection count.
	SchemaTokenBudget int
	IterationBudget   int
}

func (p ExecutionProfile) IsLight() bool {
	return strings.EqualFold(strings.TrimSpace(p.Layer), string(executionLayerLight))
}

// PromptIsLight is the loop-authorizer view of this profile. An empty prompt
// profile follows the execution layer so a light lookup cannot skip the
// light-safe grant filter.
func (p ExecutionProfile) PromptIsLight() bool {
	pp := strings.TrimSpace(p.PromptProfile)
	if pp == "" {
		return p.IsLight()
	}
	return agent.NormalizePromptProfile(pp).IsLight()
}

func (p ExecutionProfile) IsDirect() bool {
	return strings.EqualFold(strings.TrimSpace(p.Layer), string(executionLayerDirect))
}

func fullExecutionProfile(reason string) ExecutionProfile {
	return ExecutionProfile{
		Layer:           string(executionLayerFull),
		TaskType:        "general",
		PromptProfile:   "full",
		Confidence:      1,
		Reason:          reason,
		ToolBudget:      0,
		IterationBudget: 0,
	}
}

func classifyIMExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) ExecutionProfile {
	return classifyIMExecutionProfileWithSemantic(msg, workflowAgentLoop, isAskUserResponse, nil)
}

func (h *IMMessageHandler) classifyIMExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) ExecutionProfile {
	profile, _ := h.classifyIMExecutionProfileAndSemantic(msg, workflowAgentLoop, isAskUserResponse)
	return profile
}

func (h *IMMessageHandler) classifyIMExecutionProfileAndSemantic(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, *intent.ClassificationResult) {
	return h.classifyIMExecutionProfileAndSemanticContext(context.Background(), msg, workflowAgentLoop, isAskUserResponse, nil)
}

// classifyIMExecutionProfileAndSemanticContext preserves the authoritative
// semantic verdict used by capability materialization, while making its L3
// work part of the enclosing turn's cancellation tree.
func (h *IMMessageHandler) classifyIMExecutionProfileAndSemanticContext(ctx context.Context, msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, recentHistory []string) (ExecutionProfile, *intent.ClassificationResult) {
	structuralProfile, structurallyForced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse)
	if h.getUnifiedClassifier() == nil && structurallyForced {
		return structuralProfile, nil
	}
	// Classification must still happen for a structurally-full turn.  The
	// execution profile controls budgets, while the semantic result controls
	// capability selection; returning early for attachments/background/workflow
	// shapes used to leave a governed request without SemanticIntent and let it
	// re-enter the legacy router.
	var semantic *intent.ClassificationResult
	if uic := h.getUnifiedClassifier(); uic != nil {
		result := uic.ClassifyContext(ctx, classificationMessageFromHistory(msg.UserID, msg.Text, recentHistory))
		semantic = &result
	}
	normalizeSemanticClassificationForTurn(semantic)
	if structurallyForced {
		if isAskUserResponse && askUserResponseKeepsSemanticBudget(msg, workflowAgentLoop, semantic) {
			return askUserSemanticExecutionProfile(semantic, h.executionContractForRegisteredToolName), semantic
		}
		return structuralProfile, semantic
	}
	if semantic != nil && imSemanticIntentIsManaged(*semantic) {
		return classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, h.executionContractForRegisteredToolName), semantic
	}
	if profile, forced := lengthFullExecutionProfile(msg); forced {
		return profile, semantic
	}
	// ACP Mode B short turns: force light without embedding/UIC. Avoids
	// multi-second tool-routing fusion and oversized full prompts for chatty
	// editor messages while real coding asks (paths/URLs/fences/long text)
	// still hit full via structural/length gates above.
	if acpPreferLightProfile(msg) {
		// Keep the light profile, not the UIC result: a short ACP turn must
		// not become a closed managed grant. Loop start leftover then treats
		// the unset SemanticIntent as a chat leftover, not CoreToolNames+UIC.
		return ExecutionProfile{
			Layer:                string(executionLayerLight),
			TaskType:             "general",
			PromptProfile:        "light",
			Confidence:           1,
			Reason:               "acp-mode-b short programming turn",
			RequiredCapabilities: []string{"current_data", "time", "web", "fetch", "status", "files"},
			ToolBudget:           8,
			IterationBudget:      3,
		}, nil
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, h.executionContractForRegisteredToolName)
	return profile, semantic
}

func classifyIMExecutionProfileWithSemantic(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, semantic *intent.ClassificationResult) ExecutionProfile {
	return classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, nil)
}

func classifyIMExecutionProfileWithSemanticAndContracts(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, semantic *intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) ExecutionProfile {
	if profile, forced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse); forced {
		if isAskUserResponse && askUserResponseKeepsSemanticBudget(msg, workflowAgentLoop, semantic) {
			return askUserSemanticExecutionProfile(semantic, contractForTool)
		}
		return profile
	}
	normalizeSemanticClassificationForTurn(semantic)
	// A supplied semantic result is authoritative for this decision. Do not
	// reintroduce a wording-based direct route before checking whether that
	// label belongs to a capability-managed family.
	if semantic != nil && imSemanticIntentIsManaged(*semantic) {
		return executionProfileFromSemanticIntent(semantic, contractForTool)
	}
	if profile, forced := lengthFullExecutionProfile(msg); forced {
		return profile
	}
	return executionProfileFromSemanticIntent(semantic, contractForTool)
}

func structuralFullExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, bool) {
	if profile, forced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse); forced {
		return profile, true
	}
	return lengthFullExecutionProfile(msg)
}

// askUserResponseKeepsSemanticBudget is the continuation of a pending
// question whose own utterance already names a governed capability.
// The pending-reply binding is context, not an execution budget: a
// confident "画出近一月股价趋势图" stays on the visual pipeline instead of
// becoming an unbounded general agent. A weak or non-capability answer
// still uses the full continuation profile.
func askUserResponseKeepsSemanticBudget(msg IMUserMessage, workflowAgentLoop bool, semantic *intent.ClassificationResult) bool {
	if semantic == nil || semantic.Degraded || strings.TrimSpace(semantic.WorkflowType) != "" {
		return false
	}
	if semantic.Confidence < 0.85 || !imSemanticIntentIsManaged(*semantic) {
		return false
	}
	if strings.TrimSpace(msg.Text) == "" || workflowAgentLoop || msg.IsBackground || len(msg.Attachments) > 0 {
		return false
	}
	if expertDefForUserID(msg.UserID) != nil || hasStructuralFullExecutionSignal(msg.Text) {
		return false
	}
	return true
}

func askUserSemanticExecutionProfile(semantic *intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) ExecutionProfile {
	profile := executionProfileFromSemanticIntent(semantic, contractForTool)
	if profile.Reason != "" {
		profile.Reason += "; "
	}
	profile.Reason += "ask_user continuation"
	return profile
}

func hardStructuralFullExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, bool) {
	text := strings.TrimSpace(msg.Text)
	switch {
	case expertDefForUserID(msg.UserID) != nil:
		// Expert sessions always run the full profile: the light prompt carries
		// a hard "do not inspect files / do not manage tasks" fence and a
		// generic persona that would contradict the expert's system prompt.
		return fullExecutionProfile("expert session"), true
	case text == "":
		return fullExecutionProfile("empty message"), true
	case workflowAgentLoop:
		return fullExecutionProfile("workflow agent loop"), true
	case isAskUserResponse:
		return fullExecutionProfile("ask_user continuation"), true
	case msg.IsBackground:
		return fullExecutionProfile("background task"), true
	case len(msg.Attachments) > 0:
		return fullExecutionProfile("attachments present"), true
	case hasStructuralFullExecutionSignal(text):
		return fullExecutionProfile("structural execution signal"), true
	default:
		return ExecutionProfile{}, false
	}
}

func lengthFullExecutionProfile(msg IMUserMessage) (ExecutionProfile, bool) {
	if utf8.RuneCountInString(strings.TrimSpace(msg.Text)) > 40 {
		return fullExecutionProfile("message too long for light profile"), true
	}
	return ExecutionProfile{}, false
}

const shortContinuationReason = "short continuation keeps parent execution tools"

// continuationKeepsParentExecution stops a short reply from being reclassified
// as a light lookup. What stays available is the previous full turn's tool
// list, not a guess from the wording of this message. A confident live-data
// or clock lookup still starts clean. A project-task follow-up that is only
// a search or a page fetch keeps the parent surface: that is how "does this
// repo have SSO?" retains web_fetch after the GUI process restarts.
func (h *IMMessageHandler) continuationKeepsParentExecution(profile ExecutionProfile, userID, text string, semantic *intent.ClassificationResult) ExecutionProfile {
	if h == nil {
		return profile
	}
	// A page fetch is a managed full profile, not the light search profile.
	// Narrow it so the prompt stays short and the loop is not auto-extended.
	lightish := profile.IsLight() || isLightPromptProfile(profile.PromptProfile)
	if !lightish && !lookupContinuationMayNarrow(profile) {
		return profile
	}
	// "用这个" has to be short. A project-task search or page fetch is a
	// lookup even when the question is longer than that: the 40-rune gate
	// was dropping web_fetch and clearing bash on the next sentence.
	parentFull := h.parentExecutionIsFull(userID)
	lookup := parentFull && projectTaskLookupContinuation(userID, semantic)
	if !parentFull || (!shortContinuationText(text) && !lookup) {
		return profile
	}
	if !lookup && !lightish {
		return profile
	}
	if confidentNewReadOnlyTask(semantic) && !lookup {
		return profile
	}
	if lookup {
		// Stay on the light lookup. A full layer would load the coding prompt
		// and, near the iteration cap, auto-extend by another 30 rounds.
		// ToolBudget 0 is what keeps the optional web_fetch companion; the
		// light search budget of 1 publishes only web_search.
		kept := profile
		kept.Layer = string(executionLayerLight)
		kept.PromptProfile = "light"
		kept.Reason = lookupContinuationReason
		kept.ToolBudget = 0
		kept.IterationBudget = lookupContinuationIterationBudget
		return kept
	}
	return fullExecutionProfile(shortContinuationReason)
}

// lookupContinuationReason is a project-task search or page fetch that keeps
// the parent carry without becoming a coding turn.
const lookupContinuationReason = "project task lookup keeps parent execution tools"

// lookupContinuationIterationBudget caps that lookup. ToolBudget stays
// unlimited so the optional web_fetch companion is not dropped.
const lookupContinuationIterationBudget = 8

// projectTaskSession is a desktop project tab (desktop-user:<path>).
// Expert and ACP owners share the prefix but are not project paths.
func projectTaskSession(userID string) bool {
	return projectPathFromSessionOwnerID(userID) != ""
}

// projectTaskLookupContinuation is a short search or page-fetch inside a
// project task that already has a full tool surface. Live data and clock
// stay out: those are new lookups and must not inherit bash.
func projectTaskLookupContinuation(userID string, semantic *intent.ClassificationResult) bool {
	if semantic == nil || !projectTaskSession(userID) {
		return false
	}
	switch semantic.Primary {
	case intent.LabelSearch, intent.LabelWebFetch:
		return true
	default:
		return false
	}
}

func shortContinuationText(text string) bool {
	text = strings.TrimSpace(text)
	n := utf8.RuneCountInString(text)
	return n > 0 && n <= 40
}

func confidentNewReadOnlyTask(semantic *intent.ClassificationResult) bool {
	if semantic == nil || semantic.Degraded || semantic.Confidence < 0.85 || !imSemanticIntentIsManaged(*semantic) {
		return false
	}
	if semanticClassificationHasMutatingFamily(*semantic) {
		return false
	}
	switch semantic.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelCurrentTime:
		return true
	default:
		return false
	}
}

func operationalExecutionProfile(profile ExecutionProfile) bool {
	switch strings.TrimSpace(profile.Reason) {
	case shortContinuationReason, lookupContinuationReason:
		return true
	default:
		return false
	}
}

func lookupContinuationProfile(profile ExecutionProfile) bool {
	return strings.TrimSpace(profile.Reason) == lookupContinuationReason
}

// lookupContinuationCarriedGrant is a non-light tool the parent turn already
// rendered. A lookup may run it without adopting the full coding prompt.
func lookupContinuationCarriedGrant(profile ExecutionProfile, carried []string, name string) bool {
	if !lookupContinuationProfile(profile) {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, item := range carried {
		if strings.TrimSpace(item) == name {
			return true
		}
	}
	return false
}

// lookupContinuationToolPrompt tells a project-task lookup to open the page
// it just found, and names only tools this carry actually restored. Naming
// bash when it is not listed makes the model call it, get a denial, and then
// skip web_fetch.
func lookupContinuationToolPrompt(carried []string, primary intent.IntentLabel) string {
	var names []string
	for _, name := range carried {
		name = strings.TrimSpace(name)
		if name == "" || agent.IsLightTurnToolAllowed(name) {
			continue
		}
		names = append(names, name)
	}
	// The shared light prompt says to answer after one lookup and not to run
	// shell. Those lines are what made a project-task follow-up stop at the
	// search snippet and treat a listed bash as unavailable.
	var b strings.Builder
	b.WriteString("\nThis reply continues an open project task. Ignore the instructions above that say to answer after one lookup, not to run shell, or to claim that files and shell are absent from this turn.\n")
	if primary == intent.LabelWebFetch {
		b.WriteString("Call web_fetch on the requested page. Do not stop after one snippet, and do not fetch a search-engine results page.\n")
	} else {
		b.WriteString("After web_search, call web_fetch on a result page that can answer. Do not stop at the search snippet, and do not fetch the search-engine results page.\n")
	}
	if len(names) > 0 {
		b.WriteString("Tools already listed from the open task (" + strings.Join(names, ", ") + ") are authorized for this reply.\n")
	}
	return b.String()
}

// lookupContinuationRepeatAllowed is the next invocation of a tool this
// lookup already published. A light turn normally refuses that sibling, which
// stops the second web_search or bash after the first call succeeds.
func lookupContinuationRepeatAllowed(profile ExecutionProfile, carried []string, name string) bool {
	if !lookupContinuationProfile(profile) {
		return false
	}
	if lookupContinuationCarriedGrant(profile, carried, name) {
		return true
	}
	// Only the lookup pair. The static light allowlist also contains
	// download_file; opening that sibling and then rejecting it spends a
	// grant the rest of the turn still needs.
	switch strings.TrimSpace(name) {
	case "web_search", "web_fetch":
		return true
	default:
		return false
	}
}

// lookupContinuationMayNarrow is a full profile that a project-task search
// or page fetch may still pull back onto the light lookup. A path or URL
// forces "structural execution signal" before the managed search budget, so
// leaving it full published only web_search and invited bash the closed
// plan does not grant. Expert, attachment, and workflow reasons stay full.
func lookupContinuationMayNarrow(profile ExecutionProfile) bool {
	switch strings.TrimSpace(profile.Reason) {
	case "semantic capability-managed intent", "structural execution signal":
		return true
	default:
		return false
	}
}

func executionSurfaceIsFull(profile ExecutionProfile) bool {
	return !profile.IsLight() && !isLightPromptProfile(profile.PromptProfile)
}

// recordSemanticExecutionSurface updates the legacy carry for a turn that
// never enters prepareAgentLoopTools. An empty full surface is left alone so
// a planner failure does not erase a still-valid previous list.
func (h *IMMessageHandler) recordSemanticExecutionSurface(userID string, profile ExecutionProfile, tools []map[string]interface{}) {
	h.recordSemanticExecutionSurfacePlan(userID, profile, tools, nil)
}

// recordSemanticExecutionSurfacePlan is recordSemanticExecutionSurface with
// the plan this turn will settle. Baseline companions on that plan are not
// the route, and a different obligation replaces the previous one.
func (h *IMMessageHandler) recordSemanticExecutionSurfacePlan(userID string, profile ExecutionProfile, tools []map[string]interface{}, candidate []tool.CapabilityNeed) {
	if executionSurfaceIsFull(profile) && len(tools) > 0 {
		// A short continuation that the planner rendered as a lookup still
		// belongs to the parent task. Dropping the carry here is how the
		// next reply loses ssh after "it's in the knowledge base".
		if operationalExecutionProfile(profile) && len(nonLightToolNames(tools)) == 0 {
			return
		}
		if operationalExecutionProfile(profile) {
			h.noteParentExecutionUnion(userID, tools)
			return
		}
		h.noteParentExecutionForPlan(userID, true, tools, candidate)
		return
	}
	if !executionSurfaceIsFull(profile) {
		// A project-task lookup stays light so the prompt and the iteration
		// cap stay small. That must not wipe bash the parent task still
		// needs on the next non-lookup reply.
		if operationalExecutionProfile(profile) {
			if len(nonLightToolNames(tools)) > 0 {
				h.noteParentExecutionUnion(userID, tools)
			}
			return
		}
		h.noteParentExecution(userID, false, nil)
	}
}

func nonLightToolNames(tools []map[string]interface{}) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, def := range tools {
		name := extractToolName(def)
		if !parentExecutionSurfaceName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// parentExecutionSurfaceName is a tool the next continuation still has to
// run. Light lookups are not that surface. Ambient retrieval is not either:
// its managed spelling (memory_recall) is absent from the light allowlist, so
// a companion render used to replace ssh, and the next process restored the
// companion instead of the open obligation.
func parentExecutionSurfaceName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || agent.IsLightTurnToolAllowed(name) || ambientRetrievalSurfaceName(name) {
		return false
	}
	return true
}

func ambientRetrievalSurfaceName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, adapter := range []string{semanticTrustedMemoryRecallAdapter, semanticTrustedKnowledgeReadAdapter} {
		if tool.SemanticModelFunctionName(adapter) == name {
			return true
		}
	}
	return false
}

func parentExecutionSurfaceNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !parentExecutionSurfaceName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sameExecutionNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

func (h *IMMessageHandler) parentExecutionIsFull(userID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	h.hydrateParentExecution(userID)
	_, ok := h.parentExecution.Load(userID)
	return ok
}

func (h *IMMessageHandler) parentExecutionTools(userID string) []string {
	if h == nil {
		return nil
	}
	userID = strings.TrimSpace(userID)
	h.hydrateParentExecution(userID)
	v, ok := h.parentExecution.Load(userID)
	if !ok {
		return nil
	}
	names, _ := v.([]string)
	return append([]string(nil), names...)
}

func (h *IMMessageHandler) markParentExecutionResolved(userID string) {
	if h == nil || userID == "" {
		return
	}
	h.parentExecutionResolved.Store(userID, struct{}{})
}

// lockParentExecution returns the unlock function for this session.
// Callers must not re-enter it on the same goroutine.
func (h *IMMessageHandler) lockParentExecution(userID string) func() {
	if h == nil || userID == "" {
		return func() {}
	}
	v, _ := h.parentExecutionGate.LoadOrStore(userID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// parentHydrateFlight is one in-progress history read for a session.
type parentHydrateFlight struct {
	done chan struct{}
}

// hydrateParentExecution rebuilds the carry after the process-local map is
// gone. A recorded execution surface wins when it is the open obligation,
// or when nothing is open. A missing surface is the same situation whether
// the parent list was never recorded, recorded as ambient retrieval only,
// cleared while the residue stayed open, or filled with companions that do
// not petition that obligation: history supplies the tools the obligation
// already ran. Baseline bash and write_file are not that route, and a later
// local shell is not a remote one. With no open obligation, history is only
// the fallback for a session that never recorded a decision, because
// compression can drop tool names. An answer-only tail does not count as a
// light turn. The newest turn that actually called tools does: only-light
// or ambient tools stay cleared.
func (h *IMMessageHandler) hydrateParentExecution(userID string) {
	if h == nil || h.memory == nil || userID == "" {
		return
	}
	if _, done := h.parentExecutionResolved.Load(userID); done {
		return
	}
	flight := &parentHydrateFlight{done: make(chan struct{})}
	actual, loaded := h.parentHydrateFlights.LoadOrStore(userID, flight)
	if loaded {
		existing, _ := actual.(*parentHydrateFlight)
		if existing != nil {
			<-existing.done
		}
		return
	}
	defer func() {
		close(flight.done)
		h.parentHydrateFlights.Delete(userID)
	}()
	if _, done := h.parentExecutionResolved.Load(userID); done {
		return
	}
	// History can be the whole task. Do not hold this session's lock while
	// reading it, and do not start a second copy for a concurrent caller.
	// A clear that lands during the read marks the session resolved, and
	// the commit below leaves that decision alone.
	raw, known := h.memory.ParentExecutionTools(userID)
	surface := parentExecutionSurfaceNames(raw)
	companionOnly := known && len(raw) > 0 && len(surface) == 0
	// A light side question persists an empty list while the residue is
	// still open. That empty list is not a decision to forget the obligation.
	// Never recording the parent list is the same missing surface: an old
	// conversation, or a turn that rendered nothing, left known false.
	// Companions recorded in place of the obligation are that miss too.
	explicitClear := known && len(raw) == 0
	obligation := h.openExecutionObligation(userID)
	matchedSurface := parentExecutionNamesForObligation(surface, obligation)
	disjoint := known && len(surface) > 0 && len(obligation) > 0 && len(matchedSurface) == 0
	missing := !known || companionOnly || explicitClear || disjoint
	var fromHistory []string
	if !known && len(obligation) == 0 {
		fromHistory = parentExecutionNamesFromHistory(h.memory.Load(userID), nil)
	} else if len(obligation) > 0 && missing {
		fromHistory = parentExecutionNamesFromHistory(h.memory.Load(userID), obligation)
	}
	unlock := h.lockParentExecution(userID)
	defer unlock()
	if _, done := h.parentExecutionResolved.Load(userID); done {
		return
	}
	if _, ok := h.parentExecution.Load(userID); ok {
		h.markParentExecutionResolved(userID)
		return
	}
	h.markParentExecutionResolved(userID)
	if known && len(obligation) == 0 {
		if len(surface) > 0 {
			h.parentExecution.Store(userID, surface)
			if !sameExecutionNames(raw, surface) {
				h.persistParentExecution(userID, surface)
			}
		}
		return
	}
	if len(matchedSurface) > 0 {
		h.parentExecution.Store(userID, matchedSurface)
		if !sameExecutionNames(raw, matchedSurface) {
			h.persistParentExecution(userID, matchedSurface)
		}
		return
	}
	if len(fromHistory) == 0 {
		// The recorded names are not this obligation, and history has no
		// matching tool. Drop them so closing the task does not inherit bash.
		if known && len(obligation) > 0 {
			h.persistParentExecution(userID, nil)
		}
		return
	}
	h.parentExecution.Store(userID, fromHistory)
	h.persistParentExecution(userID, fromHistory)
}

// openExecutionObligation is the side-effecting capabilities the residue
// still has to finish. Ambient retrieval recorded as the parent list must
// not hide them, and a later local shell must not stand in for a remote one.
func (h *IMMessageHandler) openExecutionObligation(userID string) map[tool.CapabilityID]bool {
	if h == nil || h.memory == nil {
		return nil
	}
	persisted, ok := h.memory.SemanticSessionResidue(userID)
	if !ok {
		return nil
	}
	residue := semanticResidueFromPersisted(persisted)
	if residue.Status != semanticResidueOpen {
		return nil
	}
	needs := semanticResidueObligationNeeds(residue.Needs)
	if len(needs) == 0 {
		return nil
	}
	out := make(map[tool.CapabilityID]bool, len(needs))
	for _, need := range needs {
		if need.Capability != "" {
			out[need.Capability] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parentExecutionNamesFromHistory(entries []agent.ConversationEntry, obligation map[tool.CapabilityID]bool) []string {
	if len(entries) == 0 {
		return nil
	}
	// Walk newest turn first and stop at the first turn that called anything.
	// A light-only or ambient-only turn clears the carry; do not revive an
	// older bash behind it. An open obligation keeps walking: companion turns
	// and a different execution tool are not that route.
	end := len(entries)
	for end > 0 {
		start := end - 1
		for start > 0 && strings.TrimSpace(entries[start].Role) != "user" {
			start--
		}
		var names []string
		for _, entry := range entries[start:end] {
			names = append(names, toolNamesFromHistoryEntry(entry)...)
		}
		if len(names) > 0 {
			if len(obligation) == 0 {
				return nonLightHistoryNames(names)
			}
			if matched := parentExecutionNamesForObligation(names, obligation); len(matched) > 0 {
				return matched
			}
		}
		if start == 0 {
			break
		}
		end = start
	}
	return nil
}

func parentExecutionNamesForObligation(names []string, obligation map[tool.CapabilityID]bool) []string {
	seen := make(map[string]struct{}, len(names))
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !parentExecutionSurfaceName(name) {
			continue
		}
		capability, ok := semanticPetitionableCapabilities[name]
		if !ok || !obligation[capability] {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func nonLightHistoryNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	var out []string
	for _, name := range names {
		if !parentExecutionSurfaceName(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func toolNamesFromHistoryEntry(entry agent.ConversationEntry) []string {
	var names []string
	if name := strings.TrimSpace(entry.ToolName); name != "" {
		names = append(names, name)
	}
	names = append(names, toolNamesFromCalls(entry.ToolCalls)...)
	return names
}

func toolNamesFromCalls(calls interface{}) []string {
	switch arr := calls.(type) {
	case nil:
		return nil
	case []llm.ToolCall:
		names := make([]string, 0, len(arr))
		for _, call := range arr {
			if name := strings.TrimSpace(call.Function.Name); name != "" {
				names = append(names, name)
			}
		}
		return names
	case []interface{}:
		names := make([]string, 0, len(arr))
		for _, item := range arr {
			if name := toolNameFromHistoryCall(item); name != "" {
				names = append(names, name)
			}
		}
		return names
	case []map[string]interface{}:
		names := make([]string, 0, len(arr))
		for _, item := range arr {
			if name := toolNameFromCallMap(item); name != "" {
				names = append(names, name)
			}
		}
		return names
	case []map[string]string:
		names := make([]string, 0, len(arr))
		for _, item := range arr {
			if name := strings.TrimSpace(item["name"]); name != "" {
				names = append(names, name)
			}
		}
		return names
	default:
		// Do not json.Marshal. Tool arguments can be the whole file or command,
		// and this walk only needs the name.
		return nil
	}
}

func toolNameFromHistoryCall(item interface{}) string {
	switch v := item.(type) {
	case map[string]interface{}:
		return toolNameFromCallMap(v)
	case llm.ToolCall:
		return strings.TrimSpace(v.Function.Name)
	default:
		return ""
	}
}

func semanticResidueCandidateNeeds(ctx *LoopContext) []tool.CapabilityNeed {
	if ctx == nil {
		return nil
	}
	return ctx.semanticResidueCandidateNeeds
}

func obligationCapabilitySet(needs []tool.CapabilityNeed) map[tool.CapabilityID]bool {
	if len(needs) == 0 {
		return nil
	}
	out := make(map[tool.CapabilityID]bool, len(needs))
	for _, need := range needs {
		if need.Capability != "" {
			out[need.Capability] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func obligationOverlaps(left, right map[tool.CapabilityID]bool) bool {
	for capability := range left {
		if right[capability] {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) noteParentExecution(userID string, full bool, tools []map[string]interface{}) {
	h.noteParentExecutionForPlan(userID, full, tools, nil)
}

// noteParentExecutionForPlan records the rendered execution surface.
// candidate is the plan this turn will settle. When that plan, or the
// residue already open, has an obligation, only tools that petition it are
// the route. Baseline bash and write_file are not, and they must not replace
// ssh. A plan whose obligation is a different task replaces the old route.
func (h *IMMessageHandler) noteParentExecutionForPlan(userID string, full bool, tools []map[string]interface{}, candidate []tool.CapabilityNeed) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Read the residue before taking this session's lock. A light side
	// question used to persist an empty carry, and the next process treated
	// that empty list as the decision to forget ssh.
	previous := h.openExecutionObligation(userID)
	if !full && len(previous) > 0 {
		return
	}
	obligation := previous
	sameTask := len(previous) > 0
	if semanticResidueTaskMutates(candidate) {
		obligation = obligationCapabilitySet(semanticResidueObligationNeeds(candidate))
		sameTask = len(previous) == 0 || obligationOverlaps(obligation, previous)
	}
	unlock := h.lockParentExecution(userID)
	defer unlock()
	if !full {
		h.markParentExecutionResolved(userID)
		h.parentExecution.Delete(userID)
		h.persistParentExecution(userID, nil)
		return
	}
	names := nonLightToolNames(tools)
	if len(names) == 0 {
		// web_search / web_fetch / knowledge_search / memory_recall must not
		// become the carry, and must not erase bash or ssh the parent task
		// still needs. An empty render is a plan failure and likewise leaves
		// the stored list alone. Only an actual light turn (full == false)
		// ends it. Do not mark the session resolved: a restart hydrate still
		// has to run if this process never stored a carry.
		return
	}
	if len(obligation) > 0 {
		matched := parentExecutionNamesForObligation(names, obligation)
		if len(matched) == 0 {
			if sameTask {
				return
			}
			// The settled plan is a different task and did not render its
			// tool. Keeping ssh would put the old route on the new one.
			h.markParentExecutionResolved(userID)
			h.parentExecution.Delete(userID)
			h.persistParentExecution(userID, nil)
			return
		}
		names = matched
	}
	h.markParentExecutionResolved(userID)
	h.parentExecution.Store(userID, names)
	h.persistParentExecution(userID, names)
}

// noteParentExecutionUnion keeps tools the continuation did not render.
// Replacing the list with this turn's bash would drop an ssh the parent
// task still needs on the next reply.
func (h *IMMessageHandler) noteParentExecutionUnion(userID string, tools []map[string]interface{}) {
	added := nonLightToolNames(tools)
	if len(added) == 0 {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if obligation := h.openExecutionObligation(userID); len(obligation) > 0 {
		// A continuation may render baseline bash beside the open route.
		// Unioning that companion is how the next lookup grows a local shell.
		added = parentExecutionNamesForObligation(added, obligation)
		if len(added) == 0 {
			return
		}
	}
	// Hydrate before taking the lock: hydrate locks this session too.
	h.hydrateParentExecution(userID)
	unlock := h.lockParentExecution(userID)
	defer unlock()
	var existing []string
	if v, ok := h.parentExecution.Load(userID); ok {
		existing, _ = v.([]string)
	}
	merged := mergeParentExecutionNames(existing, added)
	if len(merged) == 0 {
		return
	}
	h.markParentExecutionResolved(userID)
	h.parentExecution.Store(userID, merged)
	h.persistParentExecution(userID, merged)
}

func mergeParentExecutionNames(existing, added []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(added))
	out := make([]string, 0, len(existing)+len(added))
	for _, name := range append(append([]string{}, existing...), added...) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// clearParentExecutionCarry drops the process-local list and persists the
// clear. Hydrate treats a resolved session as already decided, so the
// in-process map has to be marked before the durable list is emptied.
func (h *IMMessageHandler) clearParentExecutionCarry(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	unlock := h.lockParentExecution(userID)
	h.markParentExecutionResolved(userID)
	h.parentExecution.Delete(userID)
	unlock()
	h.persistParentExecution(userID, nil)
}

func (h *IMMessageHandler) persistParentExecution(userID string, names []string) {
	if h == nil || h.memory == nil || strings.TrimSpace(userID) == "" {
		return
	}
	if len(names) == 0 {
		h.memory.ClearParentExecutionTools(userID)
		return
	}
	h.memory.SetParentExecutionTools(userID, names)
}

func executionProfileFromSemanticIntent(result *intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) ExecutionProfile {
	if result == nil {
		return fullExecutionProfile("semantic classifier unavailable")
	}
	// Sub-floor search/live_data is a chat turn (gate 7). Do not promote it to
	// a full degraded profile or a managed lookup budget; that is how a typo
	// like 「北京天所」 paid for web tools after UIC kept the hint.
	if semanticNeedsChatProjection(*result) {
		reason := "semantic lookup hint below floor"
		if semanticReadOnlyUnderstandFamily(*result) {
			reason = "semantic understand hint below floor"
		}
		return ExecutionProfile{
			Layer:           string(executionLayerLight),
			TaskType:        "general",
			PromptProfile:   "light",
			Confidence:      result.Confidence,
			Reason:          reason,
			ToolBudget:      8,
			IterationBudget: 3,
		}
	}
	readOnlyHint := semanticReadOnlyGovernedHint(*result)
	if result.Degraded && !readOnlyHint && !semanticOfficeGovernedHint(*result) {
		return fullExecutionProfile("semantic classifier degraded")
	}
	if result.WorkflowType != "" {
		return fullExecutionProfile("semantic workflow intent")
	}
	if !semanticClassificationMeetsResolverFloor(*result) && !semanticClassificationPlansBelowResolverFloor(*result) {
		return fullExecutionProfile("semantic confidence below light threshold")
	}
	// Capability-managed families are materialized only through the semantic
	// catalog/planner. In particular, an old UIC ToolNames projection must not
	// turn a governed outcome into a direct name-based execution path.
	if managed, unmapped := imSemanticIntentCoverage(*result); managed {
		if unmapped != "" {
			// The semantic router will fail closed with an explicit coverage
			// error. Keep a full profile here so an incomplete migration never
			// narrows a multi-capability request to the light lookup loop.
			return fullExecutionProfile("semantic capability migration coverage incomplete")
		}
		// The capability surface remains grant-bound even on a light turn. The
		// profile only budgets the agent loop; semanticPlanForTurn replaces its
		// tool list before the model sees it, so this does not reopen the legacy
		// name-router or direct execution path.
		// A weather card searches, renders, and hands the image back. The
		// deliver step is that card, not an open-ended mutation. Treating it
		// as one made 「崇州天气」 an unbounded full agent (tool_budget=0,
		// iteration cap 300) that wandered into tools_search and screenshot.
		if semanticLiveVisualFamily(*result) {
			return ExecutionProfile{
				Layer:                string(executionLayerLight),
				TaskType:             string(intent.LabelLiveDataVisual),
				PromptProfile:        "light",
				Confidence:           result.Confidence,
				Reason:               "semantic capability-managed live visual",
				RequiredCapabilities: []string{"information.search.web", "visual.render.live_data"},
				// ToolBudget is also the planner selection cap. 1 keeps only
				// the search wave and marks render and deliver budget_exceeded,
				// which rejects the whole turn. 0 leaves the closed pipeline
				// intact; IterationBudget caps the model loop.
				ToolBudget:      0,
				IterationBudget: 4,
			}
		}
		if semanticIntentRequiresFullProfile(*result) {
			return fullExecutionProfile("semantic capability-managed mutating intent")
		}
		if result.Primary == intent.LabelSearch || result.Primary == intent.LabelLiveData {
			return ExecutionProfile{
				Layer:                string(executionLayerLight),
				TaskType:             string(result.Primary),
				PromptProfile:        "light",
				Confidence:           result.Confidence,
				Reason:               "semantic capability-managed lookup",
				RequiredCapabilities: []string{"information.search.web"},
				ToolBudget:           1,
				IterationBudget:      3,
			}
		}
		return fullExecutionProfile("semantic capability-managed intent")
	}
	if toolName, contract := directToolFromSemanticResult(*result, contractForTool); toolName != "" {
		return ExecutionProfile{
			Layer:                string(executionLayerDirect),
			TaskType:             "direct_tool",
			PromptProfile:        "none",
			Confidence:           result.Confidence,
			Reason:               "semantic direct tool intent",
			RequiredCapabilities: contract.Capabilities,
			DirectToolName:       toolName,
			ToolBudget:           1,
			IterationBudget:      0,
		}
	}
	switch result.Primary {
	case intent.LabelLiveData:
		return ExecutionProfile{
			Layer:                string(executionLayerLight),
			TaskType:             string(result.Primary),
			PromptProfile:        "light",
			Confidence:           result.Confidence,
			Reason:               "semantic low-complexity intent",
			RequiredCapabilities: []string{"current_data", "time", "web", "fetch"},
			ToolBudget:           8,
			IterationBudget:      3,
		}
	default:
		return fullExecutionProfile("semantic intent requires full agent")
	}
}

func semanticLiveVisualFamily(result intent.ClassificationResult) bool {
	sawVisual := false
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch:
		case intent.LabelLiveDataVisual:
			sawVisual = true
		default:
			return false
		}
	}
	return sawVisual
}

func semanticIntentRequiresFullProfile(result intent.ClassificationResult) bool {
	for _, label := range result.Labels() {
		if semanticLabelRequiresFullProfile(label) {
			return true
		}
	}
	return false
}

func semanticLabelRequiresFullProfile(label intent.IntentLabel) bool {
	for _, tmpl := range imSemanticIntentRuleSet[label] {
		if semanticCapabilityRequiresFullProfile(string(tmpl.Capability)) {
			return true
		}
	}
	return false
}

func semanticCapabilityRequiresFullProfile(capability string) bool {
	switch strings.TrimSpace(capability) {
	case "information.search.web", "information.current_time", "document.read.local", "visual.render.live_data",
		"visual.capture.desktop",
		string(tool.CapabilityFSReadLocal), string(tool.CapabilityRepoInspectVCS),
		string(tool.CapabilityInformationFetchWeb), string(tool.CapabilityAudioTranscribeSpeech),
		string(tool.CapabilitySecurityAuditRead), string(tool.CapabilityKnowledgeReadLocal):
		return false
	default:
		return capability != ""
	}
}

var executionProfileLocalPathPattern = regexp.MustCompile(`(?i)([a-z]:[\\/]|\.{1,2}[\\/]|/[\w.-])`)

func hasStructuralFullExecutionSignal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return executionProfileLocalPathPattern.MatchString(lower) ||
		strings.Contains(lower, "```") ||
		strings.Contains(lower, "http://") ||
		strings.Contains(lower, "https://")
}

func directToolFromSemanticResult(result intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) (string, ToolExecutionContract) {
	if result.Confidence < 0.95 || len(result.ToolNames) != 1 {
		return "", ToolExecutionContract{}
	}
	toolName := strings.TrimSpace(result.ToolNames[0])
	contract := executionContractForToolName(toolName, contractForTool)
	if contract.Explicit && contract.SupportsDirect && contract.Deterministic {
		return toolName, contract
	}
	return "", ToolExecutionContract{}
}

type ToolExecutionContract struct {
	Name                  string
	Capabilities          []string
	Deterministic         bool
	SupportsDirect        bool
	RequiresAgentPlanning bool
	AvgLatencyMS          int
	Explicit              bool
}

func filterToolsForExecutionProfile(tools []map[string]interface{}, profile ExecutionProfile) []map[string]interface{} {
	// Light execution layer OR adaptive light prompt profile both need a
	// reduced tool surface so tools match the light system prompt.
	if (!profile.IsLight() && !isLightPromptProfile(profile.PromptProfile)) || len(tools) == 0 {
		return tools
	}
	filterProfile := profile
	if !filterProfile.IsLight() && isLightPromptProfile(filterProfile.PromptProfile) {
		// Soft light: full layer but light prompt — still restrict tools.
		filterProfile = softLightExecutionProfile(filterProfile)
	}
	budget := filterProfile.ToolBudget
	if budget <= 0 {
		budget = 8
	}
	filtered := make([]map[string]interface{}, 0, budget)
	seen := make(map[string]bool, budget)
	for _, def := range tools {
		contract := executionContractForTool(def)
		if !contract.Explicit {
			continue
		}
		if !contractAllowedForLight(contract) || !contractMatchesExecutionProfile(contract, filterProfile) || seen[contract.Name] {
			continue
		}
		filtered = append(filtered, def)
		seen[contract.Name] = true
		if len(filtered) >= budget {
			break
		}
	}
	if len(filtered) == 0 {
		return tools
	}
	return filtered
}

func isLightPromptProfile(s string) bool {
	return agent.NormalizePromptProfile(s).IsLight()
}

// softLightExecutionProfile applies default light tool budgets/capabilities when
// only PromptProfile=light is set (adaptive prompt on a full execution layer).
func softLightExecutionProfile(p ExecutionProfile) ExecutionProfile {
	out := p
	out.Layer = string(executionLayerLight)
	out.PromptProfile = "light"
	if out.ToolBudget <= 0 {
		out.ToolBudget = 8
	}
	if len(out.RequiredCapabilities) == 0 {
		out.RequiredCapabilities = []string{"current_data", "time", "web", "fetch", "status"}
	}
	if strings.TrimSpace(out.Reason) == "" {
		out.Reason = "adaptive light prompt profile"
	}
	return out
}

func executionContractForTool(def map[string]interface{}) ToolExecutionContract {
	name := extractToolName(def)
	if raw, ok := def["x_execution_contract"].(map[string]interface{}); ok {
		return executionContractFromMetadata(name, raw)
	}
	return inferredExecutionContract(name)
}

func executionContractForToolName(name string, contractForTool func(string) ToolExecutionContract) ToolExecutionContract {
	name = strings.TrimSpace(name)
	if contractForTool != nil {
		contract := contractForTool(name)
		if strings.TrimSpace(contract.Name) == "" {
			contract.Name = name
		}
		return contract
	}
	return inferredExecutionContract(name)
}

func (h *IMMessageHandler) executionContractForRegisteredToolName(name string) ToolExecutionContract {
	name = strings.TrimSpace(name)
	if h == nil || h.registry == nil || name == "" {
		return inferredExecutionContract(name)
	}
	if tool, ok := h.registry.Get(name); ok && tool != nil && len(tool.ExecutionContract) > 0 {
		return executionContractFromMetadata(name, tool.ExecutionContract)
	}
	return inferredExecutionContract(name)
}

func executionContractFromMetadata(name string, raw map[string]interface{}) ToolExecutionContract {
	contract := inferredExecutionContract(name)
	contract.Explicit = true
	if caps, ok := raw["capabilities"].([]string); ok {
		contract.Capabilities = normalizedExecutionCapabilities(caps)
	} else if caps, ok := raw["capabilities"].([]interface{}); ok {
		values := make([]string, 0, len(caps))
		for _, item := range caps {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, s)
			}
		}
		contract.Capabilities = normalizedExecutionCapabilities(values)
	}
	if v, ok := raw["deterministic"].(bool); ok {
		contract.Deterministic = v
	}
	if v, ok := raw["supports_direct"].(bool); ok {
		contract.SupportsDirect = v
	}
	if v, ok := raw["requires_agent_planning"].(bool); ok {
		contract.RequiresAgentPlanning = v
	}
	switch v := raw["avg_latency_ms"].(type) {
	case int:
		contract.AvgLatencyMS = v
	case float64:
		contract.AvgLatencyMS = int(v)
	}
	return contract
}

func inferredExecutionContract(name string) ToolExecutionContract {
	contract := ToolExecutionContract{Name: strings.TrimSpace(name), RequiresAgentPlanning: true}
	switch contract.Name {
	case "manage_skill":
		contract.Capabilities = []string{"skill"}
		contract.RequiresAgentPlanning = false
	case "web_search":
		contract.Capabilities = []string{"web", "current_data"}
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "web_fetch", "download_file":
		contract.Capabilities = []string{"web", "fetch", "download"}
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "call_mcp_tool":
		contract.Capabilities = []string{"mcp", "external_tool"}
		contract.RequiresAgentPlanning = false
	case "async_wait":
		contract.Capabilities = []string{"async_status"}
		contract.Deterministic = true
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "current_datetime":
		contract.Capabilities = []string{"time"}
		contract.Deterministic = true
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
		contract.AvgLatencyMS = 5
	default:
		contract.Capabilities = []string{"general"}
	}
	return contract
}

func defaultExplicitExecutionContractMetadata(name string) map[string]interface{} {
	contract := inferredExecutionContract(name)
	if strings.TrimSpace(contract.Name) == "" || len(contract.Capabilities) == 0 || contract.RequiresAgentPlanning {
		return nil
	}
	return map[string]interface{}{
		"capabilities":            append([]string(nil), contract.Capabilities...),
		"deterministic":           contract.Deterministic,
		"supports_direct":         contract.SupportsDirect,
		"requires_agent_planning": contract.RequiresAgentPlanning,
		"avg_latency_ms":          contract.AvgLatencyMS,
	}
}

func contractAllowedForLight(contract ToolExecutionContract) bool {
	if contract.Name == "" || contract.RequiresAgentPlanning {
		return false
	}
	for _, cap := range contract.Capabilities {
		switch normalizeExecutionCapability(cap) {
		case "skill", "web", "current_data", "fetch", "mcp", "external_tool", "async_status", "time", "status":
			return true
		}
	}
	return false
}

func contractMatchesExecutionProfile(contract ToolExecutionContract, profile ExecutionProfile) bool {
	if len(profile.RequiredCapabilities) == 0 {
		return true
	}
	required := make(map[string]bool, len(profile.RequiredCapabilities))
	for _, cap := range profile.RequiredCapabilities {
		cap = normalizeExecutionCapability(cap)
		if cap != "" {
			required[cap] = true
		}
	}
	if len(required) == 0 {
		return true
	}
	for _, cap := range contract.Capabilities {
		if required[normalizeExecutionCapability(cap)] {
			return true
		}
	}
	return false
}

func normalizedExecutionCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	seen := make(map[string]bool, len(caps))
	for _, cap := range caps {
		cap = normalizeExecutionCapability(cap)
		if cap == "" || seen[cap] {
			continue
		}
		seen[cap] = true
		out = append(out, cap)
	}
	return out
}

func normalizeExecutionCapability(cap string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(cap)), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
	return strings.Join(parts, "_")
}

func executionProfileToolNames(tools []map[string]interface{}) string {
	if len(tools) == 0 {
		return ""
	}
	names := make([]string, 0, len(tools))
	for _, def := range tools {
		if name := extractToolName(def); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func stripExecutionContractMetadataForLLM(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	stripped := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if def == nil {
			stripped = append(stripped, def)
			continue
		}
		if _, ok := def["x_execution_contract"]; !ok {
			stripped = append(stripped, def)
			continue
		}
		cp := make(map[string]interface{}, len(def)-1)
		for k, v := range def {
			if k == "x_execution_contract" {
				continue
			}
			cp[k] = v
		}
		stripped = append(stripped, cp)
	}
	return stripped
}
