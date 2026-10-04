package guiapp

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticSessionResidue is the host-owned record of what this desktop
// conversation still has to do. It stores capability needs only. Grants,
// tool names, parameters, and route revisions stay on the per-turn plan.
type semanticSessionResidue struct {
	Generation uint64
	Status     semanticResidueStatus
	Needs      []tool.CapabilityNeed
	Summary    string
	// Remaining is how many further invocations of each capability this
	// session may still plan. LookupFacts records a successful web lookup.
	Remaining   map[string]int
	LookupFacts bool
	// PlanClosed is the host record that this turn's plan hit its ceiling.
	PlanClosed bool
}

type semanticResidueStatus string

const (
	semanticResidueOpen      semanticResidueStatus = "open"
	semanticResidueCompleted semanticResidueStatus = "completed"
)

type semanticResidueRelation string

const (
	semanticResidueNone     semanticResidueRelation = ""
	semanticResidueContinue semanticResidueRelation = "continue"
	semanticResidueSwitch   semanticResidueRelation = "switch"
	semanticResidueUnclear  semanticResidueRelation = "unclear"
)

func semanticResidueToPersisted(residue semanticSessionResidue) agent.SemanticSessionResidue {
	out := agent.SemanticSessionResidue{
		Generation:  residue.Generation,
		Status:      string(residue.Status),
		Summary:     residue.Summary,
		LookupFacts: residue.LookupFacts,
		PlanClosed:  residue.PlanClosed,
		Remaining:   cloneResidueRemaining(residue.Remaining),
	}
	for _, need := range residue.Needs {
		persisted := agent.SemanticSessionResidueNeed{
			ID:         need.ID,
			Capability: string(need.Capability),
			Required:   need.Required,
			Qualifiers: tool.CloneNeedQualifiers(need.Qualifiers),
		}
		if len(need.EvidenceIDs) > 0 {
			persisted.EvidenceIDs = append([]string(nil), need.EvidenceIDs...)
		}
		out.Needs = append(out.Needs, persisted)
	}
	return out
}

func semanticResidueFromPersisted(residue agent.SemanticSessionResidue) semanticSessionResidue {
	out := semanticSessionResidue{
		Generation:  residue.Generation,
		Status:      semanticResidueStatus(residue.Status),
		Summary:     residue.Summary,
		LookupFacts: residue.LookupFacts,
		PlanClosed:  residue.PlanClosed,
		Remaining:   cloneResidueRemaining(residue.Remaining),
	}
	for _, need := range residue.Needs {
		converted := tool.CapabilityNeed{
			ID:         need.ID,
			Capability: tool.CapabilityID(need.Capability),
			Required:   need.Required,
			Qualifiers: tool.CloneNeedQualifiers(need.Qualifiers),
		}
		if len(need.EvidenceIDs) > 0 {
			converted.EvidenceIDs = append([]string(nil), need.EvidenceIDs...)
		}
		if semanticResidueAmbientNeed(converted) {
			continue
		}
		out.Needs = append(out.Needs, converted)
	}
	out.Remaining = semanticResidueRemainingForNeeds(out.Remaining, out.Needs)
	return out
}

// semanticResidueAmbientNeed reports retrieval the host adds beside a plan.
// It is not an obligation of the desktop task. The id prefix survives in
// persisted residue after the evidence tag is dropped.
func semanticResidueAmbientNeed(need tool.CapabilityNeed) bool {
	if strings.HasPrefix(strings.TrimSpace(need.ID), "need:~ambient:") {
		return true
	}
	for _, evidence := range need.EvidenceIDs {
		if evidence == "ambient:retrieval" {
			return true
		}
	}
	return false
}

func semanticResidueWithoutAmbient(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	if len(needs) == 0 {
		return nil
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if semanticResidueAmbientNeed(need) {
			continue
		}
		out = append(out, need)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// semanticResidueRemainingForNeeds drops counters whose capability is no
// longer an obligation. A reloaded ambient recall must not keep a ceiling.
func semanticResidueRemainingForNeeds(remaining map[string]int, needs []tool.CapabilityNeed) map[string]int {
	if len(remaining) == 0 {
		return nil
	}
	if len(needs) == 0 {
		return nil
	}
	keep := make(map[string]bool, len(needs))
	for _, need := range needs {
		if need.Capability != "" {
			keep[string(need.Capability)] = true
		}
	}
	out := cloneResidueRemaining(remaining)
	for key := range out {
		if !keep[key] {
			delete(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticResidueSessionKey(msg IMUserMessage) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(msg.Platform), "desktop") {
		return "", false
	}
	key := strings.TrimSpace(msg.UserID)
	if key == "" {
		return "", false
	}
	return key, true
}

func (h *IMMessageHandler) loadSemanticSessionResidue(key string) (semanticSessionResidue, bool) {
	if h == nil || strings.TrimSpace(key) == "" {
		return semanticSessionResidue{}, false
	}
	value, ok := h.semanticSessionResidues.Load(key)
	if !ok {
		return semanticSessionResidue{}, false
	}
	residue, ok := value.(semanticSessionResidue)
	if !ok {
		return semanticSessionResidue{}, false
	}
	residue.Needs = cloneSessionGovernedNeeds(residue.Needs)
	residue.Remaining = cloneResidueRemaining(residue.Remaining)
	return residue, true
}

func (h *IMMessageHandler) loadOpenSemanticSessionResidue(key string) (semanticSessionResidue, bool) {
	residue, ok := h.loadSemanticSessionResidue(key)
	if !ok || residue.Status != semanticResidueOpen || len(residue.Needs) == 0 {
		return semanticSessionResidue{}, false
	}
	return residue, true
}

// loadDesktopTurnResidue returns the open task, or a completed residue that
// still carries web-lookup facts. factsOnly means the task is finished: the
// next follow-up may skip a fresh search, and must not reopen the old tools.
func (h *IMMessageHandler) loadDesktopTurnResidue(msg IMUserMessage, workflow bool, attachments []MessageAttachment) (semanticSessionResidue, bool, bool) {
	key, desktop := semanticResidueSessionKey(msg)
	if h == nil || !desktop || workflow || hostTurnSelectedLocalImage(msg.Text, attachments) {
		return semanticSessionResidue{}, false, false
	}
	residue, ok := h.loadSemanticSessionResidue(key)
	if !ok && h.memory != nil {
		if persisted, found := h.memory.SemanticSessionResidue(key); found {
			residue = semanticResidueFromPersisted(persisted)
			h.semanticSessionResidues.Store(key, residue)
			ok = true
		}
	}
	if !ok {
		return semanticSessionResidue{}, false, false
	}
	if residue.Status == semanticResidueOpen && len(residue.Needs) > 0 {
		return residue, true, false
	}
	if residue.LookupFacts {
		return residue, false, true
	}
	return semanticSessionResidue{}, false, false
}

func (h *IMMessageHandler) storeSemanticSessionResidue(key string, residue semanticSessionResidue) {
	if h == nil || strings.TrimSpace(key) == "" {
		return
	}
	residue.Needs = cloneSessionGovernedNeeds(residue.Needs)
	residue.Remaining = cloneResidueRemaining(residue.Remaining)
	residue.Summary = strings.TrimSpace(residue.Summary)
	h.semanticSessionResidues.Store(key, residue)
	if h.memory != nil {
		h.memory.SetSemanticSessionResidue(key, semanticResidueToPersisted(residue))
	}
}

// resetSemanticTurnLocalState drops residue bookkeeping that belongs to one
// inbound turn. A reused LoopContext otherwise keeps a greeting closure or a
// spent ceiling, and the next message cannot plan tools.
func resetSemanticTurnLocalState(ctx *LoopContext) {
	if ctx == nil {
		return
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.semanticTurnAnswerOnly = false
	ctx.semanticSessionCeilingSpent = false
	ctx.semanticPriorPlanClosed = false
	ctx.semanticResidueCandidateNeeds = nil
	ctx.semanticResidueCandidateText = ""
	ctx.semanticResidueRemaining = nil
	ctx.semanticResidueLookupFacts = false
	ctx.semanticResidueLookupUsed = false
	ctx.semanticResidueUsed = nil
}

func (h *IMMessageHandler) clearSemanticSessionResidue(key string) {
	if h == nil || strings.TrimSpace(key) == "" {
		return
	}
	h.semanticSessionResidues.Delete(key)
	h.forgetProducedDocument(key)
	if h.memory != nil {
		h.memory.ClearSemanticSessionResidue(key)
	}
}

// semanticResidueDocumentSurfaceTurn is a short turn whose classification
// already stayed on the open document. The host records that as
// "session residue document". A continuation that inherited the same file
// is "session residue continuation" and keeps the workspace tools.
// An empty utterance is a replan and keeps the published surface.
func semanticResidueDocumentSurfaceTurn(result intent.ClassificationResult, userText string) bool {
	if strings.TrimSpace(userText) == "" {
		return false
	}
	if !strings.Contains(result.Reason, "session residue document") {
		return false
	}
	if isGenericContinuationPrimary(result) || !semanticPureDocumentEditClassification(result) {
		return false
	}
	return utf8.RuneCountInString(strings.TrimSpace(userText)) <= 24
}

// semanticResidueShortDocumentEdit keeps the document tool and drops the
// companion bundle. It is the same turn as the slim office surface: the
// wording does not split an edit from a question while both are this label.
func semanticResidueShortDocumentEdit(result intent.ClassificationResult, userText string) bool {
	return semanticResidueDocumentSurfaceTurn(result, userText)
}

// semanticResidueSlimOfficeTurn drops workspace companions on that same
// short document turn.
func semanticResidueSlimOfficeTurn(result intent.ClassificationResult, userText string) bool {
	return semanticResidueDocumentSurfaceTurn(result, userText)
}

// semanticResidueTaskMutates reports that the plan changes something the
// user asked for. A baseline or archetype companion is not that change.
func semanticResidueTaskMutates(needs []tool.CapabilityNeed) bool {
	return len(semanticResidueObligationNeeds(needs)) > 0
}

// semanticResidueObligationNeeds is the work this conversation still has to
// finish. Ambient retrieval, a read that shared the plan, and a baseline or
// archetype companion are not that work. A companion ceiling must not keep
// the real grant closed, and must not become the label a restart restores.
func semanticResidueObligationNeeds(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	taskNeeds := semanticResidueWithoutAmbient(needs)
	if len(taskNeeds) == 0 {
		return nil
	}
	out := make([]tool.CapabilityNeed, 0, len(taskNeeds))
	for _, need := range taskNeeds {
		if semanticPlanCompanionNeed(need) || !sessionGovernedNeedHasSideEffect(need) {
			continue
		}
		out = append(out, need)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// semanticResidueWaveSpent reports that the open obligation has used every
// invocation it was granted. Companions and reads are not that grant: an
// unused knowledge read, or a spent baseline write, must not keep a spent
// shell ceiling closed and must not pin a later sentence to that wave.
//
// The task stays open so a revision can renew. A sentence that names
// different work must not inherit the zero counts: clamp would drop the
// obligation and close the turn (production 2026-09-27). A residue with
// no obligation still waits until every tracked count is zero.
func semanticResidueWaveSpent(residue semanticSessionResidue) bool {
	if residue.Status != semanticResidueOpen || len(residue.Remaining) == 0 {
		return false
	}
	obligation := semanticResidueObligationNeeds(residue.Needs)
	if len(obligation) == 0 {
		for _, left := range residue.Remaining {
			if left > 0 {
				return false
			}
		}
		return true
	}
	for _, need := range obligation {
		left, tracked := residue.Remaining[string(need.Capability)]
		if !tracked || left > 0 {
			return false
		}
	}
	return true
}

// semanticSpentWaveStays reports a turn that is still the open task.
// A continuation label stays. The same work surface stays. The sentence
// is not read.
func semanticSpentWaveStays(current intent.ClassificationResult, needs []tool.CapabilityNeed, userText string) bool {
	if isGenericContinuationPrimary(current) {
		return true
	}
	return semanticKeepsOpenWorkSurface(current, needs, userText)
}

// semanticSpentWaveRelease reports a new request sitting on a finished wave.
// Short replies ("可爱风") stay: they are answers, not new tasks. A sentence
// long enough to name different work, or a confident disjoint mutation, leaves.
func semanticSpentWaveRelease(current intent.ClassificationResult, residue semanticSessionResidue, userText string) bool {
	if !semanticResidueWaveSpent(residue) || semanticSpentWaveStays(current, residue.Needs, userText) {
		return false
	}
	if utf8.RuneCountInString(strings.TrimSpace(userText)) > 24 {
		return true
	}
	return current.Confidence >= 0.85 && semanticClassificationHasMutatingFamily(current) && semanticResidueMutatingDisjoint(current, residue.Needs)
}

// semanticResidueDropSpentCounts removes zero ceilings so a renewed wave is
// planned at its normal size. Download follow-ups must not use this: they
// drop only the acquire count and keep the rest of the grant spent.
func semanticResidueDropSpentCounts(remaining map[string]int) map[string]int {
	if len(remaining) == 0 {
		return nil
	}
	out := cloneResidueRemaining(remaining)
	for key, left := range out {
		if left <= 0 {
			delete(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *IMMessageHandler) completeSpentSemanticSessionResidue(key string, residue semanticSessionResidue) {
	if h == nil || strings.TrimSpace(key) == "" || residue.Status != semanticResidueOpen {
		return
	}
	residue.Status = semanticResidueCompleted
	h.storeSemanticSessionResidue(key, residue)
}

func decideSemanticResidueRelation(current intent.ClassificationResult, userText string, residue semanticSessionResidue) semanticResidueRelation {
	return decideSemanticResidueRelationWithBare(current, nil, userText, residue)
}

func decideSemanticResidueRelationWithBare(current intent.ClassificationResult, bare *intent.ClassificationResult, userText string, residue semanticSessionResidue) semanticResidueRelation {
	if residue.Status != semanticResidueOpen || len(residue.Needs) == 0 {
		return semanticResidueNone
	}
	// A finished wave does not swallow the next sentence. "再改一版" stays
	// above, via semanticSpentWaveStays, and renews. A new request plans
	// from its own classification, without the zero ceiling.
	// A short reply that only restates the open context is not a new request.
	// The merge stamped the summary's label onto a sentence that did not name it.
	if semanticSpentWaveRelease(current, residue, userText) && !semanticOpenTaskRestateKeeps(current, userText) {
		return semanticResidueNone
	}
	// A local label on a remote residue, or a remote label on a local residue,
	// changes surface. The matching label stays. The sentence is not read.
	// The same restate is not a surface change either.
	if semanticExplicitSurfaceChange(current, residue.Needs, userText) && !semanticOpenTaskRestateKeeps(current, userText) {
		return semanticResidueSwitch
	}
	if semanticKeepsOpenShell(current, residue.Needs, userText) {
		return semanticResidueUnclear
	}
	// "然后现在几点" is a clock question. The 然后 does not pull the open
	// office tools into this turn.
	if semanticExplicitReadOnlySideQuestion(current, userText) {
		return semanticResidueNone
	}
	// Merging the open summary can stamp that task's label onto a sentence
	// that did not ask for it. A lookup the bare classification already had
	// is the sentence's own label and falls through. A short reply whose
	// merged mutation was already in that context stays on the obligation.
	// A different mutating task still switches.
	if semanticTaskContextMerged(current) && !semanticBareAlreadyRequestedLookup(bare) {
		if semanticOpenTaskRestateKeeps(current, userText) {
			return semanticResidueUnclear
		}
		if current.Confidence >= 0.85 && semanticClassificationHasMutatingFamily(current) && semanticResidueMutatingDisjoint(current, residue.Needs) && !semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
			return semanticResidueSwitch
		}
		return semanticResidueUnclear
	}
	// An evidence label does not cancel an open lookup document. The
	// classifier already absorbed the wording, in whatever language.
	if semanticEvidenceContinuesOpenDeliverable(current, residue) {
		return semanticResidueContinue
	}
	// Lookup and document generation on the same classification is a new
	// delivery. The previous document's spent ceiling does not apply.
	if semanticDeclaresLookupDocument(current) {
		return semanticResidueNone
	}
	// A sentence the classifier called document generation, with no lookup
	// label of its own, renders the open lookup. The wording is not read.
	if semanticRendersOpenLookup(current, residue) {
		return semanticResidueContinue
	}
	if current.Confidence >= 0.85 && semanticClassificationHasMutatingFamily(current) && semanticResidueMutatingDisjoint(current, residue.Needs) {
		if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
			return semanticResidueUnclear
		}
		return semanticResidueSwitch
	}
	if isGenericContinuationPrimary(current) {
		if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
			return semanticResidueUnclear
		}
		return semanticResidueContinue
	}
	if !imSemanticIntentIsManaged(current) || current.Primary.IsNonCapabilityLabel() {
		return semanticResidueUnclear
	}
	// A read-only side question does not inherit the open task and does not
	// close it. Settle keeps the mutating residue.
	if current.Confidence >= 0.85 && !semanticClassificationHasMutatingFamily(current) {
		return semanticResidueNone
	}
	// A short edit of the open delivery stays on that grant. Everything else
	// that already classified as its own managed request is a new delivery:
	// the previous PDF or lookup being spent is not a reason to refuse the
	// next one. Follow-ups returned above.
	if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
		return semanticResidueUnclear
	}
	return semanticResidueNone
}

func semanticKeepsOpenWorkSurface(current intent.ClassificationResult, needs []tool.CapabilityNeed, userText string) bool {
	if semanticPureInteractiveClassification(current) && semanticResidueHasInteractiveSurface(needs) {
		return true
	}
	if semanticKeepsOpenShell(current, needs, userText) {
		return true
	}
	// The same document capability stays on the open file. A different
	// document capability is another delivery. The sentence is not read.
	if !semanticPureDocumentEditClassification(current) || !semanticResidueHasDocumentEdit(needs) {
		return false
	}
	return !semanticResidueMutatingDisjoint(current, needs)
}

func semanticTaskContextMerged(result intent.ClassificationResult) bool {
	return strings.Contains(result.Reason, "task-context merge")
}

// semanticOpenTaskRestateKeeps reports a short reply whose merged mutation
// was already the open context's mutation. The sentence did not add a
// family. A longer sentence can still name different work, and the length
// gate is the same one semanticSpentWaveRelease uses to let that work leave.
func semanticOpenTaskRestateKeeps(current intent.ClassificationResult, userText string) bool {
	if !strings.Contains(current.Reason, "open-task restate") {
		return false
	}
	return utf8.RuneCountInString(strings.TrimSpace(userText)) <= 24
}

// semanticMergedMutationRestates reports that every side effect on merged
// was already on the classification of the open context. The current
// sentence did not introduce that family.
func semanticMergedMutationRestates(context, merged intent.ClassificationResult) bool {
	want := semanticMutatingCapabilities(merged)
	if len(want) == 0 {
		return false
	}
	have := semanticMutatingCapabilities(context)
	for capability := range want {
		if !have[capability] {
			return false
		}
	}
	return true
}

func semanticMutatingCapabilities(current intent.ClassificationResult) map[tool.CapabilityID]bool {
	out := map[tool.CapabilityID]bool{}
	for _, capability := range semanticRuleCapabilities(current) {
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: capability}) {
			out[capability] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticBareAlreadyRequestedLookup(bare *intent.ClassificationResult) bool {
	return bare != nil && semanticResultHasLookupLabel(*bare)
}

func semanticResultHasLookupLabel(current intent.ClassificationResult) bool {
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch:
			return true
		}
	}
	return false
}

// semanticDeclaresLookupDocument reports a classification that asks for
// evidence and a new document together. That is a new delivery in any wording.
func semanticDeclaresLookupDocument(current intent.ClassificationResult) bool {
	if current.Degraded || !semanticResultHasLookupLabel(current) {
		return false
	}
	return current.HasLabel(intent.LabelDocumentGenerate)
}

// semanticEvidenceContinuesOpenDeliverable reports an evidence-only
// classification while the open task is still a lookup plus a PDF. The
// evidence is new; the document obligation stays. A label that itself
// declares a mutation, or an office file that is not a lookup document,
// does not match.
func semanticEvidenceContinuesOpenDeliverable(current intent.ClassificationResult, residue semanticSessionResidue) bool {
	if current.Degraded || semanticClassificationHasMutatingFamily(current) || !semanticLookupHalf(current) {
		return false
	}
	return semanticResidueIsLookupVisual(residue.Needs) && semanticResidueHasGenerate(residue.Needs)
}

func semanticResidueHasGenerate(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == agentservice.CapabilityDocumentGenerate {
			return true
		}
	}
	return false
}

// semanticRendersOpenLookup reports a document render of evidence the open
// task already holds. A classification that also names a lookup is a new
// delivery. The utterance is not inspected.
func semanticRendersOpenLookup(current intent.ClassificationResult, residue semanticSessionResidue) bool {
	if current.Primary != intent.LabelDocumentGenerate {
		return false
	}
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelDocumentGenerate:
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch:
			return false
		}
	}
	return semanticResidueIsLookupVisual(residue.Needs) && semanticResidueHasGenerate(residue.Needs)
}

func semanticResidueIsLookupVisual(needs []tool.CapabilityNeed) bool {
	saw := false
	for _, need := range needs {
		// An office plan's archetype search is not a lookup delivery. Counting
		// it made "另存一份 PDF" stay on the open document.
		if semanticPlanCompanionNeed(need) {
			continue
		}
		switch strings.TrimSpace(string(need.Capability)) {
		case "information.search.web", "information.fetch.web", "visual.render.live_data":
			saw = true
		}
	}
	return saw
}

func semanticResidueHasDocumentWork(needs []tool.CapabilityNeed) bool {
	return semanticResidueHasDocumentEdit(needs)
}

func semanticExplicitReadOnlySideQuestion(current intent.ClassificationResult, _ string) bool {
	if current.Degraded || current.Confidence < 0.85 || semanticClassificationHasMutatingFamily(current) || !imSemanticIntentIsManaged(current) {
		return false
	}
	return semanticPureLabel(current, intent.LabelCurrentTime)
}

func semanticFollowUpAllowsTaskMerge(result *intent.ClassificationResult, userText string) bool {
	if result == nil || result.Degraded {
		return true
	}
	// Relation already inherits, switches, or stands aside. Merging the open
	// task summary here would erase a shell switch or a weather question.
	if result.Confidence >= 0.85 && imSemanticIntentIsManaged(*result) {
		return false
	}
	return !semanticExplicitReadOnlySideQuestion(*result, userText)
}

func semanticExplicitSurfaceChange(current intent.ClassificationResult, needs []tool.CapabilityNeed, _ string) bool {
	if semanticResidueHasObligationCapability(needs, tool.CapabilityShellExecuteRemoteHost) && semanticPureLabel(current, intent.LabelShellCommand) {
		return true
	}
	if semanticResidueHasObligationCapability(needs, tool.CapabilityShellExecuteLocal) && semanticPureLabel(current, intent.LabelSSH) {
		return true
	}
	return false
}

func semanticKeepsOpenShell(current intent.ClassificationResult, needs []tool.CapabilityNeed, _ string) bool {
	openRemote := semanticResidueHasObligationCapability(needs, tool.CapabilityShellExecuteRemoteHost)
	openLocal := semanticResidueHasObligationCapability(needs, tool.CapabilityShellExecuteLocal)
	if openRemote == openLocal {
		return false
	}
	if openRemote && semanticPureLabel(current, intent.LabelSSH) {
		return true
	}
	if openLocal && semanticPureLabel(current, intent.LabelShellCommand) {
		return true
	}
	return false
}

// semanticPureExistingDocumentDelivery is a sentence whose only managed
// family delivers a document that already exists. Lookup, generate, office,
// and every other mutating family are a different request.
func semanticPureExistingDocumentDelivery(current intent.ClassificationResult) bool {
	if current.Degraded {
		return false
	}
	saw := false
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelDocumentDelivery, intent.LabelAttachmentDelivery:
			saw = true
		default:
			if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
				return false
			}
		}
	}
	return saw
}

// semanticResidueHasProducedDocument reports an open obligation that already
// materialized a document. A baseline file write is not that product.
func semanticResidueHasProducedDocument(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if semanticPlanCompanionNeed(need) {
			continue
		}
		switch need.Capability {
		case agentservice.CapabilityDocumentGenerate, tool.CapabilityDocumentWriteOffice:
			return true
		}
	}
	return false
}

func semanticBareDeliversProducedDocument(bare intent.ClassificationResult, residue semanticSessionResidue) bool {
	if residue.Status != semanticResidueOpen || !semanticResidueHasProducedDocument(residue.Needs) {
		return false
	}
	return semanticPureExistingDocumentDelivery(bare)
}

// semanticOpenResidueDelivery keeps the bare delivery of a document this task
// already produced. Inheriting the open generate obligation would render a
// new file and renew spent search. The caller must not apply the open-turn
// ceiling: that ceiling is what reopens generate.
func semanticOpenResidueDelivery(bare intent.ClassificationResult, residue semanticSessionResidue) (intent.ClassificationResult, bool) {
	if !semanticBareDeliversProducedDocument(bare, residue) {
		return intent.ClassificationResult{}, false
	}
	restored := bare
	if !strings.Contains(restored.Reason, "session residue delivery") {
		restored.Reason = strings.TrimSpace(restored.Reason + "; session residue delivery")
		restored.Reason = strings.TrimPrefix(restored.Reason, "; ")
	}
	return restored, true
}

func semanticPureLabel(current intent.ClassificationResult, want intent.IntentLabel) bool {
	saw := false
	for _, label := range current.Labels() {
		if label == want {
			saw = true
			continue
		}
		if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
			return false
		}
	}
	return saw
}

// semanticResidueHasObligationCapability ignores a baseline or archetype
// companion. A managed plan always carries a local shell fallback. That
// fallback is not an open local task, and counting it cancelled the remote
// obligation: one of each shell made a matching SSH sentence leave.
func semanticResidueHasObligationCapability(needs []tool.CapabilityNeed, capability tool.CapabilityID) bool {
	for _, need := range semanticResidueObligationNeeds(needs) {
		if need.Capability == capability {
			return true
		}
	}
	return false
}

func semanticPureDocumentEditClassification(current intent.ClassificationResult) bool {
	saw := false
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelFileWrite:
			saw = true
		default:
			if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
				return false
			}
		}
	}
	return saw
}

func semanticResidueHasDocumentEdit(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if semanticPlanCompanionNeed(need) {
			continue
		}
		switch need.Capability {
		case tool.CapabilityDocumentWriteOffice, tool.CapabilityFSWriteLocal, agentservice.CapabilityDocumentGenerate:
			return true
		}
	}
	return false
}

func semanticPureInteractiveClassification(current intent.ClassificationResult) bool {
	saw := false
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelBrowser, intent.LabelComputerUse:
			saw = true
		default:
			if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
				return false
			}
		}
	}
	return saw
}

func semanticResidueHasInteractiveSurface(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityBrowserControlWeb, tool.CapabilityComputerControlDesktop:
			return true
		}
	}
	return false
}

func semanticClassificationHasMutatingFamily(result intent.ClassificationResult) bool {
	for _, capability := range semanticRuleCapabilities(result) {
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: capability}) {
			return true
		}
	}
	return false
}

func semanticRuleCapabilities(result intent.ClassificationResult) []tool.CapabilityID {
	var capabilities []tool.CapabilityID
	seen := map[tool.CapabilityID]bool{}
	for _, label := range result.Labels() {
		for _, template := range imSemanticIntentRuleSet[label] {
			if template.Capability == "" || seen[template.Capability] {
				continue
			}
			seen[template.Capability] = true
			capabilities = append(capabilities, template.Capability)
		}
	}
	return capabilities
}

func semanticResidueMutatingDisjoint(current intent.ClassificationResult, needs []tool.CapabilityNeed) bool {
	open := map[tool.CapabilityID]bool{}
	for _, need := range semanticResidueObligationNeeds(needs) {
		open[need.Capability] = true
	}
	if len(open) == 0 {
		return true
	}
	for _, capability := range semanticRuleCapabilities(current) {
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: capability}) && open[capability] {
			return false
		}
	}
	return true
}

// semanticOpenResidueClassification rebuilds the open task from its
// obligation. ClassificationFromGrantedNeeds names whichever need was
// stored first, and a plan records a knowledge read or a baseline write
// before the shell it belongs to. After a restart that order became the
// task. A lookup delivery (search plus a card or a document) keeps
// evidence first. Any other open obligation is classified from the
// side-effecting needs that are not companions, and the reads stay
// secondary labels.
func semanticResidueWithoutCompanions(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	if len(needs) == 0 {
		return nil
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if semanticPlanCompanionNeed(need) {
			continue
		}
		out = append(out, need)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticOpenResidueClassification(needs []tool.CapabilityNeed) intent.ClassificationResult {
	taskNeeds := semanticResidueWithoutAmbient(needs)
	if semanticResidueIsLookupVisual(taskNeeds) {
		// Stored order keeps the evidence label first. Companions are
		// omitted: an archetype download on this result is a new acquire
		// wave, and the spent download ceiling opens.
		evidence := semanticResidueWithoutCompanions(taskNeeds)
		if len(evidence) == 0 {
			evidence = taskNeeds
		}
		return agentservice.ClassificationFromGrantedNeeds(evidence, imSemanticIntentRuleSet)
	}
	// Companion labels change the ceiling. A baseline download on the
	// inherited classification is read as a new acquire wave and the shell
	// grant stays closed.
	classified := semanticResidueWithoutCompanions(taskNeeds)
	if len(classified) == 0 {
		classified = taskNeeds
	}
	result := agentservice.ClassificationFromGrantedNeeds(classified, imSemanticIntentRuleSet)
	obligation := semanticResidueObligationNeeds(classified)
	if len(obligation) == 0 {
		return result
	}
	lead := agentservice.ClassificationFromGrantedNeeds(obligation, imSemanticIntentRuleSet)
	if !semanticLabelHasSideEffect(lead.Primary) || result.Primary == lead.Primary {
		return result
	}
	seen := map[intent.IntentLabel]bool{lead.Primary: true}
	var secondary []intent.IntentLabel
	for _, label := range append(lead.Labels(), result.Labels()...) {
		if label == "" || seen[label] || label.IsNonCapabilityLabel() {
			continue
		}
		seen[label] = true
		secondary = append(secondary, label)
	}
	lead.Secondary = secondary
	if result.Confidence > 0 {
		lead.Confidence = result.Confidence
	}
	if result.Layer != 0 {
		lead.Layer = result.Layer
	}
	return lead
}

func semanticLabelHasSideEffect(label intent.IntentLabel) bool {
	for _, template := range imSemanticIntentRuleSet[label] {
		if template.Capability == "" {
			continue
		}
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: template.Capability}) {
			return true
		}
	}
	return false
}

// semanticClassificationWithOpenResidue folds an open task's granted needs
// into this turn's classification. Continue unions them with a new managed
// label. Unclear, continuation, and unknown keep the open obligation and
// do not take a new mutating family from the bare sentence.
func semanticClassificationWithOpenResidue(current intent.ClassificationResult, needs []tool.CapabilityNeed, relation semanticResidueRelation) (intent.ClassificationResult, bool) {
	switch relation {
	case semanticResidueContinue, semanticResidueUnclear:
	default:
		return current, false
	}
	inherited := semanticOpenResidueClassification(needs)
	if !imSemanticIntentIsManaged(inherited) {
		return current, false
	}
	if relation == semanticResidueUnclear || !imSemanticIntentIsManaged(current) || current.Primary.IsNonCapabilityLabel() {
		// A continuation label inherits the open task and keeps its workspace
		// tools. A document label that stayed on the same file is only that
		// file: the planner slims companions from this reason. The sentence
		// is not read.
		inherited.Reason = "session residue continuation"
		if semanticPureDocumentEditClassification(current) && !isGenericContinuationPrimary(current) {
			inherited.Reason = "session residue document"
		}
		return inherited, true
	}
	out := current
	for _, label := range inherited.Labels() {
		if !out.HasLabel(label) {
			out.Secondary = append(append([]intent.IntentLabel(nil), out.Secondary...), label)
		}
	}
	out.Reason = strings.TrimSpace(out.Reason + "; session residue")
	return out, true
}

func (h *IMMessageHandler) noteSemanticSessionResidueCandidate(ctx *LoopContext, userID, channel, userText string, plan tool.ToolPlan) {
	if h == nil || ctx == nil || ctx.WorkflowAgentLoop {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(channel), "desktop") || h.isPureCodingWorkbenchSession(userID) {
		return
	}
	needs := semanticResidueWithoutAmbient(withoutLookupCarryNeeds(grantedNeedsFromPlan(plan)))
	if len(needs) == 0 {
		return
	}
	ctx.semanticResidueCandidateNeeds = cloneSessionGovernedNeeds(needs)
	ctx.semanticResidueCandidateText = strings.TrimSpace(userText)
}

func (h *IMMessageHandler) settleSemanticSessionResidue(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse) {
	key, ok := semanticResidueSessionKey(msg)
	if !ok || h == nil || loopCtx == nil || resp == nil {
		return
	}
	if strings.TrimSpace(resp.Error) != "" || len(loopCtx.semanticResidueCandidateNeeds) == 0 {
		if loopCtx.semanticSessionCeilingSpent {
			h.markSemanticSessionPlanClosed(key)
		}
		return
	}
	previous, _ := h.loadSemanticSessionResidue(key)
	// A read-only plan must not close an open mutating task, including when
	// the sentence contains 然后 or 再查. Those cues do not finish the task.
	// Baseline write and local shell ride on every managed plan; they are
	// not that plan's task, and they used to replace the open obligation.
	candidateMutates := semanticResidueTaskMutates(loopCtx.semanticResidueCandidateNeeds)
	if previous.Status == semanticResidueOpen && semanticResidueTaskMutates(previous.Needs) && !candidateMutates {
		if loopCtx.semanticSessionCeilingSpent {
			h.markSemanticSessionPlanClosed(key)
		}
		return
	}
	summary := semanticResidueSummary(msg.Text, previous.Summary)
	if semanticClassificationKeepsPriorTask(loopCtx) && previous.Summary != "" {
		summary = previous.Summary
	} else if strings.TrimSpace(loopCtx.semanticResidueCandidateText) != "" && strings.TrimSpace(msg.Text) != "" {
		summary = semanticResidueSummary(loopCtx.semanticResidueCandidateText, "")
	}
	status := semanticResidueOpen
	if !candidateMutates {
		status = semanticResidueCompleted
	}
	used, lookupUsed := loopCtx.semanticResidueUsage()
	lookupFacts := lookupUsed
	if semanticClassificationKeepsPriorTask(loopCtx) || strings.TrimSpace(msg.Text) == "" {
		lookupFacts = lookupFacts || previous.LookupFacts
	}
	h.captureProducedDocument(msg, loopCtx.semanticResidueCandidateNeeds, resp)
	h.storeSemanticSessionResidue(key, semanticSessionResidue{
		Generation:  previous.Generation + 1,
		Status:      status,
		Needs:       loopCtx.semanticResidueCandidateNeeds,
		Summary:     summary,
		Remaining:   residueRemainingAfterUse(loopCtx.semanticResidueCandidateNeeds, used),
		LookupFacts: lookupFacts,
		PlanClosed:  loopCtx.semanticSessionCeilingSpent,
	})
}

func (h *IMMessageHandler) markSemanticSessionPlanClosed(key string) {
	residue, ok := h.loadSemanticSessionResidue(key)
	if !ok {
		return
	}
	residue.PlanClosed = true
	h.storeSemanticSessionResidue(key, residue)
}

func residueRemainingAfterUse(needs []tool.CapabilityNeed, used map[string]int) map[string]int {
	counts := map[string]int{}
	for _, need := range needs {
		counts[string(need.Capability)]++
	}
	if len(counts) == 0 {
		return nil
	}
	remaining := make(map[string]int, len(counts))
	for capability, count := range counts {
		left := count - used[capability]
		if left < 0 {
			left = 0
		}
		remaining[capability] = left
	}
	return remaining
}

// semanticClassificationRequestsAcquire reports a turn whose authority is
// another wave of remote files. The wording is not read.
func semanticClassificationRequestsAcquire(current intent.ClassificationResult) bool {
	return current.HasLabel(intent.LabelFileDownload)
}

// semanticResidueRemainingForFollowUp copies the open ceiling. A download
// classification drops only the acquire count, so a new wave is published at
// DownloadRepeatBudget while the rest of the open grant stays spent.
func semanticResidueRemainingForFollowUp(remaining map[string]int, planned intent.ClassificationResult) map[string]int {
	out := cloneResidueRemaining(remaining)
	if len(out) == 0 || !semanticClassificationRequestsAcquire(planned) {
		return out
	}
	delete(out, string(tool.CapabilityArtifactAcquireRemote))
	return out
}

// semanticResidueRenewOpenLookup drops a spent evidence ceiling, and a spent
// document capability only when this residue still carries it. A zero left
// by some other family stays spent, including a stale file-write ceiling
// that baseline expansion would otherwise treat as unlimited.
func semanticResidueRenewOpenLookup(remaining map[string]int, needs []tool.CapabilityNeed) map[string]int {
	keys := []string{
		"information.search.web",
		string(tool.CapabilityInformationFetchWeb),
	}
	seen := map[string]bool{
		"information.search.web":                   true,
		string(tool.CapabilityInformationFetchWeb): true,
	}
	for _, need := range needs {
		id := string(need.Capability)
		if id == "" || seen[id] || !semanticResidueNeedRenewsWithLookup(need) {
			continue
		}
		seen[id] = true
		keys = append(keys, id)
	}
	return semanticResidueDropSpent(remaining, keys...)
}

func semanticResidueNeedRenewsWithLookup(need tool.CapabilityNeed) bool {
	if semanticPlanCompanionNeed(need) {
		return false
	}
	switch need.Capability {
	case agentservice.CapabilityLiveDataVisual,
		agentservice.CapabilityDocumentGenerate,
		tool.CapabilityDocumentWriteOffice,
		tool.CapabilityFSWriteLocal,
		agentservice.CapabilityArtifactDeliverCurrent:
		return true
	default:
		return false
	}
}

func semanticPlanCompanionNeed(need tool.CapabilityNeed) bool {
	if strings.Contains(need.ID, "zz-baseline:") {
		return true
	}
	for _, evidence := range need.EvidenceIDs {
		if evidence == "intent:baseline_workspace" || evidence == "intent:archetype_bundle" {
			return true
		}
	}
	return false
}

// semanticResidueRemainingForOpenTurn is the ceiling for a continue or
// unclear turn. A plan that still asks for evidence renews only that
// evidence and its document, even when every tracked count is already zero.
// Any other finished obligation renews that capability. A companion zero,
// including a baseline file write on another task, stays spent. An open
// file-write task drops its leftover write count: that count is not a quota.
func semanticResidueRemainingForOpenTurn(residue semanticSessionResidue, planned intent.ClassificationResult) map[string]int {
	remaining := semanticResidueRemainingForFollowUp(residue.Remaining, planned)
	// A continued render ("生成pdf报告") keeps document_generate as primary,
	// so it is not a lookup. Its spent generate ceiling must still open, or
	// a positive search count leaves the PDF clamped. Unrelated zeros stay.
	if semanticClassificationRequestsLookupEvidence(planned) || semanticContinuesOpenDocument(planned, residue) {
		return semanticResidueRenewOpenLookup(remaining, residue.Needs)
	}
	if semanticResidueWaveSpent(residue) && !semanticClassificationRequestsAcquire(planned) {
		remaining = semanticResidueRenewSpentObligation(residue, remaining)
	}
	// A leftover file-write count is not the rest of the task. The previous
	// turn spent part of the wave and the next "继续" inherited the remainder
	// as its whole write authority (production 2026-10-04: one fs.write.local
	// node, then write_file was removed for the rest of the bibliography).
	// Drop that ceiling so this turn publishes the normal iterative family.
	// A baseline write riding on another task stays spent.
	return semanticResidueRenewIterativeFileWrite(remaining, residue, planned)
}

// semanticResidueRenewIterativeFileWrite drops a carried fs.write.local
// count when this open task itself writes files. Absence is not a new
// quota: the planner publishes the capability's own repeat family, and a
// later call can append siblings up to the turn cap. A companion write on
// an unrelated task is left untouched.
func semanticResidueRenewIterativeFileWrite(remaining map[string]int, residue semanticSessionResidue, planned intent.ClassificationResult) map[string]int {
	if !semanticOpenFileWriteContinues(residue, planned) {
		return remaining
	}
	key := string(tool.CapabilityFSWriteLocal)
	if _, ok := remaining[key]; !ok {
		return remaining
	}
	out := cloneResidueRemaining(remaining)
	delete(out, key)
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticOpenFileWriteContinues(residue semanticSessionResidue, planned intent.ClassificationResult) bool {
	if !semanticResidueHasObligationCapability(residue.Needs, tool.CapabilityFSWriteLocal) {
		return false
	}
	switch planned.Primary {
	case intent.LabelFileWrite, intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		return true
	default:
		return false
	}
}

// semanticResidueRenewSpentObligation drops zero counters for the open
// obligation. A residue with no such need still renews every zero, which is
// a fully spent read-only map. Companion ceilings are left at zero.
func semanticResidueRenewSpentObligation(residue semanticSessionResidue, remaining map[string]int) map[string]int {
	obligation := semanticResidueObligationNeeds(residue.Needs)
	if len(obligation) == 0 {
		return semanticResidueDropSpentCounts(remaining)
	}
	out := cloneResidueRemaining(remaining)
	for _, need := range obligation {
		key := string(need.Capability)
		if left, ok := out[key]; !ok || left <= 0 {
			delete(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticContinuesOpenDocument(planned intent.ClassificationResult, residue semanticSessionResidue) bool {
	if planned.Primary != intent.LabelDocumentGenerate || !semanticResidueHasDocumentWork(residue.Needs) {
		return false
	}
	return !semanticClassificationRequestsLookupEvidence(planned)
}

func cloneResidueRemaining(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type semanticResidueRemainingKey struct{}

func withSemanticResidueRemaining(ctx context.Context, remaining map[string]int) context.Context {
	if len(remaining) == 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticResidueRemainingKey{}, cloneResidueRemaining(remaining))
}

func semanticResidueRemaining(ctx context.Context) map[string]int {
	if ctx == nil {
		return nil
	}
	remaining, _ := ctx.Value(semanticResidueRemainingKey{}).(map[string]int)
	return remaining
}

// dropUnusedSessionCompanions removes optional baseline and archetype
// companions that this session planned and never called. A companion that
// was used, or that this turn declares for the first time, stays.
func dropUnusedSessionCompanions(needs []tool.CapabilityNeed, remaining map[string]int) []tool.CapabilityNeed {
	if len(needs) == 0 || len(remaining) == 0 {
		return needs
	}
	expanded := map[string]int{}
	for _, need := range needs {
		expanded[string(need.Capability)]++
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if need.Required || !unusedSessionCompanion(need, remaining, expanded) {
			out = append(out, need)
		}
	}
	return out
}

func unusedSessionCompanion(need tool.CapabilityNeed, remaining, expanded map[string]int) bool {
	if !semanticPlanCompanionNeed(need) {
		return false
	}
	limit, tracked := remaining[string(need.Capability)]
	if !tracked {
		return false
	}
	return limit >= expanded[string(need.Capability)]
}

func clampNeedsToResidueRemaining(needs []tool.CapabilityNeed, remaining map[string]int) []tool.CapabilityNeed {
	if len(needs) == 0 || len(remaining) == 0 {
		return needs
	}
	seen := map[string]int{}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		capability := string(need.Capability)
		limit, ok := remaining[capability]
		if !ok {
			out = append(out, need)
			continue
		}
		if seen[capability] >= limit {
			continue
		}
		seen[capability]++
		out = append(out, need)
	}
	return out
}

func semanticPlanHasBaseline(plan tool.ToolPlan) bool {
	for _, selection := range plan.Selections {
		if strings.Contains(selection.NeedID, "zz-baseline:") {
			return true
		}
	}
	return false
}

func semanticClassificationKeepsPriorTask(loopCtx *LoopContext) bool {
	if loopCtx == nil || loopCtx.Runtime.SemanticIntent == nil {
		return false
	}
	current := *loopCtx.Runtime.SemanticIntent
	if isGenericContinuationPrimary(current) {
		return true
	}
	return strings.Contains(current.Reason, "session residue")
}

func semanticResidueSummary(current, previous string) string {
	current = truncateRunes(strings.TrimSpace(current), 120)
	previous = strings.TrimSpace(previous)
	if current != "" {
		return current
	}
	return previous
}
