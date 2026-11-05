package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (c *guiHopJITCaller) executeActFree(ctx context.Context, step hopjit.StepReady) (map[string]any, error) {
	if c.handler == nil {
		return nil, errors.New("act free requires the local GUI tool host")
	}
	if c.app == nil {
		return nil, errors.New("HopJIT LLM app is unavailable")
	}
	if len(step.Tools) == 0 {
		return nil, errors.New("act free requires an explicit tool whitelist")
	}
	provider, definitions, err := c.handler.hopJITActFreeToolProvider(step, c.identity)
	if err != nil {
		return nil, err
	}
	callbacks := &hopJITActFreeCallbacks{ctx: ctx, cfg: c.app.GetMaclawLLMConfig(), tools: definitions, provider: provider}
	result := agentruntime.RunAgentTurn(callbacks, hopJITActFreePrompt(step), nil, nil)
	if result.AskUser != nil || result.RecordAudio != nil {
		return nil, errors.New("interactive act free requires a HopJIT pause step")
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	var outputs map[string]any
	if err := json.Unmarshal([]byte(stripHopJITCodeFence(result.Text)), &outputs); err != nil {
		return nil, fmt.Errorf("act free returned invalid JSON: %w", err)
	}
	return outputs, nil
}

type hopJITActFreeCallbacks struct {
	ctx      context.Context
	cfg      corelib.MaclawLLMConfig
	tools    []map[string]interface{}
	provider *tool.HopJITPlanToolProvider
}

func (c *hopJITActFreeCallbacks) GetLLMConfig() corelib.MaclawLLMConfig { return c.cfg }
func (c *hopJITActFreeCallbacks) GetMaxIterations() int                 { return config.EffectiveMaxIterations(30) }
func (c *hopJITActFreeCallbacks) BuildSystemPrompt(string, bool) string {
	return "你在 HopJIT 的 act free 步骤中工作。只能使用当前列出的工具。完成任务后只返回符合声明的 JSON 输出，不要 Markdown 围栏。"
}
func (c *hopJITActFreeCallbacks) BuildTools(string) []map[string]interface{} { return c.tools }
func (c *hopJITActFreeCallbacks) ExecuteTool(name, argsJSON string) string {
	args, err := agentruntime.ParseToolArgumentsObject(argsJSON)
	if err != nil {
		return "Error: hopjit_tool_arguments_invalid: " + err.Error()
	}
	value, err := c.provider.Execute(c.ctx, name, args)
	if err != nil {
		return "Error: " + err.Error()
	}
	return fmt.Sprint(value)
}
func (c *hopJITActFreeCallbacks) OnToken(string)      {}
func (c *hopJITActFreeCallbacks) OnProgress(string)   {}
func (c *hopJITActFreeCallbacks) OnToolCall(string)   {}
func (c *hopJITActFreeCallbacks) OnToolResult(string) {}
func (c *hopJITActFreeCallbacks) ShouldStop() bool    { return c.ctx.Err() != nil }

func (h *IMMessageHandler) hopJITActFreeToolProvider(step hopjit.StepReady, identity hopJITExecutionIdentity) (*tool.HopJITPlanToolProvider, []map[string]interface{}, error) {
	if h == nil || h.registry == nil {
		return nil, nil, errors.New("HopJIT tool registry is unavailable")
	}
	identity.PrincipalID = strings.TrimSpace(identity.PrincipalID)
	identity.PolicyOwnerID = strings.TrimSpace(identity.PolicyOwnerID)
	identity.SessionID = strings.TrimSpace(identity.SessionID)
	if identity.PrincipalID == "" || identity.PolicyOwnerID == "" {
		return nil, nil, errors.New("HopJIT act free requires an authenticated execution owner")
	}
	if identity.SessionID == "" {
		identity.SessionID = strings.TrimSpace(step.RunID)
	}
	if identity.SessionID == "" {
		return nil, nil, errors.New("HopJIT act free requires a session identity")
	}
	rootTaskID := strings.TrimSpace(step.RunID)
	if rootTaskID == "" {
		rootTaskID = identity.SessionID
	}
	capabilityRegistry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(capabilityRegistry)
	providers := make([]tool.ProviderSpec, 0, len(step.Tools))
	needs := make([]tool.CapabilityNeed, 0, len(step.Tools))
	pins := make([]tool.ProviderPin, 0, len(step.Tools))
	definitions := make([]map[string]interface{}, 0, len(step.Tools))
	schemas := map[string]map[string]interface{}{}
	for _, name := range step.Tools {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable || len(registered.CapabilityProvisions) == 0 {
			return nil, nil, fmt.Errorf("HopJIT tool is unavailable or ungoverned: %s", name)
		}
		definition := registeredToolToDef(*registered)
		schema, err := semanticInvocationSchemaForRegisteredTool(*registered, definition)
		if err != nil {
			return nil, nil, err
		}
		authorization, err := tool.NewParameterAuthorization(schema)
		if err != nil {
			return nil, nil, err
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName: registered.Name,
			Binding: tool.ProviderBinding{
				Kind: semanticProviderKind(*registered), ProviderID: semanticProviderID(*registered), ImplementationID: registered.Name,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(definition)),
			},
			ParameterAuthorization: authorization,
			Provides:               registered.CapabilityProvisions, Consumes: registered.SemanticConsumes, Produces: registered.SemanticProduces,
			Effects: registered.SemanticEffects, Ready: true,
		})
		provision := registered.CapabilityProvisions[0]
		needs = append(needs, tool.CapabilityNeed{ID: "hopjit:" + name, Capability: provision.Capability, Qualifiers: provision.Qualifiers, Required: true})
		pins = append(pins, tool.ProviderPin{Capability: provision.Capability, Kind: semanticProviderKind(*registered), ProviderID: semanticProviderID(*registered), ImplementationID: registered.Name})
		definitions = append(definitions, definition)
		schemas[name] = schema
	}
	snapshot, err := catalog.Publish(providers, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	plan, err := tool.NewToolPlanner(capabilityRegistry).Plan(tool.RouteRequest{
		RootTaskID: rootTaskID, Snapshot: snapshot, Needs: needs, Pins: pins, Now: time.Now().UTC(),
	})
	if err != nil || len(plan.Unmet) > 0 {
		return nil, nil, fmt.Errorf("HopJIT act free plan failed: %v %+v", err, plan.Unmet)
	}
	issuer, err := h.semanticInvocationIssuer()
	if err != nil {
		return nil, nil, err
	}
	var executions tool.PlanExecutionStore = tool.NewMemoryPlanExecutionStore()
	if h.app != nil {
		coordinator, err := h.app.semanticExecutionCoordinatorForApp()
		if err != nil {
			return nil, nil, err
		}
		executions = coordinator.Executions
	}
	executor, err := tool.NewPlanExecutor(issuer, executions)
	if err != nil {
		return nil, nil, err
	}
	turnID := step.StepID
	if step.IterationID != "" {
		turnID += "@" + step.IterationID
	}
	scope := tool.InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: identity.SessionID, TurnID: turnID, PrincipalID: identity.PrincipalID}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil {
		return nil, nil, err
	}
	grantByName := map[string]tool.InvocationGrant{}
	for _, grant := range grants {
		grantByName[grant.AdapterName] = grant
	}
	var resultStore tool.HopJITResultStore = h.hopJITResults
	if resultStore == nil && h.app != nil {
		resultStore = newFileHopJITResultStore(hopJITResultRoot(h.app.GetDataDir(), identity))
	}
	provider := &tool.HopJITPlanToolProvider{
		Executor: executor, Scope: scope, Plan: plan, Grants: grantByName, Schemas: schemas,
		ResultStore: resultStore, AllowResultRecovery: step.Recovered,
		Invoke: func(ctx context.Context, selection tool.PlannedSelection, request tool.CanonicalRequest) tool.SelectionExecutionResult {
			ctx = withTrustedAuditPrincipal(ctx, identity.PrincipalID)
			result := h.executeToolDetailedWithRuntimeContext(ctx, identity.PolicyOwnerID, true, identity.Platform, selection.AdapterName, string(request.CanonicalJSON), step.Summary, nil)
			switch result.Outcome {
			case toolOutcomeSucceeded:
				return tool.SelectionExecutionResult{Result: result.Text, Succeeded: true}
			case toolOutcomeUncertain:
				if result.FailureKind == toolFailureApprovalRequired || isRegisteredToolApprovalWaitText(result.Text) {
					return tool.SelectionExecutionResult{Result: result.Text, Paused: true, ReasonCode: "hopjit_tool_approval_required"}
				}
				return tool.SelectionExecutionResult{Result: result.Text, Unknown: true, ReasonCode: "hopjit_tool_outcome_unknown"}
			default:
				return tool.SelectionExecutionResult{Result: result.Text, ReasonCode: "hopjit_tool_failed"}
			}
		},
	}
	return provider, definitions, nil
}

func hopJITActFreePrompt(step hopjit.StepReady) string {
	return fmt.Sprintf("步骤: %s\n%s", step.Summary, step.Context.Render())
}
