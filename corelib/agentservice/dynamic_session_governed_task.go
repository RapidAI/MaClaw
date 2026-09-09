package agentservice

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// SessionGovernedTask is the session-scoped replay record for a managed
// Core Agent / Hub dynamic turn. It stores only planner-granted needs, never
// the original UIC label set. RootTaskID is omitted: a later continuation
// allocates a new loop identity and must not merge the previous RouteState.
type SessionGovernedTask struct {
	Needs  []coretool.CapabilityNeed
	Status sessionGovernedTaskStatus
}

type sessionGovernedTaskStatus string

const (
	sessionGovernedPending     sessionGovernedTaskStatus = "pending"
	sessionGovernedSucceeded   sessionGovernedTaskStatus = "succeeded"
	sessionGovernedFailedUnmet sessionGovernedTaskStatus = "failed_unmet"
	sessionGovernedFailedExec  sessionGovernedTaskStatus = "failed_exec"
	// sessionGovernedUnknown is a non-replayable terminal: the host could not
	// prove success or failure after a possible mutation. Continue must not
	// auto-resend.
	sessionGovernedUnknown sessionGovernedTaskStatus = "unknown"
)

// SessionGovernedTaskStore holds the last granted semantic needs for one
// Service process. The key is tenant + user + channel + destination, never a
// loop or RootTaskID. Memory is enough: this is session continuation, not
// crash recovery of an external effect.
type SessionGovernedTaskStore struct {
	tasks sync.Map
	// Coordinator owns the durable route and continuity projection outboxes.
	// It is optional while older in-memory test hosts migrate; when present the
	// legacy map is no longer allowed to create task continuity.
	Coordinator *coretool.SQLiteSemanticExecutionCoordinator
	TenantID    string
}

func NewSessionGovernedTaskStore() *SessionGovernedTaskStore {
	return &SessionGovernedTaskStore{}
}

// BindCoordinator makes this compatibility facade read only rebuildable
// ContinuityState. It deliberately does not write the old map: publication and
// execution transactions already emit the authoritative outbox events.
func (s *SessionGovernedTaskStore) BindCoordinator(coordinator *coretool.SQLiteSemanticExecutionCoordinator, tenantID string) {
	if s == nil {
		return
	}
	s.Coordinator, s.TenantID = coordinator, strings.TrimSpace(tenantID)
}

func sessionGovernedTaskKey(request DynamicCapabilityNeedRequest) string {
	channel := strings.TrimSpace(request.ChannelScope)
	if channel == "" {
		channel = "core-agent"
	}
	return strings.TrimSpace(request.Principal.TenantID) + "\x1f" +
		strings.TrimSpace(request.Principal.UserID) + "\x1f" +
		channel + "\x1f" +
		strings.TrimSpace(request.DestinationID)
}

func sessionGovernedPrincipalComplete(request DynamicCapabilityNeedRequest) bool {
	return strings.TrimSpace(request.Principal.TenantID) != "" && strings.TrimSpace(request.Principal.UserID) != ""
}

func isGenericContinuationPrimary(result intent.ClassificationResult) bool {
	return result.IsGenericContinuationPrimary()
}

func grantedNeedsFromPlan(plan coretool.ToolPlan) []coretool.CapabilityNeed {
	return coretool.GrantedNeedsFromPlan(plan)
}

// sessionGovernedNeedHasSideEffect reports whether replaying this need would
// redo a local mutation or external effect. information.lookup,
// information.current_time, knowledge.read.local, security.audit.read,
// information.fetch.web, fs.read.local, repo.inspect.vcs, document.read.local, and audio.transcribe.speech are read-only on the reviewed registry; a
// succeeded read must not be treated as an unfinished mutation. fs.write.local,
// document.write.office, document.generate.file, audio.render.speech, audio.synthesize.local, visual.capture.desktop, system.launch.local, artifact.acquire.remote, shell.execute.local, agent.delegate.subtask,
// shell.execute.remote_host, browser.control.web, computer.control.desktop,
// message.send.im, artifact.deliver.specified_target,
// artifact.deliver.current_channel, repo.mutate.vcs,
// knowledge.ingest.local, memory.manage.agent, task.track.local, and
// goal.manage.longrunning, template.manage.session, and
// schedule.administer.local, knowledge.admin.maintenance,
// config.manage.self, and session.manage.coding are local
// mutations: persist pending, mark succeeded after the host receipt, and
// never replay a succeeded or unknown mutation.
// Unknown
// capabilities default to side-effect so a later mutation family is not
// dropped from continuation.
func sessionGovernedNeedHasSideEffect(registry *coretool.CapabilityRegistry, need coretool.CapabilityNeed) bool {
	return coretool.CapabilityNeedHasSideEffect(registry, need)
}

func sessionGovernedNeedsHaveSideEffect(registry *coretool.CapabilityRegistry, needs []coretool.CapabilityNeed) bool {
	return coretool.CapabilityNeedsHaveSideEffect(registry, needs)
}

func (task SessionGovernedTask) replayable(registry *coretool.CapabilityRegistry) bool {
	if task.Status != sessionGovernedPending && task.Status != sessionGovernedFailedExec {
		return false
	}
	return sessionGovernedNeedsHaveSideEffect(registry, task.Needs)
}

func grantedNeedStillCovered(need coretool.CapabilityNeed, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, registry *coretool.CapabilityRegistry) bool {
	return coretool.GrantedNeedStillCovered(need, CoveredCapabilitiesFromNeedTemplates(rules), registry)
}

func grantedNeedsStillCovered(needs []coretool.CapabilityNeed, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, registry *coretool.CapabilityRegistry) []coretool.CapabilityNeed {
	return coretool.FilterGrantedNeedsStillCovered(needs, CoveredCapabilitiesFromNeedTemplates(rules), registry)
}

// ClassificationFromGrantedNeeds rebuilds a UIC result from planner-granted
// needs so a generic continuation can re-enter the same reviewed rule table.
// Special cases pin composite families (generate vs attachment delivery,
// search freshness, app_launch vs document_open). Remaining capabilities
// collect every mentioning label. GUI session-governed replay uses this;
// headless ReplayContinuation injects needs directly and does not reconstruct
// labels.
func ClassificationFromGrantedNeeds(needs []coretool.CapabilityNeed, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate) intent.ClassificationResult {
	hasGenerate := false
	hasImageDeliver := false
	hasFileDeliver := false
	seen := make(map[intent.IntentLabel]bool)
	labels := make([]intent.IntentLabel, 0, len(needs))
	add := func(label intent.IntentLabel) {
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		labels = append(labels, label)
	}
	for _, need := range needs {
		switch need.Capability {
		case CapabilityDocumentGenerate:
			hasGenerate = true
			add(intent.LabelDocumentGenerate)
		case CapabilityInformationSearchWeb:
			if need.Qualifiers[QualifierSearchFreshness] == SearchFreshnessCurrent {
				add(intent.LabelLiveData)
			} else {
				add(intent.LabelSearch)
			}
		case CapabilityCurrentTime:
			add(intent.LabelCurrentTime)
		case coretool.CapabilityAudioRenderSpeech:
			add(intent.LabelAudioDeliver)
		case coretool.CapabilitySystemLaunchLocal:
			// app_launch and document_open share this capability. Replay
			// keeps a single label so the planner emits one launch need.
			add(intent.LabelAppLaunch)
		case CapabilityVisualCapture:
			add(intent.LabelScreenshot)
		case CapabilityArtifactDeliverCurrent:
			if need.Qualifiers[QualifierArtifactFormat] == ArtifactFormatImage {
				hasImageDeliver = true
			}
			if need.Qualifiers[QualifierArtifactFormat] == ArtifactFormatFile {
				hasFileDeliver = true
			}
			if need.Qualifiers[QualifierArtifactFormat] == ArtifactFormatVoice {
				add(intent.LabelAudioDeliver)
			}
		default:
			for label, templates := range rules {
				for _, tmpl := range templates {
					if tmpl.Capability == need.Capability {
						add(label)
					}
				}
			}
		}
	}
	if hasImageDeliver {
		add(intent.LabelScreenshot)
	}
	if hasFileDeliver && !hasGenerate {
		add(intent.LabelAttachmentDelivery)
	}
	if len(labels) == 0 {
		return intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.9, Layer: 3}
	}
	return intent.ClassificationResult{Primary: labels[0], Secondary: append([]intent.IntentLabel(nil), labels[1:]...), Confidence: 0.98, Layer: 3}
}

func bindSessionGovernedStore(routing *DynamicSemanticRouting) {
	if routing == nil || routing.SessionGoverned == nil {
		return
	}
	switch resolver := routing.Resolver.(type) {
	case *IntentLabelCapabilityNeedResolver:
		resolver.SessionGoverned = routing.SessionGoverned
	case *PrincipalIntentLabelCapabilityNeedResolver:
		resolver.SessionGoverned = routing.SessionGoverned
	}
}

func (s *SessionGovernedTaskStore) PersistGrantedPlan(request DynamicCapabilityNeedRequest, registry *coretool.CapabilityRegistry, plan coretool.ToolPlan) {
	if s == nil || !sessionGovernedPrincipalComplete(request) {
		return
	}
	if s.Coordinator != nil {
		// PublishSurface owns the exact durable snapshot. A best-effort consumer
		// may process it later; this pre-publication call must not speculate a
		// task fact from an uncommitted plan.
		return
	}
	needs := grantedNeedsFromPlan(plan)
	if len(needs) == 0 {
		return
	}
	status := sessionGovernedPending
	if !sessionGovernedNeedsHaveSideEffect(registry, needs) {
		status = sessionGovernedSucceeded
	}
	s.tasks.Store(sessionGovernedTaskKey(request), SessionGovernedTask{
		Needs:  cloneDynamicCapabilityNeeds(needs),
		Status: status,
	})
}

func (s *SessionGovernedTaskStore) Load(request DynamicCapabilityNeedRequest) (SessionGovernedTask, bool) {
	if s == nil || !sessionGovernedPrincipalComplete(request) {
		return SessionGovernedTask{}, false
	}
	if s.Coordinator != nil {
		tenantID := s.TenantID
		if tenantID == "" {
			tenantID = strings.TrimSpace(request.Principal.TenantID)
		}
		// Consume only committed facts. A consumer error is a safe continuity
		// miss: the resolver will make a fresh plan rather than revive a tool.
		_, _ = s.Coordinator.DrainContinuityProjections(tenantID, 32, time.Now().UTC())
		continuityScope := coretool.ContinuityScope{TenantID: tenantID, PrincipalID: memoryOwnerIDForPrincipal(request.Principal), ConversationID: strings.TrimSpace(request.SessionID), RootTaskID: strings.TrimSpace(request.RootTaskID)}
		state, err := s.Coordinator.ContinuityState(continuityScope)
		if err != nil {
			return SessionGovernedTask{}, false
		}
		return SessionGovernedTask{Needs: cloneDynamicCapabilityNeeds(state.OpenNeeds), Status: sessionGovernedPending}, len(state.OpenNeeds) > 0
	}
	value, ok := s.tasks.Load(sessionGovernedTaskKey(request))
	if !ok {
		return SessionGovernedTask{}, false
	}
	task, ok := value.(SessionGovernedTask)
	if !ok {
		return SessionGovernedTask{}, false
	}
	task.Needs = cloneDynamicCapabilityNeeds(task.Needs)
	return task, true
}

func sessionGovernedRequestForCoreAgent(principal Principal) DynamicCapabilityNeedRequest {
	return DynamicCapabilityNeedRequest{Principal: principal, ChannelScope: "core-agent"}
}

func (c *coreAgentCallbacks) markSessionGovernedAfterDynamicResult(result coretool.SelectionExecutionResult) {
	if c == nil || c.dynamicSemanticRouting == nil || c.dynamicSemanticRouting.SessionGoverned == nil {
		return
	}
	status := sessionGovernedFailedExec
	switch {
	case result.Unknown:
		status = sessionGovernedUnknown
	case result.Succeeded:
		status = sessionGovernedSucceeded
	case result.AwaitingReceipt:
		return
	}
	c.dynamicSemanticRouting.SessionGoverned.Mark(sessionGovernedRequestForCoreAgent(c.principal), status)
}

func (s *SessionGovernedTaskStore) Mark(request DynamicCapabilityNeedRequest, status sessionGovernedTaskStatus) {
	if s == nil || status == "" || !sessionGovernedPrincipalComplete(request) {
		return
	}
	if s.Coordinator != nil {
		// Terminal status is a route/execution fact, written only by its owner.
		// The facade must never race it with a mutable session-local override.
		return
	}
	key := sessionGovernedTaskKey(request)
	value, ok := s.tasks.Load(key)
	if !ok {
		return
	}
	task, ok := value.(SessionGovernedTask)
	if !ok {
		return
	}
	task.Status = status
	s.tasks.Store(key, task)
}

func (s *SessionGovernedTaskStore) ClearPrincipal(principal Principal) {
	if s == nil {
		return
	}
	prefix := strings.TrimSpace(principal.TenantID) + "\x1f" + strings.TrimSpace(principal.UserID) + "\x1f"
	s.tasks.Range(func(key, _ any) bool {
		if text, ok := key.(string); ok && strings.HasPrefix(text, prefix) {
			s.tasks.Delete(key)
		}
		return true
	})
}

// ReplayContinuation returns the previously granted needs when the current
// UIC is a generic continuation/unknown/ambiguous turn and those needs are
// still replayable and covered. It injects needs directly and never invents
// a capability (including document.generate) from the short utterance.
func (s *SessionGovernedTaskStore) ReplayContinuation(request DynamicCapabilityNeedRequest, rules map[intent.IntentLabel][]IntentCapabilityNeedTemplate, registry *coretool.CapabilityRegistry, current intent.ClassificationResult) (DynamicCapabilityNeedResolution, bool) {
	// A generic utterance such as "continue" is only a candidate. It becomes
	// an actual continuation after ingress verifies a host-issued task handle
	// bound to this exact root task. This keeps an old mutation from being
	// replayed merely because the same user/channel spoke again.
	if s == nil || !isGenericContinuationPrimary(current) {
		return DynamicCapabilityNeedResolution{}, false
	}
	// Legacy in-memory stores remain test-only compatibility facades. The
	// durable coordinator is the production authority and must never replay a
	// mutation unless ingress verified a task-bound continuation handle.
	if s.Coordinator != nil && !request.TaskRelation.permitsContinuation(request) {
		return DynamicCapabilityNeedResolution{}, false
	}
	task, ok := s.Load(request)
	if !ok || !task.replayable(registry) {
		return DynamicCapabilityNeedResolution{}, false
	}
	needs := grantedNeedsStillCovered(task.Needs, rules, registry)
	if len(needs) == 0 || !sessionGovernedNeedsHaveSideEffect(registry, needs) {
		return DynamicCapabilityNeedResolution{}, false
	}
	return DynamicCapabilityNeedResolution{Managed: true, Needs: needs}, true
}
