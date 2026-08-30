package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
const previousTurnSemanticToolName = "previous_turn_tool"

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
	if s == nil || len(s.grants) != 1 {
		return ""
	}
	for name := range s.grants {
		return name
	}
	return ""
}

func (s *semanticCallSurface) soleLiveLookupGrantName() string {
	live := s.soleLiveGrantName()
	if live == "" {
		return ""
	}
	grant, ok := s.grants[live]
	if !ok {
		return ""
	}
	selection, ok := semanticSelectionByID(s.plan, grant.SelectionID)
	if !ok || !semanticAliasableLookupSelection(selection) {
		return ""
	}
	return live
}

func semanticAliasableLookupSelection(selection tool.PlannedSelection) bool {
	if !tool.IsLightPromptSafeSelection(selection) {
		return false
	}
	id := strings.TrimSpace(string(selection.FitProof.MatchedCapability))
	// Only the web-search family shares the weather query schema. Fetch wants
	// a URL and current_time takes no query; aliasing those would fail
	// validation and consume the one-shot grant.
	return strings.HasPrefix(id, "information.search.")
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
		families[name] = tool.RepeatFamilyID(grant.SelectionID)
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
	return known && issued == tool.RepeatFamilyID(grant.SelectionID)
}

func (s *semanticCallSurface) liveGrantNames() map[string]bool {
	if s == nil || len(s.grants) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s.grants))
	for name := range s.grants {
		out[name] = true
	}
	return out
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
	registry       *tool.CapabilityRegistry
	plan           tool.ToolPlan
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
				"format": {Values: []string{"pdf", "word", "spreadsheet", "presentation", "text"}, Required: true},
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

const imSemanticMinimumConfidence = 0.78

// semanticLookupHintFloor is the lowest score we still treat as a real
// enough signal. L2 search/live_data hints use it when the tree is
// unavailable. Successful L3/fusion verdicts reuse it as the tree floor:
// 0.75 is a normal tree pick, 0.55 is not. It is not 0.78; that number is
// L2 early-exit and the resolver write-grant floor.
const semanticLookupHintFloor = intent.EmbeddingLookupMinScore

const semanticInvocationGrantTTL = 10 * time.Minute

// imSemanticIntentRuleSet is an owner-reviewed mapping from an intent meaning
// to an outcome contract. It intentionally contains no registered tool name:
// adding another builtin, Skill, or MCP implementation changes catalog choice,
// not request interpretation. Screenshot's two templates are independent
// needs, so the generic planner can create the artifact producer/consumer DAG.
// Callers must treat the map and nested qualifier maps as read-only.
var imSemanticIntentRuleSet = map[intent.IntentLabel][]agentservice.IntentCapabilityNeedTemplate{
	intent.LabelScreenshot: {
		{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
	},
	intent.LabelCurrentTime: {
		{Capability: "information.current_time", Required: true},
	},
	// Stable reference lookup and changing public facts use the same
	// implementation contract but retain distinct outcome qualifiers. This
	// allows a future provider/policy to specialize freshness without adding
	// a tool-name branch to request routing. Research turns are iterative by
	// nature ("全网搜索X" needs a broad query plus one or two refinements);
	// a single search grant strands them on a policy denial and pushes the
	// model into fetch petitions it cannot form (production 2026-08-26, the
	// 张惠妹 discography turn). Three matches the observed shape: initial
	// query + refinements, still bounded visible plan nodes (5 matches the
	// no-progress breaker's tolerance; bash-style iteration budgets are 8).
	intent.LabelSearch: {
		{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "reference"}, Required: true, MaxInvocations: 5},
	},
	intent.LabelLiveData: {
		{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
	},
	intent.LabelLiveDataVisual: {
		{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true},
		{Capability: "visual.render.live_data", Required: true},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
	},
	intent.LabelDocumentRead: {
		{Capability: "document.read.local", Required: true},
	},
	// Open an existing local document with the OS handler. This reuses
	// system.launch.local; app_launch remains the application/URL/folder
	// outcome. Specified-target send is LabelDocumentDelivery.
	intent.LabelDocumentOpen: {
		{Capability: tool.CapabilitySystemLaunchLocal, Required: true},
	},
	intent.LabelDocumentDelivery: {
		{Capability: "artifact.deliver.specified_target", Qualifiers: map[string]string{"format": "file"}, Required: true},
	},
	intent.LabelSSH: {
		{Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
	},
	intent.LabelBrowser: {
		{Capability: tool.CapabilityBrowserControlWeb, Required: true},
	},
	intent.LabelComputerUse: {
		{Capability: tool.CapabilityComputerControlDesktop, Required: true},
	},
	intent.LabelAttachmentDelivery: {
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
	},
	intent.LabelDocumentGenerate: {
		{Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Required: true},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
	},
	// Sensitive builtin families migrated in the S2a slice. The need is an
	// outcome contract only; the catalog decides which registered builtin
	// implementation satisfies it.
	// Office writes produce a workspace document the user almost always wants
	// back in the conversation, so the rule carries the same generate+deliver
	// pair as LabelDocumentGenerate: without the delivery leg the turn could
	// write the deck/sheet and then die on an unrendered send_file call.
	// The leg stays optional: channels without file delivery (e.g. VE) deny the
	// capability, and a required leg would HostReject office turns that worked
	// there before (business_data read leg precedent). The write itself gets a
	// two-invocation budget: draft-then-revise is the natural deck workflow
	// (write the frame, acquire the assets, rewrite with them embedded —
	// production 2026-08-26 PPT turn: photos landed after the first write and
	// the single-shot grant blocked the revision).
	intent.LabelOffice: {
		{Capability: tool.CapabilityDocumentWriteOffice, Required: true, MaxInvocations: 2},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: false},
	},
	// Business data is planned as both halves rather than one. The label does
	// not say whether the turn intends to read or to write, and guessing from
	// the utterance would put the mutation surface behind a lexical hunch.
	// Planning both lets the model pick, while keeping the two under separate
	// effect classes and separate policy: a restricted workflow state denies
	// the mutation half and the query half survives, which is the case a
	// single capability could not express.
	//
	// The query half is optional because it is the strictly narrower surface;
	// a host that publishes only the multiplexer still serves the family
	// rather than failing the whole turn.
	intent.LabelBusinessData: {
		{Capability: tool.CapabilityBusinessDataRead, Required: false},
		{Capability: tool.CapabilityBusinessDataMIS, Required: true},
	},
	intent.LabelKnowledgeWrite: {
		{Capability: tool.CapabilityKnowledgeIngestLocal, Required: true},
	},
	// Read-only builtin families migrated in the S2b1 slice. A read-only
	// selection needs no receipt boundary, so these rules only declare the
	// outcome contract; the catalog picks among the annotated builtin
	// implementations (the host file reader, the host repo inspector, the host
	// web fetcher, the host web searcher, the host audit reader, and the
	// knowledge retrieval entries).
	intent.LabelFileRead: {
		{Capability: tool.CapabilityFSReadLocal, Required: true},
	},
	intent.LabelGitInspect: {
		{Capability: tool.CapabilityRepoInspectVCS, Required: true},
	},
	// Repository mutation became expressible once the adapter grew a push
	// receipt read back from the remote (§11.49). Inspection is deliberately
	// not bundled in: a request to commit is not a request to read diffs, and
	// granting both would hand a write turn a capability it never asked for.
	intent.LabelGitMutate: {
		{Capability: tool.CapabilityRepoMutateVCS, Required: true},
	},
	// Page fetches follow searches: a research turn reads a few result pages,
	// not exactly one. Three matches the search budget above, read-only and
	// bounded.
	intent.LabelWebFetch: {
		{Capability: tool.CapabilityInformationFetchWeb, Required: true, MaxInvocations: 5},
	},
	intent.LabelAudioTranscribe: {
		{Capability: tool.CapabilityAudioTranscribeSpeech, Required: true},
	},
	intent.LabelAuditRead: {
		{Capability: tool.CapabilitySecurityAuditRead, Required: true},
	},
	intent.LabelKnowledgeRead: {
		{Capability: tool.CapabilityKnowledgeReadLocal, Required: true},
	},
	// Sensitive builtin families migrated in the S2b1 slice. All three execute
	// through the host-local mutation receipt boundary: the same host process
	// performs the write/command/capture and authoritatively observes the
	// outcome. repo.mutate.vcs is not one of them; it is an external effect
	// and reaches its own boundary through LabelGitMutate above.
	intent.LabelFileWrite: {
		{Capability: tool.CapabilityFSWriteLocal, Required: true},
	},
	// Shell work is iterative by nature — write a script, run it, read the
	// error, fix, rerun — so one invocation cannot express the meaning. Eight
	// matches the coding family's edit budget: enough for a real craft-and-run
	// loop, still bounded as plan nodes that review can see. This matters most
	// for petitioned shell legs (an office/PPT turn crafting its own deck via a
	// script), which would otherwise strand after a single command.
	intent.LabelShellCommand: {
		{Capability: tool.CapabilityShellExecuteLocal, Required: true, MaxInvocations: 8},
	},
	intent.LabelAudioRecord: {
		{Capability: tool.CapabilityAudioCaptureMicrophone, Required: true},
	},
	// Sensitive administration families migrated in the S2b2 slice. All of
	// them mutate host-local state whose outcome the same host process
	// observes synchronously, so every selection crosses the builtin local
	// mutation receipt boundary. The need is an outcome contract only; the
	// catalog decides which annotated builtin implementation satisfies it.
	intent.LabelAppLaunch: {
		{Capability: tool.CapabilitySystemLaunchLocal, Required: true},
	},
	// A deck or report usually embeds a handful of remote assets, not exactly
	// one: three bounded acquires cover a photo set without opening an
	// unbounded download plan.
	intent.LabelFileDownload: {
		{Capability: tool.CapabilityArtifactAcquireRemote, Required: true, MaxInvocations: 3},
	},
	intent.LabelConfigManage: {
		{Capability: tool.CapabilityConfigManageSelf, Required: true},
	},
	intent.LabelMemoryManage: {
		{Capability: tool.CapabilityMemoryManageAgent, Required: true},
	},
	intent.LabelTaskTrack: {
		{Capability: tool.CapabilityTaskTrackLocal, Required: true},
	},
	intent.LabelGoalManage: {
		{Capability: tool.CapabilityGoalManageLongRunning, Required: true},
	},
	intent.LabelTemplateManage: {
		{Capability: tool.CapabilityTemplateManageSession, Required: true},
	},
	intent.LabelSessionManage: {
		{Capability: tool.CapabilitySessionManageCoding, Required: true},
	},
	intent.LabelDelegateTask: {
		{Capability: tool.CapabilityAgentDelegateSubtask, Required: true},
	},
	intent.LabelKnowledgeAdmin: {
		{Capability: tool.CapabilityKnowledgeAdminMaintenance, Required: true},
	},
	// Local schedule administration is a host-owned mutation: list/create/
	// update/delete never bind channel Delivery on the same selection.
	// schedule.dispatch.channel stays required on delivery-bearing requests
	// and materializes only when the inbound transport already authenticated
	// a typed group:/user: destination. Missing or untrusted targets fail
	// closed instead of exposing only the administer adapter.
	intent.LabelScheduleManage: {
		{Capability: tool.CapabilityScheduleAdministerLocal, Required: true},
	},
	intent.LabelScheduleDispatch: {
		{Capability: tool.CapabilityScheduleAdministerLocal, Required: true},
		{Capability: tool.CapabilityScheduleDispatchChannel, Required: true},
	},
	// Local speech playback is host-observed: desktop/tui play never binds a
	// channel voice payload. audio.synthesize.speech stays the merged IM
	// send path and remains unmapped until a trusted voice receipt exists.
	intent.LabelAudioSynthesize: {
		{Capability: tool.CapabilityAudioSynthesizeLocal, Required: true},
	},
	// Render + current-channel voice delivery. The render selection only
	// publishes an audio ArtifactRef; send happens on the deliver selection
	// through DeliveryRecord. audio.synthesize.speech stays the merged IM
	// tts path and remains unmapped.
	intent.LabelAudioDeliver: {
		{Capability: tool.CapabilityAudioRenderSpeech, Required: true},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "voice"}, Required: true},
	},
	// Agentic coding family. These three labels name one outcome — change
	// code in the bound workspace — so they share a single rule.
	//
	// Until this slice they had no rule at all, and an unmapped capability
	// label HostRejects before the managed gate. A plain "改一下这个函数" on
	// the shared loop was therefore refused rather than served. Dedicated
	// local/remote coding workbench never consults this map: executePreparedIMEntry
	// hands those turns to CodingSubAgent / RemoteCodingSubAgent before UIC.
	intent.LabelCoding:      semanticCodingCapabilityRule,
	intent.LabelBugFix:      semanticCodingCapabilityRule,
	intent.LabelMaintenance: semanticCodingCapabilityRule,
	// audio.synthesize.speech and message.send.im stay quarantined: they have
	// catalog adapters but no UIC label. interaction.ask.user and
	// governance.inspect.experience have no reliable utterance label.
}

// semanticCodingCapabilityRule is the outcome contract shared by coding,
// bug_fix, and maintenance. It is one variable rather than three copies
// because the three labels describe the same surface: a capability added for
// coding but missed for bug_fix would be a silent behavior split rather than a
// visible one. Callers must treat it as read-only, like the map itself.
//
// The set is what the static coding belt actually reaches for, minus the two
// capabilities this migration refuses to hand over wholesale:
//
// shell.execute.local is deliberately absent. The legacy belt reaches build
// and test through bash, and granting that here would carry arbitrary local
// execution — file and repository mutation included — inside every coding
// grant. build.verify.local names the reviewed task instead and leaves the
// command line with the host.
//
// repo.mutate.vcs is deliberately absent in the other direction: asking for a
// code change is not asking to commit and push it. An explicit commit request
// classifies as git_mutate and receives that capability on its own.
//
// Coding is the first migrated family whose turn is iterative rather than a
// single outcome, so it is also the first to declare invocation budgets. One
// invocation each would let the turn read one file, edit one file, look at the
// diff once and check once — an ordering no real change survives. The counts
// below are shaped by how the work actually proceeds: reading dominates,
// editing follows it, checking follows editing, and the diff is a self-review
// read near the end.
var semanticCodingCapabilityRule = []agentservice.IntentCapabilityNeedTemplate{
	{Capability: tool.CapabilityFSReadLocal, Required: true, MaxInvocations: 12},
	{Capability: tool.CapabilityFSWriteLocal, Required: true, MaxInvocations: 8},
	{Capability: tool.CapabilityRepoInspectVCS, Required: true, MaxInvocations: 4},
	{Capability: tool.CapabilityBuildVerifyLocal, Required: true, MaxInvocations: 6},
}

// imSemanticIntentCoverage is the single migration gate for a classification.
// managed means at least one label has an owner-published capability rule.
// unmapped is the first non-generic label without a rule; it is independent of
// managed so a search+document_delivery request can fail closed before catalog
// construction.
func imSemanticIntentCoverage(result intent.ClassificationResult) (managed bool, unmapped intent.IntentLabel) {
	for _, label := range result.Labels() {
		if len(imSemanticIntentRuleSet[label]) > 0 {
			managed = true
			continue
		}
		if unmapped == "" && !label.IsNonCapabilityLabel() {
			unmapped = label
		}
	}
	return managed, unmapped
}

func imSemanticIntentIsManaged(result intent.ClassificationResult) bool {
	managed, _ := imSemanticIntentCoverage(result)
	return managed
}

// lexicalWebSearchRequest detects an explicit new-search request only when
// deciding whether prior, trusted lookup evidence may be reused. It is not an
// intent classifier and must never grant, select, or reclassify a capability.
func lexicalWebSearchRequest(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}
	for _, marker := range []string{
		"全网搜索", "全网查", "上网搜索", "上网搜", "上网查",
		"网上搜索", "网上搜", "网上找", "网上查",
		"在网上搜索", "在网上查找", "在网上找",
		"联网搜索", "互联网搜索", "联网查",
		"search the web", "search online", "web search",
		"google this", "bing this", "look up online",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
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
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "trusted_document_input_missing") || strings.Contains(err.Error(), "trusted_document_input_ambiguous")
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

func loopContextBlocksLegacyToolRouter(ctx *LoopContext) bool {
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
// operation. DocumentRead is intentionally excluded: an image attachment with
// a DocumentRead label must fail as trusted_document_input_missing (so the
// active desktop document is not silently reused for a new image), not be
// demoted to chat via vision fallthrough.
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

func classificationHasLabel(result intent.ClassificationResult, label intent.IntentLabel) bool {
	for _, candidate := range result.Labels() {
		if candidate == label {
			return true
		}
	}
	return false
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
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(ctx, agentservice.DynamicCapabilityNeedRequest{UserText: "im semantic routing"})
	if err != nil {
		return nil, false, err
	}
	if resolution.Managed && intent.WantsAmbientRetrieval(result.Primary) {
		resolution.Needs = agentservice.AppendAmbientRetrievalNeeds(registry, resolution.Needs)
	}
	resolution.Needs = semanticArchetypeBundleNeeds(registry, result, resolution.Managed, resolution.Needs, semanticArchetypeBundleKeyFor(ctx, result))
	return resolution.Needs, resolution.Managed, nil
}

// semanticArchetypeBundles maps a classification primary label to the
// companion labels of its task archetype. A task class almost always reaches
// for the same small family of capabilities — a document-producing turn looks
// material up, fetches pages, downloads assets, and reads local files — so
// those legs ride along as optional offers on every turn of the class instead
// of depending on secondary-label classification confidence. The same PPT
// request routed to three different surfaces on 2026-08-26 (office leg /
// browser leg / pdf leg) because whether the lookup leg was planned depended
// on a single classification sample; the last three production incidents
// shared that small-face root cause. Determinism here comes from the bundle
// being a fixed table plus the existing budget/fence machinery, not from
// shrinking the face.
//
// Four design constraints, all pinned by test:
//
//  1. Members are labels, and their needs are derived from
//     imSemanticIntentRuleSet templates — no hand-maintained capability
//     approximations that can drift from the rule set (§4.18).
//  2. The repeat budget lives in the templates (MaxInvocations), and the
//     effective budget of one capability is the MAX across its sources: a
//     bundle offer raises a lower-budget declared family in place (never a
//     second family — one stable function name cannot bind two families), and
//     never shrinks a higher one. Classification jitter between labels that
//     declare the same capability therefore cannot shrink the turn's budget
//     (production 2026-08-28 PPT turn).
//  3. Delivery legs (artifact.deliver.*) never enter a bundle: delivery is
//     carried by the producing label's own rule and unlocked by the plan DAG
//     phase, so a bundle offering it would duplicate the leg and blur the
//     producer/consumer phase boundary.
//  4. bash/delegate stay out of bundles on purpose: they are general
//     fallbacks, not archetype members, and the petition mechanism already
//     covers them — bundles cover the high-frequency head, petitions the
//     long tail.
//
// Callers must treat the map as read-only.
var semanticArchetypeBundles = map[intent.IntentLabel][]intent.IntentLabel{
	// Document production: lookup + acquire + local read.
	intent.LabelOffice:           {intent.LabelSearch, intent.LabelWebFetch, intent.LabelFileDownload, intent.LabelFileRead},
	intent.LabelDocumentGenerate: {intent.LabelSearch, intent.LabelWebFetch, intent.LabelFileDownload, intent.LabelFileRead},
	// Retrieval research: search and fetch carry each other; the capability
	// duplicate (information.search.web is already declared on a search turn)
	// is absorbed by the dedup rule in semanticArchetypeBundleNeeds.
	intent.LabelSearch:   {intent.LabelWebFetch, intent.LabelSearch},
	intent.LabelLiveData: {intent.LabelWebFetch, intent.LabelSearch},
	intent.LabelWebFetch: {intent.LabelWebFetch, intent.LabelSearch},
	// Local files: read and write travel together.
	intent.LabelFileRead:     {intent.LabelFileRead, intent.LabelFileWrite},
	intent.LabelFileWrite:    {intent.LabelFileRead, intent.LabelFileWrite},
	intent.LabelDocumentRead: {intent.LabelFileRead, intent.LabelFileWrite},
	// Command/coding: shell and delegated work iterate over local files.
	intent.LabelShellCommand: {intent.LabelFileRead, intent.LabelFileWrite},
	intent.LabelDelegateTask: {intent.LabelFileRead, intent.LabelFileWrite},
	// Desktop automation: observe the screen, launch the target app.
	intent.LabelBrowser:     {intent.LabelScreenshot, intent.LabelAppLaunch},
	intent.LabelComputerUse: {intent.LabelScreenshot, intent.LabelAppLaunch},
}

// semanticArchetypeBundleKey picks the archetype whose bundle the turn
// expands with. A LOOKUP-primary classification (search/live_data/web_fetch —
// the normalized lookup+generate composite keeps live_data as primary) that
// also declares document production is a document-archetype turn: keying the
// bundle on the primary alone stranded the 2026-08-28 birthday-PPT turn —
// download_file stayed petition-only, the model spent the turn's single
// effectful petition on it, and the office petition the task actually needed
// then hit the spent budget. The document bundle is a superset of the
// retrieval pair, so a lookup+document composite loses nothing. The override
// is deliberately scoped to lookup primaries: a coding primary that declares
// document_generate keeps the coding face and must not gain web_search from
// the document bundle.
func semanticArchetypeBundleKey(result intent.ClassificationResult) intent.IntentLabel {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch:
		if classificationHasLabel(result, intent.LabelOffice) {
			return intent.LabelOffice
		}
		if classificationHasLabel(result, intent.LabelDocumentGenerate) {
			return intent.LabelDocumentGenerate
		}
	}
	return result.Primary
}

// semanticArchetypeBundleKeyOverrideKey carries the bundle key a published
// plan was built with into a later re-plan. A petition expansion adds one
// label to the classification; if that label is office/document_generate, an
// overriding re-derivation would also swap the archetype bundle and the child
// would gain bundle-offer legs the strict-superset expansion validator must
// (correctly) reject. The re-plan therefore reuses the parent plan's recorded
// key: the petition adds exactly the petitioned label's template legs.
type semanticArchetypeBundleKeyOverrideKey struct{}

func withSemanticArchetypeBundleKeyOverride(ctx context.Context, key intent.IntentLabel) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(string(key)) == "" {
		return ctx
	}
	return context.WithValue(ctx, semanticArchetypeBundleKeyOverrideKey{}, key)
}

func semanticArchetypeBundleKeyFor(ctx context.Context, result intent.ClassificationResult) intent.IntentLabel {
	if ctx != nil {
		if override, ok := ctx.Value(semanticArchetypeBundleKeyOverrideKey{}).(intent.IntentLabel); ok && strings.TrimSpace(string(override)) != "" {
			return override
		}
	}
	return semanticArchetypeBundleKey(result)
}

// semanticArchetypeBundleNeeds appends the turn's archetype bundle (chosen by
// semanticArchetypeBundleKey) to the resolved needs as optional offers
// the resolved needs as optional offers (Required:false — an offer, never an
// obligation). The same classification always expands to the same face:
// iteration follows the fixed bundle order, each companion label contributes
// its rule templates in template order, and each template emits
// RepeatSiblingBudget(MaxInvocations) sibling needs (one when the template
// declares no budget). Need IDs derive exactly like the resolver's:
// capability+polarity+qualifiers digest plus the sibling index.
//
// Dedup is by capability regardless of qualifiers: a capability the resolved
// needs already carry (declared or ambient) never grows a SECOND family from
// the bundle, and neither does a capability an earlier bundle label already
// offered — a search turn never ends up with two search families. Two
// families bound to the same capability would render the same stable function
// name (web_search) and collide on the surface, so a qualifier-keyed coexistence
// is deliberately not an option.
//
// Within that one-family rule the budget is the MAX across sources, not
// first-declared-wins: when the bundle template budgets a capability higher
// than the family the resolved needs already carry, the existing family grows
// the missing siblings in place (keeping its own ID base, qualifiers and
// polarity). Production 2026-08-28 PPT turn: the classification carried
// live_data (one-off freshness=current search) instead of search, and
// capability-only dedup let that single invocation shadow the archetype's
// five — web_search died after one success on a turn that still needed more
// lookups. The classifier cannot tell a one-shot weather lookup from an
// iterative research turn at label granularity, so the ceiling must not
// depend on which label happened to declare the capability. The added
// siblings stay bundle offers (Required:false, bundle evidence): the declared
// leg keeps its required invocation, while the raised ceiling is an offer the
// planner sheds first under a tight planning budget — a MaxSelections=1 turn
// still keeps its one search instead of losing the whole wave. The upgrade
// applies only while the capability still has exactly one family; a
// classification that legitimately declared two qualifier-distinct families
// (search+live_data) keeps both untouched. Bundle needs whose capability the
// registry does not know are skipped rather than failing the turn: an offer
// must never gate the required legs. Bundle needs are appended only for
// managed classifications.
func semanticArchetypeBundleNeeds(registry *tool.CapabilityRegistry, result intent.ClassificationResult, managed bool, needs []tool.CapabilityNeed, bundleKey intent.IntentLabel) []tool.CapabilityNeed {
	if !managed {
		return needs
	}
	companions, ok := semanticArchetypeBundles[bundleKey]
	if !ok || len(companions) == 0 {
		return needs
	}
	// offered records, per capability, the needs-range of the single family
	// already carrying it. A capability with two qualifier-distinct families
	// is marked as ambiguous and never upgraded in place.
	type familyRange struct {
		start, count int
		ambiguous    bool
	}
	offered := make(map[tool.CapabilityID]familyRange, len(needs))
	for index, need := range needs {
		family := tool.RepeatFamilyID(need.ID)
		entry, exists := offered[need.Capability]
		if !exists {
			offered[need.Capability] = familyRange{start: index, count: 1}
			continue
		}
		if tool.RepeatFamilyID(needs[entry.start].ID) != family {
			entry.ambiguous = true
		}
		entry.count++
		offered[need.Capability] = entry
	}
	out := needs
	for _, label := range companions {
		for _, template := range imSemanticIntentRuleSet[label] {
			// Delivery legs stay out of bundles by construction (constraint 3
			// above); the skip is also enforced here so a future label whose
			// rule pairs produce+deliver (screenshot, live_data_visual) can
			// still lend its producer half to a bundle.
			if strings.HasPrefix(string(template.Capability), "artifact.deliver.") {
				continue
			}
			budget := tool.RepeatSiblingBudget(template.MaxInvocations)
			if entry, exists := offered[template.Capability]; exists {
				// The capability is already carried. Raise the family's
				// ceiling to the max across sources by appending the missing
				// siblings to the existing family; never shrink it, never
				// spawn a second family, and leave a legitimately
				// qualifier-split capability alone. The appended siblings stay
				// optional bundle offers even when the base leg is required:
				// the ceiling is an offer, not an obligation, and a tight
				// planning budget sheds these before touching the declared
				// required invocation.
				if entry.ambiguous || budget <= entry.count {
					continue
				}
				base := out[entry.start]
				baseID := tool.RepeatFamilyID(base.ID)
				for index := entry.count; index < budget; index++ {
					out = append(out, tool.CapabilityNeed{
						ID:         tool.RepeatSiblingNeedID(baseID, index),
						Capability: base.Capability, Qualifiers: semanticArchetypeCloneQualifiers(base.Qualifiers), Polarity: base.Polarity,
						Required: false, Confidence: result.Confidence, EvidenceIDs: []string{"intent:archetype_bundle"},
					})
				}
				entry.count = budget
				offered[template.Capability] = entry
				continue
			}
			if _, exists := registry.Lookup(template.Capability); !exists {
				continue
			}
			offered[template.Capability] = familyRange{start: len(out), count: budget}
			polarity := template.Polarity
			if polarity == "" {
				polarity = tool.NeedRequire
			}
			key := string(template.Capability) + "\x00" + string(polarity) + "\x00" + semanticArchetypeQualifierKey(template.Qualifiers)
			baseID := "need:" + string(template.Capability) + ":" + tool.SchemaDigest([]byte(key))[:12]
			// Siblings share the optional flag as a family: the whole leg
			// stays an offer, never an obligation, and lands in the plan's
			// omitted list together when no provider serves it.
			for index := 0; index < tool.RepeatSiblingBudget(template.MaxInvocations); index++ {
				out = append(out, tool.CapabilityNeed{
					ID:         tool.RepeatSiblingNeedID(baseID, index),
					Capability: template.Capability, Qualifiers: semanticArchetypeCloneQualifiers(template.Qualifiers), Polarity: polarity,
					Required: false, Confidence: result.Confidence, EvidenceIDs: []string{"intent:archetype_bundle"},
				})
			}
		}
	}
	return out
}

// semanticArchetypeQualifierKey renders qualifiers exactly like the resolver's
// need-ID key (sorted k=v pairs), so a bundle offer and a label-declared need
// for the same contract share one need identity.
func semanticArchetypeQualifierKey(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strings.TrimSpace(key)+"="+strings.TrimSpace(values[key]))
	}
	return strings.Join(parts, "\x1f")
}

func semanticArchetypeCloneQualifiers(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
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
		PlanID:      "input:" + strings.TrimSpace(turnID),
		SessionID:   sessionID,
		TurnID:      turnID,
		PrincipalID: principalID,
	}
	inputs := make([]semanticTrustedArtifactInput, 0, len(attachments))
	for index, attachment := range attachments {
		format, mimeType, ok := semanticDocumentFormat(attachment.FileName, attachment.MimeType)
		if !ok {
			continue
		}
		if strings.TrimSpace(attachment.Data) == "" {
			return nil, fmt.Errorf("trusted_document_attachment_content_missing")
		}
		sourceID := strings.TrimSpace(attachment.SourceMediaID)
		if sourceID == "" {
			// The canonical bytes are part of the ArtifactRef identity, but the
			// ingress producer still needs a stable per-slot identity to preserve
			// provenance when a channel has no media handle.
			sourceID = fmt.Sprintf("attachment:%d:%s:%s", index, filepath.Base(attachment.FileName), mimeType)
		}
		producer := "trusted-input:channel-attachment:" + tool.SchemaDigest([]byte(sourceID))[:24]
		payload, err := tool.NewArtifactPayload(inputScope, producer, "document", mimeType, attachment.Data, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("trusted_document_attachment_invalid: %w", err)
		}
		inputs = append(inputs, semanticTrustedArtifactInput{Payload: payload, Format: format, Suffix: semanticDocumentTempSuffix(attachment.FileName, format)})
	}
	return inputs, nil
}

func semanticDocumentTempSuffix(fileName, format string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	switch ext {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".csv", ".ppt", ".pptx", ".txt", ".md", ".markdown", ".json", ".xml", ".yaml", ".yml", ".log":
		return ext
	}
	switch format {
	case "pdf":
		return ".pdf"
	case "word":
		return ".docx"
	case "spreadsheet":
		return ".xlsx"
	case "presentation":
		return ".pptx"
	default:
		return ".txt"
	}
}

// semanticDocumentFormat is an ingress classification only. The document
// reader still validates the bytes with its native parser; MIME/name metadata
// cannot turn arbitrary content into a successful document read.
func semanticDocumentFormat(fileName, mimeType string) (format, canonicalMIME string, ok bool) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".pdf":
		return "pdf", "application/pdf", true
	case ".doc", ".docx":
		return "word", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true
	case ".xls", ".xlsx", ".csv":
		return "spreadsheet", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true
	case ".ppt", ".pptx":
		return "presentation", "application/vnd.openxmlformats-officedocument.presentationml.presentation", true
	case ".txt", ".md", ".markdown", ".json", ".xml", ".yaml", ".yml", ".log":
		return "text", "text/plain", true
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "application/pdf":
		return "pdf", "application/pdf", true
	case "application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "word", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true
	case "application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "text/csv":
		return "spreadsheet", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true
	case "application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "presentation", "application/vnd.openxmlformats-officedocument.presentationml.presentation", true
	case "text/plain", "text/markdown", "application/json", "application/xml", "text/xml", "application/yaml", "text/yaml":
		return "text", "text/plain", true
	default:
		return "", "", false
	}
}

func semanticNeedsForTrustedDocumentInputs(needs []tool.CapabilityNeed, inputs []semanticTrustedArtifactInput) ([]tool.CapabilityNeed, error) {
	if len(needs) == 0 {
		return nil, nil
	}
	hasGenerate := false
	for _, need := range needs {
		if need.Capability == "document.generate.file" || need.Capability == tool.CapabilityDocumentWriteOffice {
			hasGenerate = true
			break
		}
	}
	resolved := append([]tool.CapabilityNeed(nil), needs...)
	for index := range resolved {
		switch resolved[index].Capability {
		case "document.read.local":
		case "artifact.deliver.current_channel":
			if resolved[index].Qualifiers["format"] != "file" {
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
		if len(inputs) != 1 {
			if len(inputs) == 0 {
				return nil, fmt.Errorf("trusted_document_input_missing")
			}
			return nil, fmt.Errorf("trusted_document_input_ambiguous")
		}
		if resolved[index].Capability == "document.read.local" {
			resolved[index].Qualifiers = map[string]string{"format": inputs[0].Format}
		}
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
	// Identity and replacement generation form one host-private lifecycle
	// snapshot. Reading them separately would permit a fresh ingress to rotate
	// the identity between reads, then incorrectly register an old scope as the
	// new generation's surface. The later fence check would eventually revoke
	// it, but we must never present that cross-turn construction as current.
	invocation, turnGeneration := semanticLoopInvocationSnapshotFor(ctx)
	rootTaskID, turnID := invocation.RootTaskID, invocation.TurnID
	sessionID := invocation.SessionID
	requestCtx, cancel := semanticRoutingContext(ctx)
	defer cancel()
	// LoopContext.Context observes terminal cancellation only. Register a
	// sibling cancellation for fresh ingress replacement before any classifier,
	// catalog, or planner I/O starts, so an old request cannot finish planning
	// and publish a new surface after its replacement is accepted.
	requestCtx, cancelReplacement := context.WithCancel(requestCtx)
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
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(requestCtx, userID, userText, channel, rootTaskID, turnID, sessionID, semanticIntentFromLoopContext(ctx), attachments)
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
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, true, err
	}
	issuer, err := h.semanticInvocationIssuer()
	if err != nil {
		return nil, nil, true, err
	}
	hostCalls, err := h.semanticHostCallJournal()
	if err != nil {
		return nil, nil, true, err
	}
	coordinator, err := h.semanticExecutionCoordinator()
	if err != nil {
		return nil, nil, true, err
	}
	routeState, err := h.semanticRouteStateStore()
	if err != nil {
		return nil, nil, true, err
	}
	executor, err := h.semanticPlanExecutorWithRouteState(issuer, routeState)
	if err != nil {
		return nil, nil, true, err
	}
	artifactStore, err := h.semanticArtifactStore()
	if err != nil {
		return nil, nil, true, err
	}
	scope := tool.InvocationScope{RootTaskID: prepared.rootTaskID, PlanID: prepared.plan.ID, SessionID: sessionID, TurnID: prepared.turnID, PrincipalID: userID}
	var parent *tool.RouteRevisionRef
	if current, currentErr := routeState.CurrentRevision(scope); currentErr == nil {
		// Publish and route-state registration live in distinct durable stores.
		// Before advancing to a child revision, repair the sole recoverable gap:
		// a completed producer may have published a scoped artifact just before a
		// process loss prevented its metadata registration. This enumerates only
		// the current revision's completed producers, never a model-controlled
		// artifact lookup.
		if _, err := routeState.ReconcileCurrentArtifacts(scope, artifactStore, time.Now().UTC()); err != nil {
			return nil, nil, true, fmt.Errorf("reconcile semantic route artifacts: %w", err)
		}
		parent = &current
	} else if currentErr.Error() != "route_revision_not_found" {
		return nil, nil, true, fmt.Errorf("load semantic route revision: %w", currentErr)
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, true, err
	}
	publishRequest := tool.RouteRevisionPublishRequest{
		Scope: scope, Plan: prepared.plan, ExpectedParent: parent, SnapshotDigest: prepared.plan.SnapshotDigest,
	}
	var state tool.RouteState
	var initialGrants []tool.InvocationGrant
	if coordinator != nil {
		state, initialGrants, err = coordinator.PublishSurface(tool.SurfacePublishRequest{Revision: publishRequest, TenantID: h.semanticContinuityTenantID(), Issuer: issuer, GrantTTL: semanticInvocationGrantTTL, Now: time.Now().UTC()})
	} else {
		state, err = routeState.PublishRevision(publishRequest, time.Now().UTC())
	}
	if err != nil {
		return nil, nil, true, fmt.Errorf("open semantic route state: %w", err)
	}
	classVal := intent.ClassificationResult{}
	if classification != nil {
		classVal = *classification
	}
	surface := &semanticCallSurface{
		plan: state.Plan, scope: scope, issuer: issuer, executor: executor, routeState: routeState, hostCalls: hostCalls, coordinator: coordinator, tenantID: h.semanticContinuityTenantID(), registry: prepared.registry,
		hostConnectionID: "agent-loop-surface:" + newSemanticEphemeralIdentity(),
		completed:        make(map[string]bool), materialized: make(map[string]bool), schemas: prepared.definitions, parameterSchemas: prepared.schemas,
		grants: make(map[string]tool.InvocationGrant), retiredGrants: make(map[string]tool.InvocationGrant), rendered: make(map[string]bool), artifacts: newSemanticArtifactBroker(scope, artifactStore, routeState, coordinator), pendingArtifacts: make(map[string][]tool.ArtifactPayload),
		replan: &semanticReplanInput{UserID: userID, Channel: channel, RootTaskID: prepared.rootTaskID, Classification: classVal, Attachments: cloneSemanticMessageAttachments(attachments), ConversationLookupReused: prepared.conversationLookupReused, BundleKey: semanticArchetypeBundleKey(classVal)},
	}
	for _, input := range prepared.documentInputs {
		if err := semanticRoutingRequestErr(requestCtx); err != nil {
			return nil, nil, true, err
		}
		if _, err := artifactStore.Publish(input.Payload); err != nil {
			return nil, nil, true, fmt.Errorf("publish trusted document input: %w", err)
		}
	}
	for _, input := range prepared.audioInputs {
		if err := semanticRoutingRequestErr(requestCtx); err != nil {
			return nil, nil, true, err
		}
		if _, err := artifactStore.Publish(input.Payload); err != nil {
			return nil, nil, true, fmt.Errorf("publish trusted audio input: %w", err)
		}
	}
	for _, materialization := range state.Materializations {
		surface.materialized[materialization.Grant.SelectionID] = true
		name := semanticSurfaceGrantName(materialization.Grant)
		if materialization.State == tool.RouteMaterializationRetired {
			surface.retiredGrants[name] = materialization.Grant
			continue
		}
		if existing, exists := surface.grants[name]; exists && existing.Token != materialization.Grant.Token {
			return nil, nil, true, fmt.Errorf("function-name collision for grant %q", materialization.Grant.SelectionID)
		}
		surface.grants[name] = materialization.Grant
	}
	// The coordinator has already issued and materialized the initial closure
	// in the same commit as PublishRevision. Populate the callback from that
	// authoritative result rather than calling refresh (which would mint a
	// second, separate grant batch).
	for _, grant := range initialGrants {
		name := semanticSurfaceGrantName(grant)
		if name == "" {
			return nil, nil, true, fmt.Errorf("semantic grant %q has no model function name", grant.SelectionID)
		}
		surface.materialized[grant.SelectionID] = true
		surface.grants[name] = grant
	}
	// A terminal execution consumes its one-time model grant and must remain
	// hidden on recovery. This includes a prepared channel delivery awaiting a
	// receipt, as well as a failed/unknown execution such as a rejected
	// parameter schema. The gateway/outbox owns receipt reconciliation; failed
	// selections require an explicit new route revision, never a stale retry.
	for functionName, grant := range surface.grants {
		record, executionErr := executor.Execution(scope, grant.SelectionID)
		if executionErr != nil || !semanticExecutionConsumesModelGrant(record.State) {
			continue
		}
		if _, err := routeState.RetireMaterialization(scope, state.Plan.ID, grant.Token, time.Now().UTC()); err != nil {
			return nil, nil, true, fmt.Errorf("retire terminal semantic materialization: %w", err)
		}
		surface.retiredGrants[functionName] = grant
		delete(surface.grants, functionName)
	}
	completed, err := executor.Completed(scope)
	if err != nil {
		return nil, nil, true, fmt.Errorf("recover semantic plan completion: %w", err)
	}
	projected, err := routeState.CompletedSelections(scope)
	if err != nil {
		return nil, nil, true, fmt.Errorf("recover projected semantic completion: %w", err)
	}
	for selectionID := range projected {
		completed[selectionID] = true
	}
	surface.completed = completed
	// Do not issue a new opaque grant or render a model-visible function after
	// the owning turn was cancelled.  A revision may already have been durably
	// published at this point (which is safe and has no external effect), but a
	// cancelled request must not materialize an executable surface from it.
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, nil, true, err
	}
	var definitions []map[string]interface{}
	if coordinator != nil {
		definitions, err = visibleSemanticCallSurfaceDefinitions(surface)
	} else {
		definitions, err = refreshSemanticCallSurface(surface)
	}
	if err != nil {
		return nil, nil, true, err
	}
	h.persistSessionGovernedTask(userID, channel, semanticDestination(requestCtx), semanticWorkflowAgentLoop(requestCtx), surface.plan)
	return definitions, surface, true, nil
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
	for functionName, grant := range surface.grants {
		if grant.SelectionID != completedSelectionID {
			continue
		}
		if _, err := surface.routeState.RetireMaterialization(surface.scope, surface.plan.ID, grant.Token, time.Now().UTC()); err != nil {
			return err
		}
		surface.retiredGrants[functionName] = grant
		delete(surface.grants, functionName)
	}
	return nil
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
	for functionName, grant := range surface.grants {
		if grant.SelectionID != selectionID {
			continue
		}
		if _, err := surface.routeState.RetireMaterialization(surface.scope, surface.plan.ID, grant.Token, time.Now().UTC()); err != nil {
			return nil, err
		}
		surface.retiredGrants[functionName] = grant
		delete(surface.grants, functionName)
	}
	return refreshSemanticCallSurface(surface)
}

// semanticExecutionConsumesModelGrant identifies durable execution states
// whose opaque model function must never be exposed again. A plan may later be
// revised under the original capability constraints, but a consumed grant is
// not retry authority for a different parameter set or provider attempt.
func semanticExecutionConsumesModelGrant(state tool.PlanExecutionState) bool {
	switch state {
	case tool.PlanExecutionAwaitingReceipt, tool.PlanExecutionFailed, tool.PlanExecutionUnknown:
		return true
	default:
		return false
	}
}

// semanticNextRepeatSiblings states this host's exposure closure in the shared
// vocabulary. The choice itself lives in corelib so both hosts cannot drift on
// it; only the maps differ, because each host names them its own way.
func semanticNextRepeatSiblings(surface *semanticCallSurface, ready []tool.PlannedSelection) map[string]bool {
	live := make(map[string]bool, len(surface.grants))
	for _, grant := range surface.grants {
		live[grant.SelectionID] = true
	}
	return tool.NextRepeatSelections(tool.RepeatExposure{
		Ready:     ready,
		Completed: surface.completed,
		Granted:   surface.materialized,
		Live:      live,
		Unsettled: func(selectionID string) bool { return semanticSelectionIsUnsettled(surface, selectionID) },
	})
}

// semanticSelectionIsUnsettled reads the durable execution record for a spent
// selection. A failed attempt is settled — it cost its budget and the family
// moves on — while awaiting-receipt, running, or lost outcomes are not, and a
// family must not step past them.
func semanticSelectionIsUnsettled(surface *semanticCallSurface, selectionID string) bool {
	if surface.executor == nil {
		return false
	}
	record, err := surface.executor.Execution(surface.scope, selectionID)
	if err != nil {
		return false
	}
	switch record.State {
	case tool.PlanExecutionAwaitingReceipt, tool.PlanExecutionUnknown, tool.PlanExecutionRunning:
		return true
	default:
		return false
	}
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
	family := tool.RepeatFamilyID(selectionID)
	budget := 0
	capability := tool.CapabilityID("")
	for _, selection := range surface.plan.Selections {
		if tool.RepeatFamilyID(selection.ID) != family {
			continue
		}
		budget++
		capability = selection.FitProof.MatchedCapability
		if !surface.materialized[selection.ID] {
			return ""
		}
	}
	if budget < 2 {
		return ""
	}
	// A live grant means the family still has a call in hand; the closure was
	// held back for some other reason, which is not the same as exhaustion.
	for _, grant := range surface.grants {
		if tool.RepeatFamilyID(grant.SelectionID) == family {
			return ""
		}
	}
	return fmt.Sprintf("\n\n[system] %s reached its limit of %d calls for this turn and is no longer available. Continue from what you already have, and state plainly what remains unfinished.", capability, budget)
}

// semanticTurnDeliveryComplete reports that the turn's goal is fully reached:
// every REQUIRED planned selection completed and at least one completed
// selection is a current-channel delivery adapter. The closing LLM round trip
// after this point only produces summary text, so the loop may stop cleanly
// instead of paying one more call's latency and outage exposure. Optional
// offers never gate completion: the ambient knowledge/memory lookups and the
// archetype bundle offers are open offers the model may ignore, and
// holding the stop hostage to them would make it dead code — production
// 2026-08-26 turns never called the ambient legs, so the all-selections
// variant never fired. A turn whose delivery finished while a required
// selection is still open (deliver, then remind me) does not match: the loop
// continues.
func semanticTurnDeliveryComplete(surface *semanticCallSurface) bool {
	if surface == nil || len(surface.plan.Selections) == 0 || surface.registry == nil || surface.replan == nil {
		return false
	}
	// Optionality is need-level and the plan does not retain needs; recompute
	// them deterministically from the stored classification (rule templates
	// only, no LLM) under the bundle key the plan was published with. Unknown
	// need IDs fail toward required — a selection whose optionality cannot be
	// proven must still complete.
	needsCtx := withSemanticArchetypeBundleKeyOverride(context.Background(), surface.replan.BundleKey)
	needs, _, err := semanticNeedsFromClassificationContext(needsCtx, surface.registry, surface.replan.Classification)
	if err != nil {
		return false
	}
	optionalByNeed := make(map[string]bool, len(needs))
	for _, need := range needs {
		if !need.Required {
			optionalByNeed[need.ID] = true
		}
	}
	delivered := false
	for _, selection := range surface.plan.Selections {
		if !surface.completed[selection.ID] {
			if !optionalByNeed[selection.NeedID] {
				return false
			}
			continue
		}
		switch selection.AdapterName {
		case "semantic_deliver_current_file", "semantic_deliver_current_image", "semantic_deliver_current_voice":
			delivered = true
		}
	}
	return delivered
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
	ready := surface.plan.ReadySelections(surface.completed)
	activeReady := make(map[string]tool.PlannedSelection, len(ready))
	for _, selection := range ready {
		// ToolPlan retains completed nodes as immutable decision evidence. They
		// must not reappear in a recovered model surface merely because their
		// own prerequisite set is empty.
		if !surface.completed[selection.ID] {
			activeReady[selection.ID] = selection
		}
	}
	newIDs := semanticNextRepeatSiblings(surface, ready)
	if skip != nil {
		for id := range newIDs {
			selection, ok := semanticSelectionByID(surface.plan, id)
			if ok && skip(selection) {
				delete(newIDs, id)
			}
		}
	}
	if len(newIDs) > 0 {
		partialPlan := semanticPlanWithSelections(surface.plan, newIDs)
		var grants []tool.InvocationGrant
		var err error
		if surface.coordinator != nil {
			_, grants, err = surface.coordinator.MaterializeReadySurface(surface.scope, surface.issuer, semanticInvocationGrantTTL, surface.completed, newIDs, time.Now().UTC())
		} else {
			grants, err = surface.issuer.IssueReady(partialPlan, surface.scope, semanticInvocationGrantTTL, surface.completed)
		}
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			name := semanticSurfaceGrantName(grant)
			if name == "" {
				return nil, fmt.Errorf("semantic grant %q has no model function name", grant.SelectionID)
			}
			if existing, exists := surface.grants[name]; exists && existing.Token != grant.Token {
				return nil, fmt.Errorf("function-name collision for grant %q", grant.SelectionID)
			}
			if surface.coordinator == nil {
				if _, err := surface.routeState.RecordMaterialization(surface.scope, surface.plan.ID, tool.RouteMaterialization{FunctionName: grant.Token, Grant: grant, State: tool.RouteMaterializationExposed}, time.Now().UTC()); err != nil {
					return nil, err
				}
			}
			surface.grants[name] = grant
			surface.materialized[grant.SelectionID] = true
		}
	}
	// A recovered surface already has its immutable grants. Re-render exactly
	// those still-exposed, currently-ready mappings; do not mint replacement
	// identities or look up a provider by name.
	renderIDs := make(map[string]bool, len(activeReady))
	renderGrants := make([]tool.InvocationGrant, 0, len(activeReady))
	for functionName, grant := range surface.grants {
		if _, ready := activeReady[grant.SelectionID]; !ready {
			continue
		}
		if surface.rendered[functionName] {
			continue
		}
		renderIDs[grant.SelectionID] = true
		renderGrants = append(renderGrants, grant)
	}
	if len(renderIDs) == 0 {
		return nil, nil
	}
	partialPlan := semanticPlanWithSelections(surface.plan, renderIDs)
	rendered, err := tool.NewCatalogRenderer(surface.registry).RenderReady(partialPlan, renderGrants, surface.schemas, surface.completed)
	if err != nil {
		return nil, err
	}
	definitions := make([]map[string]interface{}, 0, len(rendered))
	for _, item := range rendered {
		surface.rendered[item.FunctionName] = true
		definitions = append(definitions, item.Definition)
	}
	return definitions, nil
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
	ready := surface.plan.ReadySelections(surface.completed)
	readyIDs := make(map[string]bool, len(ready))
	for _, selection := range ready {
		if !surface.completed[selection.ID] {
			readyIDs[selection.ID] = true
		}
	}
	if len(readyIDs) == 0 {
		return nil, nil
	}
	grants := make([]tool.InvocationGrant, 0, len(surface.grants))
	visibleIDs := make(map[string]bool, len(readyIDs))
	for _, grant := range surface.grants {
		if !readyIDs[grant.SelectionID] {
			continue
		}
		grants = append(grants, grant)
		visibleIDs[grant.SelectionID] = true
	}
	if len(grants) == 0 {
		return nil, nil
	}
	plan := semanticPlanWithSelections(surface.plan, visibleIDs)
	rendered, err := tool.NewCatalogRenderer(surface.registry).RenderReady(plan, grants, surface.schemas, surface.completed)
	if err != nil {
		return nil, err
	}
	definitions := make([]map[string]interface{}, 0, len(rendered))
	for _, item := range rendered {
		definitions = append(definitions, item.Definition)
	}
	return definitions, nil
}

func semanticPlanWithSelections(plan tool.ToolPlan, allowed map[string]bool) tool.ToolPlan {
	filtered := plan
	filtered.Selections = make([]tool.PlannedSelection, 0, len(allowed))
	for _, selection := range plan.Selections {
		if allowed[selection.ID] {
			filtered.Selections = append(filtered.Selections, selection)
		}
	}
	return filtered
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
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(userID) == "" {
		return nil, true, fmt.Errorf("semantic_invocation_identity_required")
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	if err := semanticRoutingRequestErr(requestCtx); err != nil {
		return nil, true, err
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
	activeDocumentUse := h.activeLocalDocumentUseForTurn(userID, channel, semanticDestination(requestCtx), userText)
	if len(attachments) == 0 && activeDocumentUse == activeLocalDocumentReuse && activeDocumentContinuationIntent(classification, userText) {
		classification = classificationWithActiveDocumentRead(classification)
	}
	if replayed, ok := h.applySessionGovernedContinuation(userID, channel, semanticDestination(requestCtx), semanticWorkflowAgentLoop(requestCtx), classification, userText, attachments); ok {
		classification = replayed
	}
	// A phase turn that still sounds like the workflow it belongs to is not
	// asking for an unserved capability.
	classification = semanticClassificationForWorkflowLoop(semanticWorkflowAgentLoop(requestCtx), classification)
	// Named skill runs belong to the current agent (skill-doc inject + loop),
	// the same path the main assistant uses. workflow_task is only a
	// workflow_v2 panel start and must not HostReject that turn. Agent-guided
	// inject (Book-PDF) is the same act even when the user omitted "skill".
	h.releaseNamedSkillIntercept(&classification, userID, userText)
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
	if !imSemanticIntentIsManagedForLoop(semanticWorkflowAgentLoop(requestCtx), classification) {
		return nil, false, nil
	}
	if classificationHasLabel(classification, intent.LabelAttachmentDelivery) && classificationHasLabel(classification, intent.LabelDocumentGenerate) {
		return nil, true, errSemanticGenerateDeliveryConflict
	}
	// 0.78 is the resolver mint-writes floor and L2 early-exit. It is not a
	// second vote after L3 already named the family. Tree-confirmed (layer
	// 3/23, not degraded, ≥ 0.70) plans; leftover is for unknown/hint, not
	// for a named shell_command at 0.75. Read-only lookup hints still plan
	// above 0.70, and a degraded L2 office hint at the lookup floor plans
	// through the governed office surface. Other degraded mutating families
	// stay a miss so leftover cannot mint bash from a timeout guess. Weak L2
	// generate stays a miss.
	declaredManaged, unmapped := imSemanticIntentCoverage(classification)
	planning := classification
	if !semanticClassificationMeetsResolverFloor(classification) {
		if !semanticClassificationPlansBelowResolverFloor(classification) {
			return nil, false, nil
		}
		planning = semanticLookupClassificationForPlanning(classification)
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
	needs, managed, err := semanticNeedsFromClassificationContext(requestCtx, registry, planning)
	if err != nil {
		return nil, true, fmt.Errorf("resolve IM semantic capability needs: %w", err)
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
	if len(documentInputs) == 0 && len(attachments) == 0 && activeDocumentUse == activeLocalDocumentReuse && semanticDocumentReadNeedPresent(needs) {
		activeInputs, active, activeErr := h.semanticActiveLocalDocumentInputsForTurn(rootTaskID, turnID, userID, channel, semanticDestination(requestCtx))
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
	needs, conversationLookupReused := semanticNeedsForReusableConversationLookupReport(needs, requestCtx, userText)
	// A petition expansion re-plan has no user text, so the drop above cannot
	// re-fire; when the parent surface recorded it, mirror it here for every
	// lookup leg except the petitioned label's own templates.
	needs = semanticNeedsForPetitionExpansionLookup(needs, requestCtx)
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
	dynamicInventory, err := h.semanticDynamicInventory(requestCtx, userID)
	if err != nil {
		return nil, true, fmt.Errorf("load GUI dynamic semantic inventory: %w", err)
	}
	dynamicCatalog, err := agentservice.BuildDynamicSemanticCatalog(dynamicInventory.mcpEntries, dynamicInventory.skillEntries)
	if err != nil {
		return nil, true, fmt.Errorf("build GUI dynamic semantic catalog: %w", err)
	}
	for _, provider := range dynamicCatalog.Providers {
		definition, ok := dynamicCatalog.Definitions[provider.AdapterName]
		if !ok {
			return nil, true, fmt.Errorf("dynamic semantic definition missing for %q", provider.AdapterName)
		}
		invocationSchema, err := semanticInvocationSchema(definition)
		if err != nil {
			return nil, true, fmt.Errorf("validate dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		defsByName[provider.AdapterName] = definition
		semanticSchemas[provider.AdapterName] = invocationSchema
		authorization, err := tool.NewParameterAuthorization(invocationSchema)
		if err != nil {
			return nil, true, fmt.Errorf("authorize dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		provider.ParameterAuthorization = authorization
		providers = append(providers, provider)
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
	if err := appendClosedHostSemanticProviders(&providers, defsByName, semanticSchemas, channel, h); err != nil {
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
	snapshot, err := catalog.PublishWithCoverage(providers, coverage, time.Now().UTC())
	if err != nil {
		return nil, true, fmt.Errorf("publish IM semantic catalog: %w", err)
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
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: rootTaskID, SessionID: sessionID, TurnID: turnID, ChannelScope: semanticChannelScope(channel), Snapshot: snapshot, Needs: needs, Facts: facts, Constraints: policyConstraints,
		Budget: semanticHostPlanningBudget(semanticPlanningBudget(requestCtx), semanticSchemaTokenBudget(requestCtx)),
	})
	if err != nil {
		return nil, true, fmt.Errorf("plan IM semantic route: %w", err)
	}
	// A managed family never falls through to old name routing: the caller sees
	// a planner result only. Missing capability providers are explicit errors.
	if len(plan.Unmet) > 0 {
		return &semanticPlanPreparation{registry: registry, plan: plan, definitions: defsByName, schemas: semanticSchemas, rootTaskID: rootTaskID, turnID: turnID, documentInputs: documentInputs, audioInputs: audioInputs, conversationLookupReused: conversationLookupReused}, true, semanticUnmetNeedsError{Unmet: plan.Unmet}
	}
	// Confirmation is represented as a plan dependency and therefore has no
	// executable tool surface in this phase. The existing confirmation UX owns
	// the user interaction; a later snapshot can materialize the selection.
	for _, selection := range plan.Selections {
		if selection.RequiresConfirm {
			return nil, true, errSemanticAwaitingConfirmation
		}
	}
	return &semanticPlanPreparation{registry: registry, plan: plan, definitions: defsByName, schemas: semanticSchemas, rootTaskID: rootTaskID, turnID: turnID, documentInputs: documentInputs, audioInputs: audioInputs, conversationLookupReused: conversationLookupReused}, true, nil
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
const semanticGrantPromptFence = "\nGoverned tools: the live tool list is the ground truth — call any listed name, and call it again while it stays listed. After a successful call a name may briefly leave the list and reappear for a later step in this same turn; lookup tools (search, fetch) are expected to be used several times. Call ONE tool per response: the list is a state machine, not a static catalog — every result changes what is listed next, so a batched second call races the first call's outcome and is rejected. Unlisted capability? Query tools_search for the exact name; a name it marks 可请愿 (petitionable) — for example an unlisted web_search, web_fetch, bash, or office — may be called once even though unlisted: the host may authorize it on the spot. A name it marks planned, exhausted, or unavailable stays invalid. Later steps such as PDF or image render unlock after the current step succeeds, in this same reply; when a render or delivery tool is listed, call it immediately and do not say please wait; do not tell the user a tool is missing. Never claim a tool is used up or unavailable unless its own denial message just said so — an untried petitionable name is available, so try it instead of concluding it is gone. Do not narrate tool limits or availability mechanics to the user; just do the work or state plainly what could not be done. Do not invent previous_turn_tool or reuse leftover invoke_* names. Do not fetch a search-engine page.\n"

func ensureSemanticGrantPromptFence(prompt string) string {
	if strings.Contains(prompt, "Governed tools:") {
		return prompt
	}
	return prompt + semanticGrantPromptFence
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
	if errors.Is(err, errSemanticAwaitingConfirmation) || errors.Is(err, errSemanticGenerateDeliveryConflict) ||
		semanticTrustedDocumentInputError(err) || semanticUnmetHasReason(err, "policy_denied") {
		return semanticHostRejectResponseForPlanError(err)
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) {
		return semanticHostRejectResponseForPlanError(err)
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) {
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
	if len(defs) == 0 || len(grants) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(extractToolName(def))
		if name == "" || isLegacySemanticBypassName(name) {
			continue
		}
		if _, ok := grants[name]; !ok {
			continue
		}
		out = append(out, def)
	}
	return out
}

// closedManagedSemanticDefinitionsForTurn applies the same grant close used at
// loop start, then the light-safe selection filter the authorizer will apply.
// Without this, a light turn could enter RunLoop with tools that the first
// FilterToolDefinitionsByAuthorizer immediately drops to empty.
func closedManagedSemanticDefinitionsForTurn(defs []map[string]interface{}, surface *semanticCallSurface, light bool) []map[string]interface{} {
	if surface == nil {
		return nil
	}
	closed := closedManagedSemanticDefinitions(defs, surface.grants)
	if !light {
		return closed
	}
	out := make([]map[string]interface{}, 0, len(closed))
	for _, def := range closed {
		name := strings.TrimSpace(extractToolName(def))
		grant, ok := surface.grants[name]
		if !ok {
			continue
		}
		selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
		if !ok || !tool.IsLightPromptSafeSelection(selection) {
			continue
		}
		out = append(out, def)
	}
	return out
}

func isLegacySemanticBypassName(name string) bool {
	return tool.IsLegacyDynamicGatewayName(name)
}

func semanticFileDeliveryPublished(channel string) bool {
	switch normalizeIMMessagePlatformKind(channel) {
	case imMessagePlatformDesktop, imMessagePlatformTUI, imMessagePlatformLansenger, imMessagePlatformLansengerLocal, imMessagePlatformWeixin, imMessagePlatformWeixinLocal:
		return true
	default:
		return false
	}
}

func semanticTrustedDispatchDestination(destination string) bool {
	destination = strings.TrimSpace(destination)
	switch {
	case strings.HasPrefix(destination, "group:") && len(destination) > len("group:"):
		return true
	case strings.HasPrefix(destination, "user:") && len(destination) > len("user:"):
		return true
	default:
		return false
	}
}

func semanticScheduleDispatchPublished(channel, destination string) bool {
	if !semanticTrustedDispatchDestination(destination) {
		return false
	}
	switch normalizeIMMessagePlatformKind(channel) {
	case imMessagePlatformDesktop, imMessagePlatformTUI, imMessagePlatformLansenger, imMessagePlatformLansengerLocal:
		return true
	default:
		return false
	}
}

func semanticVoiceDeliveryPublished(channel, destination string) bool {
	switch normalizeIMMessagePlatformKind(channel) {
	case imMessagePlatformLansenger, imMessagePlatformLansengerLocal:
		return semanticTrustedDispatchDestination(destination)
	default:
		return false
	}
}

func semanticAudioSynthesizeLocalPublished(channel string) bool {
	switch normalizeIMMessagePlatformKind(channel) {
	case imMessagePlatformDesktop, imMessagePlatformTUI:
		return true
	default:
		return false
	}
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
	return strings.TrimSpace(channel) != ""
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
			"description": "Deliver the approved file artifact to the current channel.",
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
type semanticPlanningBudgetKey struct{}
type semanticDestinationKey struct{}
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
	if maxSelections < 0 {
		maxSelections = 0
	}
	if maxSchemaTokens < 0 {
		maxSchemaTokens = 0
	}
	return tool.PlanningBudget{MaxSelections: maxSelections, MaxSchemaTokens: maxSchemaTokens}
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

func semanticRoutingContext(loop *LoopContext) (context.Context, context.CancelFunc) {
	if loop != nil {
		ctx, cancel := loop.Context()
		ctx = withSemanticWorkflowLoop(ctx, loop.WorkflowAgentLoop)
		ctx = withSemanticDestination(ctx, sessionGovernedDestination(loop))
		ctx = withSemanticPlanningBudget(ctx, loop.Runtime.Execution.ToolBudget)
		ctx = withSemanticSchemaTokenBudget(ctx, loop.Runtime.Execution.SchemaTokenBudget)
		ctx = withSemanticExecutionLayer(ctx, loop.Runtime.Execution.Layer)
		if expertDefForUserID(loop.UserID) != nil {
			ctx = withSemanticExpertSession(ctx, true)
		}
		if strings.TrimSpace(loop.ComputerUseRoutingText) != "" && !loop.ComputerUseBlockedForLocalFileWork {
			ctx = withSemanticComputerUseActive(ctx, true)
		}
		if loop.LansengerGroupPermissions != nil {
			ctx = withSemanticGroupPermissions(ctx, loop.LansengerGroupPermissions)
		}
		ctx = withSemanticConversationHistory(ctx, loop.History)
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
	planCtx := withSemanticArchetypeBundleKeyOverride(requestCtx, bundleKey)
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
		&semanticReplanInput{UserID: input.UserID, Channel: input.Channel, RootTaskID: input.RootTaskID, Classification: input.Classification, Attachments: cloneSemanticMessageAttachments(input.Attachments), Attempts: input.Attempts + 1, ConversationLookupReused: input.ConversationLookupReused, BundleKey: bundleKey},
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
	if surface.coordinator != nil {
		state, initialGrants, err = surface.coordinator.PublishSurface(tool.SurfacePublishRequest{Revision: publishRequest, TenantID: surface.tenantID, Issuer: surface.issuer, GrantTTL: semanticInvocationGrantTTL, Now: time.Now().UTC()})
	} else {
		state, err = surface.routeState.PublishRevision(publishRequest, time.Now().UTC())
	}
	if err != nil {
		return nil, nil, fmt.Errorf("publish semantic replan: %w", err)
	}
	artifactStore, err := h.semanticArtifactStore()
	if err != nil {
		return nil, nil, err
	}
	child := &semanticCallSurface{
		plan: state.Plan, scope: childScope, issuer: surface.issuer, executor: surface.executor, routeState: surface.routeState, hostCalls: surface.hostCalls, coordinator: surface.coordinator, tenantID: surface.tenantID, registry: prepared.registry,
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
			name := semanticSurfaceGrantName(materialization.Grant)
			child.materialized[materialization.Grant.SelectionID] = true
			child.grants[name] = materialization.Grant
		}
	}
	for _, grant := range initialGrants {
		name := semanticSurfaceGrantName(grant)
		child.materialized[grant.SelectionID] = true
		child.grants[name] = grant
	}
	completed, err := child.executor.Completed(child.scope)
	if err != nil {
		return nil, nil, err
	}
	projected, err := child.routeState.CompletedSelections(child.scope)
	if err != nil {
		return nil, nil, err
	}
	for selectionID := range projected {
		completed[selectionID] = true
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
	return "replan:" + tool.SchemaDigest([]byte(strings.TrimSpace(parentTurnID) + fmt.Sprintf(":%d", attempt)))[:24]
}

// semanticPetitionTurnID derives the expansion child's turn identity from the
// parent turn and the added label, keeping it distinct from failure-replan
// identities without consuming the failure-replan attempt budget.
func semanticPetitionTurnID(parentTurnID string, label intent.IntentLabel) string {
	return "petition:" + tool.SchemaDigest([]byte(strings.TrimSpace(parentTurnID) + ":" + strings.TrimSpace(string(label))))[:24]
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
}

// semanticPetitionLookupLabels is the deterministic preference order used when
// one capability is backed by several rule labels (information.search.web is
// the sole required template of both search and live_data). Labels not listed
// here fall back to lexicographic label-name order.
var semanticPetitionLookupLabels = []intent.IntentLabel{intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch}

// semanticPetitionLabelForCapability resolves the intent label a petition for
// the capability may add, derived deterministically from the rule set rather
// than from the turn's classification: a label whose single required template
// is exactly this capability wins (LabelFileRead for fs.read.local, LabelOffice
// for document.write.office — its delivery leg stays optional); when no such
// label exists, any label whose templates mention the capability qualifies
// (LabelDocumentGenerate for document.generate.file, LabelScreenshot for
// visual.capture.desktop), because the expansion validator admits exactly the
// label's template needs either way. Ties break by semanticPetitionLookupLabels
// preference, then by label name. A capability no rule label backs is not
// petitionable.
func semanticPetitionLabelForCapability(capability tool.CapabilityID) (intent.IntentLabel, bool) {
	var sole, mentioned []intent.IntentLabel
	for label, templates := range imSemanticIntentRuleSet {
		required := 0
		requiredMatch := false
		contains := false
		for _, template := range templates {
			if template.Required {
				required++
			}
			if template.Capability == capability {
				contains = true
				if template.Required {
					requiredMatch = true
				}
			}
		}
		if required == 1 && requiredMatch {
			sole = append(sole, label)
		} else if contains {
			mentioned = append(mentioned, label)
		}
	}
	if label, ok := semanticPetitionPreferredLabel(sole); ok {
		return label, true
	}
	return semanticPetitionPreferredLabel(mentioned)
}

// semanticPetitionPreferredLabel picks one candidate deterministically:
// semanticPetitionLookupLabels preference order first, then label name.
func semanticPetitionPreferredLabel(candidates []intent.IntentLabel) (intent.IntentLabel, bool) {
	if len(candidates) == 0 {
		return "", false
	}
	for _, preferred := range semanticPetitionLookupLabels {
		for _, candidate := range candidates {
			if candidate == preferred {
				return candidate, true
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates[0], true
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
	if strings.TrimSpace(parent.RootTaskID) == "" || parent.RootTaskID != child.RootTaskID {
		return fmt.Errorf("semantic petition expansion root task mismatch")
	}
	if len(child.Unmet) > 0 {
		return fmt.Errorf("semantic petition expansion has unmet needs")
	}
	templates := imSemanticIntentRuleSet[label]
	if len(templates) == 0 {
		return fmt.Errorf("semantic petition label has no rule template")
	}
	parentByNeed := make(map[string]tool.PlannedSelection, len(parent.Selections))
	for _, selection := range parent.Selections {
		needID := strings.TrimSpace(selection.NeedID)
		if needID == "" {
			return fmt.Errorf("semantic petition parent needs are not identifiable")
		}
		if _, exists := parentByNeed[needID]; exists {
			return fmt.Errorf("semantic petition parent needs are not unique")
		}
		parentByNeed[needID] = selection
	}
	childByNeed := make(map[string]tool.PlannedSelection, len(child.Selections))
	for _, selection := range child.Selections {
		needID := strings.TrimSpace(selection.NeedID)
		if needID == "" {
			return fmt.Errorf("semantic petition child needs are not identifiable")
		}
		if _, exists := childByNeed[needID]; exists {
			return fmt.Errorf("semantic petition child needs are not unique")
		}
		childByNeed[needID] = selection
	}
	for needID, selection := range parentByNeed {
		replacement, ok := childByNeed[needID]
		if !ok || !sameSemanticSelectionAuthorityIgnoringProvider(selection, replacement) {
			return fmt.Errorf("semantic petition expansion alters parent authority")
		}
	}
	added := 0
	for needID, selection := range childByNeed {
		if _, ok := parentByNeed[needID]; ok {
			continue
		}
		matched := false
		for _, template := range templates {
			if selection.FitProof.MatchedCapability == template.Capability && sameSemanticQualifiers(selection.FitProof.QualifierBindings, template.Qualifiers) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("semantic petition expansion adds a need outside the petitioned label")
		}
		added++
	}
	if added == 0 {
		return fmt.Errorf("semantic petition expansion added no governed need")
	}
	return nil
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
	planCtx := withSemanticArchetypeBundleKeyOverride(requestCtx, bundleKey)
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
	return h.publishSemanticChildRevision(requestCtx, surface, prepared,
		&semanticReplanInput{UserID: input.UserID, Channel: input.Channel, RootTaskID: input.RootTaskID, Classification: expanded, Attachments: cloneSemanticMessageAttachments(input.Attachments), Attempts: input.Attempts, BundleKey: bundleKey},
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

func sameSemanticSelectionAuthorityIgnoringProvider(parent, child tool.PlannedSelection) bool {
	return parent.NeedID == child.NeedID &&
		parent.Phase == child.Phase &&
		parent.RequiresConfirm == child.RequiresConfirm &&
		parent.ConfirmationID == child.ConfirmationID &&
		// A binding replacement may use a different schema encoding but cannot
		// gain a model-controlled field. The child renderer/validator remains
		// independently bound to its new immutable authorization digest.
		semanticReplanParametersDoNotExpand(parent.ParameterAuthorization, child.ParameterAuthorization) &&
		parent.FitProof.MatchedCapability == child.FitProof.MatchedCapability &&
		sameSemanticQualifiers(parent.FitProof.QualifierBindings, child.FitProof.QualifierBindings) &&
		sameSemanticEffects(parent.Effects, child.Effects) &&
		sameSemanticArtifactContracts(parent.Consumes, child.Consumes) &&
		sameSemanticArtifactContracts(parent.Produces, child.Produces)
}

func semanticReplanParametersDoNotExpand(parent, child tool.ParameterAuthorization) bool {
	if parent.CanonicalizerVer == "" || parent.CanonicalizerVer != child.CanonicalizerVer {
		return false
	}
	parentFields := make(map[string]struct{}, len(parent.AllowedFields))
	for _, field := range parent.AllowedFields {
		parentFields[field] = struct{}{}
	}
	for _, field := range child.AllowedFields {
		if _, allowed := parentFields[field]; !allowed {
			return false
		}
	}
	return true
}

func sameSemanticSelectionAuthority(parent, child tool.PlannedSelection) bool {
	return parent.NeedID == child.NeedID && parent.Phase == child.Phase && parent.RequiresConfirm == child.RequiresConfirm && parent.ConfirmationID == child.ConfirmationID && parent.ParameterAuthorization.Digest == child.ParameterAuthorization.Digest && parent.ParameterAuthorization.CanonicalizerVer == child.ParameterAuthorization.CanonicalizerVer && parent.FitProof.MatchedCapability == child.FitProof.MatchedCapability && sameSemanticQualifiers(parent.FitProof.QualifierBindings, child.FitProof.QualifierBindings) && sameSemanticEffects(parent.Effects, child.Effects) && sameSemanticArtifactContracts(parent.Consumes, child.Consumes) && sameSemanticArtifactContracts(parent.Produces, child.Produces)
}

func sameSemanticQualifiers(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func sameSemanticEffects(left, right []tool.EffectClass) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[tool.EffectClass]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

func sameSemanticArtifactContracts(left, right []tool.ArtifactContract) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value.Kind+"\x00"+value.MIMEType+fmt.Sprintf("\x00%t", value.Required)]++
	}
	for _, value := range right {
		seen[value.Kind+"\x00"+value.MIMEType+fmt.Sprintf("\x00%t", value.Required)]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
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

func appendClosedHostSemanticProviders(providers *[]tool.ProviderSpec, defsByName, schemas map[string]map[string]interface{}, channel string, h *IMMessageHandler) error {
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
		{semanticTrustedBuildVerifyAdapter, semanticTrustedBuildVerifyImplementation, semanticTrustedBuildVerifyDefinition(), semanticTrustedBuildVerifyInvocationSchema(), tool.CapabilityBuildVerifyLocal, nil, []tool.EffectClass{tool.EffectSensitive}, true},
		{semanticTrustedAcquireRemoteAdapter, semanticTrustedAcquireRemoteImplementation, semanticTrustedAcquireRemoteDefinition(), semanticTrustedAcquireRemoteInvocationSchema(), tool.CapabilityArtifactAcquireRemote, nil, []tool.EffectClass{tool.EffectSensitive}, true},
	}
	if semanticTrustedDelegatePublished(h) {
		items = append(items, hostProvider{semanticTrustedDelegateAdapter, semanticTrustedDelegateImplementation, semanticTrustedDelegateDefinition(), semanticTrustedDelegateInvocationSchema(), tool.CapabilityAgentDelegateSubtask, nil, []tool.EffectClass{tool.EffectSensitive}, true})
	}
	if semanticTrustedSSHPublished(h) {
		items = append(items, hostProvider{semanticTrustedSSHAdapter, semanticTrustedSSHImplementation, semanticTrustedSSHDefinition(), semanticTrustedSSHInvocationSchema(), tool.CapabilityShellExecuteRemoteHost, nil, []tool.EffectClass{tool.EffectExternalEffect}, true})
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
