package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const srvDynamicIntentClassificationTimeout = 12 * time.Second

// srvPrincipalIntentClassifier is a host-owned semantic classifier. It loads
// only the requesting principal's configured LLM, sends a fixed intent-tree
// prompt without tools, and returns a governed intent label. It does not
// receive an MCP/Skill inventory, provider metadata, or ToolNames.
type srvPrincipalIntentClassifier struct {
	svc    *agentservice.Service
	client *http.Client
}

func (c srvPrincipalIntentClassifier) ClassifyDynamicIntent(ctx context.Context, p agentservice.Principal, userText string) (intent.ClassificationResult, error) {
	if c.svc == nil {
		return intent.ClassificationResult{}, fmt.Errorf("semantic intent classifier service is unavailable")
	}
	config, err := c.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		return intent.ClassificationResult{}, fmt.Errorf("load principal semantic classifier configuration: %w", err)
	}
	llmConfig, err := agentservice.ResolveLLMConfig(config.AppConfig)
	if err != nil {
		return intent.ClassificationResult{}, fmt.Errorf("resolve principal semantic classifier configuration: %w", err)
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: srvDynamicIntentClassificationTimeout}
	}
	tree := intent.BuildIntentTreeText(intent.DefaultDefinitions())
	messages := []interface{}{
		map[string]string{"role": "system", "content": intent.BuildTreePrompt(tree, userText)},
		map[string]string{"role": "user", "content": userText},
	}
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "maclawsrv-dynamic-semantic-intent"})
	response, err := agent.DoSimpleLLMRequestContextWithOptions(ctx, llmConfig, messages, client, srvDynamicIntentClassificationTimeout, agent.SimpleLLMRequestOptions{
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

// configureSrvDynamicSemanticRouting activates the reviewed
// information.lookup family (LabelSearch / LabelLiveData) and the host-owned
// information.current_time clock, knowledge.read.local store read,
// security.audit.read, information.fetch.web, fs.read.local, repo.inspect.vcs,
// document.read.local, fs.write.local, document.write.office (spreadsheet),
// shell.execute.local, agent.delegate.subtask, knowledge.ingest.local,
// memory.manage.agent, task.track.local, goal.manage.longrunning,
// template.manage.session, schedule.administer.local,
// knowledge.admin.maintenance, config.manage.self,
// session.manage.coding, and audio.transcribe.speech. GUI desktop families such as
// information.search.web and schedule.dispatch.channel live in the IM builtin
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
	resolver := &agentservice.PrincipalIntentLabelCapabilityNeedResolver{
		Classifier:        srvPrincipalIntentClassifier{svc: svc},
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
