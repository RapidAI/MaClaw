package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// srvIntentTreeHTTPTimeout is only a stuck-connection backstop. The unified
// classifier's own context is the classification budget: 12s after an
// embedding guess, 30s when embedding is unavailable.
const srvIntentTreeHTTPTimeout = 35 * time.Second

type srvIntentPrincipalContextKey struct{}

// srvIntentTreeFunc classifies one utterance with the requesting principal's
// LLM. Tests replace it; production loads that principal's config.
type srvIntentTreeFunc func(ctx, parentCtx context.Context, p agentservice.Principal, systemPrompt, userText string) (string, error)

type srvIntentLeaseKey struct {
	tenantID string
	userID   string
	text     string
}

// srvPrincipalIntentClassifier is the same UnifiedIntentClassifier the GUI
// uses. Layer 2 (the shared embedding model) decides a plain weather lookup
// locally. Layer 3 runs only when that guess is ambiguous, and a timeout or
// protocol failure comes back as a classification result. Returning that
// failure as an error used to mark the turn managed with an empty tool list,
// so the model answered that it could not read live weather.
type srvPrincipalIntentClassifier struct {
	svc    *agentservice.Service
	client *http.Client
	uic    *intent.UnifiedIntentClassifier
	tree   srvIntentTreeFunc
	// leases keeps the principal for a late tree verdict. That retry runs on
	// context.Background after the turn's deadline, so the request context is
	// gone. The map is keyed by tenant and user; a shared utterance never
	// borrows another tenant's model.
	leases sync.Map // srvIntentLeaseKey -> time.Time
}

// srvDynamicIntentClassifier is the process classifier so the embedding model
// can be attached after knowledge startup, the same late wiring the GUI uses.
var srvDynamicIntentClassifier *srvPrincipalIntentClassifier

func newSrvPrincipalIntentClassifier(svc *agentservice.Service) *srvPrincipalIntentClassifier {
	c := &srvPrincipalIntentClassifier{
		svc:    svc,
		client: &http.Client{Timeout: srvIntentTreeHTTPTimeout},
	}
	c.tree = c.defaultIntentTree
	c.uic = intent.New(intent.Config{
		LLMContextFunc:     c.classifyTree,
		LLMTimeout:         intent.DefaultLLMTimeout,
		FusionTreeDeadline: intent.DefaultFusionTreeDeadline,
	})
	return c
}

func (c *srvPrincipalIntentClassifier) setEmbedder(emb embedding.Embedder) {
	if c == nil || c.uic == nil {
		return
	}
	c.uic.SetEmbedder(emb)
}

func attachSrvIntentEmbedder(emb embedding.Embedder) {
	if srvDynamicIntentClassifier == nil {
		return
	}
	srvDynamicIntentClassifier.setEmbedder(emb)
}

func (c *srvPrincipalIntentClassifier) ClassifyDynamicIntent(ctx context.Context, p agentservice.Principal, userText string) (intent.ClassificationResult, error) {
	if c == nil || c.uic == nil {
		return intent.ClassificationResult{}, fmt.Errorf("semantic intent classifier service is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.rememberPrincipal(p, userText)
	ctx = context.WithValue(ctx, srvIntentPrincipalContextKey{}, p)
	result := c.uic.ClassifyContext(ctx, intent.MessageContext{Text: userText, UserID: p.UserID})
	return projectSrvReadOnlyLookupForPlanning(result), nil
}

func (c *srvPrincipalIntentClassifier) rememberPrincipal(p agentservice.Principal, text string) {
	if c == nil || strings.TrimSpace(text) == "" || (p.TenantID == "" && p.UserID == "") {
		return
	}
	now := time.Now()
	c.leases.Range(func(key, value any) bool {
		until, ok := value.(time.Time)
		if ok && !now.Before(until) {
			c.leases.Delete(key)
		}
		return true
	})
	c.leases.Store(srvIntentLeaseKey{tenantID: p.TenantID, userID: p.UserID, text: text}, now.Add(intent.DefaultLLMTimeout))
}

func (c *srvPrincipalIntentClassifier) principalForLateTree(text string) (agentservice.Principal, bool) {
	if c == nil || text == "" {
		return agentservice.Principal{}, false
	}
	now := time.Now()
	var found agentservice.Principal
	matches := 0
	c.leases.Range(func(key, value any) bool {
		lease, ok := key.(srvIntentLeaseKey)
		until, untilOK := value.(time.Time)
		if !ok || !untilOK || !now.Before(until) {
			c.leases.Delete(key)
			return true
		}
		if lease.text != text {
			return true
		}
		matches++
		found = agentservice.Principal{TenantID: lease.tenantID, UserID: lease.userID}
		return true
	})
	if matches != 1 {
		return agentservice.Principal{}, false
	}
	return found, true
}

func (c *srvPrincipalIntentClassifier) classifyTree(ctx, parentCtx context.Context, systemPrompt, userText string) (string, error) {
	p, ok := principalFromIntentContext(ctx)
	if !ok {
		p, ok = principalFromIntentContext(parentCtx)
	}
	if !ok {
		p, ok = c.principalForLateTree(userText)
	}
	if !ok {
		return "", fmt.Errorf("semantic classifier principal is unavailable")
	}
	if c == nil || c.tree == nil {
		return "", fmt.Errorf("semantic intent classifier service is unavailable")
	}
	return c.tree(ctx, parentCtx, p, systemPrompt, userText)
}

func (c *srvPrincipalIntentClassifier) defaultIntentTree(ctx, _ context.Context, p agentservice.Principal, systemPrompt, userText string) (string, error) {
	if c == nil || c.svc == nil {
		return "", fmt.Errorf("semantic intent classifier service is unavailable")
	}
	config, err := c.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		return "", fmt.Errorf("load principal semantic classifier configuration: %w", err)
	}
	llmConfig, err := agentservice.ResolveLLMConfig(config.AppConfig)
	if err != nil {
		return "", fmt.Errorf("resolve principal semantic classifier configuration: %w", err)
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: srvIntentTreeHTTPTimeout}
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userText},
	}
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "maclawsrv-dynamic-semantic-intent"})
	response, err := agent.DoSimpleLLMRequestContextWithOptions(ctx, llmConfig, messages, client, srvIntentCallTimeout(ctx), agent.SimpleLLMRequestOptions{
		ResponseFormat:         intent.TreeResponseFormat(),
		PreserveResponseFormat: true,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

func srvIntentCallTimeout(ctx context.Context) time.Duration {
	timeout := srvIntentTreeHTTPTimeout
	if ctx == nil {
		return timeout
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return timeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return time.Millisecond
	}
	if remaining < timeout {
		return remaining
	}
	return timeout
}

func principalFromIntentContext(ctx context.Context) (agentservice.Principal, bool) {
	if ctx == nil {
		return agentservice.Principal{}, false
	}
	p, ok := ctx.Value(srvIntentPrincipalContextKey{}).(agentservice.Principal)
	if !ok || (p.TenantID == "" && p.UserID == "") {
		return agentservice.Principal{}, false
	}
	return p, true
}

// projectSrvReadOnlyLookupForPlanning applies the GUI lookup planning
// projection. A search/live_data hint at or above 0.70, including the
// degraded hint left when the tree times out after a local guess, still has
// to clear the shared 0.78 resolver floor or the governed web_search grant
// never appears. A protocol failure and every non-lookup label stay put:
// this projection must not mint a write.
func projectSrvReadOnlyLookupForPlanning(result intent.ClassificationResult) intent.ClassificationResult {
	if result.ControlPlaneFailure || result.Confidence < intent.EmbeddingLookupMinScore || !srvReadOnlyLookupFamily(result) {
		return result
	}
	result.Degraded = false
	if result.Confidence < agentservice.ReviewedIntentMinimumConfidence {
		result.Confidence = agentservice.ReviewedIntentMinimumConfidence
	}
	return result
}

func srvReadOnlyLookupFamily(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelLiveData:
	default:
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

// configureSrvDynamicSemanticRouting activates the reviewed
// information.search.web family (LabelSearch / LabelLiveData) via the host
// web-search provider, keeps information.lookup for published MCP/Skill
// contracts, and the host-owned
// information.current_time clock, knowledge.read.local store read,
// security.audit.read, information.fetch.web, fs.read.local, repo.inspect.vcs,
// document.read.local, fs.write.local, document.write.office (spreadsheet),
// shell.execute.local, agent.delegate.subtask, knowledge.ingest.local,
// memory.manage.agent, task.track.local, goal.manage.longrunning,
// template.manage.session, schedule.administer.local,
// knowledge.admin.maintenance, config.manage.self,
// session.manage.coding, and audio.transcribe.speech. GUI desktop families such as
// schedule.dispatch.channel live in the IM builtin
// catalog and are not copied here: this host has no IM catalog and no
// delivery-receipt worker. current_time, knowledge_read,
// audit_read (events plus principal conversation snippets), web_fetch,
// file_read (workspace text, native Office/PDF extract, optional search), git_inspect,
// document_read (one trusted current-turn attachment from host-published
// ingress), file_write (workspace overwrite/append with a host-owned
// local mutation receipt), office_write (workspace spreadsheet path+sheets
// with a host-owned local mutation receipt; not word/presentation, not
// office / write_excel soup), shell (workspace command plus optional
// timeout; cwd is host-fixed, not bash / project_path soup), delegate
// (task only; wait for a child receipt; started is not completed; child
// turns set delegate_child so they cannot nest), and knowledge_write (text XOR url XOR workspace
// path ingest with a host-owned local mutation receipt; file vs directory
// is decided by the filesystem type, not keywords), and memory_manage
// (content XOR query XOR id, or empty list, with a host-owned local
// mutation receipt; not knowledge read/ingest), and task_track (title
// create, id+status update, id delete, or empty list, with a host-owned
// local mutation receipt; not goal/delegate/schedule), goal_manage
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
// audio attachment plus the host ASR manager when the model file is
// present; no path/asr/minutes)
// are not satisfied by lookup. Other labels remain unmanaged until they have descriptors, policies,
// a lifecycle publisher, and receipt semantics. This function has no keyword
// fallback.
func configureSrvDynamicSemanticRouting(svc *agentservice.Service) error {
	registry, err := agentservice.NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		return fmt.Errorf("create reviewed dynamic capability registry: %w", err)
	}
	classifier := newSrvPrincipalIntentClassifier(svc)
	srvDynamicIntentClassifier = classifier
	resolver := &agentservice.PrincipalIntentLabelCapabilityNeedResolver{
		Classifier:        classifier,
		Registry:          registry,
		Rules:             agentservice.ReviewedDynamicIntentCapabilityNeedRules(),
		MinimumConfidence: agentservice.ReviewedIntentMinimumConfidence,
		AmbientRetrieval:  true,
		ArchetypeBundles:  true,
	}
	if err := svc.ConfigureDynamicSemanticRouting(registry, resolver, agentservice.ReviewedDynamicCapabilityPolicyAdapter(), coretool.DefaultInvocationGrantTTL); err != nil {
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
	return nil
}

// startSrvDynamicEffectReceiptWorker launches the generic receipt
// reconciliation loop for dynamic external/sensitive effects. No
// binding-specific receipt source is registered yet; the loop runs empty
// until a trusted channel/provider integration attaches one. The worker
// settles only through Service.ReconcileDynamicEffectReceiptSource, so it
// holds no grant, adapter name, or dispatch path.
func startSrvDynamicEffectReceiptWorker(ctx context.Context, svc *agentservice.Service) (*agentservice.DynamicEffectReceiptWorker, error) {
	worker, err := agentservice.NewDynamicEffectReceiptWorker(svc.ReconcileDynamicEffectReceiptSource, 0)
	if err != nil {
		return nil, fmt.Errorf("create dynamic effect receipt worker: %w", err)
	}
	worker.Logf = func(format string, args ...interface{}) {
		log.Printf("[semantic-effect-receipts] "+format, args...)
	}
	// With no source registered the loop would otherwise do nothing at all.
	// Expiring stale receipt waits is what keeps an unconfirmed operation from
	// parking in awaiting_receipt forever, where nothing -- including manual
	// resolution -- can reach it.
	worker.ExpireReceiptWaits = svc.ExpireDynamicEffectReceiptWaits
	if err := worker.Start(ctx); err != nil {
		return nil, fmt.Errorf("start dynamic effect receipt worker: %w", err)
	}
	return worker, nil
}
