package guiapp

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticCallSurface is a turn-local, immutable binding between the
// LLM-visible host function names and the catalog selections that may run.
// Those names are stable (web_search, generate_pdf); Grant.Token stays the
// one-shot consume/replay identity. It is intentionally owned by the shared
// loop callback, never cached on the handler or reused by another session.
type semanticCallSurface struct {
	plan  tool.ToolPlan
	scope tool.InvocationScope
	// scopePlan is the immutable projection used by directory discovery and
	// publish-time validation. Keeping it on the live surface prevents a
	// follow-up from reconstructing scope identity from model text.
	scopePlan tool.ToolScopePlan
	// hostConnectionID is an opaque, host-private journal partition for this
	// materialized surface.  It is not a transport connection claim and must
	// never be reconstructed from RequestID, LoopContext.ID, user input, or a
	// provider payload.  The shared IM loop has no provider-issued connection
	// identity, so its journal can only correlate retries within this exact
	// host-owned surface.
	hostConnectionID string
	// epochState is callback-owned synchronization for model-request surface
	// correlation. It is deliberately not model-visible and not reconstructed
	// from function names: a stable name such as web_search can exist on two
	// different revisions.
	epochMu      sync.RWMutex
	activeEpoch  string
	epochVersion uint64
	// epochSnapshot is the binding the most recently issued epoch observed:
	// which repeat family each stable function name held, on which plan. It
	// survives invalidateEpoch so a batched call from that exact epoch can
	// prove it is a same-family continuation rather than a late response
	// binding a successor materialization; beginEpoch replaces it, so a
	// straggler from an older request never matches. cancelSemanticCallSurface
	// clears it together with the epoch.
	epochSnapshot semanticEpochSnapshot
	issuer        *tool.InvocationIssuer
	executor      *tool.PlanExecutor
	routeState    tool.RouteStateStore
	hostCalls     tool.HostCallJournal
	coordinator   *tool.SQLiteSemanticExecutionCoordinator
	// removeTurnFence is installed only for a managed surface owned by a live
	// LoopContext. It is detached when that loop ends normally; replacement and
	// cancellation retain the fence long enough to revoke authority durably.
	removeTurnFence  func()
	tenantID         string
	grants           map[string]tool.InvocationGrant   // stable model function name -> grant
	retiredGrants    map[string]tool.InvocationGrant   // host-call replay lookup only; never re-rendered
	rendered         map[string]bool                   // function names emitted to this host surface
	completed        map[string]bool                   // trusted completed selection IDs
	materialized     map[string]bool                   // selection IDs already given a grant
	schemas          map[string]map[string]interface{} // trusted renderer definitions
	parameterSchemas map[string]map[string]interface{} // canonical invocation schemas
	registry         *tool.CapabilityRegistry
	optionalMu       sync.Mutex
	optionalCacheKey string
	hopEvidence      map[string]HopStepEvidence
	optionalByNeed   map[string]bool
	optionalKnown    bool
	artifacts        *semanticArtifactBroker
	pendingArtifacts map[string][]tool.ArtifactPayload // producer selection -> deferred/published payloads; coordinated hosts commit them with selection success
	// replan contains only host/trusted planning observations. It intentionally
	// excludes user text, model function names, opaque grants, provider IDs and
	// previous arguments: a terminal invocation must not become an authority to
	// widen, replay or steer its replacement.
	replan *semanticReplanInput
}

// semanticEpochSnapshot records what one issued model-request epoch saw: the
// plan (route revision) and, per stable function name, the repeat family its
// grant belonged to at issuance. It is host-private correlation data, never
// model-visible.
type semanticEpochSnapshot struct {
	epoch    string
	planID   string
	families map[string]string
}

// invalidateEpoch retires every outstanding model-request correlation before
// mutating the rendered grant set. A late response can then never resolve a
// stable function name (for example web_search) against a successor surface.
// The snapshot is deliberately retained: it is the only record of which
// family each name held under the retired epoch, and the same-family batch
// continuation gate needs it for exactly one comparison per late call.
func (s *semanticCallSurface) invalidateEpoch() {
	if s == nil {
		return
	}
	s.epochMu.Lock()
	s.activeEpoch = ""
	s.epochMu.Unlock()
}

// previousTurnSemanticToolName is the leftover history placeholder from when
// expired invoke_* names were rewritten. New turns use stable host names, so
// this spelling is never a grant. History presentation may rewrite it to the
// sole live name; execution does not alias it.
const previousTurnSemanticToolName = agentruntime.PreviousTurnSemanticToolName

// resolveFunctionName accepts only a function name actually bound to this
// surface (or a retired name used by its same-call journal replay). Historical
// invoke_* spellings are presentation data, never an execution alias: mapping
// a stale name to a sole live lookup would turn a prior model request into this
// request's authority and violate complete surface replacement.
func (s *semanticCallSurface) resolveFunctionName(name string) string {
	name = strings.TrimSpace(name)
	if s == nil || name == "" {
		return name
	}
	if _, ok := s.grants[name]; ok {
		return name
	}
	if _, ok := s.retiredGrants[name]; ok {
		return name
	}
	return name
}

func (s *semanticCallSurface) soleLiveGrantName() string {
	if s == nil {
		return ""
	}
	return tool.SoleLiveGrantName(s.grants)
}

func (s *semanticCallSurface) soleLiveLookupGrantName() string {
	if s == nil {
		return ""
	}
	return tool.SoleLiveLookupGrantName(s.plan, s.grants)
}

// semanticSearchQueryArgs reports whether the model payload is exactly a
// web-search query. Alias execution is gated on this so leftover write/fetch
// arguments cannot consume the one-shot search grant.
func semanticSearchQueryArgs(argsJSON string) bool {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return false
	}
	query, _ := parsed["query"].(string)
	if strings.TrimSpace(query) == "" {
		return false
	}
	if len(parsed) != 1 {
		return false
	}
	return true
}

func semanticSurfaceGrantName(grant tool.InvocationGrant) string {
	return tool.RenderedSemanticFunctionName(grant.AdapterName, grant.Token)
}

// beginEpoch creates the server-owned identity for exactly one model request.
// The nonce prevents an adapter or model from predicting an epoch from public
// plan fields, while the digest keeps it scoped to the revision and current
// rendered materialization set for diagnostics.
func (s *semanticCallSurface) beginEpoch() string {
	if s == nil {
		return ""
	}
	s.epochMu.Lock()
	defer s.epochMu.Unlock()
	s.epochVersion++
	nonce := newSemanticEphemeralIdentity()
	parts := []string{s.scope.RootTaskID, s.scope.PlanID, s.scope.SessionID, s.scope.TurnID, s.scope.PrincipalID, fmt.Sprintf("%d", s.epochVersion), nonce}
	names := make([]string, 0, len(s.grants))
	for name := range s.grants {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, name+":"+s.grants[name].Token)
	}
	s.activeEpoch = "surface:" + tool.SchemaDigest([]byte(strings.Join(parts, "\x00")))
	families := make(map[string]string, len(s.grants))
	for name, grant := range s.grants {
		families[name] = tool.RepeatFamilyKey(grant.SelectionID)
	}
	s.epochSnapshot = semanticEpochSnapshot{epoch: s.activeEpoch, planID: s.plan.ID, families: families}
	return s.activeEpoch
}

func (s *semanticCallSurface) epochIsCurrent(epoch string) bool {
	if s == nil || strings.TrimSpace(epoch) == "" {
		return false
	}
	s.epochMu.RLock()
	defer s.epochMu.RUnlock()
	return epoch == s.activeEpoch
}

// adoptRequestEpoch binds this surface to an epoch the loop already minted
// for the current model request. The installed-MCP sidecar does not mint a
// second epoch: one response carries one epoch, and a late call from the
// previous response must fail against this snapshot. An empty epoch leaves
// the surface unchanged so a publish that runs before BeginToolSurfaceEpoch
// cannot wipe a fence that was already issued.
func (s *semanticCallSurface) adoptRequestEpoch(epoch string) {
	if s == nil {
		return
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" {
		return
	}
	s.epochMu.Lock()
	defer s.epochMu.Unlock()
	s.activeEpoch = epoch
	families := make(map[string]string, len(s.grants))
	for name, grant := range s.grants {
		families[name] = tool.RepeatFamilyKey(grant.SelectionID)
	}
	s.epochSnapshot = semanticEpochSnapshot{epoch: epoch, planID: s.plan.ID, families: families}
}

// retireEpochSnapshots drops the continuation evidence together with the
// epoch. Cancellation/replacement is terminal: no late call may bind anything
// on this surface afterwards, so the snapshot must not outlive the fence.
func (s *semanticCallSurface) retireEpochSnapshots() {
	if s == nil {
		return
	}
	s.epochMu.Lock()
	s.epochSnapshot = semanticEpochSnapshot{}
	s.epochMu.Unlock()
}

// staleEpochSameFamilyContinuation admits exactly one stale-epoch shape: a
// call from the most recently issued epoch whose model response batched
// several calls into one message. The core loop drains a batch sequentially,
// so when an earlier batched call succeeded, the surface advanced, retired
// the epoch, and issued the family's next sibling under the SAME stable name
// — the late call is then stale only because it carries the retired epoch.
//
// The admission re-proves the binding rather than trusting the name:
//
//  1. the snapshot belongs to the caller's epoch — a successor model request
//     has not begun since (beginEpoch replaces the snapshot), so a straggler
//     from an older response can never bind the current grant;
//  2. the route revision is unchanged — a petition child or a failure replan
//     publishes a new plan and replaces the surface, so this snapshot cannot
//     outlive it;
//  3. the name still holds a LIVE grant (a retired name stays rejected);
//  4. that grant's selection belongs to the same repeat family the name held
//     at issuance — cross-family rebinding stays rejected.
//
// Authorization is not widened: the call binds the grant the surface itself
// just issued for the family's next sibling — the exact grant the model would
// have received had it waited one round — and the normal admission path
// (validation, journal, one-shot consumption) applies unchanged.
func (s *semanticCallSurface) staleEpochSameFamilyContinuation(epoch, name string) bool {
	if s == nil || strings.TrimSpace(epoch) == "" || strings.TrimSpace(name) == "" {
		return false
	}
	s.epochMu.RLock()
	snapshot := s.epochSnapshot
	s.epochMu.RUnlock()
	if snapshot.epoch != epoch || snapshot.planID == "" || snapshot.planID != s.plan.ID {
		return false
	}
	grant, live := s.grants[name]
	if !live {
		return false
	}
	issued, known := snapshot.families[name]
	return known && issued == tool.RepeatFamilyKey(grant.SelectionID)
}

func (s *semanticCallSurface) liveGrantNames() map[string]bool {
	if s == nil {
		return nil
	}
	return tool.LiveGrantNames(s.grants)
}

// semanticReplanInput is the replay-safe portion of a route request. The UIC
// classification, channel facts and ingress attachments were already accepted
// by the host at the original route boundary; a replan may refresh the catalog
// but may not re-interpret model output or recover a request from prompt text.
type semanticReplanInput struct {
	UserID         string
	Channel        string
	RootTaskID     string
	Classification intent.ClassificationResult
	Attachments    []MessageAttachment
	Attempts       uint8
	// ConversationLookupReused is the host-trusted record that the parent
	// plan dropped its lookup legs on same-topic conversation evidence. A
	// petition expansion re-plans without user text, so it reads this flag
	// instead of re-deriving the heuristic.
	ConversationLookupReused bool
	// BundleKey is the archetype key the published plan's bundle offers were
	// derived with. A petition expansion or failure replan re-derives needs
	// from a classification that may now declare a document-production label
	// the original plan did not expand with; without this record the child
	// would swap archetype bundles and gain legs the expansion/replan
	// validators must reject.
	BundleKey intent.IntentLabel
	// SlimOffice records that this turn kept only the document surface.
	// Replan and petition call the planner with an empty utterance, so they
	// cannot see "然后结论是什么" and would otherwise grow the office writer
	// back from the stored classification.
	SlimOffice        bool
	ShortDocumentEdit bool
	// BaselineWorkspace records that the published plan raised the workspace
	// ceiling (read/write/shell repeats). A shell archetype already offers
	// file read, so the extra siblings keep the archetype need id and do not
	// carry a zz-baseline prefix. Re-deriving "did this plan have baseline?"
	// from that prefix drops those siblings and the expansion validator
	// rejects the petition.
	BaselineWorkspace bool
}

type semanticRouteDiagnostic struct {
	Handled bool
	PlanID  string
	Reason  string
}

// semanticPlanPreparation is the trusted, non-materialized result of semantic
// planning. Shadow routing must stop here: issuing a grant is an execution
// capability, not a diagnostic operation.
type semanticPlanPreparation struct {
	registry *tool.CapabilityRegistry
	plan     tool.ToolPlan
	// scopePlan is the immutable, name-free-of-user-text projection validated
	// before this preparation can reach surface publication. Keeping it with the
	// preparation lets the publication tail re-check the exact same closure.
	scopePlan      tool.ToolScopePlan
	definitions    map[string]map[string]interface{}
	schemas        map[string]map[string]interface{}
	rootTaskID     string
	turnID         string
	documentInputs []semanticTrustedArtifactInput
	audioInputs    []semanticTrustedArtifactInput
	// conversationLookupReused records that same-topic conversation evidence
	// dropped this plan's lookup legs. It is a host/trusted planning
	// observation carried into the replan input: a later petition expansion
	// re-plans without the turn's user text and must mirror the drop instead
	// of resurrecting non-petitioned lookup legs.
	conversationLookupReused bool
	// baselineWorkspace is the applyBaseline decision for this plan. Petition
	// expansion replays it; need-id prefixes do not preserve a ceiling raise.
	baselineWorkspace bool
	policy            corelib.EffectiveRoutingPolicy
}

func newIMSemanticCapabilityRegistry() *tool.CapabilityRegistry {
	registry := tool.NewCapabilityRegistry("im-semantic-v2")
	for _, descriptor := range []tool.CapabilityDescriptor{
		{
			ID: "visual.capture.desktop", Version: "v1", Owner: "im",
			Summary: "Capture the approved primary desktop display.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"display": {Values: []string{"primary"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectReadOnly},
		},
		{
			ID: "visual.render.live_data", Version: "v1", Owner: "im",
			Summary:  "Render host-trusted current lookup facts as a PNG image artifact.",
			Effects:  []tool.EffectClass{tool.EffectLocalMutation},
			Produces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		},
		{
			ID: "artifact.deliver.current_channel", Version: "v1", Owner: "im",
			Summary: "Deliver an approved artifact to the current conversation channel.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"format": {Values: []string{"file", "image", "voice"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectExternalEffect},
		},
		{
			ID: "artifact.deliver.specified_target", Version: "v1", Owner: "im",
			Summary: "Deliver an approved artifact to a host-authenticated group or user destination.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"format": {Values: []string{"file"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectExternalEffect},
		},
		{
			ID: "information.current_time", Version: "v1", Owner: "im",
			Summary: "Read the current local date, time, weekday and timezone.",
			Effects: []tool.EffectClass{tool.EffectReadOnly},
		},
		{
			ID: "information.search.web", Version: "v1", Owner: "im",
			Summary: "Search public web information for the approved request.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"freshness": {Values: []string{"reference", "current"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectReadOnly},
		},
		{
			ID: "document.read.local", Version: "v1", Owner: "im",
			Summary: "Read one trusted document attachment without exposing its local path.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"format": {Values: []string{"pdf", "word", "spreadsheet", "presentation", "text", "mobi"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectReadOnly},
		},
		{
			ID: "document.generate.file", Version: "v1", Owner: "im",
			Summary: "Render current facts into a PDF file and publish an ArtifactRef without delivering it.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"format": {Values: []string{"pdf"}, Required: true},
			},
			Effects:  []tool.EffectClass{tool.EffectLocalMutation},
			Produces: []tool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
		},
		// information.lookup is the open dynamic family: an arbitrary installed
		// MCP or Skill may serve it without a per-name review, and the open
		// envelope now also permits an unreviewed tool to perform a write that
		// settles through the external-effect receipt boundary. It is declared
		// here with the same descriptor the reviewed dynamic registry uses,
		// because the IM publisher validates every contract against this
		// registry and would otherwise reject the open contract as an unknown
		// capability. The required scope qualifier keeps it distinct from a
		// freshness-bearing web search: an arbitrary tool answers from whatever
		// it is pointed at, so it is reference retrieval; a write by an
		// unannotated tool is fail-safe gated by the receipt coordinator.
		{
			ID: "information.lookup", Version: "v1", Owner: "im",
			Summary: "Retrieve information or call an installed MCP tool; writes settle through the external-effect receipt boundary.",
			Qualifiers: map[string]tool.QualifierConstraint{
				"scope": {Values: []string{"reference", "current"}, Required: true},
			},
			Effects: []tool.EffectClass{tool.EffectReadOnly, tool.EffectExternalEffect},
		},
	} {
		if err := registry.Register(descriptor); err != nil {
			panic("invalid IM semantic capability declaration: " + err.Error())
		}
	}
	// Product-domain descriptors are registered first; the reviewed builtin
	// ontology then fills in the shared vocabulary (skipping any ID the product
	// domain already owns) before the registry is sealed for planning.
	if err := tool.RegisterBuiltinCapabilityOntology(registry); err != nil {
		panic("invalid builtin capability ontology: " + err.Error())
	}
	if err := registry.Seal(); err != nil {
		panic("seal IM semantic capability registry: " + err.Error())
	}
	return registry
}

// imSemanticIntentSource makes an already trusted UIC result available to the
// shared resolver without granting it access to legacy ToolNames, affinity
// tables, provider inventories, or provider descriptions.
type imSemanticIntentSource struct{ result intent.ClassificationResult }

func (s imSemanticIntentSource) Classify(intent.MessageContext) intent.ClassificationResult {
	return s.result
}

const imSemanticMinimumConfidence = agentservice.ReviewedIntentMinimumConfidence

// semanticLookupHintFloor is the lowest score we still treat as a real
// enough signal. L2 search/live_data hints use it when the tree is
// unavailable. Successful L3/fusion verdicts reuse it as the tree floor:
// 0.75 is a normal tree pick, 0.55 is not. It is not 0.78; that number is
// L2 early-exit and the resolver write-grant floor.
const semanticLookupHintFloor = intent.EmbeddingLookupMinScore

const semanticInvocationGrantTTL = tool.DefaultInvocationGrantTTL

// imSemanticIntentRuleSet is the GUI alias of the shared IM semantic rule
// table. Adding a family belongs in agentservice.IMSemanticIntentCapabilityNeedRules.
var imSemanticIntentRuleSet = agentservice.IMSemanticIntentCapabilityNeedRules()

// semanticCodingCapabilityRule is the GUI alias of the shared coding family.
var semanticCodingCapabilityRule = agentservice.ReviewedCodingCapabilityNeedRule()

// imSemanticIntentCoverage is the single migration gate for a classification.
// managed means at least one label has an owner-published capability rule.
// unmapped is the first non-generic label without a rule; it is independent of
// managed so a search+document_delivery request can fail closed before catalog
// construction.
func imSemanticIntentCoverage(result intent.ClassificationResult) (managed bool, unmapped intent.IntentLabel) {
	coverage := agentservice.IntentRuleCoverageFromClassification(result, imSemanticIntentRuleSet)
	return coverage.Managed, coverage.Unmapped
}

func imSemanticIntentIsManaged(result intent.ClassificationResult) bool {
	managed, _ := imSemanticIntentCoverage(result)
	return managed
}

// normalizeSemanticClassificationForTurn performs only a structural
// normalization of labels already declared by UIC. It never derives an intent,
// changes confidence/degradation, or exposes a capability from user wording.
func normalizeSemanticClassificationForTurn(result *intent.ClassificationResult) {
	intent.NormalizeDeclaredComposite(result)
}

// semanticReadOnlyLookupHint reports a search/live_data classification that
// carries no mutating capability and scored high enough to plan a lookup.
// An L2 weather lookup that failed tree parse is still a lookup; it must not
// HostReject just because Degraded is set.
func semanticReadOnlyLookupHint(result intent.ClassificationResult) bool {
	if !semanticReadOnlyLookupFamily(result) {
		return false
	}
	return result.Confidence >= semanticLookupHintFloor
}

// semanticLookupHalf is the lookup side of a (possibly composite) turn.
func semanticLookupHalf(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch:
		return true
	default:
		return false
	}
}

// semanticReadOnlyLookupFamily is the lookup-only half: search/live_data with
// no mutating labels. Sub-floor scores still belong here and fall through to
// chat. Weather+PDF is not this predicate; after lexical it is a declared
// composite with Degraded cleared and does not enter the hint gates.
func semanticReadOnlyLookupFamily(result intent.ClassificationResult) bool {
	if !semanticLookupHalf(result) {
		return false
	}
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		if label != intent.LabelSearch && label != intent.LabelLiveData {
			return false
		}
	}
	return true
}

// semanticDeclaredLookupGenerateComposite is the generate half of the declared
// {search,live_data,web_fetch}×{document_generate} pair. Other mutating labels
// stay outside this exception. The lookup half must match semanticLookupHalf
// exactly: the L3 tree's natural verdict for a lookup task is web_fetch, and
// excluding it here dropped the declared pair at every downstream planning
// gate (2026-08-28 张惠妹 turn: tree-synthesized web_fetch(0.62)+
// document_generate(0.683) composite planned no surface at all, and the
// generate_pdf petition then expanded nothing because the label was already
// declared).
func semanticDeclaredLookupGenerateComposite(result intent.ClassificationResult) bool {
	if !semanticLookupHalf(result) || !classificationHasLabel(result, intent.LabelDocumentGenerate) {
		return false
	}
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch, intent.LabelDocumentGenerate:
		default:
			return false
		}
	}
	return true
}

func semanticDeclaredLookupVisualComposite(result intent.ClassificationResult) bool {
	if !semanticLookupHalf(result) || !classificationHasLabel(result, intent.LabelLiveDataVisual) {
		return false
	}
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch, intent.LabelLiveDataVisual:
		default:
			return false
		}
	}
	return true
}

// semanticReadOnlyGovernedLabel reports whether a rule label is governed and
// read-only. semanticReadOnlyGovernedLabels is the single source for this
// set: the planning gates below and the petition budget/group-policy gate
// must agree on what "read-only" means, or a weak classification could plan a
// leg the petition path treats as effectful (or vice versa).
func semanticReadOnlyGovernedLabel(label intent.IntentLabel) bool {
	return semanticReadOnlyGovernedLabels[label]
}

func semanticReadOnlyGovernedFamily(result intent.ClassificationResult) bool {
	saw := false
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		if !semanticReadOnlyGovernedLabel(label) {
			return false
		}
		saw = true
	}
	return saw
}

// semanticReadOnlyUnderstandFamily is a read-only governed turn that is not
// a pure search/live_data lookup. Weak scores must fall through to chat: UIC
// often guesses document_read for 「图上有什么」, or mixes knowledge_read with
// search, and a red HostReject is worse than answering in chat. A mutating
// label on the same turn keeps the fail-closed path.
func semanticReadOnlyUnderstandFamily(result intent.ClassificationResult) bool {
	return semanticReadOnlyGovernedFamily(result) && !semanticReadOnlyLookupFamily(result)
}

func semanticReadOnlyUnderstandHint(result intent.ClassificationResult) bool {
	if !semanticReadOnlyUnderstandFamily(result) {
		return false
	}
	// Degraded file_read ≥ 0.78 still plans (hot embedding fallback).
	// semanticClassificationMeetsResolverFloor requires !Degraded; do not
	// use it here.
	if result.Confidence >= imSemanticMinimumConfidence {
		return true
	}
	return semanticTreeConfirmedClassification(result)
}

func semanticReadOnlyGovernedHint(result intent.ClassificationResult) bool {
	return semanticReadOnlyLookupHint(result) || semanticReadOnlyUnderstandHint(result)
}

// semanticOfficeGovernedHint is the mutating counterpart of the read-only
// hint gate: an L2 office guess that met the lookup floor but whose tree
// ruling timed out. The office family plans through the same governed
// catalog as a tree-confirmed verdict (fit-proof bound, one-shot grants), so
// keeping the hint cannot mint bash or arbitrary writes — it only stops a
// slow hub from stripping document tools off 「生成PPT/报告」 turns.
// Generate/coding secondaries never reach here: the classifier collapses
// those composites to unknown before this gate.
func semanticOfficeGovernedHint(result intent.ClassificationResult) bool {
	if result.Primary != intent.LabelOffice || !result.Degraded {
		return false
	}
	return result.Confidence >= semanticLookupHintFloor
}

func semanticNeedsChatProjection(result intent.ClassificationResult) bool {
	if semanticDeclaredLookupGenerateComposite(result) || semanticDeclaredLookupVisualComposite(result) {
		return false
	}
	if semanticReadOnlyLookupFamily(result) {
		return !semanticReadOnlyLookupHint(result)
	}
	return semanticReadOnlyUnderstandFamily(result) && !semanticReadOnlyUnderstandHint(result)
}

func semanticChatProjection(result intent.ClassificationResult) intent.ClassificationResult {
	reason := stripReasonToken(stripReasonToken(strings.TrimSpace(result.Reason), routingMissHostAdapterReason), routingMissFallbackReason)
	if reason == "" {
		reason = string(result.Primary)
	}
	if !strings.Contains(reason, "chat projection") {
		reason = strings.TrimSpace(reason + "; chat projection")
	}
	return intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Layer:      result.Layer,
		Reason:     reason,
		Degraded:   true,
	}
}

func applySemanticChatProjection(ctx *LoopContext) {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil {
		return
	}
	if !semanticNeedsChatProjection(*ctx.Runtime.SemanticIntent) {
		return
	}
	projected := semanticChatProjection(*ctx.Runtime.SemanticIntent)
	ctx.Runtime.SemanticIntent = &projected
}

func markLoopRoutingMissChatLeftover(ctx *LoopContext, result intent.ClassificationResult) {
	if ctx == nil {
		return
	}
	stampRoutingMissReason(&result, false)
	ctx.Runtime.SemanticIntent = &result
	ctx.Runtime.RoutingMissFallback = true
	ctx.Runtime.HostAdapterLeftover = false
}

func markLoopRoutingMissHostAdapterLeftover(ctx *LoopContext, result intent.ClassificationResult) {
	if ctx == nil {
		return
	}
	stampRoutingMissReason(&result, true)
	ctx.Runtime.SemanticIntent = &result
	ctx.Runtime.RoutingMissFallback = true
	ctx.Runtime.HostAdapterLeftover = true
}

// applySemanticRoutingMissFallback unlocks leftover tools after a precise
// semantic surface could not be materialized. Routing exists to shrink the
// tool list, not to stop the turn: a miss pays a bounded leftover surface
// and continues. Privilege-expanding core tools are stripped later.
func applySemanticRoutingMissFallback(ctx *LoopContext) {
	if ctx == nil || loopContextIsVisionFallthrough(ctx) {
		return
	}
	// ACP light and coding-workbench consume-miss can leave SemanticIntent
	// unset. A plan miss still has to pay leftover, not CoreToolNames+UIC.
	if ctx.Runtime.SemanticIntent == nil {
		markLoopRoutingMissChatLeftover(ctx, semanticChatProjection(intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
		}))
		return
	}
	hostAdapter := routingMissWantsHostAdapter(*ctx.Runtime.SemanticIntent) && semanticFileDeliveryPublished(ctx.Platform)
	if leftoverKeepsRemoteHostClassification(*ctx.Runtime.SemanticIntent) {
		// Keep LabelSSH. Chat-projecting it to unknown would make leftover
		// skip hide builtin ssh, which is the only connect/exec surface for
		// an unbound classified SSH turn.
		markLoopRoutingMissChatLeftover(ctx, *ctx.Runtime.SemanticIntent)
		return
	}
	if loopContextIsSemanticManaged(ctx) {
		projected := semanticChatProjection(*ctx.Runtime.SemanticIntent)
		if hostAdapter {
			markLoopRoutingMissHostAdapterLeftover(ctx, projected)
			return
		}
		markLoopRoutingMissChatLeftover(ctx, projected)
		return
	}
	// Unknown / continuation / workflow+generate are not chat-projected and
	// not loop-managed. They are still a plan miss: leftover, not bash+UIC.
	if hostAdapter {
		markLoopRoutingMissHostAdapterLeftover(ctx, *ctx.Runtime.SemanticIntent)
		return
	}
	markLoopRoutingMissChatLeftover(ctx, semanticChatProjection(*ctx.Runtime.SemanticIntent))
}

func semanticPlanErrorBlocksSession(err error) bool {
	// `handled` means that this request belongs to a capability family owned by
	// the semantic planner.  Once that ownership decision has been made, no
	// planner failure is allowed to revive the legacy name router: doing so
	// would make an unavailable catalog indistinguishable from an authorization
	// to use every old tool.  Callers use this only after `handled == true`.
	return err != nil
}

func semanticTrustedDocumentInputError(err error) bool {
	return semanticTrustedDocumentContextError(err) || semanticTrustedDocumentInputMissingOrAmbiguous(err)
}

func semanticTrustedDocumentInputMissingOrAmbiguous(err error) bool {
	return tool.IsTrustedInputMissingOrAmbiguous(err)
}

func semanticProducedDocumentError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "produced_document_stale") || strings.Contains(message, "produced_document_invalid")
}

func semanticTrustedDocumentContextError(err error) bool {
	if err == nil {
		return false
	}
	for _, code := range []string{
		"trusted_document_context_stale",
		"trusted_document_context_expired",
		"trusted_document_context_invalid",
		"trusted_document_context_unavailable",
		"trusted_document_context_path_invalid",
		"trusted_document_context_unsupported",
		"trusted_document_context_picker_mismatch",
	} {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}

func loopContextHasChatProjection(ctx *LoopContext) bool {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil {
		return false
	}
	result := *ctx.Runtime.SemanticIntent
	return result.Primary == intent.LabelUnknown && strings.Contains(result.Reason, "chat projection")
}

// semanticHostStagedImageUnderstand reports that this turn should not mint a
// semantic grant because the host already staged an image. file_read still
// HostRejects or exposes read_file; live_data from 「这张图里的天气如何」
// would search instead of reading the photo. A confident screenshot request
// still plans capture: the attached image is context, not a substitute for a
// new shot. Bare/degraded document_generate stays fail-closed.
//
// A DocumentRead verdict is in scope too, but only when the turn's ingress
// cannot satisfy it at all (semanticUnsatisfiableDocumentReadOnStagedImage):
// a document turn that merely also carries an image keeps its governed plan.
func semanticHostStagedImageUnderstand(userText string, attachments []MessageAttachment, result intent.ClassificationResult) bool {
	if strings.TrimSpace(result.WorkflowType) != "" || !semanticTurnHasHostStagedImage(userText, attachments) {
		return false
	}
	if result.Primary == intent.LabelScreenshot && !semanticWeakImageUnderstandClassification(result) {
		return false
	}
	if semanticImageUnderstandCapabilityLabels(result) {
		return true
	}
	// Checked after the image-understanding labels and before the lookup
	// steal: a DocumentRead verdict this turn cannot satisfy is not a request
	// for a document, and it is not a lookup either. One predicate drives
	// both call sites, so planning and the runtime demotion agree.
	if semanticUnsatisfiableDocumentReadOnStagedImage(userText, attachments, result) {
		return true
	}
	return semanticHostStagedImageLookupSteal(result)
}

func demotedStagedImageUnderstandIntent(result intent.ClassificationResult) intent.ClassificationResult {
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = string(result.Primary)
	}
	if !strings.Contains(reason, "staged image understand") {
		reason = strings.TrimSpace(reason + "; staged image understand")
	}
	return intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Layer:      result.Layer,
		Reason:     reason,
		Degraded:   true,
	}
}

func applyStagedImageUnderstandRuntime(ctx *LoopContext, userText string, attachments []MessageAttachment) {
	if ctx == nil {
		return
	}
	// A picker path is not a photo. Entry runs before materialize and must
	// not destroy live_data/search evidence; it may only widen the prompt so
	// a later successful load is not stuck on a light managed-lookup script.
	if !semanticTurnHasImageAttachment(attachments) {
		ctx.Runtime.VisionFallthrough = false
		if len(selectedLocalImagePaths(userText)) > 0 && (ctx.Runtime.Execution.IsLight() || ctx.Runtime.Execution.IsDirect()) {
			ctx.Runtime.Execution = fullExecutionProfile("pending local image staging")
		}
		return
	}
	if ctx.Runtime.SemanticIntent == nil || !semanticHostStagedImageUnderstand(userText, attachments, *ctx.Runtime.SemanticIntent) {
		ctx.Runtime.VisionFallthrough = false
		return
	}
	ctx.Runtime.VisionFallthrough = true
	demoted := demotedStagedImageUnderstandIntent(*ctx.Runtime.SemanticIntent)
	ctx.Runtime.SemanticIntent = &demoted
	if ctx.Runtime.Execution.IsLight() || ctx.Runtime.Execution.IsDirect() {
		ctx.Runtime.Execution = fullExecutionProfile("staged image understand")
	}
}

func loopContextIsVisionFallthrough(ctx *LoopContext) bool {
	return ctx != nil && ctx.Runtime.VisionFallthrough
}

// leftoverKeepsRemoteHostClassification is a capability-family check, not a
// user-text heuristic. Managed SSH is the bound-session adapter; an unbound
// UIC LabelSSH turn (primary, secondary, or RunnerUp escalation evidence)
// misses to leftover so builtin ssh can connect/exec. Chat-projecting that
// classification would strip the leftover skip's PreResolved keep and hide
// the tool.
func leftoverKeepsRemoteHostClassification(result intent.ClassificationResult) bool {
	if result.Degraded || result.Confidence < intent.EmbeddingLookupMinScore {
		return false
	}
	return classificationHasSSHSignal(result)
}

func loopContextTurnAnswerOnly(ctx *LoopContext) bool {
	return ctx != nil && ctx.semanticTurnAnswerOnly
}

func loopContextBlocksLegacyToolRouter(ctx *LoopContext) bool {
	// Same continuation as the shared-turn planner skip. A managed label left
	// on the loop would make prepareAgentLoopTools return an empty list, so
	// the approved tool would be the only name and read_file would disappear.
	if discoveredMCPContinuationTurn(ctx) {
		return false
	}
	// The worker already left the semantic lookup surface. A managed label
	// copied from the host prompt must not leave it with an empty tool list.
	if ctx != nil && strings.TrimSpace(ctx.ChatTaskID) != "" {
		return false
	}
	if loopContextTurnAnswerOnly(ctx) {
		return true
	}
	if loopContextHasRoutingMissFallback(ctx) {
		// Leftover still carries the turn's UIC result. LabelSSH is a managed
		// family, so treating leftover as "managed, skip the name-router"
		// would leave an empty surface after the unbound miss. Vision and
		// classifier protocol failures stay blocked.
		return loopContextIsVisionFallthrough(ctx) || loopContextHasClassificationProtocolFailure(ctx)
	}
	return loopContextIsSemanticManaged(ctx) || loopContextIsVisionFallthrough(ctx) || loopContextHasClassificationProtocolFailure(ctx)
}

// loopContextHasClassificationProtocolFailure keeps an L3 contract violation
// on the control-plane path. Treating it as an ordinary unknown intent would
// re-enter the legacy name router, whose incomplete catalog can produce a
// zero-tool model request. This is not a user-text heuristic and grants no
// capability; it only preserves the request boundary until the host returns a
// retryable control-plane error.
func loopContextHasClassificationProtocolFailure(ctx *LoopContext) bool {
	return ctx != nil && ctx.Runtime.SemanticIntent != nil && ctx.Runtime.SemanticIntent.ControlPlaneFailure
}

func semanticHostStagedImageLookupSteal(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelKnowledgeRead, intent.LabelWebFetch,
		intent.LabelUnknown, intent.LabelNonCoding, intent.LabelContinuation, intent.LabelAmbiguous:
	default:
		return false
	}
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelKnowledgeRead, intent.LabelWebFetch,
			intent.LabelUnknown, intent.LabelNonCoding, intent.LabelContinuation, intent.LabelAmbiguous,
			intent.LabelDocumentGenerate:
			continue
		default:
			return false
		}
	}
	return true
}

// semanticImageUnderstandCapabilityLabels reports whether this turn should be
// treated as a staged-image understanding request rather than a file/document
// operation.
//
// DocumentRead is absent from the switch on purpose, but that only means it is
// not an *image-understanding* label here: whether a DocumentRead verdict on a
// staged image is a misroute is decided by
// semanticUnsatisfiableDocumentReadOnStagedImage, which is stricter (it
// requires that the turn hold no document input at all). Keeping the two
// apart is what lets a real document turn that also carries an image keep its
// governed plan.
func semanticImageUnderstandCapabilityLabels(result intent.ClassificationResult) bool {
	sawManaged := false
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelFileRead, intent.LabelScreenshot:
			sawManaged = true
		default:
			return false
		}
	}
	return sawManaged
}

// semanticTurnHasDocumentAttachment reports whether the turn carries a
// document-format ingress artifact at all. Image bytes are vision input, not a
// document: agent.DocumentAttachmentFormat rejects them, so a turn whose only
// attachment is an image can never satisfy a document.read.local need.
//
// Deliberately says nothing about the bytes. A document attachment with no
// content is a broken ingress, not an absent one, and the two must not be
// merged: semanticDocumentInputsForTurn fails it loudly with
// trusted_document_attachment_content_missing, and treating it as "no
// document" here would demote the turn to a vision answer — silently
// swallowing a real attachment failure and answering a question the user did
// not ask (2026-10-11 review). A broken document still blocks the demotion.
// TestSemanticTurnHasDocumentAttachmentMatchesDocumentInputsForTurn pins the
// two views together.
func semanticTurnHasDocumentAttachment(attachments []MessageAttachment) bool {
	for _, attachment := range attachments {
		if _, _, ok := agent.DocumentAttachmentFormat(attachment.FileName, attachment.MimeType); ok {
			return true
		}
	}
	return false
}

// semanticUnsatisfiableDocumentReadOnStagedImage reports a DocumentRead
// verdict this turn's ingress cannot possibly satisfy: the host staged image
// bytes, and nothing in the turn is a document — no document attachment, and
// this turn's picker selected no document path.
//
// Such a verdict is unsatisfiable by construction, so planning it has exactly
// one ending: trusted_document_input_missing and a red HostReject. It is also
// a known misroute rather than a real request, because the classifier only
// ever sees the user text with the picker block stripped, so a bare
// 「图上有什么」 is an embedding coin-flip between screenshot/file_read and
// document_read. Production 2026-10-11: L2 said screenshot 0.72 (under the
// 0.78 early-exit), the L3 tree overrode it with document_read 0.75, and a
// turn whose image the host had already staged ended in a refusal.
//
// Falling through here is a demotion, not a grant: the turn leaves governed
// planning entirely (handled=false) with VisionFallthrough set, so the legacy
// name router stays closed and no document tool is minted. That is strictly
// stronger than the HostReject it replaces. The reason DocumentRead was kept
// out of semanticImageUnderstandCapabilityLabels was to stop an *older*
// desktop document from being reused for a new image — and that protection is
// owned by the separate gate in planning that only reuses the active document
// when the turn carries no attachment at all, which a demotion never reaches.
func semanticUnsatisfiableDocumentReadOnStagedImage(userText string, attachments []MessageAttachment, result intent.ClassificationResult) bool {
	if !classificationHasLabel(result, intent.LabelDocumentRead) {
		return false
	}
	if !semanticTurnHasImageAttachment(attachments) {
		return false
	}
	if semanticTurnHasDocumentAttachment(attachments) {
		return false
	}
	// A current picker selection of a real document is host-authorized
	// document input even when a staged image rides along; the verdict is
	// satisfiable and must keep its governed plan. The binding gate that
	// follows is widened to match: see the activeLocalDocumentReuse branch in
	// semanticPlanForTurnWithContextAndClassificationAndAttachments.
	if hasCurrentLocalDocumentPickerInput(userText) {
		return false
	}
	// Only a verdict whose every label is answered by looking at what the
	// user already supplied may be demoted; see
	// semanticStagedImageDemotableVerdict.
	return semanticStagedImageDemotableVerdict(result)
}

// semanticStagedImageDemotableVerdict reports a verdict that no tool is needed
// to answer — and therefore safe to demote to a vision fallthrough instead of
// planning it. The only such verdict is the document_read misroute itself
// (plus a screenshot secondary, which the host already staged the bytes for).
//
// This must NOT be widened to "any read-only governed label". The predicate is
// consulted only from the document_read misroute path
// (semanticUnsatisfiableDocumentReadOnStagedImage), and a misrouted
// document_read verdict can legitimately carry a *real* second capability as a
// low-confidence secondary — knowledge_read, file_read, git_inspect,
// audio_transcribe, current_time, audit_read, worklog_read. Those are
// satisfiable on an image-only turn (they need no document), so demoting the
// whole turn would silently drop a tool the user asked for and is strictly
// worse than letting the governed planner keep it (where it either succeeds or
// fails closed on its own terms). Lookup labels are already excluded here, but
// the rule below is the blunt instrument: anything other than
// document_read/screenshot fails the demotion. (2026-10-11 R7)
func semanticStagedImageDemotableVerdict(result intent.ClassificationResult) bool {
	saw := false
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() {
			continue
		}
		switch label {
		case intent.LabelDocumentRead, intent.LabelScreenshot:
			saw = true
		default:
			return false
		}
	}
	return saw
}

func semanticWeakImageUnderstandClassification(result intent.ClassificationResult) bool {
	if strings.TrimSpace(result.WorkflowType) != "" {
		return false
	}
	if semanticClassificationMeetsResolverFloor(result) || semanticClassificationPlansBelowResolverFloor(result) {
		return false
	}
	return semanticImageUnderstandCapabilityLabels(result)
}

// semanticTurnHasHostStagedImage is true only when this turn has image bytes
// the model can see. A desktop picker path is a staging hint, not a photo:
// using it as the vision fact forced entry to demote weather to unknown and
// then parse English host notes to undo that. Failed loads and pre-materialize
// turns keep the user's lookup/generate classification.
func semanticTurnHasHostStagedImage(_ string, attachments []MessageAttachment) bool {
	return semanticTurnHasImageAttachment(attachments)
}

func hostTurnSelectedLocalImage(userText string, attachments []MessageAttachment) bool {
	return len(selectedLocalImagePaths(userText)) > 0 || semanticTurnHasImageAttachment(attachments)
}

func semanticTurnHasImageAttachment(attachments []MessageAttachment) bool {
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Data) == "" {
			continue
		}
		if isImageMime(attachment.MimeType) || (strings.EqualFold(strings.TrimSpace(attachment.Type), "image") && strings.TrimSpace(attachment.MimeType) == "") {
			return true
		}
	}
	return false
}

// semanticClassificationMeetsResolverFloor is an L2/resolver-grade verdict:
// not degraded and ≥ 0.78. Tree scores do not live on this scale.
func semanticClassificationMeetsResolverFloor(result intent.ClassificationResult) bool {
	return !result.Degraded && result.Confidence >= imSemanticMinimumConfidence
}

// semanticTreeConfirmedClassification is a successful L3/fusion verdict.
// Classifier: after an ambiguous L2, the tree is the route authority. Its
// score lives on a different scale than L2 early-exit 0.78. A tree pick of
// 0.75 is normal, not a weak generate guess.
//
// The floor is semanticLookupHintFloor (0.70), the existing "real enough
// signal" line, not 0.50. Production tree success never sets Degraded; a
// 0.50 floor would mint document_read for 「图上有什么」 at 0.55. Degraded
// stays a miss: L3 timeout / skip-tree must not mint writes because Layer
// is 3.
func semanticTreeConfirmedClassification(result intent.ClassificationResult) bool {
	if result.Degraded {
		return false
	}
	if result.Layer != 3 && result.Layer != 23 {
		return false
	}
	return result.Confidence >= semanticLookupHintFloor
}

// semanticClassificationPlansBelowResolverFloor is the only way a score below
// 0.78 may still mint needs. 0.78 is the resolver/L2 write-grant floor, not a
// second vote after the tree already named the family. Read-only hints and
// degraded office hints at the lookup floor plan; every other degraded
// mutating family stays a miss.
//
// A declared lookup+generate/visual composite plans too — even under the 0.70
// tree floor. That shape is two independent authorities agreeing on the
// reviewed pair (L2 runner-up evidence plus the tree verdict on the lookup
// half), which is stronger than any single weak score; the classifier's
// synthesis comment calls dropping it at 0.68 the incident being fixed
// ("PDF 生成工具不可用", 2026-08-25), yet the planning gate here re-created
// exactly that drop for the web_fetch-verdict shape on 2026-08-28. The
// predicate still bounds what can be minted: only the declared pair's own
// legs. Degraded stays a miss — a timeout guess must not mint writes.
//
// A non-degraded, non-tree classification whose declared capability labels
// are all governed read-only labels plans as well, at any score below the
// floor (semanticSubFloorGovernedReadOnlyClassification). For L2 verdicts
// Degraded is the trust boundary: a non-degraded result means the embedding
// actually named this family (the 2026-08-28 张惠妹 turn: search at 0.69,
// layer 2, tree unavailable). Dropping it re-opened the legacy name router,
// which does not carry the managed tools at all, and the petition rescue
// could not fire without a semantic surface. The planning projection only
// mints the declared read-only legs; effectful capabilities such as
// generate_pdf/send_file still arrive through the petition path, with its
// budget and expansion validator as the safety boundary. Weak effectful
// labels (generate/office/bash …), tree scores under the 0.70 signal floor,
// and every Degraded shape stay a miss.
func semanticClassificationPlansBelowResolverFloor(result intent.ClassificationResult) bool {
	if semanticReadOnlyGovernedHint(result) || semanticOfficeGovernedHint(result) {
		return true
	}
	if semanticSubFloorGovernedReadOnlyClassification(result) {
		return true
	}
	if !result.Degraded && (semanticDeclaredLookupGenerateComposite(result) || semanticDeclaredLookupVisualComposite(result)) {
		return true
	}
	return semanticTreeConfirmedClassification(result)
}

// semanticSubFloorGovernedReadOnlyClassification is the defect-1 planning
// gate: not degraded, a non-tree verdict, and every declared capability label
// is a governed read-only label. Tree/fusion scores (layer 3/23) live on a
// different scale and stay governed by semanticTreeConfirmedClassification's
// 0.70 signal floor — a 0.55 tree pick is noise, not a hint. For an L2
// verdict, non-degraded is the trust signal: the embedding actually named
// this family and the tree was unavailable. The read-only set is the single
// source shared with the petition gate, so a label either is or is not
// read-only governed everywhere. Non-capability labels
// (unknown/continuation/…) ride along.
func semanticSubFloorGovernedReadOnlyClassification(result intent.ClassificationResult) bool {
	if result.Degraded {
		return false
	}
	if result.Layer == 3 || result.Layer == 23 {
		return false
	}
	return semanticReadOnlyGovernedFamily(result)
}

// semanticLookupClassificationForPlanning is a resolver-only projection.
// IntentLabelCapabilityNeedResolver fail-closes on Degraded or confidence
// below 0.78. A tree-confirmed or read-only hint that already passed the
// planning gate must still resolve needs. The original classification
// remains the replan/evidence record.
func semanticLookupClassificationForPlanning(result intent.ClassificationResult) intent.ClassificationResult {
	out := result
	out.Degraded = false
	if out.Confidence < imSemanticMinimumConfidence {
		out.Confidence = imSemanticMinimumConfidence
	}
	return out
}

// semanticBareContinueQuery reports a bare continue/retry token after
// unwrapping acpProgrammingUserText. The workspace template is not a continue.
func semanticBareContinueQuery(userText string) bool {
	q := strings.ToLower(strings.TrimSpace(acpInnerUserRequest(userText)))
	q = strings.TrimSpace(strings.Trim(q, "。.!！?？~～、,，;；:：\"'“”‘’…⋯"))
	switch q {
	case "继续", "繼續", "continue", "重试", "重試", "retry", "请继续", "請繼續":
		return true
	default:
		return false
	}
}

// markdownFileWritePlanningText maps a bare "继续" onto the previous
// markdown-file write so leftover/planner still HostKeep write_file.
// Production 2026-09-21: the GUI asked the user to reply 继续 after
// write_file grants failed; the next turn's query was 继续, which does not
// match QueryWantsMarkdownFile. ACP Mode B wraps the same 继续 in
// acpProgrammingUserText; leftover must see the User request body, not the
// workspace template (it contains ASCII "write").
func markdownFileWritePlanningText(userText string, history []agent.ConversationEntry) string {
	inner := acpInnerUserRequest(userText)
	if tool.QueryWantsMarkdownFile(inner) {
		return inner
	}
	if !semanticBareContinueQuery(inner) {
		return inner
	}
	for i := len(history) - 1; i >= 0; i-- {
		if strings.ToLower(strings.TrimSpace(history[i].Role)) != "user" {
			continue
		}
		text := acpInnerUserRequest(markdownHistoryUserText(history[i].Content))
		if text == "" || semanticBareContinueQuery(text) {
			continue
		}
		if tool.QueryWantsMarkdownFile(text) {
			return text
		}
		return inner
	}
	return inner
}

// markdownHistoryUserText reads user prose only. fmt.Sprint of maps/structs
// can contain "markdown" as a key name and must not count as a write request.
func markdownHistoryUserText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		return strings.TrimSpace(conversationEntryText(v))
	case map[string]interface{}:
		s, _ := v["text"].(string)
		return strings.TrimSpace(s)
	default:
		return ""
	}
}

// semanticLocalFileDeletePlanningClassification projects removal of one
// workspace file onto file_delete. Query evidence outranks a weak file_write
// or memory_manage verdict. The granted tool is delete_file, not a shell and
// not a rewrite.
func semanticLocalFileDeletePlanningClassification(result intent.ClassificationResult) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	out.Primary = intent.LabelFileDelete
	kept := make([]intent.IntentLabel, 0, len(out.Secondary))
	for _, label := range out.Secondary {
		switch label {
		case intent.LabelFileWrite, intent.LabelDocumentGenerate, intent.LabelMemoryManage, intent.LabelShellCommand:
			continue
		}
		kept = append(kept, label)
	}
	out.Secondary = kept
	return out
}

// semanticPlanningClassification applies query evidence before the resolver
// floor. File delete wins, then a schedule whose body is a mailbox delete,
// then an immediate mailbox-message delete, then an email send (including a
// PDF that must be generated and then mailed), then a markdown file write,
// then a worklog record, then a web lookup whose body is a document to read.
// A follow-up that does not itself name the format is left unchanged, so a
// bare 「真读内容」 cannot strip labels the entry hook already stored.
func semanticPlanningClassification(classification intent.ClassificationResult, planningQuery string) intent.ClassificationResult {
	if tool.QueryWantsLocalFileDelete(planningQuery) {
		return semanticLocalFileDeletePlanningClassification(classification)
	}
	if rewritten := semanticGovernedEffectPlanningRewrite(&classification, planningQuery); rewritten != nil {
		return *rewritten
	}
	if tool.QueryWantsMarkdownFile(planningQuery) {
		return semanticMarkdownFileWritePlanningClassification(classification, planningQuery)
	}
	if kind := classifyWorklogRecord(planningQuery); kind != worklogRecordNone {
		return semanticWorklogPlanningClassification(classification, kind)
	}
	if rewritten := semanticFetchedDocumentReadRewrite(&classification, planningQuery); rewritten != nil {
		return *rewritten
	}
	return classification
}

// semanticMarkdownFileWritePlanningClassification projects a markdown-file
// write onto LabelFileWrite. Query evidence is stronger than a weak tree
// score or an L2 document_generate collision. Strip generate, office, and
// attachment delivery unless the prompt names an Office source to read.
func semanticMarkdownFileWritePlanningClassification(result intent.ClassificationResult, userText string) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	out.Primary = intent.LabelFileWrite
	keepOffice := tool.QueryMentionsOfficeSource(userText)
	kept := make([]intent.IntentLabel, 0, len(out.Secondary))
	for _, label := range out.Secondary {
		switch label {
		case intent.LabelDocumentGenerate, intent.LabelFileWrite, intent.LabelAttachmentDelivery:
			continue
		case intent.LabelOffice:
			if !keepOffice {
				continue
			}
		}
		kept = append(kept, label)
	}
	out.Secondary = kept
	return out
}

// semanticSSHDegradedPlanningClassification projects a degraded
// classification that carries an ssh signal into one that may resolve needs
// during a petition re-plan: ssh itself plus the governed read-only families
// (the same set the below-floor lookup gate admits) survive; every other
// mutating capability label is dropped, so a classifier-timeout guess still
// cannot mint bash or file writes through this exemption. Degraded is cleared
// and the confidence is raised to the resolver floor, exactly like the lookup
// projection — the authorization evidence here is the petition itself
// (model-named tool, effectful budget spent, provider mode bounding the
// surface), not the classifier score.
func semanticSSHDegradedPlanningClassification(result intent.ClassificationResult) intent.ClassificationResult {
	out := semanticLookupClassificationForPlanning(result)
	if !out.Primary.IsNonCapabilityLabel() && out.Primary != intent.LabelSSH && !semanticReadOnlyGovernedLabel(out.Primary) {
		out.Primary = intent.LabelUnknown
	}
	kept := make([]intent.IntentLabel, 0, len(out.Secondary))
	for _, label := range out.Secondary {
		if label == intent.LabelSSH || label.IsNonCapabilityLabel() || semanticReadOnlyGovernedLabel(label) {
			kept = append(kept, label)
		}
	}
	out.Secondary = kept
	return out
}

func classificationHasLabel(result intent.ClassificationResult, label intent.IntentLabel) bool {
	return result.HasLabel(label)
}

// classificationHasSSHSignal reports whether the classification carries an
// ssh signal in any machine-readable field: declared labels, or the L2
// RunnerUp escalation evidence preserved by the classifier's tree-timeout
// collapse (2026-09-18 — previously the signal survived only in the Reason
// text and every availability layer stayed dark). RunnerUp is never an
// authorized intent; honoring it here IS the reviewed ssh exemption path.
func classificationHasSSHSignal(result intent.ClassificationResult) bool {
	if classificationHasLabel(result, intent.LabelSSH) {
		return true
	}
	return result.RunnerUp == intent.LabelSSH && result.RunnerUpScore >= intent.EmbeddingLookupMinScore
}

// imSemanticIntentIsManagedForLoop is the shared workflow gate for dispatcher
// loop selection and semanticPlanForTurn. A WorkflowAgentLoop turn that also
// carries document_generate must not become semantically managed: stage PDFs
// stay on generate_pdf+[file_base64].
func imSemanticIntentIsManagedForLoop(workflowAgentLoop bool, result intent.ClassificationResult) bool {
	if workflowAgentLoop && classificationHasLabel(result, intent.LabelDocumentGenerate) {
		return false
	}
	return imSemanticIntentIsManaged(result)
}

// loopContextIsSemanticManaged reports whether this turn already carries a
// capability-managed UIC result. Injection, session-pin, and recover-prompt
// tool rebuilds must not reopen the name router on that surface.
func loopContextIsSemanticManaged(ctx *LoopContext) bool {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil {
		return false
	}
	return imSemanticIntentIsManagedForLoop(ctx.WorkflowAgentLoop, *ctx.Runtime.SemanticIntent)
}

func imSemanticUnmappedCapabilityLabel(result intent.ClassificationResult) (intent.IntentLabel, bool) {
	_, unmapped := imSemanticIntentCoverage(result)
	if unmapped == "" {
		return "", false
	}
	return unmapped, true
}

// semanticNeedsFromClassification uses the same governed label-template
// resolver as the dynamic Skill/MCP path. Classification determines only a
// capability outcome; legacy ToolNames deliberately have no input path here.
func semanticNeedsFromClassification(registry *tool.CapabilityRegistry, result intent.ClassificationResult) ([]tool.CapabilityNeed, bool, error) {
	return semanticNeedsFromClassificationContext(context.Background(), registry, result)
}

func semanticBaselineWorkspaceApplies(result intent.ClassificationResult) bool {
	// "继续查一下天气" keeps the open document as a secondary label. Bash and
	// write_file belong to an office or shell turn, not to that lookup.
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelCurrentTime:
		return false
	}
	for _, label := range result.Labels() {
		switch label {
		case intent.LabelOffice, intent.LabelFileRead, intent.LabelFileWrite,
			intent.LabelShellCommand, intent.LabelDelegateTask, intent.LabelDocumentRead:
			return true
		}
	}
	return false
}

// semanticApplyBaselineWorkspace is the single ceiling decision. A petition
// expansion replays its own recorded bit. A failure replan forces the ceiling
// only when the parent stored it, so a classification that would not re-derive
// a ceiling-only raise (no zz-baseline prefix) cannot drop those siblings.
// With neither key set, the classification decides. A slim office turn never
// grows the ceiling.
func semanticApplyBaselineWorkspace(ctx context.Context, planning intent.ClassificationResult, slimOffice bool) bool {
	if semanticPetitionExpansion(ctx) {
		return semanticPetitionBaseline(ctx)
	}
	if semanticForceBaselineWorkspace(ctx) {
		return !slimOffice
	}
	return semanticBaselineWorkspaceApplies(planning) && !slimOffice
}

// semanticNeedsFromClassificationContext is the request-bound counterpart of
// semanticNeedsFromClassification.  Need extraction currently uses a supplied
// UIC result, but it must still share the incoming turn context with future
// semantic-contract/index resolvers.  In particular, a cancelled turn must not
// keep a catalog/planning operation alive just because the current resolver is
// locally pure.
func semanticNeedsFromClassificationContext(ctx context.Context, registry *tool.CapabilityRegistry, result intent.ClassificationResult) ([]tool.CapabilityNeed, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Some trusted callers supply a previously classified result directly,
	// bypassing the normal UIC return path. Normalize this local copy so every
	// planner entry observes the same declared lookup+document dependency,
	// without changing the caller-owned classification.
	intent.NormalizeDeclaredComposite(&result)
	resolver := &agentservice.IntentLabelCapabilityNeedResolver{
		Classifier: imSemanticIntentSource{result: result}, Registry: registry,
		Rules: imSemanticIntentRuleSet, MinimumConfidence: imSemanticMinimumConfidence,
		AmbientRetrieval: true, ArchetypeBundles: true,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(ctx, agentservice.DynamicCapabilityNeedRequest{UserText: "im semantic routing"})
	if err != nil {
		return nil, false, err
	}
	return resolution.Needs, resolution.Managed, nil
}

var semanticArchetypeBundles = agentservice.SemanticArchetypeBundles()

func semanticArchetypeBundleKey(result intent.ClassificationResult) intent.IntentLabel {
	// A lookup that only inherited an open document must not grow the document
	// bundle. "继续查一下天气" keeps the office tool and the search, not
	// download_file and read_file. A user-declared office composite still does.
	if semanticResidueInheritedLookup(result) {
		return result.Primary
	}
	return agentservice.ArchetypeBundleKey(result)
}

const semanticNoCompanionBundle intent.IntentLabel = "no_companion_bundle"

func semanticArchetypeBundleKeyForTurn(result intent.ClassificationResult, userText string) intent.IntentLabel {
	if semanticResidueSlimOfficeTurn(result, userText) {
		return semanticNoCompanionBundle
	}
	return semanticArchetypeBundleKey(result)
}

func withoutDocumentMutationNeeds(needs []tool.CapabilityNeed, keep map[tool.CapabilityID]bool) []tool.CapabilityNeed {
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if keep[need.Capability] {
			out = append(out, need)
			continue
		}
		switch need.Capability {
		case tool.CapabilityDocumentWriteOffice, tool.CapabilityFSWriteLocal, agentservice.CapabilityDocumentGenerate, agentservice.CapabilityArtifactDeliverCurrent:
			continue
		default:
			out = append(out, need)
		}
	}
	return out
}

func withSlimDocumentRead(registry *tool.CapabilityRegistry, needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	for _, need := range needs {
		if need.Capability == tool.CapabilityFSReadLocal {
			return needs
		}
	}
	if registry == nil {
		return needs
	}
	if _, exists := registry.Lookup(tool.CapabilityFSReadLocal); !exists {
		return needs
	}
	out := append([]tool.CapabilityNeed(nil), needs...)
	return append(out, tool.CapabilityNeed{
		ID:          "need:~slim-document-read",
		Capability:  tool.CapabilityFSReadLocal,
		Polarity:    tool.NeedRequire,
		Required:    false,
		Confidence:  1,
		EvidenceIDs: []string{"residue:document-read"},
	})
}

// semanticNeedsWithDynamicLookupForProviders attaches the open read-only
// family's optional need when a USABLE dynamic provider actually serves it.
// One provider is enough — the planner picks the best quality — so two
// installed MCP servers do not multiply needs. The contract-only graft
// removes this need when it lists each installed tool: leaving it would
// force-call one arbitrary sibling beside those tools.
//
// It deliberately shares providerProvisionList with the graft below: both need
// "installed and this planner can select it". Before sharing, this looked at
// every published provision including ones whose observation had failed, so a
// down-only server still put the need on the plan — the planner would then
// select nothing and record an unmet row whose reason ("not ready") pointed at
// a capability the turn never actually wanted.
func semanticNeedsWithDynamicLookupForProviders(needs []tool.CapabilityNeed, providers []tool.ProviderSpec) []tool.CapabilityNeed {
	for _, need := range needs {
		if need.Capability == agentservice.CapabilityInformationLookup {
			return needs
		}
	}
	for _, provision := range providerProvisionList(providers) {
		if provision == agentservice.CapabilityInformationLookup {
			return append(needs, agentservice.AmbientDynamicLookupNeed())
		}
	}
	return needs
}

// semanticNeedsWithContractOnlyGraft gives a required need that only a user
// installed implementation can serve a second chance through installed and
// already published provider, instead of failing the turn.
//
// The problem it solves: an intent may demand a capability the host never owns
// and that only a reviewed MCP tool can provide. When nothing is installed, the
// turn ends in no_feasible_provider before the model is called once — which is
// indistinguishable, from the outside, from "no such ability exists". Shipping
// that for a family whose entire purpose is to be served by whichever backend
// the site installed makes the product unusable on any MCP the reviewers did
// not happen to pre-approve by name.
//
// What makes this safe rather than a blanket fallback:
//
//  1. Only a reviewed contract-only capability qualifies. These have no host
//     attach site on any instance, so the need cannot be re-satisfied at all
//     without an installed provider.
//  2. Only when that capability genuinely has no *ready* provider in this
//     turn's own finished provider list. This is what would have rejected the
//     turn. "Ready" excludes a tool whose latest observation finished negative:
//     a down tool must not suppress the graft, because the graft's whole job is
//     to reach whatever installed implementation can still answer.
//  3. Only through an installed information.lookup tool — the reviewed family
//     whose meaning is "call whatever tool was installed". Each observed tool
//     is published under TrustTierUnreviewed. Its effect class is the
//     execution boundary, not a reachability veto: an unannotated tool is
//     external_effect and still answers a read, and the call keeps that
//     effect so it settles through the receipt boundary instead of running
//     as a silent read. A read-only tool still cannot carry a write.
//  4. Never over a pin of this capability itself. The user named an
//     implementation of the contract-only need, and an open lookup tool would
//     answer a different question. A pin of some other capability — a server
//     mentioned beside the request, or a lookup pin — does not hide installed
//     tools and does not leave the need required. Each exact capability
//     already names one tool, and the planner applies a pin only to the
//     capability it was derived for.
//
// The original unservable need is relaxed to optional in the same pass.
// Installing nothing still fails cleanly. A reviewed provider of the exact
// capability keeps its binding and never reaches this path.
//
// When any carrying tool has a registry-known x:mcp:<server>:<tool>
// capability, each such tool becomes its own optional need. A sibling
// without one does not collapse the set onto one information.lookup winner:
// that winner is force-called and may be a different tool. The shared family
// is used only when no carrying tool has a usable exact capability. An
// ambient information.lookup need already on the turn is that shared family,
// so it is not demanded again. When exact tools are listed, that ambient
// need is removed so its force-call cue does not also invoke an arbitrary
// sibling.
func semanticNeedsWithContractOnlyGraft(needs []tool.CapabilityNeed, providers []tool.ProviderSpec, pins []tool.ProviderPin, registry *tool.CapabilityRegistry) []tool.CapabilityNeed {
	if len(needs) == 0 || len(providers) == 0 {
		return needs
	}
	usable := providerProvisionList(providers)
	if len(usable) == 0 {
		return needs
	}
	// The graft target must itself be installed and ready, or adding the need
	// just moves the failure one step later with a worse explanation. "Ready"
	// here is what the planner will accept, so this is also the honest answer
	// to "can this turn call anything installed right now".
	var graftTargetPublished bool
	for _, provision := range usable {
		if provision == agentservice.CapabilityInformationLookup {
			graftTargetPublished = true
			break
		}
	}
	if !graftTargetPublished {
		return needs
	}
	// Ready lookup tools are the only honest replacements. A down tool must
	// not license relaxing an obligation the planner is forbidden to select.
	lookupProviders := readyInstalledLookupProviders(providers)
	out := append([]tool.CapabilityNeed(nil), needs...)
	// Deterministic per-capability iteration: map iteration order varies, and
	// the graft ID must be stable across runs or plans stop being reproducible.
	type contractOnlyFallback struct {
		indices  []int
		exposure []tool.ProviderSpec
	}
	fallbacks := map[tool.CapabilityID]contractOnlyFallback{}
	for index := range out {
		need := out[index]
		if !need.Required {
			continue
		}
		if !agentservice.IsReviewedContractOnlyCapability(need.Capability) {
			continue
		}
		if need.Capability == agentservice.CapabilityInformationLookup {
			continue
		}
		if _, pinned := tool.PinForCapability(pins, need.Capability); pinned {
			continue
		}
		if providerExistsForCapability(providers, need.Capability) {
			continue
		}
		// The replacement must be able to do the job the need describes. An
		// unreadable descriptor means we cannot prove that, and proving it is
		// the whole license for relaxing the obligation, so stay closed.
		descriptor, declared := registry.Lookup(need.Capability)
		if !declared {
			continue
		}
		exposure := installedLookupExposure(lookupProviders, descriptor.Effects)
		if len(exposure) == 0 {
			continue
		}
		fallback := fallbacks[need.Capability]
		fallback.indices = append(fallback.indices, index)
		fallback.exposure = exposure
		fallbacks[need.Capability] = fallback
	}
	capabilities := make([]tool.CapabilityID, 0, len(fallbacks))
	for capability := range fallbacks {
		capabilities = append(capabilities, capability)
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	// One graft set per CAPABILITY, not per need. Two required needs for the
	// same capability must not mint duplicate ids: the planner rejects those
	// outright, which would turn a rescuable turn into a hard error.
	//
	// The obligation is downgraded, not merely supplemented. Leaving the
	// original Required need in the plan records it as Unmet, and the turn is
	// refused exactly as before. This capability has no reviewed provider on
	// this host, so keeping it obligatory only reproduces the failure the
	// graft exists to prevent.
	var changed bool
	planned := make([][]tool.CapabilityNeed, 0, len(capabilities))
	anyExact := false
	for _, capability := range capabilities {
		fallback := fallbacks[capability]
		for _, index := range fallback.indices {
			out[index].Required = false
			changed = true
		}
		grafts := installedMCPFallbackNeeds(capability, fallback.exposure, registry)
		if fallbackNeedsAreExact(grafts) {
			anyExact = true
		}
		planned = append(planned, grafts)
	}
	// Exact tools replace the ambient open-family need. Its reuse cue
	// force-calls one quality-ranked winner, which can be a mutating sibling
	// of the tool the request named.
	if anyExact {
		out = dropAmbientInformationLookup(out)
	}
	seenGraftID := map[string]bool{}
	for _, grafts := range planned {
		// The shared family beside exact tools reintroduces that force-called
		// winner. Use it only when nothing exact was available.
		if anyExact && !fallbackNeedsAreExact(grafts) {
			continue
		}
		for _, graft := range grafts {
			if seenGraftID[graft.ID] || capabilityNeedPresent(out, graft.Capability) {
				continue
			}
			seenGraftID[graft.ID] = true
			out = append(out, graft)
			changed = true
		}
	}
	if !changed {
		return needs
	}
	return out
}

// readyInstalledLookupProviders returns ready providers of the open lookup
// family, in stable binding order.
func readyInstalledLookupProviders(providers []tool.ProviderSpec) []tool.ProviderSpec {
	out := make([]tool.ProviderSpec, 0)
	for _, provider := range providers {
		if !provider.Ready {
			continue
		}
		for _, provision := range provider.Provides {
			if provision.Capability == agentservice.CapabilityInformationLookup {
				out = append(out, provider)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Binding.StableID() < out[j].Binding.StableID() })
	return out
}

// installedLookupExposure is the ready lookup tools that can perform the
// need. Effect class is an execution boundary: an external-effect tool can
// answer a read, and a read-only tool cannot answer a write. Server pins are
// not applied here. A pin belongs to one capability, and applying it to every
// carrying tool hid the server that could do the job — or left the need
// required, which is the catalog miss this graft exists to prevent.
func installedLookupExposure(carriers []tool.ProviderSpec, needEffects []tool.EffectClass) []tool.ProviderSpec {
	matching := make([]tool.ProviderSpec, 0, len(carriers))
	for _, provider := range carriers {
		if lookupProviderCarriesNeed(provider.Effects, needEffects) {
			matching = append(matching, provider)
		}
	}
	return matching
}

// lookupProviderCarriesNeed reports whether one installed lookup tool can
// perform the need. Effect class is the execution boundary. An external-effect
// tool can answer a read and still confirms through the receipt path. A
// read-only tool cannot answer a write: relaxing that obligation would let
// the turn proceed and silently not write.
func lookupProviderCarriesNeed(providerEffects, needEffects []tool.EffectClass) bool {
	if tool.EffectsWithinCeiling(needEffects, providerEffects) {
		return true
	}
	if len(needEffects) == 1 && needEffects[0] == tool.EffectReadOnly {
		for _, effect := range providerEffects {
			if effect == tool.EffectExternalEffect {
				return true
			}
		}
	}
	return false
}

// installedMCPFallbackNeeds is the demand side of an unserved contract-only
// capability. A tool whose x:mcp capability is registered is demanded by
// that capability, so the model can call the tool named in the description.
// A sibling without a registered capability does not pull the others onto
// the shared family: that family force-calls one quality-ranked winner.
// The shared family is used only when no carrying tool has a usable exact
// capability.
func installedMCPFallbackNeeds(capability tool.CapabilityID, providers []tool.ProviderSpec, registry *tool.CapabilityRegistry) []tool.CapabilityNeed {
	auto := make([]tool.CapabilityNeed, 0, len(providers))
	seen := map[tool.CapabilityID]bool{}
	for _, provider := range providers {
		id, ok := autoMCPCapabilityOf(provider)
		if !ok || registry == nil {
			continue
		}
		if _, declared := registry.Lookup(id); !declared {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		auto = append(auto, tool.CapabilityNeed{
			ID:          "need:~installed-mcp:" + string(id),
			Capability:  id,
			Polarity:    tool.NeedRequire,
			Required:    false,
			Confidence:  1,
			EvidenceIDs: []string{"installed_mcp:" + string(capability)},
		})
	}
	if len(auto) > 0 {
		sort.Slice(auto, func(i, j int) bool { return auto[i].ID < auto[j].ID })
		return auto
	}
	graft := agentservice.AmbientDynamicLookupNeed()
	graft.ID = "need:~installed-fallback:" + string(capability)
	graft.EvidenceIDs = append(graft.EvidenceIDs, "installed_fallback:"+string(capability))
	return []tool.CapabilityNeed{graft}
}

// fallbackNeedsAreExact reports whether grafts name installed tools rather
// than the shared information.lookup family.
func fallbackNeedsAreExact(needs []tool.CapabilityNeed) bool {
	if len(needs) == 0 {
		return false
	}
	for _, need := range needs {
		if !strings.HasPrefix(string(need.Capability), "x:mcp:") {
			return false
		}
	}
	return true
}

// dropAmbientInformationLookup removes the optional open-family need the
// dynamic-lookup pass attaches. It leaves a required lookup need alone.
func dropAmbientInformationLookup(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	dropped := false
	for _, need := range needs {
		if isAmbientInformationLookupNeed(need) {
			dropped = true
			continue
		}
		kept = append(kept, need)
	}
	if !dropped {
		return needs
	}
	return kept
}

func isAmbientInformationLookupNeed(need tool.CapabilityNeed) bool {
	if need.Required {
		return false
	}
	ambient := agentservice.AmbientDynamicLookupNeed()
	return need.ID == ambient.ID && need.Capability == ambient.Capability
}

func capabilityNeedPresent(needs []tool.CapabilityNeed, capability tool.CapabilityID) bool {
	for _, need := range needs {
		if need.Capability == capability {
			return true
		}
	}
	return false
}

// autoMCPCapabilityOf returns the host-issued address of this tool,
// x:mcp:<server>:<tool>. A vendor may also declare an x:mcp capability.
// Picking the lexicographically smallest one lets that declaration steal the
// address, so the model is shown a different tool than the binding that runs.
// The host address wins when this provider publishes it. A provider that only
// has some other x:mcp provision keeps the smallest one, so a tool is not
// dropped solely because its id was not derived from the binding.
func autoMCPCapabilityOf(provider tool.ProviderSpec) (tool.CapabilityID, bool) {
	want := agentservice.AutoMCPCapabilityIDForBinding(provider.Binding.ProviderID, provider.Binding.ImplementationID)
	var found tool.CapabilityID
	for _, provision := range provider.Provides {
		if want != "" && provision.Capability == want {
			return want, true
		}
		if !strings.HasPrefix(string(provision.Capability), "x:mcp:") {
			continue
		}
		if found == "" || provision.Capability < found {
			found = provision.Capability
		}
	}
	return found, found != ""
}

// providerProvisionList flattens the capabilities this turn's published,
// usable providers declare. It excludes a provider whose latest observation
// finished negative (RuntimeBlocked projects to Ready=false): a down or
// quarantined tool is precisely the case the graft exists to route around, so
// counting it here would make "something was installed that serves this" true
// on a provider the planner is forbidden to select. Keeping one definition of
// "usable" is also what stops the graft and the pruning below from disagreeing.
func providerProvisionList(providers []tool.ProviderSpec) []tool.CapabilityID {
	var out []tool.CapabilityID
	for _, provider := range providers {
		if !provider.Ready {
			continue
		}
		for _, provision := range provider.Provides {
			out = append(out, provision.Capability)
		}
	}
	return out
}

// providerExistsForCapability reports whether any published, ready provider of
// this turn can serve capability. It deliberately matches need.Capability only
// and ignores qualifiers: the reviewed work-log contracts declare no qualifiers,
// so a qualifier-sensitive test here would report "not served" for the one
// binding that is installed for it and graft over it on every turn.
func providerExistsForCapability(providers []tool.ProviderSpec, capability tool.CapabilityID) bool {
	for _, provision := range providerProvisionList(providers) {
		if provision == capability {
			return true
		}
	}
	return false
}

// logSemanticUnmetWithInventory records the installed side of an unserved-need
// refusal, so "nothing can serve this" and "the MCP that can serve this is
// installed and healthy but nothing asked for it" are distinguishable in logs.
//
// It reads only already-published contract entries, so it can never widen a
// surface: it is observation of what the turn could have used, nothing more.
func logSemanticUnmetWithInventory(unmet []tool.UnmetNeed, inventory semanticDynamicInventory) {
	unserved := make([]string, 0, len(unmet))
	for _, item := range unmet {
		if item.ReasonCode == "no_feasible_provider" || agentservice.IsReviewedContractOnlyCapability(tool.CapabilityID(needCapabilityFromUnmetID(item.NeedID))) {
			unserved = append(unserved, item.NeedID+"="+item.ReasonCode)
		}
	}
	if len(unserved) == 0 {
		return
	}
	servers := make(map[string]int, len(inventory.mcpEntries))
	for _, entry := range inventory.mcpEntries {
		servers[strings.TrimSpace(entry.ServerID)]++
	}
	names := make([]string, 0, len(servers))
	for serverID, tools := range servers {
		if serverID == "" {
			continue
		}
		names = append(names, serverID+"("+strconv.Itoa(tools)+")")
	}
	sort.Strings(names)
	// A set both dedupes and sorts deterministically, so two identical failures
	// produce byte-identical log lines and are greppable by either half.
	servingSet := make(map[string]bool, len(inventory.mcpEntries))
	for _, entry := range inventory.mcpEntries {
		for _, provision := range entry.Contract.Provisions {
			if agentservice.IsReviewedContractOnlyCapability(provision.Capability) {
				servingSet[string(provision.Capability)] = true
			}
		}
	}
	serving := make([]string, 0, len(servingSet))
	for capability := range servingSet {
		serving = append(serving, capability)
	}
	sort.Strings(serving)
	log.Printf("[semantic-routing] unserved needs unmet=%v installed_mcp=%v contract_only_published=%v",
		unserved, names, serving)
}

// needCapabilityFromUnmetID recovers the capability from a need id of the form
// "need:<capability>:<hash>". It is only used for logging, so an unexpected
// shape degrades to an empty string rather than failing the turn.
func needCapabilityFromUnmetID(needID string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(needID), "need:")
	if trimmed == "" {
		return ""
	}
	if cut := strings.Index(trimmed, ":"); cut > 0 {
		return trimmed[:cut]
	}
	return trimmed
}

// semanticPermitsDynamicLookup combines the two genuinely different reasons to
// withhold the open MCP family, and nothing else.
//
//  1. Outcome shape — intent.CallerWantsDynamicLookup: some results cannot be
//     produced by "call some vendor tool" at all. Encoded by what makes them
//     unsafe, not by listing every label that happens to work today.
//  2. Session continuity — a petition re-plan or a slim 继续 keeps the surface
//     the parent already had. Growing a vendor grant there reframes the turn
//     rather than extending it. This is about *this message continuing another*,
//     so it is the caller's own state, not a property of the intent label.
//
// Warehouse companions (WantsAmbientRetrieval) are a separate decision owned by
// the host and are deliberately not consulted here.
// slimOffice is passed in rather than recomputed: the caller already derived it
// from this turn's classification and text, and a second evaluation could drift
// from it and silently disagree about whether this is a continuing turn.
func semanticPermitsDynamicLookup(ctx context.Context, primary intent.IntentLabel, slimOffice bool) bool {
	if semanticPetitionExpansion(ctx) {
		return false
	}
	if slimOffice {
		return false
	}
	return intent.CallerWantsDynamicLookup(primary)
}

func withoutAmbientRetrievalNeeds(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	if len(needs) == 0 {
		return needs
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		ambient := false
		for _, evidence := range need.EvidenceIDs {
			if evidence == "ambient:retrieval" {
				ambient = true
				break
			}
		}
		if !ambient {
			out = append(out, need)
		}
	}
	return out
}

func semanticResidueInheritedLookup(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelCurrentTime:
	default:
		return false
	}
	return strings.Contains(result.Reason, "session residue")
}

func withSemanticArchetypeBundleKeyOverride(ctx context.Context, key intent.IntentLabel) context.Context {
	return agentservice.WithArchetypeBundleKeyOverride(ctx, key)
}

type semanticSlimOfficeContextKey struct{}

type semanticSlimOfficeContext struct {
	slim      bool
	shortEdit bool
}

type semanticPetitionedLabelKey struct{}

func withSemanticPetitionedLabel(ctx context.Context, label intent.IntentLabel) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticPetitionedLabelKey{}, label)
}

func semanticPetitionedKeep(ctx context.Context) map[tool.CapabilityID]bool {
	if ctx == nil {
		return nil
	}
	label, ok := ctx.Value(semanticPetitionedLabelKey{}).(intent.IntentLabel)
	if !ok || label == "" {
		return nil
	}
	templates := imSemanticIntentRuleSet[label]
	if len(templates) == 0 {
		return nil
	}
	keep := make(map[tool.CapabilityID]bool, len(templates))
	for _, template := range templates {
		keep[template.Capability] = true
	}
	return keep
}

func semanticDocumentMutationLabel(label intent.IntentLabel) bool {
	switch label {
	case intent.LabelOffice, intent.LabelFileWrite, intent.LabelDocumentGenerate:
		return true
	default:
		return false
	}
}

func withSemanticSlimOffice(ctx context.Context, slim, shortEdit bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticSlimOfficeContextKey{}, semanticSlimOfficeContext{slim: slim, shortEdit: shortEdit})
}

func semanticOfficeTurnSlim(ctx context.Context, planning intent.ClassificationResult, userText string) (bool, bool) {
	if ctx != nil {
		if mode, ok := ctx.Value(semanticSlimOfficeContextKey{}).(semanticSlimOfficeContext); ok {
			return mode.slim, mode.shortEdit
		}
	}
	return semanticResidueSlimOfficeTurn(planning, userText), semanticResidueShortDocumentEdit(planning, userText)
}

func semanticArchetypeBundleKeyFor(ctx context.Context, result intent.ClassificationResult) intent.IntentLabel {
	return agentservice.ArchetypeBundleKeyFor(ctx, result)
}

func semanticArchetypeBundleNeeds(registry *tool.CapabilityRegistry, result intent.ClassificationResult, managed bool, needs []tool.CapabilityNeed, bundleKey intent.IntentLabel) []tool.CapabilityNeed {
	return agentservice.ExpandArchetypeBundleNeeds(registry, imSemanticIntentRuleSet, result, managed, needs, bundleKey)
}

// semanticTrustedArtifactInput is a channel-authorized payload normalized at
// ingress. The host-only adapter metadata may guide a native consumer, while
// the ArtifactRef is the sole plan/execution authority.
type semanticTrustedArtifactInput struct {
	Payload tool.ArtifactPayload
	Format  string // document reader qualifier for the current first consumer
	Suffix  string // executor-private temporary-file suffix for that consumer
}

func semanticDocumentInputsForTurn(rootTaskID, turnID, sessionID, principalID string, attachments []MessageAttachment) ([]semanticTrustedArtifactInput, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(principalID) == "" {
		return nil, fmt.Errorf("trusted_document_input_identity_required")
	}
	inputScope := tool.InvocationScope{
		RootTaskID:  rootTaskID,
		PlanID:      tool.TrustedInputPlanID(turnID),
		SessionID:   sessionID,
		TurnID:      turnID,
		PrincipalID: principalID,
	}
	inputs := make([]semanticTrustedArtifactInput, 0, len(attachments))
	for index, attachment := range attachments {
		format, mimeType, ok := agent.DocumentAttachmentFormat(attachment.FileName, attachment.MimeType)
		if !ok {
			continue
		}
		if strings.TrimSpace(attachment.Data) == "" {
			return nil, fmt.Errorf("trusted_document_attachment_content_missing")
		}
		// The canonical bytes are part of the ArtifactRef identity, but the
		// ingress producer still needs a stable per-slot identity to preserve
		// provenance when a channel has no media handle.
		sourceID := tool.TrustedAttachmentSourceID(index, attachment.FileName, mimeType, attachment.SourceMediaID)
		producer := "trusted-input:channel-attachment:" + tool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := tool.NewArtifactPayload(inputScope, producer, "document", mimeType, attachment.Data, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_document_attachment_invalid: %w", err)
		}
		inputs = append(inputs, semanticTrustedArtifactInput{Payload: payload, Format: format, Suffix: agent.DocumentAttachmentTempSuffix(attachment.FileName, format)})
	}
	return inputs, nil
}

func semanticNeedsForTrustedDocumentInputs(needs []tool.CapabilityNeed, inputs []semanticTrustedArtifactInput) ([]tool.CapabilityNeed, error) {
	if len(needs) == 0 {
		return nil, nil
	}
	hasGenerate := tool.InTurnArtifactProducerPresent(needs, tool.CapabilityDocumentWriteOffice)
	resolved := append([]tool.CapabilityNeed(nil), needs...)
	for index := range resolved {
		switch resolved[index].Capability {
		case "document.read.local":
		case "artifact.deliver.current_channel":
			if !tool.CurrentChannelDeliverAccepts(resolved[index], "file") {
				continue
			}
			if hasGenerate {
				// A generate/office-write file deliver consumes the in-turn
				// producer ArtifactRef. It must not look up a MessageAttachment.
				continue
			}
		default:
			continue
		}
		if err := tool.UniqueTrustedInputCount(len(inputs)); err != nil {
			return nil, err
		}
		resolved[index] = tool.BindDocumentReadFormat(resolved[index], inputs[0].Format)
	}
	return resolved, nil
}

// semanticCallSurfaceForSharedTurn builds the first migration family directly
// from semantic UIC output. It returns handled=false for capability families
// not yet declared, leaving those families on the explicitly temporary legacy
// path rather than mixing the two resulting tool lists.
func (h *IMMessageHandler) semanticCallSurfaceForSharedTurn(userID, userText, channel string) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithIdentity(userID, userText, channel, "", "")
}

func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithContext(ctx *LoopContext, userID, userText, channel string) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, userID, userText, channel, nil)
}

func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx *LoopContext, userID, userText, channel string, attachments []MessageAttachment) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	// A goal continuation already names goal(action=plan|step|complete|fail).
	// Classifying that host prompt as goal_manage publishes the closed
	// create/get/end adapter under the same capability and the lifecycle tool
	// never reaches the model. The execution-profile bypass is not enough:
	// this planner classifies again when SemanticIntent is empty. Leave the
	// turn on the legacy surface, which pins that lifecycle tool.
	loopPlatform := ""
	if ctx != nil {
		loopPlatform = ctx.Platform
	}
	if goalContinuationTurn(userText, channel) || goalContinuationTurn(userText, loopPlatform) {
		log.Printf("[semantic-routing] goal continuation keeps lifecycle goal tool user=%q channel=%q platform=%q", userID, channel, loopPlatform)
		return nil, nil, false, nil
	}
	if sessionRoundTurn(userText, channel) || sessionRoundTurn(userText, loopPlatform) {
		log.Printf("[semantic-routing] session round keeps the current tool surface user=%q channel=%q platform=%q", userID, channel, loopPlatform)
		return nil, nil, false, nil
	}
	// A background chat task is the rest of a turn the foreground loop already
	// refused to finish inline. Classifying the host prompt again often yields
	// one lookup family and the same short budget.
	if ctx != nil && strings.TrimSpace(ctx.ChatTaskID) != "" {
		log.Printf("[semantic-routing] background chat task keeps the full tool surface user=%q", userID)
		return nil, nil, false, nil
	}
	// The reply that approves an installed MCP write already has that tool
	// on the loop. Task-context merge classifies the short answer from the
	// surrounding task and would publish a different surface, or HostReject
	// it, instead of the function the approval hint listed.
	if discoveredMCPContinuationTurn(ctx) {
		log.Printf("[semantic-routing] discovered MCP continuation stays on the legacy surface user=%q", userID)
		return nil, nil, false, nil
	}
	// Identity and replacement generation form one host-private lifecycle
	// snapshot. Reading them separately would permit a fresh ingress to rotate
	// the identity between reads, then incorrectly register an old scope as the
	// new generation's surface. The later fence check would eventually revoke
	// it, but we must never present that cross-turn construction as current.
	invocation, turnGeneration := semanticLoopInvocationSnapshotFor(ctx)
	rootTaskID, turnID := invocation.RootTaskID, invocation.TurnID
	sessionID := invocation.SessionID
	// LoopContext.Context observes terminal cancellation only. Register a
	// sibling cancellation for fresh ingress replacement before any classifier,
	// catalog, or planner I/O starts, so an old request cannot finish planning
	// and publish a new surface after its replacement is accepted.
	// The routing values are taken after a late tree verdict is adopted.
	// Building them first planned the timeout profile: a project-task search
	// that arrived late never received web_fetch, and the light budget then
	// cleared the parent carry.
	replacement, cancelReplacement := context.WithCancel(context.Background())
	defer cancelReplacement()
	removeReplacementCancel := func() {}
	if ctx != nil {
		var current bool
		removeReplacementCancel, current = ctx.RegisterSemanticTurnReplacementCancel(turnGeneration, cancelReplacement)
		if !current {
			return nil, nil, true, fmt.Errorf("semantic_turn_replaced")
		}
	}
	defer removeReplacementCancel()
	h.releaseNamedSkillOnLoopIntent(ctx, userID, userText)
	if ctx != nil {
		h.adoptLateTreeSemanticIntent(ctx, userID, userText, ctx.History)
	}
	routed, cancelRouted := semanticRoutingContext(ctx)
	defer cancelRouted()
	requestCtx, cancel := context.WithCancel(routed)
	defer cancel()
	stopReplacement := context.AfterFunc(replacement, cancel)
	defer stopReplacement()
	// AfterFunc starts the cancel in another goroutine when the parent is
	// already done. A replacement that won during adoption must fail this
	// request before planning, not race the planner.
	if replacement.Err() != nil {
		cancel()
		return nil, nil, true, fmt.Errorf("semantic_turn_replaced")
	}
	planningText := semanticManagedPlanningText(userText, loopHistory(ctx))
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(requestCtx, userID, planningText, channel, rootTaskID, turnID, sessionID, semanticIntentFromLoopContext(ctx), attachments)
	if handled && err == nil && surface != nil && ctx != nil {
		h.noteSemanticSessionResidueCandidate(ctx, userID, channel, userText, surface.plan)
	}
	if handled && err == nil && surface != nil && ctx != nil {
		removeFence, current := ctx.RegisterSemanticTurnFence(turnGeneration, func() {
			cancelSemanticCallSurface(surface)
		})
		if !current {
			return nil, nil, true, fmt.Errorf("semantic_turn_replaced")
		}
		surface.removeTurnFence = removeFence
	}
	if !handled && err == nil {
		applySemanticChatProjection(ctx)
	}
	return defs, surface, handled, err
}

func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithIdentity(userID, userText, channel, rootTaskID, turnID string) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(userID, userText, channel, rootTaskID, turnID, nil)
}

func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithIdentityAndClassification(userID, userText, channel, rootTaskID, turnID string, classification *intent.ClassificationResult) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(userID, userText, channel, rootTaskID, turnID, classification, nil)
}

func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(userID, userText, channel, rootTaskID, turnID string, classification *intent.ClassificationResult, attachments []MessageAttachment) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(context.Background(), userID, userText, channel, rootTaskID, turnID, classification, attachments)
}

// semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments
// keeps compatibility callers on the background context while the production
// shared-loop entry supplies its cancellable turn context.  The request context
// is deliberately used only while deriving and publishing this new route; an
// already-published plan has its own durable recovery boundary.
func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(requestCtx context.Context, userID, userText, channel, rootTaskID, turnID string, classification *intent.ClassificationResult, attachments []MessageAttachment) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	return h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(requestCtx, userID, userText, channel, rootTaskID, turnID, semanticCompatibilitySessionID(turnID), classification, attachments)
}

// semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession
// is the trusted-identity entry point. Compatibility callers receive an
// ephemeral session identity that is intentionally independent from userID;
// live loops pass their host-owned SessionID below.
func (h *IMMessageHandler) semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(requestCtx context.Context, userID, userText, channel, rootTaskID, turnID, sessionID string, classification *intent.ClassificationResult, attachments []MessageAttachment) ([]map[string]interface{}, *semanticCallSurface, bool, error) {
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachmentsWithSession(requestCtx, userID, userText, channel, rootTaskID, turnID, sessionID, classification, attachments)
	if !handled || err != nil {
		return nil, nil, handled, err
	}
	definitions, surface, err := h.openPreparedSemanticCallSurface(requestCtx, userID, userText, channel, sessionID, classification, attachments, prepared)
	if err != nil {
		return nil, nil, true, err
	}
	return definitions, surface, true, nil
}

// openPreparedSemanticCallSurface publishes one already-planned preparation.
// Callers that must not enter semanticPlanForTurn — a legacy discover bind of
// an installed MCP tool is the case — reuse this tail so grant issuance,
// route-state registration, and the visible closure stay one mechanism.
// It does not note session residue: that record belongs to the managed turn
// that classified the user's text, not to a sidecar planned from a tool name.
func (h *IMMessageHandler) openPreparedSemanticCallSurface(requestCtx context.Context, userID, userText, channel, sessionID string, classification *intent.ClassificationResult, attachments []MessageAttachment, prepared *semanticPlanPreparation) ([]map[string]interface{}, *semanticCallSurface, error) {
	if h == nil || prepared == nil {
		return nil, nil, fmt.Errorf("semantic_scope_preparation_required")
	}
	if prepared.policy.Enabled && !prepared.policy.AllowsExecutableScope() {
		return nil, nil, fmt.Errorf("semantic_routing_policy_mode_%s", prepared.policy.Mode)
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	// Re-check the immutable scope immediately before any durable publication.
	// This closes the gap where a caller could mutate or replace the definition
	// map between planning and grant issuance; an incomplete closure must never
	// become a partially rendered executable surface.
	if err := validatePreparedSemanticToolScope(prepared); err != nil {
		return nil, nil, err
	}
	issuer, err := h.semanticInvocationIssuer()
	if err != nil {
		return nil, nil, err
	}
	hostCalls, err := h.semanticHostCallJournal()
	if err != nil {
		return nil, nil, err
	}
	coordinator, err := h.semanticExecutionCoordinator()
	if err != nil {
		return nil, nil, err
	}
	routeState, err := h.semanticRouteStateStore()
	if err != nil {
		return nil, nil, err
	}
	executor, err := h.semanticPlanExecutorWithRouteState(issuer, routeState)
	if err != nil {
		return nil, nil, err
	}
	artifactStore, err := h.semanticArtifactStore()
	if err != nil {
		return nil, nil, err
	}
	toolSnapshotID := strings.TrimSpace(prepared.plan.SnapshotDigest)
	if toolSnapshotID == "" {
		toolSnapshotID = strings.TrimSpace(prepared.plan.CatalogDigest)
	}
	scope := tool.InvocationScope{RootTaskID: prepared.rootTaskID, PlanID: prepared.plan.ID, SessionID: sessionID, TurnID: prepared.turnID, PrincipalID: userID, ToolSnapshotID: toolSnapshotID}
	var parent *tool.RouteRevisionRef
	if current, currentErr := routeState.CurrentRevision(scope); currentErr == nil {
		// Publish and route-state registration live in distinct durable stores.
		// Before advancing to a child revision, repair the sole recoverable gap:
		// a completed producer may have published a scoped artifact just before a
		// process loss prevented its metadata registration. This enumerates only
		// the current revision's completed producers, never a model-controlled
		// artifact lookup.
		if _, err := routeState.ReconcileCurrentArtifacts(scope, artifactStore, time.Now().UTC()); err != nil {
			return nil, nil, fmt.Errorf("reconcile semantic route artifacts: %w", err)
		}
		parent = &current
	} else if currentErr.Error() != "route_revision_not_found" {
		return nil, nil, fmt.Errorf("load semantic route revision: %w", currentErr)
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	publishRequest := tool.RouteRevisionPublishRequest{
		Scope: scope, Plan: prepared.plan, ExpectedParent: parent, SnapshotDigest: prepared.plan.SnapshotDigest,
	}
	var state tool.RouteState
	var initialGrants []tool.InvocationGrant
	state, initialGrants, err = tool.PublishCurrentSurface(coordinator, routeState, tool.NewSurfacePublishRequest(publishRequest, h.semanticContinuityTenantID(), issuer, semanticInvocationGrantTTL, time.Now().UTC()))
	if err != nil {
		return nil, nil, fmt.Errorf("open semantic route state: %w", err)
	}
	classVal := intent.ClassificationResult{}
	if classification != nil {
		classVal = *classification
	}
	surface := &semanticCallSurface{
		plan: state.Plan, scope: scope, scopePlan: prepared.scopePlan, issuer: issuer, executor: executor, routeState: routeState, hostCalls: hostCalls, coordinator: coordinator, tenantID: h.semanticContinuityTenantID(), registry: prepared.registry,
		hostConnectionID: "agent-loop-surface:" + newSemanticEphemeralIdentity(),
		completed:        make(map[string]bool), materialized: make(map[string]bool), schemas: prepared.definitions, parameterSchemas: prepared.schemas,
		grants: make(map[string]tool.InvocationGrant), retiredGrants: make(map[string]tool.InvocationGrant), rendered: make(map[string]bool), artifacts: newSemanticArtifactBroker(scope, artifactStore, routeState, coordinator), pendingArtifacts: make(map[string][]tool.ArtifactPayload),
		replan: &semanticReplanInput{UserID: userID, Channel: channel, RootTaskID: prepared.rootTaskID, Classification: classVal, Attachments: cloneSemanticMessageAttachments(attachments), ConversationLookupReused: prepared.conversationLookupReused, BundleKey: semanticArchetypeBundleKeyForTurn(classVal, userText), SlimOffice: semanticResidueSlimOfficeTurn(classVal, userText), ShortDocumentEdit: semanticResidueShortDocumentEdit(classVal, userText), BaselineWorkspace: prepared.baselineWorkspace},
	}
	for _, input := range prepared.documentInputs {
		if err := semanticRoutingRequestErr(requestCtx); err != nil {
			return nil, nil, err
		}
		if _, err := artifactStore.Publish(input.Payload); err != nil {
			return nil, nil, fmt.Errorf("publish trusted document input: %w", err)
		}
	}
	for _, input := range prepared.audioInputs {
		if err := semanticRoutingRequestErr(requestCtx); err != nil {
			return nil, nil, err
		}
		if _, err := artifactStore.Publish(input.Payload); err != nil {
			return nil, nil, fmt.Errorf("publish trusted audio input: %w", err)
		}
	}
	for _, materialization := range state.Materializations {
		surface.materialized[materialization.Grant.SelectionID] = true
		if err := tool.PlaceMaterializedGrant(materialization, surface.grants, surface.retiredGrants); err != nil {
			return nil, nil, err
		}
	}
	// The coordinator has already issued and materialized the initial closure
	// in the same commit as PublishRevision. Populate the callback from that
	// authoritative result rather than calling refresh (which would mint a
	// second, separate grant batch).
	for _, grant := range initialGrants {
		if _, err := tool.BindIssuedGrant(grant, surface.grants); err != nil {
			return nil, nil, err
		}
		surface.materialized[grant.SelectionID] = true
	}
	// A terminal execution consumes its one-time model grant and must remain
	// hidden on recovery. This includes a prepared channel delivery awaiting a
	// receipt, as well as a failed/unknown execution such as a rejected
	// parameter schema. The gateway/outbox owns receipt reconciliation; failed
	// selections require an explicit new route revision, never a stale retry.
	if err := tool.RetireConsumedLiveGrants(surface.grants, surface.retiredGrants, executor, scope, func(grant tool.InvocationGrant) error {
		_, err := routeState.RetireMaterialization(scope, state.Plan.ID, grant.Token, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("retire terminal semantic materialization: %w", err)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	completed, err := tool.LoadCompletedSelections(executor, routeState, scope)
	if err != nil {
		return nil, nil, fmt.Errorf("recover semantic plan completion: %w", err)
	}
	surface.completed = completed
	// Do not issue a new opaque grant or render a model-visible function after
	// the owning turn was cancelled.  A revision may already have been durably
	// published at this point (which is safe and has no external effect), but a
	// cancelled request must not materialize an executable surface from it.
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	var definitions []map[string]interface{}
	if coordinator != nil {
		definitions, err = visibleSemanticCallSurfaceDefinitions(surface)
	} else {
		definitions, err = refreshSemanticCallSurface(surface)
	}
	if err != nil {
		return nil, nil, err
	}
	h.persistSessionGovernedTask(userID, channel, semanticDestination(requestCtx), semanticWorkflowAgentLoop(requestCtx), surface.plan)
	return definitions, surface, nil
}

// advanceSemanticCallSurface refreshes the exposure closure after a trusted
// selection completed. Existing grants remain valid only for their original
// selection; new grants are issued solely for newly-ready DAG nodes.
func advanceSemanticCallSurface(surface *semanticCallSurface, completedSelectionID string) ([]map[string]interface{}, error) {
	if err := completeSemanticCallSurfaceSelection(surface, completedSelectionID); err != nil {
		return nil, err
	}
	return refreshSemanticCallSurface(surface)
}

// completeSemanticCallSurfaceSelection records DAG completion and retires the
// spent grant without issuing dependants. A parallel tool batch uses this so
// generate_pdf cannot become executable in the same assistant message as search.
func completeSemanticCallSurfaceSelection(surface *semanticCallSurface, completedSelectionID string) error {
	if surface == nil || surface.issuer == nil || surface.registry == nil || surface.routeState == nil {
		return fmt.Errorf("semantic tool surface is unavailable")
	}
	completedSelectionID = strings.TrimSpace(completedSelectionID)
	if completedSelectionID == "" {
		return nil
	}
	surface.invalidateEpoch()
	surface.completed[completedSelectionID] = true
	// A completed selection is never part of the next exposure closure.
	// Its signed nonce is already durably consumed; removing the host lookup
	// also prevents a model from treating an obsolete function name as a
	// current action in a later round.
	return tool.RetireLiveGrantsForSelection(surface.grants, surface.retiredGrants, completedSelectionID, func(_ string, grant tool.InvocationGrant) error {
		_, err := surface.routeState.RetireMaterialization(surface.scope, surface.plan.ID, grant.Token, time.Now().UTC())
		return err
	})
}

// retireSemanticCallSurfaceSelection removes an admitted one-time invocation
// from the model-visible surface without asserting that its plan selection
// completed. It is the only valid transition for receipt-bound external
// effects: the provider has prepared an operation, but its transport has not
// yet supplied authoritative accepted/failed evidence.
//
// Retiring an exposed materialization prevents a later model round or process
// recovery from treating an opaque function as a retry authority. Unlike
// advanceSemanticCallSurface it deliberately does not mutate completed, so no
// dependent selection can become ready before receipt reconciliation settles
// the durable PlanExecution record.
func retireSemanticCallSurfaceSelection(surface *semanticCallSurface, selectionID string) ([]map[string]interface{}, error) {
	if surface == nil || surface.issuer == nil || surface.registry == nil || surface.routeState == nil {
		return nil, fmt.Errorf("semantic tool surface is unavailable")
	}
	selectionID = strings.TrimSpace(selectionID)
	if selectionID == "" {
		return nil, fmt.Errorf("semantic selection id is required")
	}
	surface.invalidateEpoch()
	if err := tool.RetireLiveGrantsForSelection(surface.grants, surface.retiredGrants, selectionID, func(_ string, grant tool.InvocationGrant) error {
		_, err := surface.routeState.RetireMaterialization(surface.scope, surface.plan.ID, grant.Token, time.Now().UTC())
		return err
	}); err != nil {
		return nil, err
	}
	return refreshSemanticCallSurface(surface)
}

// semanticExecutionConsumesModelGrant identifies durable execution states
// whose opaque model function must never be exposed again. A plan may later be
// revised under the original capability constraints, but a consumed grant is
// not retry authority for a different parameter set or provider attempt.
func semanticExecutionConsumesModelGrant(state tool.PlanExecutionState) bool {
	return tool.ExecutionConsumesModelGrant(state)
}

// semanticSelectionIsUnsettled reads the durable execution record for a spent
// selection. A failed attempt is settled — it cost its budget and the family
// moves on — while awaiting-receipt, running, or lost outcomes are not, and a
// family must not step past them.
func semanticSelectionIsUnsettled(surface *semanticCallSurface, selectionID string) bool {
	if surface == nil {
		return false
	}
	return tool.SelectionUnsettled(surface.executor, surface.scope, selectionID)
}

// semanticSpentBudgetNote reports that the call which just succeeded spent the
// last invocation its family was budgeted for.
//
// A single-invocation family gets nothing. There the tool disappearing is the
// outcome itself — the turn asked for one search and got it — and that is how
// every family behaved before budgets existed. A budgeted family is different:
// the work was still in progress, so a tool vanishing with no explanation
// invites the model to assume the task is done. Losing eight edits mid-change
// and reporting success is a worse failure than being told the limit is
// reached.
//
// The note rides on the result of the call that spent the budget instead of
// arriving as a separate message: the model reads it exactly where it is
// relevant, and the host-call journal replays it together with that result.
func semanticSpentBudgetNote(surface *semanticCallSurface, selectionID string) string {
	if surface == nil {
		return ""
	}
	return tool.SpentBudgetNoteForGrants(surface.plan, selectionID, surface.materialized, surface.grants)
}

// semanticTurnDeliveryComplete reports that the turn's goal is fully reached:
// every REQUIRED planned selection completed and at least one completed
// selection delivered the file, on the current channel or to the specified
// IM target. The closing LLM round trip after this point only produces
// summary text, so the loop may stop cleanly instead of paying one more
// call's latency and outage exposure. Optional offers never gate completion:
// the ambient knowledge/memory lookups and the archetype bundle offers are
// open offers the model may ignore, and holding the stop hostage to them
// would make it dead code — production 2026-08-26 turns never called the
// ambient legs, so the all-selections variant never fired. A turn whose
// delivery finished while a required selection is still open does not match:
// the loop continues. A specified-target send that is still awaiting the
// channel gateway is not completed, so that turn does not stop here.
// A required unmet need keeps the turn open too: TurnRequiredDeliveryComplete
// only walks selections, while the hop run counts a non-optional unmet need
// whose need family no selection covers as remaining work. Without this gate
// a clean early stop could close a turn whose execution contract still says
// failed, and the hard-exit guard (which runs only on tool-less stops) would
// never get a look.
func semanticTurnDeliveryComplete(surface *semanticCallSurface) bool {
	// Optionality is need-level and the plan does not retain needs; recompute
	// them deterministically from the stored classification (rule templates
	// only, no LLM) under the bundle key the plan was published with. Unknown
	// base need IDs fail toward required — a selection whose optionality cannot
	// be proven must still complete. A later repeat sibling is not that case:
	// the repeat contract makes only the family base an obligation, including
	// a baseline ceiling the bundle-key recompute does not replay.
	optionalByNeed, ok := semanticSurfaceOptionalNeeds(surface)
	if !ok {
		return false
	}
	if !tool.TurnRequiredDeliveryComplete(surface.plan, completedWithSatisfiedFamilies(surface), optionalByNeed) {
		return false
	}
	// Mirror hopRunSnapshot's unmet accounting: skip unmet needs a selection
	// already covers, then hold the turn open on every non-optional one.
	selectedNeeds := make(map[string]bool, len(surface.plan.Selections))
	for _, selection := range surface.plan.Selections {
		selectedNeeds[selection.NeedID] = true
	}
	for _, unmet := range surface.plan.Unmet {
		if selectedNeeds[unmet.NeedID] || optionalByNeed[unmet.NeedID] {
			continue
		}
		return false
	}
	return true
}

func refreshSemanticCallSurface(surface *semanticCallSurface) ([]map[string]interface{}, error) {
	return refreshSemanticCallSurfaceSkipping(surface, nil)
}

func refreshSemanticCallSurfaceSkipping(surface *semanticCallSurface, skip func(tool.PlannedSelection) bool) ([]map[string]interface{}, error) {
	if surface == nil || surface.issuer == nil || surface.registry == nil || surface.routeState == nil {
		return nil, fmt.Errorf("semantic tool surface is unavailable")
	}
	// Rerendering may issue a newly-ready grant. Retire any prior model-request
	// epoch before the grant table changes so its response cannot bind a stable
	// function name to this successor materialization.
	surface.invalidateEpoch()
	return tool.MaterializeReadySurface(tool.ReadySurfaceRequest{
		Coordinator:   surface.coordinator,
		RouteState:    surface.routeState,
		Issuer:        surface.issuer,
		Executor:      surface.executor,
		Registry:      surface.registry,
		Plan:          surface.plan,
		Scope:         surface.scope,
		TTL:           semanticInvocationGrantTTL,
		Completed:     surface.completed,
		Unsettled:     func(selectionID string) bool { return semanticSelectionIsUnsettled(surface, selectionID) },
		Skip:          skip,
		Grants:        surface.grants,
		Issued:        surface.materialized,
		Schemas:       surface.schemas,
		RenderedNames: surface.rendered,
		MarkRendered:  surface.rendered,
		Now:           time.Now().UTC(),
	})
}

// visibleSemanticCallSurfaceDefinitions renders the complete current exposure
// closure from durable-plan state. Unlike refreshSemanticCallSurface it never
// issues a grant or changes materialization state; hosts call it after an
// execution transition to replace their transient function list rather than
// incrementally appending/removing definitions in separate branches.
func visibleSemanticCallSurfaceDefinitions(surface *semanticCallSurface) ([]map[string]interface{}, error) {
	if surface == nil || surface.registry == nil {
		return nil, fmt.Errorf("semantic tool surface is unavailable")
	}
	rendered, err := tool.RenderVisibleReadyDefinitions(surface.registry, surface.plan, surface.plan.ReadySelections(surface.completed), surface.completed, surface.grants, surface.schemas, surface.completed)
	if err != nil {
		return nil, err
	}
	return tool.ProjectRenderedDefinitions(rendered, nil, nil), nil
}

// semanticPlanForTurn performs classification, catalog publication and
// capability planning only. It intentionally does not create opaque adapters
// or invocation grants, so it is safe for diagnostics and shadow evaluation.
func (h *IMMessageHandler) semanticPlanForTurn(userID, userText, channel, rootTaskID, turnID string) (*semanticPlanPreparation, bool, error) {
	return h.semanticPlanForTurnWithClassification(userID, userText, channel, rootTaskID, turnID, nil)
}

// semanticPlanForTurnWithClassification accepts only the result already
// computed for this turn by the entry path. It avoids a second UIC decision
// whose cache/fusion timing could turn the profile and the materialized tool
// surface into different request interpretations.
func (h *IMMessageHandler) semanticPlanForTurnWithClassification(userID, userText, channel, rootTaskID, turnID string, supplied *intent.ClassificationResult) (*semanticPlanPreparation, bool, error) {
	return h.semanticPlanForTurnWithClassificationAndAttachments(userID, userText, channel, rootTaskID, turnID, supplied, nil)
}

func (h *IMMessageHandler) semanticPlanForTurnWithClassificationAndAttachments(userID, userText, channel, rootTaskID, turnID string, supplied *intent.ClassificationResult, attachments []MessageAttachment) (*semanticPlanPreparation, bool, error) {
	return h.semanticPlanForTurnWithContextAndClassificationAndAttachments(context.Background(), userID, userText, channel, rootTaskID, turnID, supplied, attachments)
}

// semanticPlanForTurnWithContextAndClassificationAndAttachments is the
// request-lifecycle owning planning entry point.  The compatibility helpers
// above intentionally remain useful for diagnostics and unit tests, but a live
// IM turn must never detach catalog reads from its cancellation boundary.
func (h *IMMessageHandler) semanticPlanForTurnWithContextAndClassificationAndAttachments(requestCtx context.Context, userID, userText, channel, rootTaskID, turnID string, supplied *intent.ClassificationResult, attachments []MessageAttachment) (*semanticPlanPreparation, bool, error) {
	return h.semanticPlanForTurnWithContextAndClassificationAndAttachmentsWithSession(requestCtx, userID, userText, channel, rootTaskID, turnID, semanticCompatibilitySessionID(turnID), supplied, attachments)
}

func (h *IMMessageHandler) semanticPlanForTurnWithContextAndClassificationAndAttachmentsWithSession(requestCtx context.Context, userID, userText, channel, rootTaskID, turnID, sessionID string, supplied *intent.ClassificationResult, attachments []MessageAttachment) (*semanticPlanPreparation, bool, error) {
	if h == nil {
		return nil, false, nil
	}
	// Same contract as the shared-turn surface. A shadow diagnostic or a
	// direct planner call must not classify a continuation as goal_manage
	// and publish the closed adapter beside the lifecycle tool.
	if goalContinuationTurn(userText, channel) {
		return nil, false, nil
	}
	if sessionRoundTurn(userText, channel) {
		return nil, false, nil
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(userID) == "" {
		return nil, true, fmt.Errorf("semantic_invocation_identity_required")
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, true, err
	}
	var policy corelib.EffectiveRoutingPolicy
	var configErr error
	if cfg, cfgErr := h.loadConfig(); cfgErr == nil {
		policy = cfg.EffectiveRoutingPolicy()
		// Disabled policy preserves the existing compatibility path. Once
		// explicitly enabled, invalid/shadow modes fail closed before surface
		// publication and never fall through to text routing.
		if policy.Enabled && !policy.Valid {
			return nil, true, fmt.Errorf("semantic_routing_policy_invalid:%s", policy.InvalidReason)
		}
	} else {
		// Configuration is part of the governed route decision. Keep generic
		// conversations compatible, but never publish a managed surface from a
		// zero-value policy after a failed read.
		configErr = cfgErr
	}
	var classification intent.ClassificationResult
	if supplied != nil {
		classification = *supplied
	} else {
		uic := h.getUnifiedClassifier()
		if uic == nil {
			return nil, false, nil
		}
		classification = uic.ClassifyContext(requestCtx, intent.MessageContext{Text: semanticUserIntentText(userText), UserID: userID})
	}
	normalizeSemanticClassificationForTurn(&classification)
	// 2026-09-18 (17:42 production incident): a tree-timeout collapse preserves
	// a strong L2 ssh signal only as RunnerUp — no declared label — so every
	// downstream gate (managed check, floor exemption, leftover keep) saw a
	// bare unknown and the turn lost ssh entirely. Promote RunnerUp-ssh into
	// Secondary here: the same promotion fusion applies to RunnerUp on
	// ambiguous verdicts, applied one layer up for the L2-fallback collapse.
	// Only ssh gets this treatment (reviewed exception); the floor exemption's
	// protective projection remains the authorization boundary on degraded
	// turns, and the miss-to-leftover gate below still applies when no ssh
	// provider is published.
	if classification.RunnerUp == intent.LabelSSH &&
		classification.RunnerUpScore >= intent.EmbeddingLookupMinScore &&
		!classificationHasLabel(classification, intent.LabelSSH) {
		classification.Secondary = append(append([]intent.IntentLabel(nil), classification.Secondary...), intent.LabelSSH)
		classification.RunnerUp = ""
		classification.RunnerUpScore = 0
	}
	activeDocumentUse := h.activeLocalDocumentUseForTurn(userID, channel, semanticDestination(requestCtx), userText)
	if len(attachments) == 0 && activeDocumentUse == activeLocalDocumentReuse && activeDocumentContinuationIntent(classification, userText) {
		classification = classificationWithActiveDocumentRead(classification)
	}
	if replayed, ok := h.applySessionGovernedContinuation(userID, channel, semanticDestination(requestCtx), semanticWorkflowAgentLoop(requestCtx), classification, userText, attachments); ok {
		classification = replayed
	}
	// A phase turn that still sounds like the workflow it belongs to is not
	// asking for an unserved capability. An ordinary turn the catalog does
	// not recognize as a workflow project is not one either: the tree's
	// design-latitude test is wider than the catalog, and refusing it here
	// would close the agent path the workflow entry already declined to take.
	classification = semanticClassificationForWorkflowLoop(semanticWorkflowAgentLoop(requestCtx), classification)
	// paper_writing on the LaTeX paper expert is the document already on disk.
	// This runs before the catalog check so a noise-band score cannot close
	// the session the template library just opened.
	classification = semanticReleaseLatexExpertWorkflowTask(userID, userText, classification)
	classification = semanticReleaseUncataloguedWorkflowTask(userText, classification)
	// Named skill runs belong to the current agent (skill-doc inject + loop),
	// the same path the main assistant uses. workflow_task is only a
	// workflow_v2 panel start and must not HostReject that turn. Agent-guided
	// inject (Book-PDF) is the same act even when the user omitted "skill".
	h.releaseNamedSkillIntercept(&classification, userID, userText)
	classification = applyHostDatabaseReadGrant(classification, h.databaseHostReadGrant(userText))
	// Host opt-out is applied after the catalog and skill releases. Those
	// answer "is this a panel project?"; the flag answers "did the host
	// already keep this message in chat?". A corroborated competitive
	// analysis still must not HostReject when the wizard sent「无」.
	classification = semanticReleaseOptedOutWorkflowTask(semanticNoWorkflowInterception(requestCtx), userText, classification)
	// Unmapped capability labels (coding, bug_fix, …) must HostReject before
	// the managed-for-loop gate. Checking managed first reopened the legacy
	// name router for those families.
	if _, unmapped := imSemanticIntentCoverage(classification); unmapped != "" {
		return nil, true, semanticUnmappedCapabilityError{Label: unmapped}
	}
	// A family withdrawn by its runtime dial leaves through the same door, with
	// the same rejection: to everything downstream, rolled back and never
	// migrated are the same fact, and neither may reopen the name router.
	if withdrawn, ok := semanticWithdrawnCapabilityLabel(h, userID, classification); ok {
		semanticLogCodingWithdrawal(h, userID, withdrawn)
		return nil, true, semanticUnmappedCapabilityError{Label: withdrawn}
	}
	// Image bytes mean the model can see a photo. Do not mint search/PDF
	// grants or replay the previous generate over that turn. A picker path
	// without bytes is not this fact. A confident primary screenshot still
	// plans capture.
	if semanticHostStagedImageUnderstand(userText, attachments, classification) {
		return nil, false, nil
	}
	// Managed SSH publishes exactly one mode per catalog build, scoped to this
	// principal: the exec adapter when this conversation owns one live
	// session, or the connect adapter when it owns none. Another
	// conversation's session is not this turn's session. Only when NEITHER
	// mode is published (this principal has several live sessions and no
	// runtime binding) does a declared SSH turn miss to leftover, where the
	// builtin ssh tool lives. Planning the other family first HostRejects
	// the whole turn as unmet.
	if classificationHasLabel(classification, intent.LabelSSH) && !semanticTrustedSSHPublished(h, userID) && !semanticTrustedSSHConnectPublished(h, userID) {
		return nil, false, nil
	}
	// Rewrite before coverage gates: a bare "继续" arrives as
	// continuation/unknown (not managed), and L2 document_generate+delivery
	// would HostReject before write_file could replace it.
	// A local file delete is the same kind of query evidence. Tree file_write
	// at 0.60 (2026-09-26 「删除刚才的markdown文件」) misses the resolver floor.
	// Leftover then ranks write_file and the petition contract invites a
	// search for a delete name. The outcome is file_delete, so the plan
	// grants delete_file before that search starts.
	classification = semanticPlanningClassification(classification, acpInnerUserRequest(userText))
	// The opening utterance cannot mean both "send the attachment I was given"
	// and "generate a new document". A petition is not that utterance: the
	// document is already planned, and delivering the file is the next step.
	// Applying the opening conflict there rejected send_file after generate_pdf
	// (production 2026-09-26: COM-exported PDF, petition send_file).
	if !semanticPetitionExpansion(requestCtx) && classificationHasLabel(classification, intent.LabelAttachmentDelivery) && classificationHasLabel(classification, intent.LabelDocumentGenerate) {
		return nil, true, errSemanticGenerateDeliveryConflict
	}
	if !imSemanticIntentIsManagedForLoop(semanticWorkflowAgentLoop(requestCtx), classification) {
		return nil, false, nil
	}
	if configErr != nil {
		return nil, true, fmt.Errorf("semantic_routing_config_unavailable: %w", configErr)
	}
	// 0.78 is the resolver mint-writes floor and L2 early-exit. It is not a
	// second vote after L3 already named the family. Tree-confirmed (layer
	// 3/23, not degraded, ≥ 0.70) plans; leftover is for unknown/hint, not
	// for a named shell_command at 0.75. Read-only lookup hints still plan
	// above 0.70, and a degraded L2 office hint at the lookup floor plans
	// through the governed office surface. Other degraded mutating families
	// stay a miss so leftover cannot mint bash from a timeout guess. Weak L2
	// generate stays a miss. ssh is the one reviewed exception below: the
	// published adapter mode bounds the surface (connect needs real
	// credentials, exec needs a live bound session), so a degraded or
	// sub-floor ssh label still plans instead of losing the only connect
	// path to a classifier outage (production 2026-09-18 11:04).
	// Markdown-file queries are rewritten above and therefore meet the floor.
	declaredManaged, unmapped := imSemanticIntentCoverage(classification)
	planning := classification
	if !semanticClassificationMeetsResolverFloor(classification) {
		if !semanticClassificationPlansBelowResolverFloor(classification) {
			// Degraded verdicts must not mint mutating families from a
			// timeout guess — with one reviewed exception: a declared or
			// petitioned ssh label while an ssh provider is published. The
			// published mode bounds what the surface can do (connect needs
			// real credentials; exec needs a live bound session), and
			// without this the connect path dies with the classifier: no
			// plan, no leftover keep (degraded drops it), and the petition
			// re-plan dies right here (production 2026-09-18 11:04:
			// "连接驱网服务器，查看状态" answered "SSH 工具不在可用工具列表"
			// after the tree channel 502'd).
			if !classificationHasSSHSignal(classification) || !semanticTrustedSSHAnyPublished(h, userID) {
				return nil, false, nil
			}
			planning = semanticSSHDegradedPlanningClassification(classification)
			logSSHAvailabilityEvent("rescue", "layer=plan_exemption", "mode="+sshAvailabilityMode(h, userID))
		} else {
			planning = semanticLookupClassificationForPlanning(classification)
		}
		declaredManaged, unmapped = imSemanticIntentCoverage(planning)
	}
	if unmapped != "" {
		return nil, true, semanticUnmappedCapabilityError{Label: unmapped}
	}
	if !declaredManaged {
		// Only generic Q&A (unknown/non_coding/continuation) may skip the
		// semantic surface. Capability labels without a rule are HostReject.
		return nil, false, nil
	}
	if h.registry == nil {
		return nil, true, fmt.Errorf("semantic capability registry unavailable")
	}
	registry := newIMSemanticCapabilityRegistry()
	if agentservice.ArchetypeBundleKeyFor(requestCtx, planning) == agentservice.ArchetypeBundleKey(planning) {
		requestCtx = withSemanticArchetypeBundleKeyOverride(requestCtx, semanticArchetypeBundleKeyForTurn(planning, userText))
	}
	needs, managed, err := semanticNeedsFromClassificationContext(requestCtx, registry, planning)
	if err != nil {
		return nil, true, fmt.Errorf("resolve IM semantic capability needs: %w", err)
	}
	// The tool head scores the same vector the intent pass already computed.
	// Managed turns never enter the name router, so this is the injection
	// point: intent needs ∪ head top-k, still inside the capability plan.
	needs = h.unionToolHeadNeeds(requestCtx, registry, needs, planning)
	var carriedCaps []string
	if lookupContinuationFetches(requestCtx) && planning.Primary == intent.LabelSearch {
		// A cold search leaves web_fetch latent. Raising the selection budget
		// cannot publish a need that was never added. A project-task follow-up
		// ("does this repo have SSO?") has to open the page it just found.
		needs = ensureLookupContinuationFetchNeed(needs, planning.Confidence)
	}
	if lookupContinuationOpenCeiling(requestCtx) || lookupContinuationFetches(requestCtx) {
		// The parent turn's bash/ssh stay on this lookup. Baseline read/write
		// is not added: a carry that was only ssh must not grow a shell.
		needs, carriedCaps = ensureLookupContinuationCarriedNeeds(needs, h.parentExecutionTools(userID), planning.Confidence)
	}
	slimOffice, shortDocumentEdit := semanticOfficeTurnSlim(requestCtx, planning, userText)
	if slimOffice {
		needs = withoutAmbientRetrievalNeeds(needs)
		needs = withSlimDocumentRead(registry, needs)
		if !shortDocumentEdit {
			needs = withoutDocumentMutationNeeds(needs, semanticPetitionedKeep(requestCtx))
		}
	}
	if classificationHasLabel(planning, intent.LabelSSH) && semanticTrustedSSHAnyPublished(h, userID) && sshAvailabilityCountEligible(turnID) {
		// Denominator for the ssh rescue-rate observability stream (§D1).
		logSSHAvailabilityEvent("turn_total", "mode="+sshAvailabilityMode(h, userID))
	}
	if !managed || len(needs) == 0 {
		// A governed family already passed coverage. Falling through to the
		// legacy name-router here would re-open the workaround this migration
		// exists to prevent.
		return nil, true, fmt.Errorf("semantic route has no governed capability needs")
	}
	if strings.TrimSpace(rootTaskID) == "" || strings.TrimSpace(turnID) == "" {
		rootTaskID, turnID = semanticRoutingIdentity(nil, userID, userText)
	}
	attachments = agentservice.CanonicalizeReviewedHostMessageAttachments(attachments)
	documentInputs, err := semanticDocumentInputsForTurn(rootTaskID, turnID, sessionID, userID, attachments)
	if err != nil {
		return nil, true, err
	}
	// A current channel attachment is the only candidate for this turn. Do not
	// fill a malformed/image attachment request from an older desktop document.
	// Likewise, a bare path in prose is not an authorized picker selection.
	//
	// A *current* picker selection is the exception: it is this turn's own
	// authorization, and activeLocalDocumentUseForTurn only reported reuse for
	// it because the captured context matches the paths this very message
	// named (activeLocalDocumentContextsMatchPicker). Binding that is not
	// reaching back to an older document, so a staged image riding along must
	// not strip it — otherwise a document_read verdict the satisfiability
	// predicate called satisfiable still ends in trusted_document_input_missing
	// (2026-10-11). Without a picker selection the attachment-count half still
	// blocks the reuse it was written for.
	if len(documentInputs) == 0 && (len(attachments) == 0 || hasCurrentLocalDocumentPickerInput(userText)) && activeDocumentUse == activeLocalDocumentReuse && semanticDocumentReadNeedPresent(needs) {
		activeInputs, active, activeErr := h.semanticActiveLocalDocumentInputsForTurn(rootTaskID, turnID, sessionID, userID, channel, semanticDestination(requestCtx))
		if activeErr != nil {
			return nil, true, activeErr
		}
		if active {
			documentInputs = activeInputs
		}
	}
	if len(documentInputs) == 0 && activeDocumentUse == activeLocalDocumentPickerMismatch && semanticDocumentReadNeedPresent(needs) {
		return nil, true, fmt.Errorf("trusted_document_context_picker_mismatch")
	}
	// A delivery that consumes an existing document binds the file this task
	// already materialized. Message attachments and the picker stay first.
	// A turn that itself generates or writes office consumes that producer
	// and must not also bind the previous file.
	//
	// This gate deliberately keeps the bare len(attachments) == 0 test even
	// though the read gate above now also accepts a current picker selection.
	// The two look alike but are not the same decision: this one binds what
	// *this task produced*, and it never consults activeDocumentUse, so a
	// picker document is not evidence about the producer. Widening it would
	// let a picker turn bind an unrelated earlier output — the exact hazard
	// this branch exists to prevent.
	if len(documentInputs) == 0 && len(attachments) == 0 && semanticDeliveryConsumesExistingDocument(needs) {
		producedInputs, produced, producedErr := h.semanticProducedDocumentInputsForTurn(rootTaskID, turnID, sessionID, userID, channel)
		if producedErr != nil {
			return nil, true, producedErr
		}
		if produced {
			documentInputs = producedInputs
		}
	}
	needs, err = semanticNeedsForTrustedDocumentInputs(needs, documentInputs)
	if err != nil {
		return nil, true, err
	}
	audioInputs, err := semanticAudioInputsForTurn(rootTaskID, turnID, sessionID, userID, attachments)
	if err != nil {
		return nil, true, err
	}
	needs, err = semanticNeedsForTrustedAudioInputs(h, needs, audioInputs)
	if err != nil {
		return nil, true, err
	}
	// Lookup->generate Requires means "facts needed", not "search again".
	// Same-topic conversation evidence drops the this-turn lookup need so
	// generate_pdf is issued on the first model request.
	var conversationLookupReused bool
	if _, petition := semanticPetitionKeptLookup(requestCtx); !petition {
		needs, conversationLookupReused = semanticNeedsForReusableConversationLookupReport(needs, requestCtx, userText)
	}
	// A petition expansion re-plan has no user text, so the drop above cannot
	// re-fire; when the parent surface recorded it, mirror it here for every
	// lookup leg except the petitioned label's own templates.
	needs = semanticNeedsForPetitionExpansionLookup(needs, requestCtx)
	applyBaseline := semanticApplyBaselineWorkspace(requestCtx, planning, slimOffice)
	if applyBaseline {
		needs = agentservice.ExpandBaselineWorkspaceNeeds(registry, imSemanticIntentRuleSet, planning, true, needs)
	} else if shortDocumentEdit {
		// Slim office drops the baseline bundle so a short "继续" does not
		// grow shell and read companions. The write the turn kept is still
		// the edit, so it keeps the iterative floor instead of the one-shot
		// file_write rule.
		needs = agentservice.RaiseExistingLocalFileWriteFloor(needs, planning.Confidence)
	}
	remaining := semanticResidueRemaining(requestCtx)
	if lookupContinuationOpenCeiling(requestCtx) || lookupContinuationFetches(requestCtx) {
		remaining = semanticResidueWithoutLookupCeiling(remaining)
		remaining = semanticResidueDropSpent(remaining, carriedCaps...)
	}
	if !semanticPetitionExpansion(requestCtx) {
		needs = dropUnusedSessionCompanions(needs, remaining)
	}
	if semanticPetitionExpansion(requestCtx) {
		needs = clampPetitionNeedsToResidue(needs, remaining, requestCtx)
	} else {
		needs = clampNeedsToResidueRemaining(needs, remaining)
	}
	// Drop companions the parent budget already omitted and that the
	// petitioned label does not declare. Raising the selection cap below
	// would otherwise put them back, and the expansion validator would
	// reject the child.
	needs = semanticNeedsForPetitionWhitelist(needs, requestCtx)
	if len(needs) == 0 {
		// The session ceiling is spent and no declared need remains. Stay on
		// the closed semantic turn so the legacy catalog is not reopened.
		return nil, true, errSemanticSessionCeilingSpent
	}
	catalog := tool.NewToolCatalog(registry)
	// A semantic provider's registered schema is the trusted source for its
	// model-facing definition.  Do not build the legacy presentation surface
	// here: doing so can synchronously load Skills/MCP adapters and run the old
	// name-router preparation before the planner has selected a capability.
	registeredTools := h.registry.ListAvailable()
	defsByName := make(map[string]map[string]interface{}, len(registeredTools)+3)
	semanticSchemas := make(map[string]map[string]interface{})
	providers := make([]tool.ProviderSpec, 0, len(registeredTools))
	destination := semanticDestination(requestCtx)
	for _, registered := range registeredTools {
		if semanticUnpublishedManagedProvider(registered, channel, destination) {
			continue
		}
		definition, exists := defsByName[registered.Name]
		if !exists {
			// The legacy definition list is a compatibility presentation surface,
			// not the authority for a governed provider. A registered semantic
			// implementation supplies its own trusted schema at registration, so
			// materialization must remain available even when the old list's
			// name-based filters omit that implementation.
			definition = registeredToolToDef(registered)
		}
		if override, ok := semanticManagedDefinitionOverride(registered.Name); ok {
			definition = override
		}
		defsByName[registered.Name] = definition
		invocationSchema, err := semanticInvocationSchemaForRegisteredTool(registered, definition)
		if err != nil {
			return nil, true, fmt.Errorf("validate semantic invocation schema for %q: %w", registered.Name, err)
		}
		semanticSchemas[registered.Name] = invocationSchema
		authorization, err := tool.NewParameterAuthorization(invocationSchema)
		if err != nil {
			return nil, true, fmt.Errorf("authorize semantic invocation schema for %q: %w", registered.Name, err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName: registered.Name,
			Binding: tool.ProviderBinding{
				Kind: semanticProviderKind(registered), ProviderID: semanticProviderID(registered), ImplementationID: registered.Name,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(definition)),
			},
			ParameterAuthorization: authorization,
			Provides:               registered.CapabilityProvisions, Consumes: registered.SemanticConsumes, Produces: registered.SemanticProduces,
			Effects: registered.SemanticEffects, Ready: true, ChannelScopes: semanticChannelScopes(channel),
		})
	}
	// Dynamic Skill/MCP providers are read from the GUI lifecycle owner as one
	// principal-scoped inventory snapshot, then projected by the same trusted
	// descriptor used by Core Agent. Discovery descriptions never enter the
	// prompt or capability decision. An unavailable lifecycle is represented by
	// coverage, not by pretending an empty list proves no implementation exists.
	dynamicInventory, err := h.semanticDynamicInventory(requestCtx)
	if err != nil {
		return nil, true, fmt.Errorf("load GUI dynamic semantic inventory: %w", err)
	}
	dynamicCatalog, err := agentservice.BuildDynamicSemanticCatalog(dynamicInventory.mcpEntries, dynamicInventory.skillEntries)
	if err != nil {
		return nil, true, fmt.Errorf("build GUI dynamic semantic catalog: %w", err)
	}
	for _, provider := range dynamicCatalog.Providers {
		// One MCP or skill definition the host cannot authorize must not
		// hide the clock, the mailbox, or the sibling that already projected.
		definition, ok := dynamicCatalog.Definitions[provider.AdapterName]
		if !ok {
			if dynamicProviderLeftUnpublished(provider, fmt.Errorf("dynamic semantic definition missing for %q", provider.AdapterName)) {
				continue
			}
			return nil, true, fmt.Errorf("dynamic semantic definition missing for %q", provider.AdapterName)
		}
		invocationSchema, err := semanticInvocationSchema(definition)
		if err != nil {
			if dynamicProviderLeftUnpublished(provider, fmt.Errorf("validate dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)) {
				continue
			}
			return nil, true, fmt.Errorf("validate dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		authorization, err := tool.NewParameterAuthorization(invocationSchema)
		if err != nil {
			if dynamicProviderLeftUnpublished(provider, fmt.Errorf("authorize dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)) {
				continue
			}
			return nil, true, fmt.Errorf("authorize dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		defsByName[provider.AdapterName] = definition
		semanticSchemas[provider.AdapterName] = invocationSchema
		provider.ParameterAuthorization = authorization
		providers = append(providers, provider)
	}
	// The open family is demanded whenever a published provider can answer it,
	// subject only to the outcome-shape rule above. The need is optional, so
	// with no installed dynamic provider this is a no-op rather than a new
	// unmet row on every managed turn.
	//
	// Label enumeration is intentionally not how this is decided: routing to an
	// already-installed, already-published MCP must not depend on someone
	// remembering to add its intent label to an allowlist. See
	// intent.CallerWantsDynamicLookup for what the rule does encode.
	if semanticPermitsDynamicLookup(requestCtx, planning.Primary, slimOffice) {
		needs = semanticNeedsWithDynamicLookupForProviders(needs, dynamicCatalog.Providers)
	}
	defsByName["semantic_read_trusted_document"] = semanticTrustedDocumentReadDefinition()
	semanticSchemas["semantic_read_trusted_document"] = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"max_chars":    map[string]interface{}{"type": "integer"},
			"offset":       map[string]interface{}{"type": "integer"},
			"line_numbers": map[string]interface{}{"type": "boolean"},
			"sheet":        map[string]interface{}{"type": "string"},
			"range":        map[string]interface{}{"type": "string"},
			"max_rows":     map[string]interface{}{"type": "integer"},
			"max_slides":   map[string]interface{}{"type": "integer"},
			"slide_offset": map[string]interface{}{"type": "integer"},
		},
		"additionalProperties": false,
	}
	documentAuthorization, err := tool.NewParameterAuthorization(semanticSchemas["semantic_read_trusted_document"])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted document invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: "semantic_read_trusted_document",
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: "trusted-document-read-v1",
			SchemaDigest: tool.SchemaDigest([]byte("trusted-document-read-v1")),
		},
		ParameterAuthorization: documentAuthorization,
		Provides: []tool.CapabilityProvision{
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "pdf"}, Quality: 1},
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "word"}, Quality: 1},
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "spreadsheet"}, Quality: 1},
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "presentation"}, Quality: 1},
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "text"}, Quality: 1},
			{Capability: "document.read.local", Qualifiers: map[string]string{"format": "mobi"}, Quality: 1},
		},
		Consumes: []tool.ArtifactContract{{Kind: "document", Required: true}},
		Effects:  []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedAudioTranscribeAdapter] = semanticTrustedAudioTranscribeDefinition()
	semanticSchemas[semanticTrustedAudioTranscribeAdapter] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	audioAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedAudioTranscribeAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted audio invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedAudioTranscribeAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: "trusted-audio-transcribe-v1",
			SchemaDigest: tool.SchemaDigest([]byte("trusted-audio-transcribe-v1")),
		},
		ParameterAuthorization: audioAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityAudioTranscribeSpeech, Quality: 2}},
		Consumes:               []tool.ArtifactContract{{Kind: "audio", Required: true}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedAuditReadAdapter] = semanticTrustedAuditReadDefinition()
	semanticSchemas[semanticTrustedAuditReadAdapter] = semanticTrustedAuditInvocationSchema()
	auditAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedAuditReadAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted audit invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedAuditReadAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedAuditReadImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedAuditReadImplementation)),
		},
		ParameterAuthorization: auditAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilitySecurityAuditRead, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedKnowledgeAdminAdapter] = semanticTrustedKnowledgeAdminDefinition()
	semanticSchemas[semanticTrustedKnowledgeAdminAdapter] = semanticTrustedKnowledgeAdminInvocationSchema()
	knowledgeAdminAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedKnowledgeAdminAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted knowledge admin invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedKnowledgeAdminAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedKnowledgeAdminImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedKnowledgeAdminImplementation)),
		},
		ParameterAuthorization: knowledgeAdminAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityKnowledgeAdminMaintenance, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedKnowledgeIngestAdapter] = semanticTrustedKnowledgeIngestDefinition()
	semanticSchemas[semanticTrustedKnowledgeIngestAdapter] = semanticTrustedKnowledgeIngestInvocationSchema()
	knowledgeIngestAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedKnowledgeIngestAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted knowledge ingest invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedKnowledgeIngestAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedKnowledgeIngestImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedKnowledgeIngestImplementation)),
		},
		ParameterAuthorization: knowledgeIngestAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityKnowledgeIngestLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedKnowledgeReadAdapter] = semanticTrustedKnowledgeReadDefinition()
	semanticSchemas[semanticTrustedKnowledgeReadAdapter] = semanticTrustedKnowledgeReadInvocationSchema()
	knowledgeReadAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedKnowledgeReadAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted knowledge read invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedKnowledgeReadAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedKnowledgeReadImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedKnowledgeReadImplementation)),
		},
		ParameterAuthorization: knowledgeReadAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityKnowledgeReadLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedFileWriteAdapter] = semanticTrustedFileWriteDefinition()
	semanticSchemas[semanticTrustedFileWriteAdapter] = semanticTrustedFileWriteInvocationSchema()
	fileWriteAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedFileWriteAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted file write invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedFileWriteAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedFileWriteImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedFileWriteImplementation)),
		},
		ParameterAuthorization: fileWriteAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityFSWriteLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedFileDeleteAdapter] = semanticTrustedFileDeleteDefinition()
	semanticSchemas[semanticTrustedFileDeleteAdapter] = semanticTrustedFileDeleteInvocationSchema()
	fileDeleteAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedFileDeleteAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted file delete invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedFileDeleteAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedFileDeleteImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedFileDeleteImplementation)),
		},
		ParameterAuthorization: fileDeleteAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityFSDeleteLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedFileReadAdapter] = semanticTrustedFileReadDefinition()
	semanticSchemas[semanticTrustedFileReadAdapter] = semanticTrustedFileReadInvocationSchema()
	fileReadAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedFileReadAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted file read invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedFileReadAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedFileReadImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedFileReadImplementation)),
		},
		ParameterAuthorization: fileReadAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedRepoInspectAdapter] = semanticTrustedRepoInspectDefinition()
	semanticSchemas[semanticTrustedRepoInspectAdapter] = semanticTrustedRepoInspectInvocationSchema()
	repoInspectAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedRepoInspectAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted repo inspect invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedRepoInspectAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedRepoInspectImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedRepoInspectImplementation)),
		},
		ParameterAuthorization: repoInspectAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityRepoInspectVCS, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedWebFetchAdapter] = semanticTrustedWebFetchDefinition()
	semanticSchemas[semanticTrustedWebFetchAdapter] = semanticTrustedWebFetchInvocationSchema()
	webFetchAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedWebFetchAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted web fetch invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedWebFetchAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedWebFetchImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedWebFetchImplementation)),
		},
		ParameterAuthorization: webFetchAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityInformationFetchWeb, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedWebSearchAdapter] = semanticTrustedWebSearchDefinition()
	semanticSchemas[semanticTrustedWebSearchAdapter] = semanticTrustedWebSearchInvocationSchema()
	webSearchAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedWebSearchAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted web search invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedWebSearchAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedWebSearchImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedWebSearchImplementation)),
		},
		ParameterAuthorization: webSearchAuthorization,
		Provides: []tool.CapabilityProvision{
			{Capability: semanticTrustedWebSearchCapability, Qualifiers: map[string]string{"freshness": "reference"}, Quality: 2},
			{Capability: semanticTrustedWebSearchCapability, Qualifiers: map[string]string{"freshness": "current"}, Quality: 2},
		},
		Effects: []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedClockAdapter] = semanticTrustedClockDefinition()
	semanticSchemas[semanticTrustedClockAdapter] = semanticTrustedClockInvocationSchema()
	clockAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedClockAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted clock invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedClockAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedClockImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedClockImplementation)),
		},
		ParameterAuthorization: clockAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: semanticTrustedClockCapability, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedConfigAdapter] = semanticTrustedConfigDefinition()
	semanticSchemas[semanticTrustedConfigAdapter] = semanticTrustedConfigInvocationSchema()
	configAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedConfigAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted config invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedConfigAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedConfigImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedConfigImplementation)),
		},
		ParameterAuthorization: configAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityConfigManageSelf, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedMemoryAdapter] = semanticTrustedMemoryDefinition()
	semanticSchemas[semanticTrustedMemoryAdapter] = semanticTrustedMemoryInvocationSchema()
	memoryAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedMemoryAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted memory invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedMemoryAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedMemoryImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedMemoryImplementation)),
		},
		ParameterAuthorization: memoryAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityMemoryManageAgent, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedMemoryRecallAdapter] = semanticTrustedMemoryRecallDefinition()
	semanticSchemas[semanticTrustedMemoryRecallAdapter] = semanticTrustedMemoryRecallInvocationSchema()
	memoryRecallAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedMemoryRecallAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted memory recall invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedMemoryRecallAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedMemoryRecallImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedMemoryRecallImplementation)),
		},
		ParameterAuthorization: memoryRecallAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityMemoryRecallAgent, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedTaskAdapter] = semanticTrustedTaskDefinition()
	semanticSchemas[semanticTrustedTaskAdapter] = semanticTrustedTaskInvocationSchema()
	taskAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedTaskAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted task invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedTaskAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedTaskImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedTaskImplementation)),
		},
		ParameterAuthorization: taskAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityTaskTrackLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedGoalAdapter] = semanticTrustedGoalDefinition()
	semanticSchemas[semanticTrustedGoalAdapter] = semanticTrustedGoalInvocationSchema()
	goalAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedGoalAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted goal invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedGoalAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedGoalImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedGoalImplementation)),
		},
		ParameterAuthorization: goalAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityGoalManageLongRunning, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedTemplateAdapter] = semanticTrustedTemplateDefinition()
	semanticSchemas[semanticTrustedTemplateAdapter] = semanticTrustedTemplateInvocationSchema()
	templateAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedTemplateAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted template invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedTemplateAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedTemplateImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedTemplateImplementation)),
		},
		ParameterAuthorization: templateAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityTemplateManageSession, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedSessionAdapter] = semanticTrustedSessionDefinition()
	semanticSchemas[semanticTrustedSessionAdapter] = semanticTrustedSessionInvocationSchema()
	sessionAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedSessionAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted session invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedSessionAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedSessionImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedSessionImplementation)),
		},
		ParameterAuthorization: sessionAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilitySessionManageCoding, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectSensitive}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	defsByName[semanticTrustedScheduleAdapter] = semanticTrustedScheduleDefinition()
	semanticSchemas[semanticTrustedScheduleAdapter] = semanticTrustedScheduleInvocationSchema()
	scheduleAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedScheduleAdapter])
	if err != nil {
		return nil, true, fmt.Errorf("authorize trusted schedule invocation schema: %w", err)
	}
	providers = append(providers, tool.ProviderSpec{
		AdapterName: semanticTrustedScheduleAdapter,
		Binding: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "im", ImplementationID: semanticTrustedScheduleImplementation,
			SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedScheduleImplementation)),
		},
		ParameterAuthorization: scheduleAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityScheduleAdministerLocal, Quality: 2}},
		Effects:                []tool.EffectClass{tool.EffectLocalMutation}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	if err := appendClosedHostSemanticProviders(&providers, defsByName, semanticSchemas, channel, h, userID); err != nil {
		return nil, true, err
	}
	defsByName["semantic_deliver_current_image"] = semanticCurrentImageDeliveryDefinition()
	semanticSchemas["semantic_deliver_current_image"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	imageDeliveryAuthorization, err := tool.NewParameterAuthorization(semanticSchemas["semantic_deliver_current_image"])
	if err != nil {
		return nil, true, fmt.Errorf("authorize image delivery invocation schema: %w", err)
	}
	// Delivery is a channel adapter, rather than the old path-taking send_to_im
	// gateway. Its only input is a broker-resolved ArtifactRef, so no local path,
	// base64 payload or target can be invented by the model.
	providers = append(providers, tool.ProviderSpec{
		AdapterName:            "semantic_deliver_current_image",
		Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: semanticChannelScope(channel), ImplementationID: "current-image-delivery-v1", SchemaDigest: tool.SchemaDigest([]byte("semantic-current-image-delivery-v1"))},
		ParameterAuthorization: imageDeliveryAuthorization,
		Provides:               []tool.CapabilityProvision{{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Quality: 1}},
		Consumes:               []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: semanticChannelScopes(channel),
	})
	// A file is not an image delivery with a different parameter. It has a
	// distinct artifact contract but shares the same capability and bounded
	// current-channel target semantics. The adapter is published only for a
	// channel that has a receipt-aware file transport implementation.
	if semanticFileDeliveryPublished(channel) {
		scope := semanticChannelScope(channel)
		defsByName["semantic_deliver_current_file"] = semanticCurrentFileDeliveryDefinition()
		semanticSchemas["semantic_deliver_current_file"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
		fileDeliveryAuthorization, err := tool.NewParameterAuthorization(semanticSchemas["semantic_deliver_current_file"])
		if err != nil {
			return nil, true, fmt.Errorf("authorize file delivery invocation schema: %w", err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName:            "semantic_deliver_current_file",
			Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: scope, ImplementationID: "current-file-delivery-v1", SchemaDigest: tool.SchemaDigest([]byte("semantic-current-file-delivery-v1"))},
			ParameterAuthorization: fileDeliveryAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Quality: 2}},
			Consumes:               []tool.ArtifactContract{{Kind: "document", Required: true}},
			Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: []string{scope},
		})
	}
	// Due-time channel dispatch is a separate receipt-aware selection. It is
	// published only when the inbound transport already authenticated a typed
	// group:/user: destination. The model schema stays empty: targets never
	// come from channel/destination/group_name arguments.
	if semanticVoiceDeliveryPublished(channel, semanticDestination(requestCtx)) {
		scope := semanticChannelScope(channel)
		defsByName["semantic_deliver_current_voice"] = semanticCurrentVoiceDeliveryDefinition()
		semanticSchemas["semantic_deliver_current_voice"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
		voiceDeliveryAuthorization, err := tool.NewParameterAuthorization(semanticSchemas["semantic_deliver_current_voice"])
		if err != nil {
			return nil, true, fmt.Errorf("authorize voice delivery invocation schema: %w", err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName:            "semantic_deliver_current_voice",
			Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: scope, ImplementationID: "current-voice-delivery-v1", SchemaDigest: tool.SchemaDigest([]byte("semantic-current-voice-delivery-v1"))},
			ParameterAuthorization: voiceDeliveryAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "voice"}, Quality: 1}},
			Consumes:               []tool.ArtifactContract{{Kind: "audio", MIMEType: "audio/wav", Required: true}},
			Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: []string{scope},
		})
	}
	if semanticSpecifiedTargetDeliveryPublished(channel, semanticDestination(requestCtx)) {
		scope := semanticChannelScope(channel)
		defsByName[semanticSpecifiedTargetDeliveryAdapter] = semanticSpecifiedTargetDeliveryDefinition()
		semanticSchemas[semanticSpecifiedTargetDeliveryAdapter] = semanticSpecifiedTargetDeliveryInvocationSchema()
		specifiedAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticSpecifiedTargetDeliveryAdapter])
		if err != nil {
			return nil, true, fmt.Errorf("authorize specified target delivery invocation schema: %w", err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName:            semanticSpecifiedTargetDeliveryAdapter,
			Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: scope, ImplementationID: semanticSpecifiedTargetDeliveryImplementation, SchemaDigest: tool.SchemaDigest([]byte(semanticSpecifiedTargetDeliveryImplementation))},
			ParameterAuthorization: specifiedAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: semanticSpecifiedTargetDeliveryCapability, Qualifiers: map[string]string{"format": "file"}, Quality: 2}},
			Consumes:               []tool.ArtifactContract{{Kind: "document", Required: true}},
			Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: []string{scope},
		})
	}
	if semanticTrustedMessageSendPublished(channel, semanticDestination(requestCtx)) {
		scope := semanticChannelScope(channel)
		defsByName[semanticTrustedMessageSendAdapter] = semanticTrustedMessageSendDefinition()
		semanticSchemas[semanticTrustedMessageSendAdapter] = semanticTrustedMessageSendInvocationSchema()
		messageAuthorization, err := tool.NewParameterAuthorization(semanticSchemas[semanticTrustedMessageSendAdapter])
		if err != nil {
			return nil, true, fmt.Errorf("authorize trusted message send invocation schema: %w", err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName:            semanticTrustedMessageSendAdapter,
			Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: scope, ImplementationID: semanticTrustedMessageSendImplementation, SchemaDigest: tool.SchemaDigest([]byte(semanticTrustedMessageSendImplementation))},
			ParameterAuthorization: messageAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityMessageSendIM, Quality: 1}},
			Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: []string{scope},
		})
	}
	if semanticScheduleDispatchPublished(channel, semanticDestination(requestCtx)) {
		scope := semanticChannelScope(channel)
		defsByName["semantic_schedule_dispatch"] = semanticScheduleDispatchDefinition()
		semanticSchemas["semantic_schedule_dispatch"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
		dispatchAuthorization, err := tool.NewParameterAuthorization(semanticSchemas["semantic_schedule_dispatch"])
		if err != nil {
			return nil, true, fmt.Errorf("authorize schedule dispatch invocation schema: %w", err)
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName:            "semantic_schedule_dispatch",
			Binding:                tool.ProviderBinding{Kind: "channel", ProviderID: scope, ImplementationID: "schedule-dispatch-v1", SchemaDigest: tool.SchemaDigest([]byte("semantic-schedule-dispatch-v1"))},
			ParameterAuthorization: dispatchAuthorization,
			Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityScheduleDispatchChannel, Quality: 1}},
			Effects:                []tool.EffectClass{tool.EffectExternalEffect}, Ready: true, ChannelScopes: []string{scope},
		})
	}
	coverage := dynamicInventory.coverage
	coverage.Families = append(coverage.Families,
		tool.CatalogCoverageFamily{Kind: "builtin", State: tool.CatalogCoverageComplete},
		tool.CatalogCoverageFamily{Kind: "channel", State: tool.CatalogCoverageComplete},
	)
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, true, err
	}
	// The planning registry is rebuilt per turn and sealed, while installed
	// MCP contracts carry host-issued and vendor-declared x: capabilities the
	// publisher registered at observation time. Seed the vocabulary from this
	// turn's provider set BEFORE snapshot validation — PublishWithCoverage
	// rejects a provider whose provisions name a capability the registry does
	// not know. One MCP or skill contract that cannot be seeded or published
	// is left out. A host, builtin, or channel provider still fails the turn.
	providers, err = seedExternalVocabularyDroppingDynamic(registry, providers)
	if err != nil {
		return nil, true, fmt.Errorf("seed external capability vocabulary: %w", err)
	}
	snapshot, providers, err := publishCatalogDroppingDynamicProviders(catalog, providers, coverage)
	if err != nil {
		return nil, true, fmt.Errorf("publish IM semantic catalog: %w", err)
	}
	retainDiscoveredMCPDefinitions(providers, defsByName, semanticSchemas)
	if policy.Enabled {
		if !policy.AllowsExecutableScope() {
			return nil, true, fmt.Errorf("semantic_routing_policy_mode_%s", policy.Mode)
		}
		if !policy.AllowsCatalogCoverage(string(snapshot.Coverage.State)) {
			return nil, true, fmt.Errorf("semantic_routing_catalog_coverage_%s", snapshot.Coverage.State)
		}
	}
	host := semanticHostContextFromRequest(requestCtx, channel)
	facts := semanticHostContextFacts(host)
	facts = append(make([]tool.RoutingFact, 0, len(facts)+len(documentInputs)+len(audioInputs)), facts...)
	for _, input := range documentInputs {
		binding := tool.ArtifactBindingFromRef(input.Payload.Ref)
		facts = append(facts, tool.RoutingFact{
			ID:   "trusted-document:" + input.Payload.Ref.ID,
			Kind: "artifact_available",
			Attributes: map[string]string{
				"artifact_id": input.Payload.Ref.ID,
				"kind":        input.Payload.Ref.Kind,
				"mime_type":   input.Payload.Ref.MIMEType,
			},
			Artifact:  &binding,
			Authority: tool.AuthorityChannel,
		})
	}
	facts = append(facts, semanticTrustedAudioFacts(audioInputs)...)
	policyConstraints, err := h.semanticCapabilityPolicyConstraints(userID)
	if err != nil {
		return nil, true, fmt.Errorf("resolve IM semantic capability policy: %w", err)
	}
	policyConstraints = append(policyConstraints, semanticHostContextConstraints(host)...)
	maxSelections, maxSchemaTokens := semanticPlanningBudget(requestCtx), semanticSchemaTokenBudget(requestCtx)
	if policy.Enabled {
		maxSelections, maxSchemaTokens = policy.MaxSelections, policy.MaxSchemaTokens
	}
	// The lookup profile caps the plan at the search family. A petition the
	// host already admitted adds a required wave past that cap, the planner
	// records it as unmet, and the expansion validator spends the petition
	// budget on a child it must reject. The whitelist above is the validator's
	// own set, so the cap must not trim it. This does not widen the live turn's
	// budget; only this re-plan is uncapped.
	if semanticPetitionExpansion(requestCtx) {
		maxSelections, maxSchemaTokens = 0, 0
	}
	// The user can name an installed MCP product in the turn text. A managed
	// surface never shows a vendor name to the model, so without this the only
	// way to reach that implementation would be quality ranking picking it by
	// accident — unrequested, unobservable, and silently replaced the moment
	// the observation goes bad. The pin narrows selection to what the user
	// named and nothing else.
	providerPins := semanticProviderPins(userText, providers, dynamicInventory.mcpEntries)
	// A compound vendor tool name in the user text is an exact address, not
	// just a provider preference: mint the x:mcp:<server>:<tool> need and the
	// impl-narrowed pin so the plan resolves to that binding or reports it
	// unavailable instead of reaching a sibling behind a shared capability.
	namedNeeds, namedPins := semanticNamedProviderNeeds(userText, registry, dynamicCatalog.Providers)
	needs = append(needs, namedNeeds...)
	providerPins = append(providerPins, namedPins...)
	// A required need the user installed an MCP to serve must not be answered
	// by killing the turn. See semanticNeedsWithContractOnlyGraft. It needs the
	// finished provider list and therefore runs here; it changes the needs the
	// planner sees, so it must run before Plan.
	needs = semanticNeedsWithContractOnlyGraft(needs, providers, providerPins, registry)
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: rootTaskID, SessionID: sessionID, TurnID: turnID, ChannelScope: semanticChannelScope(channel), Snapshot: snapshot, Needs: needs, Facts: facts, Constraints: policyConstraints,
		Pins:                              providerPins,
		Budget:                            semanticHostPlanningBudget(maxSelections, maxSchemaTokens),
		ReadyMCPMutationsOnPartialCatalog: true,
	})
	if err != nil {
		return nil, true, fmt.Errorf("plan IM semantic route: %w", err)
	}
	// An installed MCP tool is callable because the user installed it. A
	// confirmation dependency on that selection used to reject the whole turn
	// before any tool was listed.
	releaseInstalledMCPConfirmation(&plan)
	scopePlan, scopeResult := validateSemanticToolScope(plan, defsByName, rootTaskID, sessionID, turnID)
	preparation := &semanticPlanPreparation{
		registry: registry, plan: plan, scopePlan: scopePlan, definitions: defsByName, schemas: semanticSchemas,
		rootTaskID: rootTaskID, turnID: turnID, documentInputs: documentInputs, audioInputs: audioInputs,
		conversationLookupReused: conversationLookupReused,
		baselineWorkspace:        applyBaseline,
		policy:                   policy,
	}
	// A managed family never falls through to old name routing: the caller sees
	// a planner result only. Missing capability providers are explicit errors.
	// A selection budget that keeps an earlier required wave and only cuts
	// later ones is not a catalog miss. Refusing the whole turn made 「北京天气」
	// show "当前能力目录未覆盖这项请求" when render and deliver merely exceeded
	// the selection cap. An installed MCP need is the same shape: the tool the
	// user named may be unpublished, and the selections that did fit must still
	// open so the model loop can list the tools that published. A required
	// host need, including record.update.worklog with no ready writer, still
	// stops the turn.
	selectedDespiteUnmet := len(plan.Selections) > 0 && semanticUnmetAllowsSelectedTurn(plan.Unmet)
	if len(plan.Unmet) > 0 && !selectedDespiteUnmet {
		// Record what WAS installed at the moment a need went unserved.
		//
		// A no_feasible_provider row says "no provider was selectable", which is
		// true and nearly useless on its own: it is identical whether nothing is
		// installed at all, or whether the one MCP the user installed is sitting
		// there healthy with a published contract that no need ever named. Those
		// two situations have completely different fixes, and without the
		// installed side here the difference is unobservable from logs — which is
		// what made this class of failure take hours to find.
		logSemanticUnmetWithInventory(plan.Unmet, dynamicInventory)
		if pinned := semanticUnmetPinnedProvider(plan.Unmet); pinned {
			// The turn named the implementation. Saying the capability is
			// uncovered would blame the catalog for a decision the pin made on
			// purpose, and would leave the user believing their installed
			// product is unusable.
			labels := semanticPinnedProviderLabels(providerPins, providers, dynamicInventory.mcpEntries)
			return preparation, true, semanticPinnedProviderUnavailableError{Providers: labels, Unmet: plan.Unmet}
		}
		return preparation, true, semanticUnmetNeedsError{Unmet: plan.Unmet}
	}
	if selectedDespiteUnmet && !semanticUnmetOnlyBudget(plan.Unmet) {
		logSemanticUnmetWithInventory(plan.Unmet, dynamicInventory)
	}
	if err := semanticScopeSurfaceError(scopePlan, scopeResult); err != nil {
		return preparation, true, err
	}
	// Confirmation is represented as a plan dependency and therefore has no
	// executable tool surface in this phase. The existing confirmation UX owns
	// the user interaction; a later snapshot can materialize the selection.
	for _, selection := range plan.Selections {
		if selection.RequiresConfirm {
			return nil, true, errSemanticAwaitingConfirmation
		}
	}
	return preparation, true, nil
}

// dynamicProviderKindDroppable reports an installed MCP or skill provider.
// A failure of one of those is that tool's contract. Host, builtin, and
// channel providers stay fatal: they are the turn.
func dynamicProviderKindDroppable(provider tool.ProviderSpec) bool {
	switch strings.ToLower(strings.TrimSpace(provider.Binding.Kind)) {
	case "mcp", "skill":
		return true
	default:
		return false
	}
}

func dynamicProviderLeftUnpublished(provider tool.ProviderSpec, err error) bool {
	if err == nil || !dynamicProviderKindDroppable(provider) {
		return false
	}
	log.Printf("[semantic-routing] dynamic provider %s left unpublished: %v", strings.TrimSpace(provider.AdapterName), err)
	return true
}

func providersWithoutAdapter(providers []tool.ProviderSpec, adapter string) []tool.ProviderSpec {
	adapter = strings.TrimSpace(adapter)
	next := make([]tool.ProviderSpec, 0, len(providers))
	for _, provider := range providers {
		if strings.TrimSpace(provider.AdapterName) == adapter {
			continue
		}
		next = append(next, provider)
	}
	return next
}

func providerDeclaresCapability(provider tool.ProviderSpec, capID tool.CapabilityID) bool {
	for _, provision := range provider.Provides {
		if provision.Capability == capID {
			return true
		}
	}
	return false
}

// seedExternalVocabularyDroppingDynamic registers one external capability at
// a time. A capability only an MCP or skill provider declares, and that the
// registry rejects, drops those providers. A host provider that declares the
// same capability still fails the turn.
func seedExternalVocabularyDroppingDynamic(registry *tool.CapabilityRegistry, providers []tool.ProviderSpec) ([]tool.ProviderSpec, error) {
	kept := providers
	for _, capID := range semanticExternalCapabilityVocabulary(providers) {
		if !providersDeclareCapability(kept, capID) {
			continue
		}
		err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{capID})
		if err == nil {
			continue
		}
		if !dynamicProvidersDeclareCapability(kept, capID) || providersHaveNonDynamicCapability(kept, capID) {
			return kept, err
		}
		next := providersWithoutDynamicCapability(kept, capID)
		if len(next) == 0 || len(next) == len(kept) {
			return kept, err
		}
		log.Printf("[semantic-routing] dynamic capability %s left unseeded: %v", capID, err)
		kept = next
	}
	return kept, nil
}

func providersDeclareCapability(providers []tool.ProviderSpec, capID tool.CapabilityID) bool {
	for _, provider := range providers {
		if providerDeclaresCapability(provider, capID) {
			return true
		}
	}
	return false
}

func dynamicProvidersDeclareCapability(providers []tool.ProviderSpec, capID tool.CapabilityID) bool {
	for _, provider := range providers {
		if dynamicProviderKindDroppable(provider) && providerDeclaresCapability(provider, capID) {
			return true
		}
	}
	return false
}

func providersHaveNonDynamicCapability(providers []tool.ProviderSpec, capID tool.CapabilityID) bool {
	for _, provider := range providers {
		if dynamicProviderKindDroppable(provider) {
			continue
		}
		if providerDeclaresCapability(provider, capID) {
			return true
		}
	}
	return false
}

func providersWithoutDynamicCapability(providers []tool.ProviderSpec, capID tool.CapabilityID) []tool.ProviderSpec {
	next := make([]tool.ProviderSpec, 0, len(providers))
	for _, provider := range providers {
		if dynamicProviderKindDroppable(provider) && providerDeclaresCapability(provider, capID) {
			continue
		}
		next = append(next, provider)
	}
	return next
}

// publishCatalogDroppingDynamicProviders publishes the managed catalog. One
// MCP or skill provider the catalog rejects is removed and the rest are
// published again. An error that names a host provider, names nobody, or
// would leave the catalog empty still fails the turn.
func publishCatalogDroppingDynamicProviders(catalog *tool.ToolCatalog, providers []tool.ProviderSpec, coverage tool.CatalogCoverage) (tool.ToolCatalogSnapshot, []tool.ProviderSpec, error) {
	kept := providers
	for {
		snapshot, err := catalog.PublishWithCoverage(kept, coverage, time.Now().UTC())
		if err == nil {
			return snapshot, kept, nil
		}
		dropped, ok := discoveredMCPProviderNamedInError(err, kept)
		if !ok || !dynamicProviderKindDroppable(dropped) {
			return tool.ToolCatalogSnapshot{}, kept, err
		}
		next := providersWithoutAdapter(kept, dropped.AdapterName)
		if len(next) == 0 || len(next) == len(kept) {
			return tool.ToolCatalogSnapshot{}, kept, err
		}
		log.Printf("[semantic-routing] dynamic provider %s left unpublished: %v", strings.TrimSpace(dropped.AdapterName), err)
		kept = next
	}
}

// semanticInvocationSchema converts either a full JSON Schema definition or a
// legacy flat InputSchema into the closed object schema used by the semantic
// parameter authorizer. The renderer continues to use the trusted full tool
// definition; execution must never recover schema from model arguments.
func semanticInvocationSchema(definition map[string]interface{}) (map[string]interface{}, error) {
	function, ok := definition["function"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tool definition has no function")
	}
	parameters, ok := function["parameters"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("tool definition has no parameters")
	}
	if kind, _ := parameters["type"].(string); kind == "object" || parameters["properties"] != nil {
		return parameters, nil
	}
	// Legacy registered schemas are a flat properties map. Treat every listed
	// key as a property and preserve a closed root object.
	properties := make(map[string]interface{}, len(parameters))
	for name, spec := range parameters {
		if name == "required" || name == "additionalProperties" || name == "type" {
			continue
		}
		properties[name] = spec
	}
	return map[string]interface{}{"type": "object", "properties": properties, "additionalProperties": false}, nil
}

func semanticInvocationSchemaForRegisteredTool(registered RegisteredTool, definition map[string]interface{}) (map[string]interface{}, error) {
	if override, ok := semanticManagedDefinitionOverride(registered.Name); ok {
		return semanticInvocationSchema(override)
	}
	if len(registered.InputSchema) > 0 {
		return tool.CanonicalRegisteredToolInvocationSchema(registered.InputSchema, registered.Required)
	}
	return semanticInvocationSchema(definition)
}

// semanticManagedDefinitionOverride returns the managed model-facing definition
// for implementations whose legacy registration schema is wider than the
// capability they provide. It only ever narrows the parameter surface: the
// planner still selects the provider by capability, and the override cannot
// add a field the legacy handler does not already accept. Each entry is a
// migration bridge that disappears when the family gets a trusted adapter.
func semanticManagedDefinitionOverride(name string) (map[string]interface{}, bool) {
	switch name {
	case "generate_pdf":
		return semanticGeneratePDFDefinition(), true
	case "screenshot":
		return semanticScreenshotDefinition(), true
	}
	return nil, false
}

// The managed capability is a desktop capture, and the display index is the
// only part of it a model may choose. Which remote session to capture is a
// binding decision the host makes from its own session inventory, so the
// legacy session_id argument stays out of the managed schema; the executor
// rejects it as an unknown field. Unmanaged turns keep the legacy schema.
func semanticScreenshotDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "screenshot",
			"description": "Capture the desktop and publish an ArtifactRef. This does not deliver the image.",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"display": map[string]interface{}{"type": "integer", "description": "Optional display index; 0 is the primary display."},
				},
			},
		},
	}
}

func semanticGeneratePDFDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "generate_pdf",
			"description": "Render Markdown into a PDF and publish an ArtifactRef. This does not deliver the file.",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []interface{}{"content"},
				"properties": map[string]interface{}{
					"content":  map[string]interface{}{"type": "string", "description": "Markdown document content"},
					"title":    map[string]interface{}{"type": "string", "description": "Optional PDF cover title"},
					"doc_type": map[string]interface{}{"type": "string", "description": "Optional filename prefix: requirements/design/task_plan"},
				},
			},
		},
	}
}

// semanticGrantPromptFence is appended on every governed turn. Listed names
// stay stable for the whole turn; each listing is still a one-time grant.
// Later DAG steps appear after the current grant succeeds.
// semanticGrantPromptFence is appended on every governed turn. It must
// describe the real contract and nothing else: the live tool list is the
// ground truth (a listed name is callable, and a name that leaves after a
// success may reappear for a later step in the same turn), and lookup tools
// are legitimately reusable within their turn budget. Earlier wording
// ("one-time grants") contradicted the repeat-sibling budget and taught the
// model to declare used tools dead without ever retrying them.
const semanticGrantPromptFence = agentruntime.SemanticGrantPromptFence

func ensureSemanticGrantPromptFence(prompt string) string {
	return agentruntime.EnsureSemanticGrantPromptFence(prompt)
}

func semanticHostRejectResponse() *IMAgentResponse {
	return &IMAgentResponse{
		Text:           "当前能力目录未覆盖这项请求。",
		Error:          "semantic_capability_unmet",
		ResponseSource: "semantic_host_reject",
	}
}

func semanticHostRejectResponseForClassifierProtocolFailure() *IMAgentResponse {
	return &IMAgentResponse{
		Text:           "意图识别服务返回了不符合协议的结果，本次未执行。请重试；若持续出现，请检查模型的结构化输出兼容性。",
		Error:          "semantic_classifier_protocol_violation",
		ResponseSource: "semantic_host_reject",
	}
}

// semanticUnmappedCapabilityError names the label a turn was refused for.
//
// The label is carried as a value rather than left to be read back out of the
// message, so the refusal a user sees can depend on which family was missing
// without anything downstream parsing prose to find out.
type semanticUnmappedCapabilityError struct {
	Label intent.IntentLabel
}

func (e semanticUnmappedCapabilityError) Error() string {
	return fmt.Sprintf("semantic route has unmapped capability label %q", e.Label)
}

// semanticHostRejectResponseForPlanError turns a planning failure into what the
// user is told.
//
// Most unmapped families have nothing better to say than that the catalog does
// not cover the request. workflow_task does: the work is available, just not
// from here. Multi-phase projects are started from the workflow panel or
// /workflow, and an ordinary message deliberately never auto-starts one
// (im_entry_context.go). Answering "the capability catalog does not cover this
// request" to someone asking for a business plan describes an internal
// migration state and hides a route that exists.
func semanticHostRejectResponseForPlanError(err error) *IMAgentResponse {
	if semanticTrustedDocumentContextError(err) {
		return &IMAgentResponse{
			Text:           "之前选择的本地文档已变更、不可用或已过期。请重新选择该文档后再继续处理。",
			Error:          "semantic_trusted_document_context_stale",
			ResponseSource: "semantic_host_reject",
		}
	}
	if semanticTrustedDocumentInputMissingOrAmbiguous(err) {
		return &IMAgentResponse{
			Text:           "请在当前消息中重新选择一份要读取的文档；多个文档请先明确指定其中一份。",
			Error:          "semantic_trusted_document_input_required",
			ResponseSource: "semantic_host_reject",
		}
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) && unmapped.Label == intent.LabelWorkflowTask {
		return &IMAgentResponse{
			Text:           "这类多阶段任务要从工作流发起，普通对话不会自动启动它。用 /workflow 或界面上的工作流面板开始，就能按阶段推进。",
			Error:          "semantic_workflow_entry_required",
			ResponseSource: "semantic_host_reject",
		}
	}
	if errors.Is(err, errSemanticGenerateDeliveryConflict) {
		return &IMAgentResponse{
			Text:           "同一轮不能既按附件投递又生成新文档。分开说要发送哪份附件，或要生成什么内容。",
			Error:          "semantic_generate_delivery_conflict",
			ResponseSource: "semantic_host_reject",
		}
	}
	if semanticUnmetHasReason(err, "policy_denied") {
		return &IMAgentResponse{
			Text:           "当前渠道或策略不允许这项操作。",
			Error:          "semantic_policy_denied",
			ResponseSource: "semantic_host_reject",
		}
	}
	if semanticProducedDocumentError(err) {
		return &IMAgentResponse{
			Text:           "本任务已生成的文档已变更或不可用。这一轮不会另找文件，也不会重新投递。",
			Error:          "semantic_produced_document_stale",
			ResponseSource: "semantic_host_reject",
		}
	}
	// The turn named an implementation. Falling back to another backend without
	// saying so is the one outcome the user explicitly ruled out, so name the
	// product and stop rather than blaming catalog coverage.
	var pinnedUnavailable semanticPinnedProviderUnavailableError
	if errors.As(err, &pinnedUnavailable) {
		subject := "你指定的实现"
		if len(pinnedUnavailable.Providers) > 0 {
			subject = strings.Join(pinnedUnavailable.Providers, "、")
		}
		return &IMAgentResponse{
			Text:           subject + " 当前不可用（可能尚未完成连接，或该实现暂不支持这项能力），这一轮没有执行。已拒绝改用其他实现，避免结果来路不符你的要求。",
			Error:          "semantic_pinned_provider_unavailable",
			ResponseSource: "semantic_host_reject",
		}
	}
	if semanticUnmetHasReason(err, "artifact_dependency_missing") {
		return &IMAgentResponse{
			Text:           "这次投递没有绑定到本任务已经生成的那份文档。这一轮不会另选文件，也不会生成新文档。",
			Error:          "semantic_artifact_dependency_missing",
			ResponseSource: "semantic_host_reject",
		}
	}
	// An incomplete, stale, or not-ready observation is not proof that the
	// capability is absent. no_feasible_provider stays on the generic catalog
	// refusal, including when it is mixed with an availability reason: retry
	// would not create the missing capability.
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) && semanticUnmetOnlyReasons(unmet.Unmet,
		tool.CatalogCoverageReasonNotReady,
		tool.CatalogCoverageReasonIncomplete,
		tool.CatalogCoverageReasonStale) {
		if semanticUnmetHasReason(err, tool.CatalogCoverageReasonNotReady) {
			return &IMAgentResponse{
				Text:           "外部服务尚未就绪，这一轮没有执行。请稍后重试。",
				Error:          "semantic_provider_not_ready",
				ResponseSource: "semantic_host_reject",
			}
		}
		if semanticUnmetHasReason(err, tool.CatalogCoverageReasonIncomplete) {
			return &IMAgentResponse{
				Text:           "能力目录尚未准备完成，这一轮没有执行。请稍后重试。",
				Error:          "semantic_catalog_incomplete",
				ResponseSource: "semantic_host_reject",
			}
		}
		return &IMAgentResponse{
			Text:           "能力目录尚未完成刷新，这一轮没有执行。请稍后重试。",
			Error:          "semantic_catalog_stale",
			ResponseSource: "semantic_host_reject",
		}
	}
	return semanticHostRejectResponse()
}

// semanticHostRejectResponseForManagedSurfaceFailure preserves the user-facing
// explanations for known planning outcomes while distinguishing an unavailable
// managed control plane from a genuine catalog coverage miss.  In either case
// the host, rather than the legacy tool router, owns the response.
func semanticHostRejectResponseForManagedSurfaceFailure(err error) *IMAgentResponse {
	if err == nil {
		return &IMAgentResponse{
			Text:           "受管工具面暂时不可用，请稍后重试。",
			Error:          "semantic_surface_unavailable",
			ResponseSource: "semantic_host_reject",
		}
	}
	if errors.Is(err, errSemanticSessionCeilingSpent) {
		return &IMAgentResponse{ResponseSource: "semantic_session_ceiling"}
	}
	if errors.Is(err, errSemanticAwaitingConfirmation) || errors.Is(err, errSemanticGenerateDeliveryConflict) ||
		semanticTrustedDocumentInputError(err) || semanticProducedDocumentError(err) || semanticUnmetHasReason(err, "policy_denied") {
		return semanticHostRejectResponseForPlanError(err)
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) {
		return semanticHostRejectResponseForPlanError(err)
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) {
		if semanticUnmetOnlyBudget(unmet.Unmet) {
			return &IMAgentResponse{
				Text:           "这次请求的步骤超出了本轮计划额度，没有开始执行。",
				Error:          "semantic_plan_budget_exceeded",
				ResponseSource: "semantic_host_reject",
			}
		}
		return semanticHostRejectResponseForPlanError(err)
	}
	return &IMAgentResponse{
		Text:           "受管工具面暂时不可用，请稍后重试。",
		Error:          "semantic_surface_unavailable",
		ResponseSource: "semantic_host_reject",
	}
}

func semanticChannelCapabilityConstraints(channel string) []tool.RoutingConstraint {
	return semanticHostRoutingConstraints(channel, false)
}

func semanticHostRoutingConstraints(channel string, workflowAgentLoop bool) []tool.RoutingConstraint {
	return semanticHostContextConstraints(semanticHostContext{Channel: channel, WorkflowAgentLoop: workflowAgentLoop})
}

type semanticHostContext struct {
	Channel              string
	WorkflowAgentLoop    bool
	Destination          string
	ExecutionLayer       string
	ExpertSession        bool
	ComputerUseActive    bool
	GroupPolicyPresent   bool
	GroupWebSearchDenied bool
	GroupKnowledgeDenied bool
	GroupFileReadDenied  bool
}

func semanticHostContextFromRequest(ctx context.Context, channel string) semanticHostContext {
	host := semanticHostContext{
		Channel:           channel,
		WorkflowAgentLoop: semanticWorkflowAgentLoop(ctx),
		Destination:       semanticDestination(ctx),
		ExecutionLayer:    semanticExecutionLayer(ctx),
		ExpertSession:     semanticExpertSession(ctx),
		ComputerUseActive: semanticComputerUseActive(ctx),
	}
	if policy := semanticGroupPermissions(ctx); policy != nil {
		host.GroupPolicyPresent = true
		host.GroupWebSearchDenied = !policy.AllowWebSearch
		host.GroupKnowledgeDenied = !policy.allowsKnowledge()
		host.GroupFileReadDenied = !policy.AllowAllDirectories && len(policy.AllowedDirectories) == 0
	}
	return host
}

func semanticHostContextConstraints(host semanticHostContext) []tool.RoutingConstraint {
	constraints := make([]tool.RoutingConstraint, 0, 4)
	if normalizeIMMessagePlatformKind(host.Channel) == imMessagePlatformVEGroupExecutor {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:ve:deny-document.generate.file", Capability: "document.generate.file", Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if !semanticFileDeliveryPublished(host.Channel) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-artifact.deliver.file", Capability: "artifact.deliver.current_channel", Effect: "deny",
			Authority: tool.AuthorityChannel, Attributes: map[string]string{"format": "file"},
		})
	}
	if !semanticImageDeliveryPublished(host.Channel) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-visual.render.live_data", Capability: "visual.render.live_data", Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if !semanticSpecifiedTargetDeliveryPublished(host.Channel, host.Destination) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-artifact.deliver.specified_target", Capability: semanticSpecifiedTargetDeliveryCapability, Effect: "deny",
			Authority: tool.AuthorityChannel, Attributes: map[string]string{"format": "file"},
		})
	}
	if !semanticAudioSynthesizeLocalPublished(host.Channel) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-audio.synthesize.local", Capability: tool.CapabilityAudioSynthesizeLocal, Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if !semanticVoiceDeliveryPublished(host.Channel, host.Destination) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-artifact.deliver.voice", Capability: "artifact.deliver.current_channel", Effect: "deny",
			Authority: tool.AuthorityChannel, Attributes: map[string]string{"format": "voice"},
		})
		// Render exists only to feed current-channel voice delivery. Local
		// playback is audio.synthesize.local; do not leave a render-only plan.
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "channel:deny-audio.render.speech", Capability: tool.CapabilityAudioRenderSpeech, Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if host.WorkflowAgentLoop {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "workflow:deny-document.generate.file", Capability: "document.generate.file", Effect: "deny", Authority: tool.AuthorityRuntime,
		})
	}
	if strings.EqualFold(strings.TrimSpace(host.ExecutionLayer), string(executionLayerLight)) {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "profile:light:deny-document.generate.file", Capability: "document.generate.file", Effect: "deny", Authority: tool.AuthorityRuntime,
		})
	}
	if host.GroupWebSearchDenied {
		constraints = append(constraints,
			tool.RoutingConstraint{ID: "group:deny-information.search.web", Capability: "information.search.web", Effect: "deny", Authority: tool.AuthorityChannel},
			tool.RoutingConstraint{ID: "group:deny-information.fetch.web", Capability: tool.CapabilityInformationFetchWeb, Effect: "deny", Authority: tool.AuthorityChannel},
		)
	}
	if host.GroupKnowledgeDenied {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "group:deny-knowledge.read.local", Capability: tool.CapabilityKnowledgeReadLocal, Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if host.GroupFileReadDenied {
		constraints = append(constraints, tool.RoutingConstraint{
			ID: "group:deny-fs.read.local", Capability: tool.CapabilityFSReadLocal, Effect: "deny", Authority: tool.AuthorityChannel,
		})
	}
	if host.GroupPolicyPresent {
		for _, item := range []struct {
			id         string
			capability tool.CapabilityID
		}{
			{"group:deny-fs.write.local", tool.CapabilityFSWriteLocal},
			{"group:deny-fs.delete.local", tool.CapabilityFSDeleteLocal},
			{"group:deny-repo.inspect.vcs", tool.CapabilityRepoInspectVCS},
			{"group:deny-knowledge.ingest.local", tool.CapabilityKnowledgeIngestLocal},
			{"group:deny-knowledge.admin.maintenance", tool.CapabilityKnowledgeAdminMaintenance},
			{"group:deny-config.manage.self", tool.CapabilityConfigManageSelf},
			{"group:deny-memory.manage.agent", tool.CapabilityMemoryManageAgent},
			{"group:deny-task.track.local", tool.CapabilityTaskTrackLocal},
			{"group:deny-goal.manage.longrunning", tool.CapabilityGoalManageLongRunning},
			{"group:deny-template.manage.session", tool.CapabilityTemplateManageSession},
			{"group:deny-session.manage.coding", tool.CapabilitySessionManageCoding},
			{"group:deny-schedule.administer.local", tool.CapabilityScheduleAdministerLocal},
			{"group:deny-security.audit.read", tool.CapabilitySecurityAuditRead},
			{"group:deny-audio.transcribe.speech", tool.CapabilityAudioTranscribeSpeech},
			{"group:deny-visual.capture.desktop", "visual.capture.desktop"},
			{"group:deny-shell.execute.local", tool.CapabilityShellExecuteLocal},
			{"group:deny-document.write.office", tool.CapabilityDocumentWriteOffice},
			{"group:deny-agent.delegate.subtask", tool.CapabilityAgentDelegateSubtask},
			{"group:deny-shell.execute.remote_host", tool.CapabilityShellExecuteRemoteHost},
			{"group:deny-browser.control.web", tool.CapabilityBrowserControlWeb},
			{"group:deny-computer.control.desktop", tool.CapabilityComputerControlDesktop},
			{"group:deny-repo.mutate.vcs", tool.CapabilityRepoMutateVCS},
			{"group:deny-message.send.im", tool.CapabilityMessageSendIM},
		} {
			constraints = append(constraints, tool.RoutingConstraint{
				ID: item.id, Capability: item.capability, Effect: "deny", Authority: tool.AuthorityChannel,
			})
		}
	}
	return constraints
}

func semanticHostRoutingFacts(channel string, workflowAgentLoop bool) []tool.RoutingFact {
	return semanticHostContextFacts(semanticHostContext{Channel: channel, WorkflowAgentLoop: workflowAgentLoop})
}

func semanticHostContextFacts(host semanticHostContext) []tool.RoutingFact {
	facts := []tool.RoutingFact{
		{
			ID: "channel-scope", Kind: "channel_scope", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"scope": semanticChannelScope(host.Channel)},
		},
		{
			ID: "file-delivery-published", Kind: "file_delivery_published", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"published": semanticPublishedFlag(semanticFileDeliveryPublished(host.Channel))},
		},
		{
			ID: "image-delivery-published", Kind: "image_delivery_published", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"published": semanticPublishedFlag(semanticImageDeliveryPublished(host.Channel))},
		},
		{
			ID: "schedule-dispatch-published", Kind: "schedule_dispatch_published", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"published": semanticPublishedFlag(semanticScheduleDispatchPublished(host.Channel, host.Destination))},
		},
		{
			ID: "audio-synthesize-local-published", Kind: "audio_synthesize_local_published", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"published": semanticPublishedFlag(semanticAudioSynthesizeLocalPublished(host.Channel))},
		},
		{
			ID: "voice-delivery-published", Kind: "voice_delivery_published", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"published": semanticPublishedFlag(semanticVoiceDeliveryPublished(host.Channel, host.Destination))},
		},
	}
	if host.WorkflowAgentLoop {
		facts = append(facts, tool.RoutingFact{
			ID: "workflow-agent-loop", Kind: "workflow_agent_loop", Authority: tool.AuthorityRuntime,
			Attributes: map[string]string{"active": "true"},
		})
	}
	destination := strings.TrimSpace(host.Destination)
	if destination != "" {
		kind := "dm"
		if strings.HasPrefix(destination, "group:") {
			kind = "group"
		}
		facts = append(facts, tool.RoutingFact{
			ID: "destination-kind", Kind: "destination_kind", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{"kind": kind},
		})
	}
	if layer := strings.TrimSpace(host.ExecutionLayer); layer != "" {
		facts = append(facts, tool.RoutingFact{
			ID: "execution-layer", Kind: "execution_layer", Authority: tool.AuthorityRuntime,
			Attributes: map[string]string{"layer": layer},
		})
	}
	if host.ExpertSession {
		facts = append(facts, tool.RoutingFact{
			ID: "expert-session", Kind: "expert_session", Authority: tool.AuthorityRuntime,
			Attributes: map[string]string{"active": "true"},
		})
	}
	if host.ComputerUseActive {
		facts = append(facts, tool.RoutingFact{
			ID: "computer-use-active", Kind: "computer_use_active", Authority: tool.AuthorityRuntime,
			Attributes: map[string]string{"active": "true"},
		})
	}
	if host.GroupPolicyPresent || host.GroupWebSearchDenied || host.GroupKnowledgeDenied || host.GroupFileReadDenied {
		facts = append(facts, tool.RoutingFact{
			ID: "group-permission", Kind: "group_permission", Authority: tool.AuthorityChannel,
			Attributes: map[string]string{
				"web_search": semanticPublishedFlag(!host.GroupWebSearchDenied),
				"knowledge":  semanticPublishedFlag(!host.GroupKnowledgeDenied),
				"file_read":  semanticPublishedFlag(!host.GroupFileReadDenied),
			},
		})
	}
	return facts
}

func semanticPublishedFlag(published bool) string {
	if published {
		return "true"
	}
	return "false"
}

// closedManagedSemanticDefinitions is the Phase C safety subset for a
// migrated family: the model-visible surface may only contain CatalogRenderer
// grants. Legacy gateways, session pins and ToolNames cannot union onto it.
func closedManagedSemanticDefinitions(defs []map[string]interface{}, grants map[string]tool.InvocationGrant) []map[string]interface{} {
	return tool.ClosedManagedDefinitions(defs, grants)
}

// closedManagedSemanticDefinitionsForTurn applies the same grant close used at
// loop start, then the light-safe selection filter the authorizer will apply.
// Without this, a light turn could enter RunLoop with tools that the first
// FilterToolDefinitionsByAuthorizer immediately drops to empty.
func closedManagedSemanticDefinitionsForTurn(defs []map[string]interface{}, surface *semanticCallSurface, light bool) []map[string]interface{} {
	return closedManagedSemanticDefinitionsKeeping(defs, surface, light, nil)
}

// closedManagedSemanticDefinitionsKeeping is the light close, plus names the
// lookup authorizer will still run. The plain light filter drops bash because
// its effect is sensitive, which is how a restored shell disappears from the
// list the model is shown.
func closedManagedSemanticDefinitionsKeeping(defs []map[string]interface{}, surface *semanticCallSurface, light bool, keep func(string) bool) []map[string]interface{} {
	if surface == nil {
		return nil
	}
	filtered := tool.ClosedManagedDefinitionsForProfile(defs, surface.plan, surface.grants, light)
	if keep == nil {
		return filtered
	}
	closed := tool.ClosedManagedDefinitions(defs, surface.grants)
	have := make(map[string]bool, len(filtered))
	for _, def := range filtered {
		have[extractToolName(def)] = true
	}
	for _, def := range closed {
		name := extractToolName(def)
		if name == "" || have[name] || !keep(name) {
			continue
		}
		filtered = append(filtered, def)
		have[name] = true
	}
	return filtered
}

func isLegacySemanticBypassName(name string) bool {
	return tool.IsLegacyDynamicGatewayName(name)
}

func semanticFileDeliveryPublished(channel string) bool {
	return agentruntime.SemanticFileDeliveryPublished(channel)
}

func semanticTrustedDispatchDestination(destination string) bool {
	return agentruntime.SemanticTrustedDispatchDestination(destination)
}

func semanticScheduleDispatchPublished(channel, destination string) bool {
	return agentruntime.SemanticScheduleDispatchPublished(channel, destination)
}

func semanticVoiceDeliveryPublished(channel, destination string) bool {
	return agentruntime.SemanticVoiceDeliveryPublished(channel, destination)
}

func semanticAudioSynthesizeLocalPublished(channel string) bool {
	return agentruntime.SemanticAudioSynthesizeLocalPublished(channel)
}

// semanticUnpublishedManagedProvider is the single publication filter for the
// managed catalog. Anything that reaches a model on a managed turn passes
// through here, so the schema gate can enumerate exactly the same set the
// planner publishes instead of approximating it.
func semanticUnpublishedManagedProvider(registered RegisteredTool, channel, destination string) bool {
	if registered.SemanticCatalogState != SemanticCatalogCapability || len(registered.CapabilityProvisions) == 0 || len(registered.SemanticEffects) == 0 {
		return true
	}
	if semanticUnpublishedLocalAudioProvider(registered, channel) {
		return true
	}
	if semanticUnpublishedAudioRenderProvider(registered, channel, destination) {
		return true
	}
	for _, unpublished := range []func(RegisteredTool) bool{
		semanticUnpublishedLegacyASRProvider,
		semanticUnpublishedLegacyAuditProvider,
		semanticUnpublishedLegacyKnowledgeAdminProvider,
		semanticUnpublishedLegacyKnowledgeIngestProvider,
		semanticUnpublishedLegacyKnowledgeReadProvider,
		semanticUnpublishedLegacyFileWriteProvider,
		semanticUnpublishedLegacyFileReadProvider,
		semanticUnpublishedLegacyRepoInspectProvider,
		semanticUnpublishedLegacyWebFetchProvider,
		semanticUnpublishedLegacyWebSearchProvider,
		semanticUnpublishedLegacyClockProvider,
		semanticUnpublishedLegacyConfigProvider,
		semanticUnpublishedLegacyMemoryProvider,
		semanticUnpublishedLegacyTaskProvider,
		semanticUnpublishedLegacyGoalProvider,
		semanticUnpublishedLegacyTemplateProvider,
		semanticUnpublishedLegacySessionProvider,
		semanticUnpublishedLegacyScheduleProvider,
		semanticUnpublishedLegacyOfficeProvider,
		semanticUnpublishedLegacyShellProvider,
		semanticUnpublishedLegacyDelegateProvider,
		semanticUnpublishedLegacySSHProvider,
		semanticUnpublishedLegacyBrowserProvider,
		semanticUnpublishedLegacyComputerUseProvider,
		semanticUnpublishedLegacyMessageSendProvider,
		semanticUnpublishedLegacyMailboxSendProvider,
		semanticUnpublishedLegacyMailboxConfigureProvider,
		semanticUnpublishedLegacyMailboxDeleteProvider,
		semanticUnpublishedLegacyRepoMutateProvider,
		semanticUnpublishedLegacyDownloadProvider,
	} {
		if unpublished(registered) {
			return true
		}
	}
	return false
}

func semanticUnpublishedLocalAudioProvider(registered RegisteredTool, channel string) bool {
	if semanticAudioSynthesizeLocalPublished(channel) {
		return false
	}
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityAudioSynthesizeLocal {
			return true
		}
	}
	return false
}

func semanticUnpublishedAudioRenderProvider(registered RegisteredTool, channel, destination string) bool {
	if semanticVoiceDeliveryPublished(channel, destination) {
		return false
	}
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityAudioRenderSpeech {
			return true
		}
	}
	return false
}

func semanticImageDeliveryPublished(channel string) bool {
	return agentruntime.SemanticImageDeliveryPublished(channel)
}

func semanticCurrentImageDeliveryDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "semantic_deliver_current_image",
			"description": "Deliver the approved image artifact to the current channel.",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
}

func semanticCurrentVoiceDeliveryDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "semantic_deliver_current_voice",
			"description": "Deliver the approved speech artifact to the current channel as a voice message.",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
}

func semanticCurrentFileDeliveryDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "semantic_deliver_current_file",
			"description": "Attaches the published file to this chat. Call it with no arguments when it is listed. No path and no destination. This is not a send and does not need approval.",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
}

func semanticScheduleDispatchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "semantic_schedule_dispatch",
			"description": "Register a due-time channel delivery intent for the current trusted destination. This does not send now and does not accept channel, destination, or group arguments.",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		},
	}
}

func semanticTrustedDocumentReadDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "semantic_read_trusted_document",
			"description": "Read the approved document attachment. The document identity and path are host-bound; only pagination or format-specific read options are accepted.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"max_chars":    map[string]interface{}{"type": "integer"},
					"offset":       map[string]interface{}{"type": "integer"},
					"line_numbers": map[string]interface{}{"type": "boolean"},
					"sheet":        map[string]interface{}{"type": "string"},
					"range":        map[string]interface{}{"type": "string"},
					"max_rows":     map[string]interface{}{"type": "integer"},
					"max_slides":   map[string]interface{}{"type": "integer"},
					"slide_offset": map[string]interface{}{"type": "integer"},
				},
				"additionalProperties": false,
			},
		},
	}
}

// semanticRouteDiagnosticForTurn is deliberately read-only and gives legacy
// loops the same capability-first explanation while their executor migration
// is still in progress. It does not alter their tool list.
func (h *IMMessageHandler) semanticRouteDiagnosticForTurn(userID, userText, channel string) semanticRouteDiagnostic {
	return h.semanticRouteDiagnosticForTurnWithContext(nil, userID, userText, channel, nil)
}

func (h *IMMessageHandler) semanticRouteDiagnosticForTurnWithContext(ctx *LoopContext, userID, userText, channel string, attachments []MessageAttachment) semanticRouteDiagnostic {
	rootTaskID, turnID := semanticRoutingIdentity(ctx, userID, userText)
	prepared, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(userID, userText, channel, rootTaskID, turnID, semanticIntentFromLoopContext(ctx), attachments)
	if !handled {
		return semanticRouteDiagnostic{}
	}
	if err != nil {
		return semanticRouteDiagnostic{Handled: true, Reason: err.Error()}
	}
	if prepared == nil {
		return semanticRouteDiagnostic{Handled: true, Reason: "semantic_route_no_surface"}
	}
	return semanticRouteDiagnostic{Handled: true, PlanID: prepared.plan.ID, Reason: "semantic_route_ready"}
}

func semanticIntentFromLoopContext(ctx *LoopContext) *intent.ClassificationResult {
	if ctx == nil || ctx.Runtime.SemanticIntent == nil {
		return nil
	}
	result := *ctx.Runtime.SemanticIntent
	return &result
}

// semanticRoutingContext derives the planning context from the trusted loop
// lifetime.  A request replacement/cancellation therefore stops semantic
// catalog loading before it can publish a new model-visible surface.  The
// returned cancel function must always be called because LoopContext.Context
// installs a watcher for the loop cancellation signal.
type semanticWorkflowLoopKey struct{}
type semanticNoWorkflowInterceptionKey struct{}

func withSemanticNoWorkflowInterception(ctx context.Context, optOut bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !optOut {
		return ctx
	}
	return context.WithValue(ctx, semanticNoWorkflowInterceptionKey{}, true)
}

func semanticNoWorkflowInterception(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(semanticNoWorkflowInterceptionKey{}).(bool)
	return value
}

type semanticPlanningBudgetKey struct{}
type semanticDestinationKey struct{}
type semanticParentContinuationKey struct{}
type semanticExecutionLayerKey struct{}
type semanticExpertSessionKey struct{}
type semanticComputerUseActiveKey struct{}
type semanticGroupPermissionKey struct{}

func withSemanticWorkflowLoop(ctx context.Context, workflowAgentLoop bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !workflowAgentLoop {
		return ctx
	}
	return context.WithValue(ctx, semanticWorkflowLoopKey{}, true)
}

func semanticWorkflowAgentLoop(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(semanticWorkflowLoopKey{}).(bool)
	return value
}

func withSemanticPlanningBudget(ctx context.Context, maxSelections int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxSelections < 0 {
		maxSelections = 0
	}
	return context.WithValue(ctx, semanticPlanningBudgetKey{}, maxSelections)
}

func semanticPlanningBudget(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	value, _ := ctx.Value(semanticPlanningBudgetKey{}).(int)
	return value
}

func semanticHostPlanningBudget(maxSelections, maxSchemaTokens int) tool.PlanningBudget {
	return tool.NewPlanningBudget(maxSelections, maxSchemaTokens)
}

type semanticSchemaTokenBudgetKey struct{}

func withSemanticSchemaTokenBudget(ctx context.Context, maxSchemaTokens int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxSchemaTokens < 0 {
		maxSchemaTokens = 0
	}
	return context.WithValue(ctx, semanticSchemaTokenBudgetKey{}, maxSchemaTokens)
}

func semanticSchemaTokenBudget(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	value, _ := ctx.Value(semanticSchemaTokenBudgetKey{}).(int)
	return value
}

func withSemanticDestination(ctx context.Context, destination string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return ctx
	}
	return context.WithValue(ctx, semanticDestinationKey{}, destination)
}

func semanticDestination(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(semanticDestinationKey{}).(string)
	return strings.TrimSpace(value)
}

func withSemanticExecutionLayer(ctx context.Context, layer string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	layer = strings.TrimSpace(layer)
	if layer == "" {
		return ctx
	}
	return context.WithValue(ctx, semanticExecutionLayerKey{}, layer)
}

func semanticExecutionLayer(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(semanticExecutionLayerKey{}).(string)
	return strings.TrimSpace(value)
}

func withSemanticExpertSession(ctx context.Context, active bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !active {
		return ctx
	}
	return context.WithValue(ctx, semanticExpertSessionKey{}, true)
}

func semanticExpertSession(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(semanticExpertSessionKey{}).(bool)
	return value
}

func withSemanticComputerUseActive(ctx context.Context, active bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !active {
		return ctx
	}
	return context.WithValue(ctx, semanticComputerUseActiveKey{}, true)
}

func semanticComputerUseActive(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(semanticComputerUseActiveKey{}).(bool)
	return value
}

func withSemanticGroupPermissions(ctx context.Context, policy *lansengerGroupPermissionPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if policy == nil {
		return ctx
	}
	return context.WithValue(ctx, semanticGroupPermissionKey{}, policy)
}

func semanticGroupPermissions(ctx context.Context) *lansengerGroupPermissionPolicy {
	if ctx == nil {
		return nil
	}
	policy, _ := ctx.Value(semanticGroupPermissionKey{}).(*lansengerGroupPermissionPolicy)
	return policy
}

type lookupContinuationFetchKey struct{}

type lookupContinuationOpenCeilingKey struct{}

func withLookupContinuationOpenCeiling(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, lookupContinuationOpenCeilingKey{}, true)
}

func lookupContinuationOpenCeiling(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	on, _ := ctx.Value(lookupContinuationOpenCeilingKey{}).(bool)
	return on
}

func semanticResidueWithoutLookupCeiling(remaining map[string]int) map[string]int {
	return semanticResidueDropSpent(remaining, "information.search.web", string(tool.CapabilityInformationFetchWeb))
}

// semanticResidueDropSpent removes a finished ceiling so the next lookup can
// run. A positive remaining count still limits that capability.
func semanticResidueDropSpent(remaining map[string]int, keys ...string) map[string]int {
	if len(remaining) == 0 || len(keys) == 0 {
		return remaining
	}
	out := cloneResidueRemaining(remaining)
	for _, key := range keys {
		if limit, ok := out[key]; ok && limit <= 0 {
			delete(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func withLookupContinuationFetch(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, lookupContinuationFetchKey{}, true)
}

func lookupContinuationFetches(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	on, _ := ctx.Value(lookupContinuationFetchKey{}).(bool)
	return on
}

func ensureLookupContinuationCarriedNeeds(needs []tool.CapabilityNeed, names []string, confidence float64) ([]tool.CapabilityNeed, []string) {
	if len(names) == 0 {
		return needs, nil
	}
	if confidence <= 0 {
		confidence = 1
	}
	have := make(map[tool.CapabilityID]bool, len(needs))
	for _, need := range needs {
		have[need.Capability] = true
	}
	var added []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		capability, ok := semanticPetitionableCapabilities[name]
		if !ok || have[capability] || agent.IsLightTurnToolAllowed(name) {
			continue
		}
		have[capability] = true
		added = append(added, string(capability))
		base := tool.CapabilityNeed{
			ID:          lookupCarryNeedIDPrefix + string(capability),
			Capability:  capability,
			Polarity:    tool.NeedRequire,
			Required:    false,
			Confidence:  confidence,
			EvidenceIDs: []string{"intent:lookup_continuation_carry"},
		}
		// One family, several calls. The lookup iteration cap bounds the
		// turn; a single invocation would refuse the second bash.
		needs = append(needs, base)
		needs = append(needs, tool.ExtendRepeatFamily(base, 1, lookupContinuationIterationBudget, confidence, base.EvidenceIDs)...)
	}
	return needs, added
}

// lookupCarryNeedIDPrefix sorts after need:lookup-continuation:, so a tight
// selection budget fills web_fetch before the carried shell siblings.
const lookupCarryNeedIDPrefix = "need:lookup-z-carry:"

func withoutLookupCarryNeeds(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	if len(needs) == 0 {
		return needs
	}
	kept := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if strings.HasPrefix(need.ID, lookupCarryNeedIDPrefix) {
			continue
		}
		kept = append(kept, need)
	}
	return kept
}

func ensureLookupContinuationFetchNeed(needs []tool.CapabilityNeed, confidence float64) []tool.CapabilityNeed {
	for _, need := range needs {
		if need.Capability == tool.CapabilityInformationFetchWeb {
			return needs
		}
	}
	if confidence <= 0 {
		confidence = 1
	}
	base := tool.CapabilityNeed{
		ID:          "need:lookup-continuation:information.fetch.web",
		Capability:  tool.CapabilityInformationFetchWeb,
		Polarity:    tool.NeedRequire,
		Required:    false,
		Confidence:  confidence,
		EvidenceIDs: []string{"intent:lookup_continuation"},
	}
	// The page-fetch rule allows several opens. One sibling would refuse the
	// second page after the first fetch succeeds.
	needs = append(needs, base)
	return append(needs, tool.ExtendRepeatFamily(base, 1, 5, confidence, base.EvidenceIDs)...)
}

func semanticRoutingContext(loop *LoopContext) (context.Context, context.CancelFunc) {
	if loop != nil {
		ctx, cancel := loop.Context()
		ctx = withSemanticWorkflowLoop(ctx, loop.WorkflowAgentLoop)
		ctx = withSemanticNoWorkflowInterception(ctx, loop.NoWorkflowInterception)
		ctx = withSemanticDestination(ctx, sessionGovernedDestination(loop))
		ctx = withSemanticPlanningBudget(ctx, loop.Runtime.Execution.ToolBudget)
		ctx = withSemanticSchemaTokenBudget(ctx, loop.Runtime.Execution.SchemaTokenBudget)
		ctx = withSemanticExecutionLayer(ctx, loop.Runtime.Execution.Layer)
		if expertDefForUserID(loop.UserID) != nil {
			ctx = withSemanticExpertSession(ctx, true)
		}
		// Routing text is the gate's input, not permission. Only a settled open
		// (explicit trigger, a real app operation, or an in-progress session)
		// may publish the computer-use fact. A plain chat turn always has text.
		if loop.ComputerUseActive && !loop.ComputerUseBlockedForLocalFileWork {
			ctx = withSemanticComputerUseActive(ctx, true)
		}
		if loop.LansengerGroupPermissions != nil {
			ctx = withSemanticGroupPermissions(ctx, loop.LansengerGroupPermissions)
		}
		ctx = withSemanticConversationHistory(ctx, loop.History)
		if loop.semanticResidueLookupFacts {
			ctx = withSemanticReusableLookupFacts(ctx)
		}
		if len(loop.semanticResidueRemaining) > 0 {
			ctx = withSemanticResidueRemaining(ctx, loop.semanticResidueRemaining)
		}
		if lookupContinuationProfile(loop.Runtime.Execution) {
			// A short lookup inside an open task is a new question. The
			// previous wave's zero search/fetch ceiling must not close it.
			ctx = withLookupContinuationOpenCeiling(ctx)
			if loop.Runtime.SemanticIntent != nil && loop.Runtime.SemanticIntent.Primary == intent.LabelSearch {
				ctx = withLookupContinuationFetch(ctx)
			}
		}
		return ctx, cancel
	}
	return context.WithCancel(context.Background())
}

func semanticRoutingRequestErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		// The caller cannot safely continue into the legacy name-router after a
		// semantic planning request was cancelled: its classification/catalog
		// observations may be incomplete or already superseded by a replan.
		return ctx.Err()
	default:
		return nil
	}
}

// semanticReplanFailureEligible is deliberately a closed lifecycle vocabulary.
// A model's bad arguments are not a reason to mint another authorization, and
// unknown/awaiting external effects require receipt reconciliation instead of
// redispatch. Only a confirmed selected-binding lifecycle failure may create
// one bounded child revision.
func semanticReplanFailureEligible(reasonCode string) bool {
	return tool.ReplanFailureEligible(reasonCode)
}

func cloneSemanticMessageAttachments(in []MessageAttachment) []MessageAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]MessageAttachment, len(in))
	copy(out, in)
	return out
}

// semanticFailureReplanPlanContext carries the published bundle and the stored
// workspace ceiling into the planner. The ceiling key is set only when the
// parent recorded it. A zero flag leaves classification in charge, so a
// surface that never stored the bit does not grow read/write/shell siblings.
func semanticFailureReplanPlanContext(requestCtx context.Context, input semanticReplanInput, bundleKey intent.IntentLabel) context.Context {
	planCtx := withSemanticSlimOffice(withSemanticArchetypeBundleKeyOverride(requestCtx, bundleKey), input.SlimOffice, input.ShortDocumentEdit)
	if input.BaselineWorkspace {
		planCtx = withSemanticForceBaselineWorkspace(planCtx)
	}
	return planCtx
}

// replanSemanticCallSurface is retained for direct callers that have no loop
// lifetime. Production callbacks must use the context-bearing variant below:
// a cancelled/replaced turn must not publish a new model-visible revision.
func (h *IMMessageHandler) replanSemanticCallSurface(surface *semanticCallSurface, reasonCode string) (*semanticCallSurface, []map[string]interface{}, error) {
	return h.replanSemanticCallSurfaceWithContext(context.Background(), surface, reasonCode)
}

// replanSemanticCallSurfaceWithContext publishes one child revision from the
// original trusted input. The candidate planner may see a newer catalog
// snapshot, but receives exactly the original semantic classification,
// channel and ingress attachments. It never receives a model tool-call, old
// grant or arguments. The request context is the owning loop's cancellable
// lifetime, never a detached background context.
func (h *IMMessageHandler) replanSemanticCallSurfaceWithContext(requestCtx context.Context, surface *semanticCallSurface, reasonCode string) (*semanticCallSurface, []map[string]interface{}, error) {
	if surface == nil || surface.replan == nil || surface.routeState == nil || surface.issuer == nil || surface.executor == nil || surface.registry == nil {
		return nil, nil, fmt.Errorf("semantic replan state is unavailable")
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	if !semanticReplanFailureEligible(reasonCode) {
		return nil, nil, fmt.Errorf("semantic replan reason is not eligible")
	}
	if surface.replan.Attempts >= 1 {
		return nil, nil, fmt.Errorf("semantic replan attempt exhausted")
	}
	input := *surface.replan
	// The replan re-derives needs from the stored classification; keep the
	// archetype bundle key the published plan was built with so a petitioned
	// document label on the classification cannot swap bundles mid-lineage.
	bundleKey := input.BundleKey
	if strings.TrimSpace(string(bundleKey)) == "" {
		bundleKey = semanticArchetypeBundleKey(input.Classification)
	}
	planCtx := semanticFailureReplanPlanContext(requestCtx, input, bundleKey)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachmentsWithSession(
		planCtx, input.UserID, "", input.Channel, input.RootTaskID,
		semanticReplanTurnID(surface.scope.TurnID, input.Attempts+1), surface.scope.SessionID, &input.Classification, cloneSemanticMessageAttachments(input.Attachments),
	)
	if err != nil {
		return nil, nil, err
	}
	if !handled || prepared == nil {
		return nil, nil, fmt.Errorf("semantic replan no governed candidate")
	}
	if err := validateSemanticReplanSubset(surface.plan, prepared.plan); err != nil {
		// A stale dynamic binding is no longer present in the refreshed catalog,
		// so its provider identity must be allowed to change. The replacement is
		// still constrained to the exact governed need, capability qualifiers,
		// phase, effects, artifact contract and parameter authorization captured
		// by the parent; only that narrow binding refresh is permitted.
		if !semanticReplanIsBindingOnlyReplacement(surface.plan, prepared.plan) {
			return nil, nil, err
		}
	}
	return h.publishSemanticChildRevision(requestCtx, surface, prepared,
		&semanticReplanInput{UserID: input.UserID, Channel: input.Channel, RootTaskID: input.RootTaskID, Classification: input.Classification, Attachments: cloneSemanticMessageAttachments(input.Attachments), Attempts: input.Attempts + 1, ConversationLookupReused: input.ConversationLookupReused, BundleKey: bundleKey, SlimOffice: input.SlimOffice, ShortDocumentEdit: input.ShortDocumentEdit, BaselineWorkspace: input.BaselineWorkspace},
		"replan", strings.TrimSpace(reasonCode))
}

// publishSemanticChildRevision is the shared durable tail of surface
// succession: both a failure replan and a petitioned composite expansion end
// as exactly one child revision published against the current parent state,
// with inherited materializations and the executor's completed-set projected
// forward. The caller owns admission (eligibility, subset/expansion
// validation); this helper owns publication mechanics only.
func (h *IMMessageHandler) publishSemanticChildRevision(requestCtx context.Context, surface *semanticCallSurface, prepared *semanticPlanPreparation, childReplan *semanticReplanInput, traceSubject, traceReason string) (*semanticCallSurface, []map[string]interface{}, error) {
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	if err := validatePreparedSemanticToolScope(prepared); err != nil {
		return nil, nil, err
	}
	current, err := surface.routeState.CurrentRevision(surface.scope)
	if err != nil {
		return nil, nil, fmt.Errorf("load semantic replan parent: %w", err)
	}
	childScope := surface.scope
	childScope.PlanID = prepared.plan.ID
	childScope.TurnID = prepared.turnID
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	prepared.plan.Trace.Events = append(prepared.plan.Trace.Events, tool.TraceEvent{
		Stage: tool.TraceStageRecovery, Subject: traceSubject, Event: "child_published", ReasonCode: traceReason,
	})
	publishRequest := tool.RouteRevisionPublishRequest{
		Scope: childScope, Plan: prepared.plan, ExpectedParent: &current, SnapshotDigest: prepared.plan.SnapshotDigest,
	}
	var state tool.RouteState
	var initialGrants []tool.InvocationGrant
	state, initialGrants, err = tool.PublishCurrentSurface(surface.coordinator, surface.routeState, tool.NewSurfacePublishRequest(publishRequest, surface.tenantID, surface.issuer, semanticInvocationGrantTTL, time.Now().UTC()))
	if err != nil {
		return nil, nil, fmt.Errorf("publish semantic replan: %w", err)
	}
	artifactStore, err := h.semanticArtifactStore()
	if err != nil {
		return nil, nil, err
	}
	child := &semanticCallSurface{
		plan: state.Plan, scope: childScope, scopePlan: prepared.scopePlan, issuer: surface.issuer, executor: surface.executor, routeState: surface.routeState, hostCalls: surface.hostCalls, coordinator: surface.coordinator, tenantID: surface.tenantID, registry: prepared.registry,
		hostConnectionID: "agent-loop-surface:" + newSemanticEphemeralIdentity(),
		completed:        make(map[string]bool), materialized: make(map[string]bool), schemas: prepared.definitions, parameterSchemas: prepared.schemas,
		grants: make(map[string]tool.InvocationGrant), retiredGrants: make(map[string]tool.InvocationGrant), rendered: make(map[string]bool), artifacts: newSemanticArtifactBroker(childScope, artifactStore, surface.routeState, surface.coordinator), pendingArtifacts: make(map[string][]tool.ArtifactPayload),
		replan: childReplan,
	}
	for _, materialization := range state.Materializations {
		if materialization.State == tool.RouteMaterializationExposed {
			if surface.coordinator == nil {
				return nil, nil, fmt.Errorf("semantic child revision inherited materialization")
			}
			if _, err := tool.BindIssuedGrant(materialization.Grant, child.grants); err != nil {
				return nil, nil, err
			}
			child.materialized[materialization.Grant.SelectionID] = true
		}
	}
	for _, grant := range initialGrants {
		if _, err := tool.BindIssuedGrant(grant, child.grants); err != nil {
			return nil, nil, err
		}
		child.materialized[grant.SelectionID] = true
	}
	completed, err := tool.LoadCompletedSelections(child.executor, child.routeState, child.scope)
	if err != nil {
		return nil, nil, err
	}
	child.completed = completed
	var definitions []map[string]interface{}
	if child.coordinator != nil {
		definitions, err = visibleSemanticCallSurfaceDefinitions(child)
	} else {
		definitions, err = refreshSemanticCallSurface(child)
	}
	if err != nil {
		return nil, nil, err
	}
	return child, definitions, nil
}

func semanticReplanTurnID(parentTurnID string, attempt uint8) string {
	return tool.ReplanTurnID(parentTurnID, attempt)
}

// semanticPetitionTurnID derives the expansion child's turn identity from the
// parent turn and the added label, keeping it distinct from failure-replan
// identities without consuming the failure-replan attempt budget.
func semanticPetitionTurnID(parentTurnID string, label intent.IntentLabel) string {
	return tool.PetitionTurnID(parentTurnID, string(label))
}

// semanticPetitionableCapabilities is the closed name→capability inventory a
// model petition may reference. It covers every stable model-visible name the
// planner can render on a managed chat surface whose capability carries an
// owner-reviewed rule label: the agent decides what it needs, and the harness
// only enforces deterministic safety policy (the group-policy gate, the
// one-petition-per-class turn budget, and the strict-superset expansion
// validator). A name enters this map only when its capability resolves to a
// rule label whose expansion renders exactly this name — legacy aliases the
// managed catalog unpublished (list_directory, search_files, edit_file),
// quarantined capabilities with no rule label (send_im_text), and names with
// no stable render spelling (trusted document read, tts_local, channel
// dispatch) stay out and fail closed before any planning work happens.
var semanticPetitionableCapabilities = map[string]tool.CapabilityID{
	// Read-only lookup/inspection legs.
	"web_search":       "information.search.web",
	"web_fetch":        tool.CapabilityInformationFetchWeb,
	"current_datetime": "information.current_time",
	"read_file":        tool.CapabilityFSReadLocal,
	"git_status":       tool.CapabilityRepoInspectVCS,
	"knowledge_search": tool.CapabilityKnowledgeReadLocal,
	"session_search":   tool.CapabilitySecurityAuditRead,
	"asr":              tool.CapabilityAudioTranscribeSpeech,
	// Effectful legs: the agent's general problem-solving fallback. A turn
	// whose plan under-rendered the means to finish — the 2026-08-26 PPT turn
	// died because the model could not reach a command runner to craft the
	// deck itself — may petition one of them once per turn, subject to the
	// group-policy gate in PetitionToolCall.
	"bash":                tool.CapabilityShellExecuteLocal,
	"delegate_task":       tool.CapabilityAgentDelegateSubtask,
	"download_file":       tool.CapabilityArtifactAcquireRemote,
	"office":              tool.CapabilityDocumentWriteOffice,
	"generate_pdf":        "document.generate.file",
	"send_file":           "artifact.deliver.current_channel",
	"send_to_im":          "artifact.deliver.specified_target",
	"screenshot":          "visual.capture.desktop",
	"open":                tool.CapabilitySystemLaunchLocal,
	"ssh":                 tool.CapabilityShellExecuteRemoteHost,
	"browser":             tool.CapabilityBrowserControlWeb,
	"computer_use":        tool.CapabilityComputerControlDesktop,
	"write_file":          tool.CapabilityFSWriteLocal,
	"delete_file":         tool.CapabilityFSDeleteLocal,
	"git_commit":          tool.CapabilityRepoMutateVCS,
	"record_audio":        tool.CapabilityAudioCaptureMicrophone,
	"knowledge_save_text": tool.CapabilityKnowledgeIngestLocal,
	"knowledge_maintain":  tool.CapabilityKnowledgeAdminMaintenance,
	"memory":              tool.CapabilityMemoryManageAgent,
	"task":                tool.CapabilityTaskTrackLocal,
	"goal":                tool.CapabilityGoalManageLongRunning,
	"manage_template":     tool.CapabilityTemplateManageSession,
	"list_sessions":       tool.CapabilitySessionManageCoding,
	"manage_schedule":     tool.CapabilityScheduleAdministerLocal,
	"manage_config":       tool.CapabilityConfigManageSelf,
	"mis_data":            tool.CapabilityBusinessDataMIS,
	"database":            tool.CapabilityBusinessDataMIS,
	"database_query":      tool.CapabilityBusinessDataRead,
	"mailbox_query":       tool.CapabilityMessageReadMailbox,
	// Mailbox mutations join the petition inventory because the planner can
	// render all three from their rule labels and each carries its own
	// deterministic gate: send and delete freeze a draft that only the user's
	// approval turns into an external effect, while configure only writes the
	// account the user supplied. Leaving them out stranded a read-only mailbox
	// turn after the user authorized the action (production 2026-10-27: the
	// model found mailbox_delete in the directory, called it, and the
	// unrendered fence had no petition path — the turn answered "no delete
	// tool" while the user was saying 授权删除).
	"mailbox_send":      tool.CapabilityMessageSendMailbox,
	"mailbox_delete":    tool.CapabilityMessageDeleteMailbox,
	"mailbox_configure": tool.CapabilityMessageConfigureMailbox,
}

// semanticReadOnlyGovernedLabels are the rule labels whose templates carry no
// mutation, delivery, or external effect. This is the single source for
// read-only governed membership: the planning-side family/hint gates
// (semanticReadOnlyGovernedLabel) and the petition budget/group-policy gate
// both consult it. For petitions, read-only labels are admitted in
// group-restricted contexts (restrictive policy can still fail the
// expansion), while every other label is effectful and denied outright there.
// A label not listed here is effectful by default, so a newly added rule
// label can never accidentally inherit the read-only gate.
var semanticReadOnlyGovernedLabels = map[intent.IntentLabel]bool{
	intent.LabelSearch:          true,
	intent.LabelLiveData:        true,
	intent.LabelWebFetch:        true,
	intent.LabelFileRead:        true,
	intent.LabelGitInspect:      true,
	intent.LabelKnowledgeRead:   true,
	intent.LabelAuditRead:       true,
	intent.LabelCurrentTime:     true,
	intent.LabelDocumentRead:    true,
	intent.LabelAudioTranscribe: true,
	intent.LabelWorklogRead:     true,
	intent.LabelMailboxRead:     true,
}

// semanticPetitionLookupLabels is the GUI alias of the shared petition
// preference order. information.search.web is the sole required template of
// both search and live_data; search wins so a petition expands the
// reference-freshness family.
var semanticPetitionLookupLabels = agentservice.PetitionLookupPreference

// semanticPetitionLabelForCapability resolves the intent label a petition for
// the capability may add, derived deterministically from the rule set rather
// than from the turn's classification: a label whose single required template
// is exactly this capability wins (LabelFileRead for fs.read.local, LabelOffice
// for document.write.office — its delivery leg stays optional); when no such
// label exists, any label whose templates mention the capability qualifies
// (LabelDocumentGenerate for document.generate.file, LabelScreenshot for
// visual.capture.desktop), because the expansion validator admits exactly the
// label's template needs either way. Ties break by PetitionLookupPreference,
// then by label name. A capability no rule label backs is not petitionable.
func semanticPetitionLabelForCapability(capability tool.CapabilityID) (intent.IntentLabel, bool) {
	return agentservice.PetitionLabelForCapability(capability, imSemanticIntentRuleSet)
}

func semanticPetitionPreferredLabel(candidates []intent.IntentLabel) (intent.IntentLabel, bool) {
	return agentservice.PetitionPreferredLabel(candidates)
}

// semanticPetitionLabelRewritesBoundFileDelivery reports a petition whose
// reviewed templates would mint a new document or a second file consumer.
// Image and voice current-channel templates are a different delivery and
// stay expandable. The caller uses this only when specified-target is
// already live, so a delivery still waiting on its producer is unaffected.
func semanticPetitionLabelRewritesBoundFileDelivery(label intent.IntentLabel) bool {
	for _, template := range imSemanticIntentRuleSet[label] {
		switch template.Capability {
		case agentservice.CapabilityDocumentGenerate, tool.CapabilityDocumentWriteOffice:
			return true
		case agentservice.CapabilityArtifactDeliverCurrent:
			format := ""
			if template.Qualifiers != nil {
				format = template.Qualifiers[agentservice.QualifierArtifactFormat]
			}
			if format == "" || format == agentservice.ArtifactFormatFile {
				return true
			}
		}
	}
	return false
}

// semanticPetitionIsEffectful reports whether the petitioned name resolves to
// an effectful rule label rather than a read-only one. The caller applies the
// group-policy gate and the separate effectful budget to these; read-only
// petitions keep their own budget and no group gate.
func semanticPetitionIsEffectful(name string) bool {
	capability, ok := semanticPetitionableCapabilities[strings.TrimSpace(name)]
	if !ok {
		return false
	}
	label, ok := semanticPetitionLabelForCapability(capability)
	if !ok {
		return false
	}
	return !semanticReadOnlyGovernedLabels[label]
}

// validateSemanticPetitionExpansion is the strict-superset whitelist for a
// petitioned child revision: every parent selection must survive with
// unchanged authority (a refreshed provider binding is allowed), and every
// added selection must be one of the petitioned label's rule template needs.
func validateSemanticPetitionExpansion(parent, child tool.ToolPlan, label intent.IntentLabel) error {
	return agentservice.ValidatePetitionExpansion(parent, child, imSemanticIntentRuleSet[label])
}

// petitionExpandSemanticCallSurface publishes one child revision that adds the
// petitioned label's rule template needs to the turn's plan. It reuses the
// original trusted classification plus that single label — never model output,
// tool arguments, or prompt text — and the expansion validator guarantees the
// child plan is exactly the parent plan plus the label's template needs. A
// missing provider or a restrictive policy fails the re-plan or the validator
// and the petition is denied cleanly.
func (h *IMMessageHandler) petitionExpandSemanticCallSurface(requestCtx context.Context, surface *semanticCallSurface, label intent.IntentLabel) (*semanticCallSurface, []map[string]interface{}, error) {
	if surface == nil || surface.replan == nil || surface.routeState == nil || surface.issuer == nil || surface.executor == nil || surface.registry == nil {
		return nil, nil, fmt.Errorf("semantic replan state is unavailable")
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, err
	}
	input := *surface.replan
	expanded := input.Classification
	if !classificationHasLabel(expanded, label) {
		expanded.Secondary = append(append([]intent.IntentLabel(nil), expanded.Secondary...), label)
	}
	// The expansion adds exactly the petitioned label's template legs. The
	// archetype bundle must NOT re-derive from the expanded classification —
	// a petitioned office/document_generate label would otherwise swap the
	// bundle and add offer legs the strict-superset validator rejects.
	bundleKey := input.BundleKey
	if strings.TrimSpace(string(bundleKey)) == "" {
		bundleKey = semanticArchetypeBundleKey(input.Classification)
	}
	// One bit for the planner and the child replan input. The two sites used
	// to repeat the expression, so a later edit could record a ceiling the
	// planner did not apply.
	keepBaseline := input.BaselineWorkspace || semanticPlanHasBaseline(surface.plan)
	planCtx := withSemanticPetitionedLabel(withSemanticSlimOffice(withSemanticPetitionBaseline(withSemanticPetitionExpansion(withSemanticArchetypeBundleKeyOverride(requestCtx, bundleKey)), keepBaseline), input.SlimOffice, input.ShortDocumentEdit), label)
	planCtx = withSemanticPetitionParentFamilies(planCtx, surface.plan)
	// The light execution layer denies document.generate.file so a lookup
	// cannot grow a PDF by itself. This petition already passed the host
	// gate for a label whose template needs a full profile. Planning it
	// under that deny records policy_denied, the validator rejects the
	// child, and the effectful budget is spent on a plan that was never
	// installed. The live profile stays light until that child is published.
	if semanticLabelRequiresFullProfile(label) {
		planCtx = withSemanticExecutionLayer(planCtx, string(executionLayerFull))
	}
	if input.ConversationLookupReused {
		// The parent dropped its lookup legs on same-topic conversation
		// evidence; this re-plan has no user text to re-derive that, so mirror
		// the recorded drop for every lookup leg except the petitioned one.
		planCtx = withSemanticPetitionKeptLookup(planCtx, label)
	}
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachmentsWithSession(
		planCtx, input.UserID, "", input.Channel, input.RootTaskID,
		semanticPetitionTurnID(surface.scope.TurnID, label), surface.scope.SessionID, &expanded, cloneSemanticMessageAttachments(input.Attachments),
	)
	if err != nil {
		return nil, nil, err
	}
	if !handled || prepared == nil {
		return nil, nil, fmt.Errorf("semantic petition expansion no governed candidate")
	}
	if err := validateSemanticPetitionExpansion(surface.plan, prepared.plan, label); err != nil {
		return nil, nil, err
	}
	// The child keeps the parent's failure-replan attempt budget: a petition is
	// not a binding failure, and the expanded classification must survive a
	// later legitimate replan. The child's lookup legs — including the just
	// petitioned one — are published parent authority now, so the reuse-drop
	// record must not follow: a later expansion may not drop them again.
	childShortEdit := input.ShortDocumentEdit
	if input.SlimOffice && semanticDocumentMutationLabel(label) {
		childShortEdit = true
	}
	return h.publishSemanticChildRevision(requestCtx, surface, prepared,
		&semanticReplanInput{UserID: input.UserID, Channel: input.Channel, RootTaskID: input.RootTaskID, Classification: expanded, Attachments: cloneSemanticMessageAttachments(input.Attachments), Attempts: input.Attempts, BundleKey: bundleKey, SlimOffice: input.SlimOffice, ShortDocumentEdit: childShortEdit, BaselineWorkspace: keepBaseline},
		"petition", "petition_expand:"+strings.TrimSpace(string(label)))
}

func validateSemanticReplanSubset(parent, child tool.ToolPlan) error {
	return tool.ValidateReplanSubset(parent, child)
}

// semanticReplanIsBindingOnlyReplacement admits the one intentionally narrow
// difference between revisions: a selected dynamic implementation may be
// replaced after its binding became stale. It may not be dropped, added,
// reordered into another phase, have its effect/artifact/argument authority
// changed, or replace a non-dynamic parent binding.
func semanticReplanIsBindingOnlyReplacement(parent, child tool.ToolPlan) bool {
	return tool.ReplanIsBindingOnlyReplacement(parent, child)
}

func sameSemanticQualifiers(left, right map[string]string) bool {
	return tool.QualifiersEqual(left, right)
}

func sameSemanticEffects(left, right []tool.EffectClass) bool {
	return tool.EffectsEqual(left, right)
}

func semanticChannelScope(channel string) string {
	channel = strings.TrimSpace(strings.ToLower(channel))
	if canonical := normalizeIMMessagePlatformKind(channel).ChannelScope(); canonical != "" {
		return canonical
	}
	if channel == "" {
		return "default"
	}
	return channel
}

func semanticChannelScopes(channel string) []string { return []string{semanticChannelScope(channel)} }

func semanticProviderKind(registered RegisteredTool) string {
	switch registered.Category {
	case ToolCategorySkill:
		return "skill"
	case ToolCategoryMCP:
		return "mcp"
	default:
		return "builtin"
	}
}

func semanticProviderID(registered RegisteredTool) string {
	if source := strings.TrimSpace(registered.Source); source != "" {
		return source
	}
	return "im"
}

// semanticLoopInvocationIdentity is generated by the host once for a live
// LoopContext.  It may be retained only by that context; it is not a durable
// task relation.  Durable cross-turn continuation needs an explicit verified
// task handle (as Coding does), rather than reconstructing identity from a
// request ID, loop ID, path, text, or principal.
type semanticLoopInvocationIdentity struct {
	RootTaskID string
	TurnID     string
	SessionID  string
}

func semanticLoopInvocationFor(ctx *LoopContext) semanticLoopInvocationIdentity {
	identity, _ := semanticLoopInvocationSnapshotFor(ctx)
	return identity
}

// semanticLoopInvocationSnapshotFor captures both the private invocation
// identity and the replacement generation under one lock. Neither component
// is model-visible or a transport identity; together they merely let the host
// prove that a materialized surface belongs to the inbound turn it observed.
func semanticLoopInvocationSnapshotFor(ctx *LoopContext) (semanticLoopInvocationIdentity, uint64) {
	if ctx == nil {
		return semanticLoopInvocationIdentity{
			RootTaskID: "adhoc:" + newSemanticEphemeralIdentity(),
			TurnID:     "turn:" + newSemanticEphemeralIdentity(),
			SessionID:  semanticCompatibilitySessionID(""),
		}, 0
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if strings.TrimSpace(ctx.semanticInvocation.RootTaskID) == "" {
		// A WorkflowID is an independently owned durable task identity.  It is
		// the sole exception to the fresh per-loop root below; RequestID and
		// LoopContext.ID are intentionally never candidates.
		if workflowID := strings.TrimSpace(ctx.WorkflowID); workflowID != "" {
			ctx.semanticInvocation.RootTaskID = "workflow:" + workflowID
		} else {
			ctx.semanticInvocation.RootTaskID = "semantic-root:" + newSemanticEphemeralIdentity()
		}
	}
	if strings.TrimSpace(ctx.semanticInvocation.TurnID) == "" {
		ctx.semanticInvocation.TurnID = "semantic-turn:" + newSemanticEphemeralIdentity()
	}
	if strings.TrimSpace(ctx.semanticInvocation.SessionID) == "" {
		// SessionID is provided only by a host that owns a verified transport
		// session. Runtime.Conversation.SessionKey is deliberately not accepted:
		// the generic IM envelope currently derives it from channel/user fields,
		// so treating it as a semantic session would merely hide the old
		// user-as-session fallback behind a different spelling. If the ingress
		// has no independent session, retain an isolated host nonce rather than
		// synthesizing one from RequestID, LoopContext.ID, or conversation text.
		if sessionID := strings.TrimSpace(ctx.SessionID); sessionID != "" {
			ctx.semanticInvocation.SessionID = sessionID
		} else {
			ctx.semanticInvocation.SessionID = "semantic-session:" + newSemanticEphemeralIdentity()
		}
	}
	return ctx.semanticInvocation, ctx.semanticTurnGeneration
}

func semanticRoutingIdentity(ctx *LoopContext, _, _ string) (rootTaskID, turnID string) {
	identity := semanticLoopInvocationFor(ctx)
	return identity.RootTaskID, identity.TurnID
}

func semanticRoutingSessionID(ctx *LoopContext, _ string) string {
	return semanticLoopInvocationFor(ctx).SessionID
}

// Compatibility-only callers have no trusted session manager. Keep their
// session isolated per turn instead of silently reusing the principal as a
// session key. Production call sites use semanticRoutingSessionID.
func semanticCompatibilitySessionID(turnID string) string {
	if turnID = strings.TrimSpace(turnID); turnID != "" {
		return "compat-session:" + turnID
	}
	return "compat-session:" + newSemanticEphemeralIdentity()
}

func newSemanticEphemeralIdentity() string {
	buf := make([]byte, 18)
	if _, err := cryptorand.Read(buf); err == nil {
		return fmt.Sprintf("%x", buf)
	}
	// crypto/rand failure is exceptional. The fallback is intentionally based
	// on time rather than user-controlled text, and remains confined to the
	// explicitly non-durable, one-turn call path above.
	return tool.SchemaDigest([]byte(fmt.Sprintf("%d", time.Now().UTC().UnixNano())))[:24]
}

func canonicalToolDefinitionBytes(definition map[string]interface{}) []byte {
	// encoding/json sorts map keys, so this digest remains stable across Go map
	// iteration orders and catalog refreshes with the same trusted definition.
	encoded, err := json.Marshal(definition)
	if err != nil {
		return []byte("invalid_definition")
	}
	return encoded
}

func appendClosedHostSemanticProviders(providers *[]tool.ProviderSpec, defsByName, schemas map[string]map[string]interface{}, channel string, h *IMMessageHandler, principalID string) error {
	type hostProvider struct {
		adapter, implementation string
		definition, schema      map[string]interface{}
		capability              tool.CapabilityID
		qualifiers              map[string]string
		effects                 []tool.EffectClass
		ready                   bool
	}
	items := []hostProvider{
		{semanticTrustedLiveDataVisualAdapter, semanticTrustedLiveDataVisualImplementation, semanticTrustedLiveDataVisualDefinition(), semanticTrustedLiveDataVisualInvocationSchema(), "visual.render.live_data", nil, []tool.EffectClass{tool.EffectLocalMutation}, semanticImageDeliveryPublished(channel)},
		{semanticTrustedOfficeWriteAdapter, semanticTrustedOfficeWriteImplementation, semanticTrustedOfficeWriteDefinition(), semanticTrustedOfficeWriteInvocationSchema(), tool.CapabilityDocumentWriteOffice, map[string]string{"format": "spreadsheet"}, []tool.EffectClass{tool.EffectSensitive}, true},
		{semanticTrustedShellAdapter, semanticTrustedShellImplementation, semanticTrustedShellDefinition(), semanticTrustedShellInvocationSchema(), tool.CapabilityShellExecuteLocal, nil, []tool.EffectClass{tool.EffectSensitive}, true},
		{semanticTrustedRepoMutateAdapter, semanticTrustedRepoMutateImplementation, semanticTrustedRepoMutateDefinition(), semanticTrustedRepoMutateInvocationSchema(), tool.CapabilityRepoMutateVCS, nil, []tool.EffectClass{tool.EffectExternalEffect}, true},
		{semanticTrustedMailboxSendAdapter, semanticTrustedMailboxSendImplementation, semanticTrustedMailboxSendDefinition(), semanticTrustedMailboxSendInvocationSchema(), tool.CapabilityMessageSendMailbox, nil, []tool.EffectClass{tool.EffectExternalEffect}, true},
		{semanticTrustedMailboxDeleteAdapter, semanticTrustedMailboxDeleteImplementation, semanticTrustedMailboxDeleteDefinition(), semanticTrustedMailboxDeleteInvocationSchema(), tool.CapabilityMessageDeleteMailbox, nil, []tool.EffectClass{tool.EffectExternalEffect}, true},
		{semanticTrustedMailboxConfigureAdapter, semanticTrustedMailboxConfigureImplementation, semanticTrustedMailboxConfigureDefinition(), semanticTrustedMailboxConfigureInvocationSchema(), tool.CapabilityMessageConfigureMailbox, nil, []tool.EffectClass{tool.EffectSensitive}, true},
		{semanticTrustedBuildVerifyAdapter, semanticTrustedBuildVerifyImplementation, semanticTrustedBuildVerifyDefinition(), semanticTrustedBuildVerifyInvocationSchema(), tool.CapabilityBuildVerifyLocal, nil, []tool.EffectClass{tool.EffectSensitive}, true},
		{semanticTrustedAcquireRemoteAdapter, semanticTrustedAcquireRemoteImplementation, semanticTrustedAcquireRemoteDefinition(), semanticTrustedAcquireRemoteInvocationSchema(), tool.CapabilityArtifactAcquireRemote, nil, []tool.EffectClass{tool.EffectSensitive}, true},
	}
	if semanticTrustedDelegatePublished(h) {
		items = append(items, hostProvider{semanticTrustedDelegateAdapter, semanticTrustedDelegateImplementation, semanticTrustedDelegateDefinition(), semanticTrustedDelegateInvocationSchema(), tool.CapabilityAgentDelegateSubtask, nil, []tool.EffectClass{tool.EffectSensitive}, true})
	}
	if semanticTrustedSSHPublished(h, principalID) {
		items = append(items, hostProvider{semanticTrustedSSHAdapter, semanticTrustedSSHImplementation, semanticTrustedSSHDefinition(), semanticTrustedSSHInvocationSchema(), tool.CapabilityShellExecuteRemoteHost, nil, []tool.EffectClass{tool.EffectExternalEffect}, true})
	} else if semanticTrustedSSHConnectPublished(h, principalID) {
		// Session-less turns still need a connect path: petitions and planned
		// ssh surfaces would otherwise have no provider to resolve against
		// (production 2026-09-18 08:26: "密码是 …" turn could not connect at
		// all). The same adapter name carries the connect schema for this
		// catalog build; exec and connect modes never coexist in one build.
		items = append(items, hostProvider{semanticTrustedSSHAdapter, semanticTrustedSSHConnectImplementation, semanticTrustedSSHConnectDefinition(), semanticTrustedSSHConnectSchema(), tool.CapabilityShellExecuteRemoteHost, nil, []tool.EffectClass{tool.EffectExternalEffect}, true})
	}
	if semanticTrustedBrowserPublished(h) {
		items = append(items, hostProvider{semanticTrustedBrowserAdapter, semanticTrustedBrowserImplementation, semanticTrustedBrowserDefinition(), semanticTrustedBrowserInvocationSchema(), tool.CapabilityBrowserControlWeb, nil, []tool.EffectClass{tool.EffectExternalEffect}, true})
	}
	if semanticTrustedComputerUsePublished(h) {
		items = append(items, hostProvider{semanticTrustedComputerUseAdapter, semanticTrustedComputerUseImplementation, semanticTrustedComputerUseDefinition(), semanticTrustedComputerUseInvocationSchema(), tool.CapabilityComputerControlDesktop, nil, []tool.EffectClass{tool.EffectExternalEffect}, true})
	}
	for _, item := range items {
		defsByName[item.adapter] = item.definition
		schemas[item.adapter] = item.schema
		authorization, err := tool.NewParameterAuthorization(item.schema)
		if err != nil {
			return fmt.Errorf("authorize %s invocation schema: %w", item.adapter, err)
		}
		provider := tool.ProviderSpec{
			AdapterName: item.adapter,
			Binding: tool.ProviderBinding{
				Kind: "builtin", ProviderID: "im", ImplementationID: item.implementation,
				SchemaDigest: tool.SchemaDigest([]byte(item.implementation)),
			},
			ParameterAuthorization: authorization,
			Provides:               []tool.CapabilityProvision{{Capability: item.capability, Qualifiers: item.qualifiers, Quality: 2}},
			Effects:                item.effects, Ready: item.ready, ChannelScopes: semanticChannelScopes(channel),
		}
		if item.adapter == semanticTrustedLiveDataVisualAdapter {
			provider.Produces = []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
		}
		if item.adapter == semanticTrustedOfficeWriteAdapter {
			// The concrete MIME is only known once the model picks .xlsx or
			// .pptx, so the producer declares both reviewed document contracts;
			// the execution adapter registers exactly the one it wrote.
			provider.Produces = []tool.ArtifactContract{
				{Kind: "document", MIMEType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Required: true},
				{Kind: "document", MIMEType: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Required: true},
			}
		}
		*providers = append(*providers, provider)
	}
	return nil
}
