package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const mobileDynamicIntentClassificationTimeout = 12 * time.Second

// mobileSemanticClassifierLLM is the request-scoped LLM slot for the Hub
// mobile semantic intent classifier. It is installed by mobileRunCoreAgent
// and always points at the Hub-proxied official/delegated chat path of the
// requesting principal, so classification traffic stays on the existing
// Mobile billing path.
type mobileSemanticClassifierLLM struct {
	config corelib.MaclawLLMConfig
	client *http.Client
}

type mobileSemanticClassifierLLMContextKey struct{}

func mobileWithSemanticClassifierLLM(ctx context.Context, runtime mobileSemanticClassifierLLM) context.Context {
	return context.WithValue(ctx, mobileSemanticClassifierLLMContextKey{}, runtime)
}

func mobileSemanticClassifierLLMFromContext(ctx context.Context) (mobileSemanticClassifierLLM, bool) {
	runtime, ok := ctx.Value(mobileSemanticClassifierLLMContextKey{}).(mobileSemanticClassifierLLM)
	return runtime, ok
}

// mobilePrincipalIntentClassifier is the Hub counterpart of the MaClawSrv
// srvPrincipalIntentClassifier: it sends the same fixed intent-tree prompt
// without tools and never receives an MCP/Skill inventory, provider metadata,
// or ToolNames.
//
// Deliberate difference from MaClawSrv: the srv classifier loads the
// principal's saved LLM via Service.GetRawUserConfig and calls that provider
// directly. On Hub that is not usable — mobileMergeUserAgentConfig forces the
// Hub-proxied sentinel LLM for every mobile request, and calling a
// user-supplied provider URL from the control plane would bypass Hub billing.
// The classifier therefore rides the per-request Hub LLM proxy transport
// (viewer or delegated auth) carried in the request context. A request
// without that slot fails closed instead of falling back to any other
// credentials.
type mobilePrincipalIntentClassifier struct{}

func (mobilePrincipalIntentClassifier) ClassifyDynamicIntent(ctx context.Context, _ agentservice.Principal, userText string) (intent.ClassificationResult, error) {
	runtime, ok := mobileSemanticClassifierLLMFromContext(ctx)
	if !ok || runtime.client == nil {
		return intent.ClassificationResult{}, fmt.Errorf("hub mobile semantic intent classifier requires the request-scoped Hub LLM proxy")
	}
	tree := intent.BuildIntentTreeText(intent.DefaultDefinitions())
	messages := []interface{}{
		map[string]string{"role": "system", "content": intent.BuildTreePrompt(tree, userText)},
		map[string]string{"role": "user", "content": userText},
	}
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "hub-mobile-dynamic-semantic-intent"})
	response, err := agent.DoSimpleLLMRequestContextWithOptions(ctx, runtime.config, messages, runtime.client, mobileDynamicIntentClassificationTimeout, agent.SimpleLLMRequestOptions{
		ResponseFormat:         intent.TreeResponseFormat(),
		PreserveResponseFormat: true,
	})
	if err != nil {
		return intent.ClassificationResult{}, fmt.Errorf("classify dynamic semantic intent: %w", err)
	}
	candidates := intent.ParseTreeResponse(response.Content)
	if len(candidates) == 0 {
		return intent.ClassificationResult{}, fmt.Errorf("classify dynamic semantic intent: no valid candidate")
	}
	top := candidates[0]
	result := intent.ClassificationResult{
		Primary: top.Label, Confidence: top.Score, Layer: 3,
		Reason: "host semantic intent classifier",
	}
	for _, candidate := range candidates[1:] {
		if candidate.Label == result.Primary || candidate.Score < 0.70 || top.Score-candidate.Score > 0.20 {
			continue
		}
		result.Secondary = append(result.Secondary, candidate.Label)
	}
	return result, nil
}

var (
	mobileDynamicRoutingMu          sync.RWMutex
	mobileDynamicCapabilityRegistry *coretool.CapabilityRegistry
	mobileDynamicEffectWorker       *agentservice.DynamicEffectReceiptWorker
)

// configureMobileDynamicSemanticRouting mirrors the reviewed MaClawSrv
// bootstrap: information.lookup plus the host-owned information.current_time
// clock, knowledge.read.local store read, security.audit.read,
// information.fetch.web, fs.read.local, repo.inspect.vcs,
// document.read.local, fs.write.local, knowledge.ingest.local,
// memory.manage.agent, task.track.local, goal.manage.longrunning,
// template.manage.session, schedule.administer.local,
// knowledge.admin.maintenance, config.manage.self,
// session.manage.coding, and audio.transcribe.speech are
// activated. GUI IM builtins (information.search.web,
// schedule.dispatch.channel, document.generate.file, and the rest of the
// desktop catalog) are not published here — the reviewed dynamic registry
// has no descriptors or receipt workers for them. current_time,
// knowledge_read, audit_read (events plus principal conversation snippets),
// web_fetch, file_read (workspace text, native Office/PDF extract, optional search),
// git_inspect, document_read (one trusted current-turn attachment
// published from a viewer-owned mobile draft), file_write (workspace
// overwrite/append with a host-owned local mutation receipt), and
// knowledge_write (text XOR url XOR workspace path ingest with a
// host-owned local mutation receipt; file vs directory is decided by
// the filesystem type, not keywords), and memory_manage (content XOR
// query XOR id, or empty list, with a host-owned local mutation
// receipt; not knowledge read/ingest), and task_track (title create,
// id+status update, id delete, or empty list, with a host-owned local
// mutation receipt; not goal/delegate/schedule), goal_manage
// (objective create, empty get, status complete/failed, with a
// host-owned local mutation receipt; no continuation engine, budget, or
// pause/resume), and template_manage (name+coding_tool create, name get, empty
// list, with a host-owned local mutation receipt; no launch, yolo_mode,
// or session drive), and schedule_manage (name+task_action+hour create,
// id update/delete, empty list, with a host-owned local mutation
// receipt; no Delivery, list_targets, or fire), and knowledge_admin
// (empty list, id get, id+status enable/disable/delete, id+refresh,
// with a host-owned local mutation receipt; not read/ingest, no quality
// plan or snapshot soup), and config_manage (empty get,
// max_iterations or thinking_mode alone, with a host-owned local
// mutation receipt; provider/url/key/model switch is fail-closed),
// and session_manage (empty list, id get, with a host-owned local
// mutation receipt; drive/interrupt/send/launch are fail-closed),
// and audio_transcribe (empty object only; one trusted current-turn
// audio attachment plus a host speech engine when meeting/pairing ASR
// is configured; no path/asr/minutes)
// are not
// satisfied by lookup. Every other
// label stays unmanaged on the legacy bridge surface. There is no
// keyword-routing fallback.
func configureMobileDynamicSemanticRouting(svc *agentservice.Service) error {
	registry, err := agentservice.NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		return fmt.Errorf("create reviewed dynamic capability registry: %w", err)
	}
	resolver := &agentservice.PrincipalIntentLabelCapabilityNeedResolver{
		Classifier:        mobilePrincipalIntentClassifier{},
		Registry:          registry,
		Rules:             agentservice.ReviewedDynamicIntentCapabilityNeedRules(),
		MinimumConfidence: 0.78,
		AmbientRetrieval:  true,
	}
	if err := svc.ConfigureDynamicSemanticRouting(registry, resolver, agentservice.ReviewedDynamicCapabilityPolicyAdapter(), 10*time.Minute); err != nil {
		return fmt.Errorf("configure dynamic semantic routing: %w", err)
	}
	// SessionGovernedTask is Service-owned. Continuation replays only
	// planner-granted unfinished side-effect needs; read-only families settle
	// succeeded at plan time. fs.write.local, knowledge.ingest.local,
	// memory.manage.agent, task.track.local, goal.manage.longrunning,
	// template.manage.session, schedule.administer.local,
	// knowledge.admin.maintenance, config.manage.self, and
	// session.manage.coding stay
	// pending until the host local mutation receipt, then continue must not replay.
	mobileDynamicRoutingMu.Lock()
	mobileDynamicCapabilityRegistry = registry
	mobileDynamicRoutingMu.Unlock()
	return nil
}

// mobileDynamicCapabilityContractPublisher returns the authenticated
// control-plane publisher bound to the mobile agent Service. It is only
// handed to owner-gated admin lifecycle handlers, never to request execution.
func mobileDynamicCapabilityContractPublisher() (*agentservice.Service, *agentservice.DynamicCapabilityContractPublisher, error) {
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		return nil, nil, err
	}
	mobileDynamicRoutingMu.RLock()
	registry := mobileDynamicCapabilityRegistry
	mobileDynamicRoutingMu.RUnlock()
	if registry == nil {
		return nil, nil, fmt.Errorf("mobile dynamic capability registry is not configured")
	}
	publisher, err := agentservice.NewDynamicCapabilityContractPublisher(svc, registry)
	if err != nil {
		return nil, nil, fmt.Errorf("create dynamic capability contract publisher: %w", err)
	}
	return svc, publisher, nil
}

// startMobileDynamicEffectReceiptWorker launches the generic receipt
// reconciliation loop for dynamic external/sensitive effects. No
// binding-specific receipt source is registered yet; the loop runs empty
// until a trusted channel/provider integration attaches one. The worker
// settles only through Service.ReconcileDynamicEffectReceiptSource, so it
// holds no grant, adapter name, or dispatch path.
func startMobileDynamicEffectReceiptWorker(svc *agentservice.Service) error {
	worker, err := agentservice.NewDynamicEffectReceiptWorker(svc.ReconcileDynamicEffectReceiptSource, 0)
	if err != nil {
		return fmt.Errorf("create dynamic effect receipt worker: %w", err)
	}
	worker.Logf = func(format string, args ...interface{}) {
		log.Printf("[mobile-semantic-effect-receipts] "+format, args...)
	}
	// See the MaClawSrv worker: with no source registered this is the only
	// thing standing between an unconfirmed operation and an indefinite wait.
	worker.ExpireReceiptWaits = svc.ExpireDynamicEffectReceiptWaits
	if err := worker.Start(context.Background()); err != nil {
		return fmt.Errorf("start dynamic effect receipt worker: %w", err)
	}
	mobileDynamicRoutingMu.Lock()
	mobileDynamicEffectWorker = worker
	mobileDynamicRoutingMu.Unlock()
	return nil
}

// resetMobileDynamicSemanticRoutingForTest stops the receipt worker and drops
// the retained registry so a re-initialized mobile agent starts clean.
func resetMobileDynamicSemanticRoutingForTest() {
	mobileDynamicRoutingMu.Lock()
	defer mobileDynamicRoutingMu.Unlock()
	if mobileDynamicEffectWorker != nil {
		mobileDynamicEffectWorker.Stop()
		mobileDynamicEffectWorker = nil
	}
	mobileDynamicCapabilityRegistry = nil
}
