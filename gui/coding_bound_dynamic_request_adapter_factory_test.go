package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type testCodingBoundDynamicRequestChannel struct {
	execution      agent.ToolCallExecutionContext
	do             func(context.Context, []interface{}, []map[string]interface{}, llm.TokenCallback, bool) (*llm.Response, error)
	close          func(error)
	prepared       func()
	mutateReceipt  func(*agent.ToolSurfaceReceipt)
	prepareErr     error
	prepareStarted chan struct{}
	allowPrepare   <-chan struct{}
	auditEvidence  agent.ToolSurfacePlanEvidence
	policy         agent.ToolSurfaceInvocationPolicy
	auditSet       bool
	policySet      bool
}

// testCodingBoundDynamicRequestChannelWithoutPolicy deliberately exposes every
// legacy qualified-channel seam except the final-frame policy handoff. It
// proves a future production factory cannot certify such a transport merely
// because it has correlation, receipt and audit evidence support.
type testCodingBoundDynamicRequestChannelWithoutPolicy struct {
	execution agent.ToolCallExecutionContext
}

func (c *testCodingBoundDynamicRequestChannelWithoutPolicy) ExecutionContext() agent.ToolCallExecutionContext {
	return c.execution
}

func (*testCodingBoundDynamicRequestChannelWithoutPolicy) Do(context.Context, []interface{}, []map[string]interface{}, llm.TokenCallback, bool) (*llm.Response, error) {
	return nil, fmt.Errorf("test channel does not send")
}

func (c *testCodingBoundDynamicRequestChannelWithoutPolicy) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (agent.VerifiedToolSurfaceDispatch, error) {
	response, err := c.Do(ctx, conversation, tools, onToken, stream)
	return agent.VerifiedToolSurfaceDispatch{Response: response}, err
}

func (*testCodingBoundDynamicRequestChannelWithoutPolicy) Close(error) {}

func (c *testCodingBoundDynamicRequestChannel) ExecutionContext() agent.ToolCallExecutionContext {
	return c.execution
}

func (c *testCodingBoundDynamicRequestChannel) Do(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (*llm.Response, error) {
	if c != nil && c.do != nil {
		return c.do(ctx, conversation, tools, onToken, stream)
	}
	return nil, fmt.Errorf("test channel does not send")
}

func (c *testCodingBoundDynamicRequestChannel) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (agent.VerifiedToolSurfaceDispatch, error) {
	if !c.policySet {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("surface_integrity_failure: test channel invocation policy was not set")
	}
	receipt, receiptErr := agent.VerifyToolSurfaceWirePayloadWithAuditEvidence(tools, tools, c.policy, c.auditEvidence)
	if c.mutateReceipt != nil {
		c.mutateReceipt(&receipt)
	}
	if receiptErr != nil {
		return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, receiptErr
	}
	response, err := c.Do(ctx, conversation, tools, onToken, stream)
	if err != nil {
		receipt.Handoff = agent.ToolSurfaceHandoffAmbiguous
		return agent.VerifiedToolSurfaceDispatch{Response: response, Receipt: receipt}, err
	}
	receipt.Handoff = agent.ToolSurfaceHandoffStarted
	return agent.VerifiedToolSurfaceDispatch{Response: response, Receipt: receipt}, nil
}

func (c *testCodingBoundDynamicRequestChannel) SetToolSurfaceDispatchPreparation(preparation agent.ToolSurfaceDispatchPreparation) error {
	evidence, err := agent.NormalizeToolSurfacePlanEvidence(preparation.AuditEvidence)
	if err != nil {
		return err
	}
	policy, err := agent.NormalizeToolSurfaceInvocationPolicy(preparation.InvocationPolicy)
	if err != nil {
		return err
	}
	if c.prepareStarted != nil {
		close(c.prepareStarted)
	}
	if c.allowPrepare != nil {
		<-c.allowPrepare
	}
	if c.prepareErr != nil {
		return c.prepareErr
	}
	if c.auditSet && !sameToolSurfacePlanEvidence(c.auditEvidence, evidence) {
		return fmt.Errorf("test channel dispatch preparation changed")
	}
	if c.policySet && c.policy != policy {
		return fmt.Errorf("test channel dispatch preparation changed")
	}
	c.auditEvidence, c.auditSet = evidence, true
	c.policy, c.policySet = policy, true
	if c.prepared != nil {
		c.prepared()
	}
	return nil
}

func (c *testCodingBoundDynamicRequestChannel) Close(cause error) {
	if c != nil && c.close != nil {
		c.close(cause)
	}
}

// testCodingReservationLedger is deliberately test-only evidence for D2. It
// keys each event with the complete tuple RunLoop already owns; it never uses
// task text, runtime IDs, configuration labels, or a tool name as correlation.
// It is not an authorization/journal implementation and cannot be reached by
// a production callback.
type testCodingReservationLedger struct {
	mu     sync.Mutex
	events map[string][]string
}

// These two wrappers exist only in this test file. They retain the actual
// callback's complete method set while replacing only the request epoch source
// for an explicitly injected, qualification-disabled test holder. Production
// callbacks never construct either type and continue to fail closed.
type testLocalDynamicLifecycleLoopHost struct {
	*codingSubAgentCallbacks
	ledger           *testCodingReservationLedger
	control          *testCodingDynamicRunLoopControl
	receiptExecution agent.ToolCallExecutionContext
}

type testCodingDynamicRunLoopControl struct {
	executions int
	stop       bool
	toolResult string
}

func (*testLocalDynamicLifecycleLoopHost) BeginToolSurfaceEpoch(int) string {
	return "coding-dynamic-test:" + codingDynamicSurfaceNonce()
}

func (*testLocalDynamicLifecycleLoopHost) GetMaxIterations() int { return 1 }

func (*testLocalDynamicLifecycleLoopHost) BuildSystemPrompt(string, bool) string {
	return "test system"
}

func (h *testLocalDynamicLifecycleLoopHost) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	if h != nil && h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	definitions := h.codingSubAgentCallbacks.BuildToolsForBoundModelRequest(userText, iteration, execution)
	if h != nil && h.ledger != nil && len(definitions) != 0 {
		h.ledger.record("prepared", execution)
	}
	if h != nil {
		h.receiptExecution = execution
	}
	return definitions
}

func (h *testLocalDynamicLifecycleLoopHost) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if h != nil && h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	rendered := h.codingSubAgentCallbacks.RenderPublishedBoundToolSurface(userText, iteration, execution)
	if h != nil && h.ledger != nil && rendered.Published {
		h.ledger.record("prepared", execution)
	}
	if h != nil {
		h.receiptExecution = execution
	}
	return rendered
}

func (h *testLocalDynamicLifecycleLoopHost) OnToolSurfaceReceipt(receipt agent.ToolSurfaceReceipt) {
	if h == nil || h.ledger == nil {
		return
	}
	event := "receipt:"
	if receipt.Verified {
		event += receipt.PayloadDigest + ":" + receipt.WirePayloadHash + ":" + receipt.AuditDigest
	} else {
		event += "failure:" + receipt.Failure
	}
	h.ledger.record(event, h.receiptExecution)
}

func (h *testLocalDynamicLifecycleLoopHost) ToolSurfaceExecutionContext(_ int, epoch string) agent.ToolCallExecutionContext {
	return agent.ToolCallExecutionContext{SurfaceEpoch: epoch}
}

func (*testLocalDynamicLifecycleLoopHost) IsToolAllowed(string) bool { return true }

func (h *testLocalDynamicLifecycleLoopHost) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if h != nil && h.control != nil {
		h.control.executions++
		if h.control.toolResult != "" {
			return agent.ToolExecutionResult{Result: h.control.toolResult, Outcome: agent.ToolExecutionOutcomeOK}
		}
	}
	return h.codingSubAgentCallbacks.ExecuteToolCallWithContext(name, argsJSON, callID, execution)
}

func (h *testLocalDynamicLifecycleLoopHost) ShouldStop() bool {
	return h != nil && h.control != nil && h.control.stop
}

func (h *testLocalDynamicLifecycleLoopHost) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	err := h.codingSubAgentCallbacks.BindToolSurfaceResponse(execution)
	if err == nil && h != nil && h.ledger != nil {
		h.ledger.record("bound:"+execution.ResponseID, execution)
	}
	return err
}

func (h *testLocalDynamicLifecycleLoopHost) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if h != nil && h.ledger != nil {
		h.ledger.record("terminal:"+string(disposition), execution)
	}
	h.codingSubAgentCallbacks.OnToolSurfaceDisposition(execution, disposition)
}

type testRemoteDynamicLifecycleLoopHost struct {
	*remoteCodingCallbacks
	ledger           *testCodingReservationLedger
	control          *testCodingDynamicRunLoopControl
	receiptExecution agent.ToolCallExecutionContext
}

func (*testRemoteDynamicLifecycleLoopHost) BeginToolSurfaceEpoch(int) string {
	return "remote-coding-dynamic-test:" + codingDynamicSurfaceNonce()
}

func (*testRemoteDynamicLifecycleLoopHost) GetMaxIterations() int { return 1 }

func (*testRemoteDynamicLifecycleLoopHost) BuildSystemPrompt(string, bool) string {
	return "test system"
}

func (h *testRemoteDynamicLifecycleLoopHost) BuildToolsForBoundModelRequest(userText string, iteration int, execution agent.ToolCallExecutionContext) []map[string]interface{} {
	if h != nil && h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	definitions := h.remoteCodingCallbacks.BuildToolsForBoundModelRequest(userText, iteration, execution)
	if h != nil && h.ledger != nil && len(definitions) != 0 {
		h.ledger.record("prepared", execution)
	}
	if h != nil {
		h.receiptExecution = execution
	}
	return definitions
}

func (h *testRemoteDynamicLifecycleLoopHost) RenderPublishedBoundToolSurface(userText string, iteration int, execution agent.ToolCallExecutionContext) agent.BoundToolSurfaceRender {
	if h != nil && h.ledger != nil {
		h.ledger.record("reserved", execution)
	}
	rendered := h.remoteCodingCallbacks.RenderPublishedBoundToolSurface(userText, iteration, execution)
	if h != nil && h.ledger != nil && rendered.Published {
		h.ledger.record("prepared", execution)
	}
	if h != nil {
		h.receiptExecution = execution
	}
	return rendered
}

func (h *testRemoteDynamicLifecycleLoopHost) OnToolSurfaceReceipt(receipt agent.ToolSurfaceReceipt) {
	if h == nil || h.ledger == nil {
		return
	}
	event := "receipt:"
	if receipt.Verified {
		event += receipt.PayloadDigest + ":" + receipt.WirePayloadHash + ":" + receipt.AuditDigest
	} else {
		event += "failure:" + receipt.Failure
	}
	h.ledger.record(event, h.receiptExecution)
}

func (h *testRemoteDynamicLifecycleLoopHost) ToolSurfaceExecutionContext(_ int, epoch string) agent.ToolCallExecutionContext {
	return agent.ToolCallExecutionContext{SurfaceEpoch: epoch}
}

func (*testRemoteDynamicLifecycleLoopHost) IsToolAllowed(string) bool { return true }

func (h *testRemoteDynamicLifecycleLoopHost) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if h != nil && h.control != nil {
		h.control.executions++
		if h.control.toolResult != "" {
			return agent.ToolExecutionResult{Result: h.control.toolResult, Outcome: agent.ToolExecutionOutcomeOK}
		}
	}
	return h.remoteCodingCallbacks.ExecuteToolCallWithContext(name, argsJSON, callID, execution)
}

func (h *testRemoteDynamicLifecycleLoopHost) ShouldStop() bool {
	return h != nil && h.control != nil && h.control.stop
}

func (h *testRemoteDynamicLifecycleLoopHost) BindToolSurfaceResponse(execution agent.ToolCallExecutionContext) error {
	err := h.remoteCodingCallbacks.BindToolSurfaceResponse(execution)
	if err == nil && h != nil && h.ledger != nil {
		h.ledger.record("bound:"+execution.ResponseID, execution)
	}
	return err
}

func (h *testRemoteDynamicLifecycleLoopHost) OnToolSurfaceDisposition(execution agent.ToolCallExecutionContext, disposition agent.ToolSurfaceDisposition) {
	if h != nil && h.ledger != nil {
		h.ledger.record("terminal:"+string(disposition), execution)
	}
	h.remoteCodingCallbacks.OnToolSurfaceDisposition(execution, disposition)
}

// testCodingDynamicLifecycleBatchHooks keeps the real Coding transform/replan
// behavior while adding only the optional durability callbacks that D2c needs
// to exercise through agent.RunLoop. It is test-only: production Coding still
// has no qualified dynamic lifecycle owner and no dynamic materialization.
type testCodingDynamicLifecycleBatchHooks struct {
	failStart   bool
	failCommit  bool
	afterCommit func()
	started     int
	committed   int
	abandoned   int
}

func (*testCodingDynamicLifecycleBatchHooks) OnToolExecuted(string, string, string, bool) {}
func (*testCodingDynamicLifecycleBatchHooks) OnEmptyResponse(int) bool                    { return false }
func (*testCodingDynamicLifecycleBatchHooks) TransformConversation([]interface{}) []interface{} {
	return nil
}

func (h *testCodingDynamicLifecycleBatchHooks) OnToolBatchStarting(delta []agent.ConversationEntry, meta agent.ToolBatchMetadata) error {
	if len(delta) != 1 || delta[0].Role != "assistant" || delta[0].ToolCalls == nil {
		return fmt.Errorf("test batch starter received incomplete declaration")
	}
	h.started++
	if h.failStart {
		return fmt.Errorf("test batch starter failed")
	}
	return nil
}

func (h *testCodingDynamicLifecycleBatchHooks) OnToolBatchCommitted(delta []agent.ConversationEntry, meta agent.ToolBatchMetadata) error {
	if len(delta) != 2 || delta[0].Role != "assistant" || delta[1].Role != "tool" {
		return fmt.Errorf("test batch committer received incomplete paired batch")
	}
	h.committed++
	if h.failCommit {
		return fmt.Errorf("test batch committer failed")
	}
	if h.afterCommit != nil {
		h.afterCommit()
	}
	return nil
}

func (h *testCodingDynamicLifecycleBatchHooks) OnToolBatchAbandoned(agent.ToolBatchMetadata) {
	h.abandoned++
}

func newTestCodingReservationLedger() *testCodingReservationLedger {
	return &testCodingReservationLedger{events: make(map[string][]string)}
}

func testCodingReservationKey(execution agent.ToolCallExecutionContext) string {
	if execution.Protocol == "" || execution.ConnectionID == "" || execution.SurfaceEpoch == "" {
		return ""
	}
	return execution.Protocol + "\x00" + execution.ConnectionID + "\x00" + execution.SurfaceEpoch
}

func (l *testCodingReservationLedger) record(event string, execution agent.ToolCallExecutionContext) {
	if l == nil {
		return
	}
	key := testCodingReservationKey(execution)
	if key == "" {
		return
	}
	l.mu.Lock()
	l.events[key] = append(l.events[key], event)
	l.mu.Unlock()
}

func (l *testCodingReservationLedger) eventsFor(t *testing.T, execution agent.ToolCallExecutionContext) []string {
	t.Helper()
	key := testCodingReservationKey(execution)
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events[key]...)
}

func TestReserveCodingBoundDynamicRequestAdapterFailsClosedUntilQualificationEnabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	handler := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	adapter, err := reserveCodingBoundDynamicRequestAdapter(context.Background(), handler, identity, corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if err != nil || adapter != nil {
		t.Fatalf("disabled production qualification reserved adapter=%#v err=%v", adapter, err)
	}
}

func TestValidateCodingDynamicQualifiedRequestChannelBindsCertificateToLiveTransport(t *testing.T) {
	qualification := testEligibleCodingDynamicResponsesWSQualification()
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: qualification.Capability.Protocol, ConnectionID: "live-connection"},
	}
	if err := validateCodingDynamicQualifiedRequestChannel(qualification, corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}, channel); err != nil {
		t.Fatalf("matching qualified channel rejected: %v", err)
	}
	for name, mutate := range map[string]func(*codingDynamicProductionAdapterQualification, *testCodingBoundDynamicRequestChannel, *corelib.MaclawLLMConfig){
		"wrong live protocol": func(_ *codingDynamicProductionAdapterQualification, c *testCodingBoundDynamicRequestChannel, _ *corelib.MaclawLLMConfig) {
			c.execution.Protocol = "other-protocol"
		},
		"wrong certificate envelope": func(q *codingDynamicProductionAdapterQualification, _ *testCodingBoundDynamicRequestChannel, _ *corelib.MaclawLLMConfig) {
			q.ReplacementSemantics.Envelope = agent.ToolSurfaceEnvelopeOpenAIChat
		},
		"wrong configured envelope": func(_ *codingDynamicProductionAdapterQualification, _ *testCodingBoundDynamicRequestChannel, cfg *corelib.MaclawLLMConfig) {
			cfg.WireAPI = "responses"
		},
		"missing live connection": func(_ *codingDynamicProductionAdapterQualification, c *testCodingBoundDynamicRequestChannel, _ *corelib.MaclawLLMConfig) {
			c.execution.ConnectionID = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := qualification
			certificate := *qualification.ReplacementSemantics
			candidate.ReplacementSemantics = &certificate
			candidateChannel := &testCodingBoundDynamicRequestChannel{execution: channel.execution}
			cfg := corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}
			mutate(&candidate, candidateChannel, &cfg)
			if err := validateCodingDynamicQualifiedRequestChannel(candidate, cfg, candidateChannel); err == nil {
				t.Fatalf("mismatched certificate/channel was accepted: qualification=%#v execution=%+v cfg=%#v", candidate, candidateChannel.execution, cfg)
			}
		})
	}
}

func TestValidateCodingDynamicQualifiedRequestChannelRequiresInvocationPolicyHandoff(t *testing.T) {
	qualification := testEligibleCodingDynamicResponsesWSQualification()
	channel := &testCodingBoundDynamicRequestChannelWithoutPolicy{execution: agent.ToolCallExecutionContext{Protocol: qualification.Capability.Protocol, ConnectionID: "live-connection"}}
	if err := validateCodingDynamicQualifiedRequestChannel(qualification, corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}, channel); err == nil || !strings.Contains(err.Error(), "dispatch preparation") {
		t.Fatalf("channel without invocation-policy handoff accepted: %v", err)
	}
}

func testEligibleCodingDynamicResponsesWSQualification() codingDynamicProductionAdapterQualification {
	capability := codingDynamicProviderCorrelationCapability{
		AdapterKey: "reviewed-responses-ws", Protocol: "openai-responses-ws",
		HasTransportConnectionID: true, HasProviderResponseID: true, HasProviderToolCallID: true,
		HasCancellationFence: true, HasReplayIdentitySemantics: true,
	}
	return codingDynamicProductionAdapterQualification{
		Capability: capability, AdapterVersion: "test-adapter-v1", VerifiedIngress: "test-ingress",
		LifecycleDispositionVersion: "test-disposition-v1", CatalogReceiptPolicyCovered: true,
		ReceiptDispatchVersion: "test-dispatch-v1", ReplacementSemanticsVersion: "test-responses-replace-v1",
		ReplacementSemantics: &codingDynamicReplacementSemanticsCertificate{
			Version: "test-responses-replace-v1", Protocol: capability.Protocol, Envelope: agent.ToolSurfaceEnvelopeResponses,
			ExplicitEmptySurfaceVerified: true, RejectsToolBearingRedirects: true, PolicyProjectionVersion: "test-responses-policy-v1",
			AppendContractTested: true, RetainContractTested: true,
		},
		FixedCohort: "test-fixed-cohort", KillSwitchInstalled: true, Wired: true, Enabled: true,
	}
}

func TestQualifiedCodingBoundDynamicRequestLifecycleRelayStaysAbsentForCurrentCapability(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	handler := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	if relay := newQualifiedCodingBoundDynamicRequestLifecycleRelay(handler, identity, corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}); relay != nil {
		t.Fatalf("unqualified config created lifecycle relay: %#v", relay)
	}
}

func TestCodingCallbacksDoNotAttachDynamicLifecycleRelayWhileQualificationDisabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	handler := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	cfg := corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}
	local := newCodingSubAgentCallbacks(&CodingSubAgent{handler: handler, cfg: cfg, dynamicInvocationIdentity: identity}, &TaskItem{Title: "test"}, "", "", nil)
	if local.dynamicLifecycleRelay != nil {
		t.Fatalf("local callback attached unqualified relay: %#v", local.dynamicLifecycleRelay)
	}
	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: handler, cfg: cfg, dynamicInvocationIdentity: identity}}
	remote.tryAttachQualifiedDynamicLifecycleRelay()
	if remote.dynamicLifecycleRelay != nil {
		t.Fatalf("remote callback attached unqualified relay: %#v", remote.dynamicLifecycleRelay)
	}
}

func TestCodingCallbackDynamicCompositionIsAllOrNothing(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "composition-connection"}}
	newRelay := func() (*codingBoundDynamicRequestLifecycleRelay, *codingBoundDynamicRequestAdapter) {
		var adapter *codingBoundDynamicRequestAdapter
		relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
			var err error
			adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
			return adapter, err
		})
		return relay, adapter
	}

	localRelay, _ := newRelay()
	local := &codingSubAgentCallbacks{dynamicLifecycleRelay: localRelay}
	if err := requireCodingDynamicCallbackComposition(local); err != nil {
		t.Fatalf("local composition missing an interface: %v", err)
	}
	reserved, err := local.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{})
	if err != nil || reserved == nil || localRelay.active == nil || reserved != localRelay.active {
		t.Fatalf("local reserve did not retain one active holder: reserved=%T active=%T err=%v", reserved, localRelay.active, err)
	}
	execution := localRelay.active.ExecutionContext()
	execution.SurfaceEpoch = "local-composition-epoch"
	definitions := local.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) != 1 || localRelay.active.surface == nil {
		t.Fatalf("local composition did not use active relay surface: %#v", definitions)
	}
	if err := local.BindToolSurfaceResponse(execution); err == nil {
		t.Fatal("local composition accepted missing provider response ID")
	}
	local.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
	if localRelay.active != nil {
		t.Fatal("local disposition did not clear the same relay that reserved the channel")
	}
	if got := local.ExecuteToolCallWithContext("read_file", `{}`, "call-late", execution); got.Outcome != agent.ToolExecutionOutcomeError || got.Result != "[system rejected] stale_surface" {
		t.Fatalf("local composition fell back to static dispatcher: %#v", got)
	}

	// Use an independent durable fixture: the preceding bind failure correctly
	// retired its route, so reusing its identical plan identity here would test
	// route supersession rather than remote callback composition.
	remoteHandler, remoteIdentity, remotePrepared, remoteDynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	remoteChannel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "remote-composition-connection"}}
	remoteRelay := newCodingBoundDynamicRequestLifecycleRelay(remoteHandler, remoteIdentity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		return newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, remotePrepared, remoteDynamic, remoteChannel)
	})
	remote := &remoteCodingCallbacks{dynamicLifecycleRelay: remoteRelay}
	if err := requireCodingDynamicCallbackComposition(remote); err != nil {
		t.Fatalf("remote composition missing an interface: %v", err)
	}
	reserved, err = remote.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{})
	if err != nil || reserved == nil || remoteRelay.active == nil || reserved != remoteRelay.active {
		t.Fatalf("remote reserve did not retain one active holder: reserved=%T active=%T err=%v", reserved, remoteRelay.active, err)
	}
	execution = remoteRelay.active.ExecutionContext()
	execution.SurfaceEpoch = "remote-composition-epoch"
	definitions = remote.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) != 1 || remoteRelay.active.surface == nil {
		t.Fatalf("remote composition did not use active relay surface: %#v", definitions)
	}
	if err := remote.BindToolSurfaceResponse(execution); err == nil {
		t.Fatal("remote composition accepted missing provider response ID")
	}
	remote.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
	if remoteRelay.active != nil {
		t.Fatal("remote disposition did not clear the same relay that reserved the channel")
	}
	if got := remote.ExecuteToolCallWithContext("ssh_read_file", `{}`, "call-late", execution); got.Outcome != agent.ToolExecutionOutcomeError || got.Result != "[system rejected] stale_surface" {
		t.Fatalf("remote composition fell back to static dispatcher: %#v", got)
	}
}

func TestCodingDynamicLifecycleOwnerClosesActiveRelayForHostTerminalFacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		install func() (context.Context, func(), *LoopContext)
		close   func(context.CancelFunc, *LoopContext)
	}{
		{
			name: "loop context cancellation",
			install: func() (context.Context, func(), *LoopContext) {
				loop := NewLoopContext("dynamic-lifecycle-owner", 0, nil)
				return nil, func() {}, loop
			},
			close: func(_ context.CancelFunc, loop *LoopContext) { loop.Cancel() },
		},
		{
			name: "detached runtime cancellation",
			install: func() (context.Context, func(), *LoopContext) {
				ctx, cancel := context.WithCancel(context.Background())
				return ctx, cancel, nil
			},
			close: func(cancel context.CancelFunc, _ *LoopContext) { cancel() },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "owner-" + tc.name}}
			var adapter *codingBoundDynamicRequestAdapter
			relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				var err error
				adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				return adapter, err
			})
			if relay == nil {
				t.Fatal("test relay was not created")
			}
			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatal(err)
			}
			execution := adapter.ExecutionContext()
			execution.SurfaceEpoch = "owner-epoch"
			if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
				t.Fatalf("holder did not publish: %#v", definitions)
			}
			execution.ResponseID = "owner-response"
			if err := relay.BindToolSurfaceResponse(execution); err != nil {
				t.Fatal(err)
			}

			ownedCtx, cancel, loop := tc.install()
			var owner codingDynamicLifecycleOwner
			owner.install(relay, ownedCtx, loop)
			tc.close(cancel, loop)
			waitForCodingDynamicLifecycleTerminal(t, adapter, relay)
			if got := adapter.ExecuteToolCallWithContext("unknown", `{}`, "late-call", execution); got.Outcome != agent.ToolExecutionOutcomeError || got.Result != "[system rejected] stale_surface" {
				t.Fatalf("terminal owner allowed a late execution: %#v", got)
			}
			owner.clear(relay, codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

func TestCodingDynamicLifecycleOwnerDoesNotCloseReplacementRelay(t *testing.T) {
	first := &codingBoundDynamicRequestLifecycleRelay{}
	second := &codingBoundDynamicRequestLifecycleRelay{}
	var owner codingDynamicLifecycleOwner
	owner.install(first, nil, nil)
	owner.install(second, nil, nil)
	// clear is exact: an old callback return must not tear down a successor
	// relay installed for a later verified execution.
	owner.clear(first, codingBoundDynamicRequestNestedExit)
	owner.mu.Lock()
	got := owner.relay
	owner.mu.Unlock()
	if got != second {
		t.Fatalf("old callback closed replacement relay: got=%p want=%p", got, second)
	}
	owner.clear(second, codingBoundDynamicRequestRuntimeClosed)
}

func TestNestedHandoffRetiresOnlyTheParentBoundReservation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(*CodingSubAgent, *RemoteCodingSubAgent)
	}{
		{
			name: "local",
			close: func(local *CodingSubAgent, _ *RemoteCodingSubAgent) {
				local.closeCodingSubAgentDynamicLifecycle(codingBoundDynamicRequestNestedExit)
			},
		},
		{
			name: "remote",
			close: func(_ *CodingSubAgent, remote *RemoteCodingSubAgent) {
				remote.closeCodingSubAgentDynamicLifecycle(codingBoundDynamicRequestNestedExit)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "nested-" + tc.name}}
			var adapter *codingBoundDynamicRequestAdapter
			relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				var err error
				adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				return adapter, err
			})
			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatal(err)
			}
			execution := adapter.ExecutionContext()
			execution.SurfaceEpoch = "nested-" + tc.name + "-epoch"
			if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
				t.Fatalf("bound surface definitions=%#v", definitions)
			}
			execution.ResponseID = "nested-" + tc.name + "-response"
			if err := relay.BindToolSurfaceResponse(execution); err != nil {
				t.Fatal(err)
			}
			var alias string
			for name := range adapter.surface.aliases {
				alias = name
				break
			}
			local := &CodingSubAgent{}
			remote := &RemoteCodingSubAgent{}
			if tc.name == "local" {
				local.dynamicLifecycleOwner.install(relay, nil, nil)
			} else {
				remote.dynamicLifecycleOwner.install(relay, nil, nil)
			}
			tc.close(local, remote)

			if !adapter.terminal || relay.active != nil || adapter.executionCtx.Err() == nil {
				t.Fatalf("nested handoff left parent reservation live: adapter=%#v active=%#v", adapter, relay.active)
			}
			if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "late-nested-call", execution); got.Result != "[system rejected] stale_surface" {
				t.Fatalf("nested handoff left an executable alias: %#v", got)
			}
			if _, _, err := adapter.surface.ResolveAlias(execution.ResponseID, alias); err == nil {
				t.Fatal("nested handoff left the parent alias resolvable")
			}
		})
	}
}

func TestRunNestedCodingAgentRetiresParentBoundReservationBeforeChildValidation(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "nested-run-parent"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "nested-run-parent-epoch"
	if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
		t.Fatalf("bound surface definitions=%#v", definitions)
	}
	execution.ResponseID = "nested-run-parent-response"
	if err := relay.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	parent := &CodingSubAgent{fullEnvironment: true, projectPath: t.TempDir()}
	parent.dynamicLifecycleOwner.install(relay, nil, nil)
	// The worker fails immediately because it lacks an isolated worktree. The
	// assertion is intentionally about ordering: the parent reservation must be
	// retired before any child validation/creation path returns.
	result := parent.runNestedCodingAgent(codingSpawnSpec{Role: codingRoleWorker, Task: "child work"}, nil, nil)
	if result == nil || result.Status != TaskExecFailed || !strings.Contains(result.Error, "isolated workspace") {
		t.Fatalf("nested child validation result=%#v", result)
	}
	if !adapter.terminal || relay.active != nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("nested child validation left parent reservation live: adapter=%#v active=%#v", adapter, relay.active)
	}
}

func TestVerifiedIngressRuntimeCancellationRetiresTheSameDynamicReservation(t *testing.T) {
	// This is the D2 pre-production end-to-end conformance path: an opaque
	// verified handle is bound to the runtime attempt first; only then is the
	// callback composed with a test relay. The production qualification remains
	// disabled, so the relay is injected here rather than made config-selectable.
	handler, _, _, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	relations, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relations.Close() })
	subject, err := newVerifiedCodingSubject("desktop-test", "principal-test", "session-test")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := relations.CreateCodingTask(subject, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := codingruntime.NewMemoryStore()
	loop := NewLoopContext("verified-d2-loop", 1, nil)
	ctx, cancel := loop.Context()
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var (
		adapter *codingBoundDynamicRequestAdapter
		relay   *codingBoundDynamicRequestLifecycleRelay
		result  *CodingSubAgentResult
		attempt *codingruntime.Attempt
		runErr  error
	)
	go func() {
		result, attempt, runErr = runGUICodingTaskWithLedgerWithStart(ctx, store, "gui:verified-d2", "workflow", "phase", "D:/verified", "read", nil, func(request codingruntime.ExecutionRequest) {
			identity, ok := bindVerifiedCodingTaskHandle(relations, subject, &handle, store, request)
			if !ok || identity == nil {
				t.Errorf("verified ingress did not bind runtime identity")
				return
			}
			subagent := &CodingSubAgent{handler: handler, loopCtx: loop, runtimeStore: store, runtimeAttempt: &request.Attempt, dynamicInvocationIdentity: identity}
			prepared, prepErr := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Now().UTC())
			if prepErr != nil {
				t.Errorf("prepare verified plan: %v", prepErr)
				return
			}
			channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "verified-ingress-connection"}}
			relay = newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				var adapterErr error
				adapter, adapterErr = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				return adapter, adapterErr
			})
			if relay == nil {
				t.Errorf("failed to create test relay")
				return
			}
			cb := newCodingSubAgentCallbacks(subagent, &TaskItem{Title: "verified ingress"}, "", "", nil)
			cb.dynamicLifecycleRelay = relay
			cb.registerDynamicLifecycleOwner()
			defer cb.closeDynamicLifecycleOwner(codingBoundDynamicRequestRuntimeClosed)
			if _, reserveErr := cb.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); reserveErr != nil {
				t.Errorf("reserve dynamic channel: %v", reserveErr)
				return
			}
			execution := adapter.ExecutionContext()
			execution.SurfaceEpoch = "verified-ingress-epoch"
			if definitions := cb.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
				t.Errorf("publish verified surface: %#v", definitions)
				return
			}
			execution.ResponseID = "verified-ingress-response"
			if bindErr := cb.BindToolSurfaceResponse(execution); bindErr != nil {
				t.Errorf("bind verified surface: %v", bindErr)
				return
			}
			loop.RegisterCancelHook(func() { _, _ = store.CancelTask(request.Task.TaskID, time.Now().UTC()) })
			close(started)
			<-release
			return
		}, func() *CodingSubAgentResult {
			// The actual callback work lives in onStart so it can only use the
			// verified identity emitted by the runner for this attempt.
			return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "unexpected direct executor"}
		})
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("verified runtime did not prepare a bound reservation")
	}
	if adapter == nil || relay == nil || adapter.surface == nil || adapter.identity == nil || adapter.identity.RootTaskID != handle.rootTaskID || adapter.identity.TurnID != handle.turnID {
		t.Fatalf("verified ingress did not reach bound adapter: adapter=%#v handle=%#v", adapter, handle)
	}
	loop.Cancel()
	waitForCodingDynamicLifecycleTerminal(t, adapter, relay)
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not discard late result after cancellation")
	}
	if runErr != nil || result == nil || result.Status != TaskExecInterrupted || attempt == nil || attempt.Status != codingruntime.TaskCancelled {
		t.Fatalf("runtime cancellation result=%#v attempt=%#v err=%v", result, attempt, runErr)
	}
}

func waitForCodingDynamicLifecycleTerminal(t *testing.T, adapter *codingBoundDynamicRequestAdapter, relay *codingBoundDynamicRequestLifecycleRelay) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		adapter.mu.Lock()
		terminal := adapter.terminal
		adapter.mu.Unlock()
		relay.mu.Lock()
		active := relay.active
		relay.mu.Unlock()
		if terminal && active == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("host terminal did not close dynamic relay: terminal=%v active=%#v", adapter.terminal, relay.active)
}

func TestCodingBoundDynamicRequestLifecycleRelayRejectsFactoryWithoutLiveChannel(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	factoryCalls := 0
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		factoryCalls++
		if gotHandler != handler || gotIdentity == nil || *gotIdentity != *identity {
			t.Fatal("relay passed incorrect trusted adapter inputs")
		}
		return newCodingBoundDynamicRequestAdapter(gotHandler, gotIdentity, prepared, dynamic)
	})
	if relay == nil {
		t.Fatal("test relay was not created")
	}
	channel, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{})
	if err == nil || channel != nil || factoryCalls != 1 {
		t.Fatalf("reserve channel=%#v err=%v factoryCalls=%d", channel, err, factoryCalls)
	}
	// The fixture adapter has no transport-backed channel, so it cannot become a
	// request channel. This deliberately proves the relay refuses to fabricate a
	// tuple rather than letting the test use config-derived correlation.
	if relay.active != nil {
		t.Fatal("relay retained a holder without a live channel")
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayDispositionRetiresOnlyMatchingLiveHolder(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "relay-connection"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if relay == nil {
		t.Fatal("test relay was not created")
	}
	reserved, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{})
	if err != nil || reserved != adapter {
		t.Fatalf("reserve channel=%#v adapter=%#v err=%v", reserved, adapter, err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "relay-epoch"
	if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("relay did not publish test surface")
	}
	wrong := execution
	wrong.ConnectionID = "other-connection"
	relay.OnToolSurfaceDisposition(wrong, agent.ToolSurfaceResponseAbandoned)
	if adapter.terminal || relay.active != adapter {
		t.Fatal("mismatched disposition retired active holder")
	}
	relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
	if !adapter.terminal || relay.active != nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("matching disposition did not close holder: adapter=%#v active=%#v", adapter, relay.active)
	}
	// The second notification is ignored rather than reopening or re-closing a
	// successor. Every RunLoop reservation has one terminal disposition.
	relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseSettled)
	if relay.active != nil {
		t.Fatal("duplicate disposition recreated an active holder")
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayRetainsDurableTerminalFailure(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "relay-terminal-failure"}}
	var adapter *codingBoundDynamicRequestAdapter
	factoryCalls := 0
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		factoryCalls++
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "relay-terminal-failure-epoch"
	if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("relay did not publish fixture surface")
	}
	if err := adapter.surface.coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceRuntimeTerminal)
	if relay.active != nil || relay.TerminalDurabilityError() == nil {
		t.Fatalf("relay hid durable terminal failure: active=%#v err=%v", relay.active, relay.TerminalDurabilityError())
	}
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
		t.Fatalf("durable terminal failure admitted successor: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("durable terminal failure invoked successor factory: calls=%d", factoryCalls)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayRejectsSuccessorWhileTerminalTransitionRuns(t *testing.T) {
	handler, identity, _, _ := newCodingBoundDynamicRequestAdapterFixture(t)
	factoryCalls := 0
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(context.Context, *IMMessageHandler, *trustedCodingInvocationIdentity, corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		factoryCalls++
		return nil, fmt.Errorf("factory must not run while predecessor terminal write is unresolved")
	})
	if relay == nil {
		t.Fatal("test relay was not created")
	}
	// This state is held by finishTerminal while CloseForLifecycle performs the
	// durable write outside the relay lock. Drive it directly so the admission
	// invariant is deterministic rather than timing-dependent.
	relay.terminating = true
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err == nil || !strings.Contains(err.Error(), "terminal transition in progress") {
		t.Fatalf("terminal transition admitted successor: %v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("terminal transition invoked successor factory: calls=%d", factoryCalls)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelaySerializesBindBeforeTerminalFence(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "bind-terminal-serialization-connection"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "bind-terminal-serialization-epoch"
	if rendered := relay.RenderPublishedBoundToolSurface("", 0, execution); !rendered.Published {
		t.Fatalf("publish fixture surface: %#v", rendered)
	}
	execution.ResponseID = "bind-terminal-serialization-response"

	// Hold the relay fence while binder starts. The binder must not obtain a raw
	// holder pointer and persist a late response binding after the terminal path
	// has begun; it can proceed only as the serialized predecessor of terminal.
	relay.mu.Lock()
	bindDone := make(chan error, 1)
	go func() { bindDone <- relay.BindToolSurfaceResponse(execution) }()
	terminalDone := make(chan struct{})
	go func() {
		relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		close(terminalDone)
	}()
	// Both goroutines are now queued at the same relay fence. Release it once;
	// whichever enters first is the complete linearization point, not a partial
	// read/call sequence. The first can be bind, but terminal must still finish
	// with no executable surface remaining.
	relay.mu.Unlock()
	select {
	case err := <-bindDone:
		if err != nil && !strings.Contains(err.Error(), "terminal transition") && !strings.Contains(err.Error(), "stale_surface") {
			t.Fatalf("bind unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bind did not complete")
	}
	select {
	case <-terminalDone:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not complete")
	}
	if !adapter.terminal || relay.active != nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("serialized terminal left holder live: adapter=%#v active=%#v", adapter, relay.active)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayTerminalCancelsIssuedExecutionTicket(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "execution-ticket-terminal-connection"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "execution-ticket-terminal-epoch"
	if rendered := relay.RenderPublishedBoundToolSurface("", 0, execution); !rendered.Published {
		t.Fatalf("publish fixture surface: %#v", rendered)
	}
	execution.ResponseID = "execution-ticket-terminal-response"
	if err := relay.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
		break
	}
	if alias == "" {
		t.Fatal("fixture surface has no alias")
	}

	// This is the exact ordering protected by the relay: a call may have passed
	// local holder checks immediately before terminal begins, but it must retain
	// the same lifecycle context. Once terminal writes its fence, executing that
	// already-issued ticket cannot enter alias resolution, admission or provider
	// work.
	relay.mu.Lock()
	call, ok := adapter.beginBoundToolCall(alias, `{"query":"late"}`, "execution-ticket-terminal-call", execution)
	relay.mu.Unlock()
	if !ok {
		t.Fatal("fixture could not issue execution ticket")
	}
	relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
	if got := executeCodingBoundToolCall(call); got.Result != "[system rejected] stale_surface" {
		t.Fatalf("terminally cancelled ticket reached execution bridge: %#v", got)
	}
	if !adapter.terminal || adapter.executionCtx.Err() == nil || relay.active != nil {
		t.Fatalf("terminal did not retire issued ticket holder: adapter=%#v active=%#v", adapter, relay.active)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelaySerializesAuditEvidenceBeforeTerminalFence(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "audit-terminal-serialization-connection"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "audit-terminal-serialization-epoch"
	if rendered := relay.RenderPublishedBoundToolSurface("", 0, execution); !rendered.Published {
		t.Fatalf("publish fixture surface: %#v", rendered)
	}

	// Evidence itself has no execution authority, but it is used immediately to
	// freeze the dispatch frame. Queue it with terminal at the relay mutex and
	// verify that the read is completely ordered on one side of the fence.
	relay.mu.Lock()
	evidenceDone := make(chan agent.ToolSurfacePlanEvidence, 1)
	go func() { evidenceDone <- relay.ToolSurfaceAuditEvidence(execution) }()
	terminalDone := make(chan struct{})
	go func() {
		relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		close(terminalDone)
	}()
	relay.mu.Unlock()

	select {
	case evidence := <-evidenceDone:
		if evidence.Available && evidence.PlanID != prepared.Plan.ID {
			t.Fatalf("evidence crossed relay fence with wrong plan: %#v", evidence)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audit evidence did not complete")
	}
	select {
	case <-terminalDone:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not complete")
	}
	if !adapter.terminal || relay.active != nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("serialized terminal left holder live: adapter=%#v active=%#v", adapter, relay.active)
	}
}

func TestCodingBoundDynamicRequestAdapterRejectsConcurrentDispatchPreparation(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	prepareStarted := make(chan struct{})
	allowPrepare := make(chan struct{})
	channel := &testCodingBoundDynamicRequestChannel{
		execution:      agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "concurrent-preparation-connection"},
		prepareStarted: prepareStarted,
		allowPrepare:   allowPrepare,
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "concurrent-preparation-epoch"
	if rendered := adapter.RenderPublishedBoundToolSurface("", 0, execution); !rendered.Published {
		t.Fatalf("publish fixture surface: %#v", rendered)
	}
	preparation := agent.ToolSurfaceDispatchPreparation{
		AuditEvidence:    adapter.ToolSurfaceAuditEvidence(execution),
		InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat),
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- adapter.SetToolSurfaceDispatchPreparation(preparation) }()
	select {
	case <-prepareStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first preparation did not reach lower channel")
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(preparation); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("concurrent preparation was accepted: %v", err)
	}
	close(allowPrepare)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first preparation failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first preparation did not complete")
	}
	if !adapter.invocationPolicySet || adapter.preparationInFlight {
		t.Fatalf("preparation did not settle exactly once: %#v", adapter)
	}
}

func TestCodingBoundDynamicRequestAdapterRetiresAfterAmbiguousPreparationFailure(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	dispatches, closeCalls := 0, 0
	channel := &testCodingBoundDynamicRequestChannel{
		execution:  agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "ambiguous-preparation-failure-connection"},
		prepareErr: fmt.Errorf("lower transport setup outcome is ambiguous"),
		do: func(context.Context, []interface{}, []map[string]interface{}, llm.TokenCallback, bool) (*llm.Response, error) {
			dispatches++
			return nil, fmt.Errorf("transport must not start after preparation failure")
		},
		close: func(error) { closeCalls++ },
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "ambiguous-preparation-failure-epoch"
	if rendered := adapter.RenderPublishedBoundToolSurface("", 0, execution); !rendered.Published {
		t.Fatalf("publish fixture surface: %#v", rendered)
	}
	preparation := agent.ToolSurfaceDispatchPreparation{
		AuditEvidence:    adapter.ToolSurfaceAuditEvidence(execution),
		InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat),
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(preparation); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("lower preparation failure was hidden: %v", err)
	}
	if !adapter.terminal || adapter.executionCtx == nil || adapter.executionCtx.Err() == nil || closeCalls != 1 {
		t.Fatalf("ambiguous preparation left holder reusable: adapter=%#v close=%d", adapter, closeCalls)
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(preparation); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("terminal holder accepted retry setup: %v", err)
	}
	if _, err := adapter.DoVerified(context.Background(), nil, nil, nil, true); err == nil || !strings.Contains(err.Error(), "surface is not prepared") {
		t.Fatalf("terminal holder accepted dispatch: %v", err)
	}
	if dispatches != 0 || closeCalls != 1 {
		t.Fatalf("ambiguous preparation failure retried transport: dispatches=%d close=%d", dispatches, closeCalls)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayRejectsSuccessorAfterPublicationFailure(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	factoryCalls := 0
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		factoryCalls++
		var err error
		channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "publication-failure-relay-connection"}}
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "publication-failure-relay-epoch"
	coordinator, err := handler.app.semanticExecutionCoordinatorForApp()
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	rendered := relay.RenderPublishedBoundToolSurface("", 0, execution)
	if rendered.Published || rendered.Failure == "" || !adapter.terminal || adapter.terminalDurabilityErr == nil {
		t.Fatalf("publication failure did not close adapter: rendered=%#v adapter=%#v", rendered, adapter)
	}
	if adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
		t.Fatal("publication failure left request execution context live before disposition")
	}
	// RunLoop delivers its integrity disposition after this render failure. The
	// terminal helper must propagate the already-latched durable uncertainty to
	// relay admission rather than clearing it as a clean stale holder.
	relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceIntegrityFailure)
	if relay.TerminalDurabilityError() == nil || relay.active != nil {
		t.Fatalf("relay hid publication durability failure: active=%#v err=%v", relay.active, relay.TerminalDurabilityError())
	}
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
		t.Fatalf("publication failure admitted successor: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("publication failure invoked successor factory: calls=%d", factoryCalls)
	}
}

func TestCodingBoundDynamicRequestLifecycleRelayForgetsHolderAfterBindFailureDisposition(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "bind-failure-connection"}}
	var adapter *codingBoundDynamicRequestAdapter
	relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
		var err error
		adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
		return adapter, err
	})
	if relay == nil {
		t.Fatal("test relay was not created")
	}
	if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "bind-failure-epoch"
	if definitions := relay.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("relay did not publish test surface")
	}
	if err := relay.BindToolSurfaceResponse(execution); err == nil {
		t.Fatal("missing response ID unexpectedly bound holder")
	}
	if !adapter.terminal || relay.active != adapter {
		t.Fatalf("binder failure state adapter=%#v active=%#v", adapter, relay.active)
	}
	if adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
		t.Fatal("bind failure left request execution context live before disposition")
	}
	// RunLoop sends response_abandoned after binder failure. The relay must clear
	// the already-retired exact reservation, not leave it blocking successors.
	relay.OnToolSurfaceDisposition(execution, agent.ToolSurfaceResponseAbandoned)
	if relay.active != nil {
		t.Fatal("bind-failure disposition retained a terminal holder")
	}
}

func TestCodingCallbacksObserveAndConsumeLoopReplans(t *testing.T) {
	for _, test := range []struct {
		name      string
		callbacks func(*LoopContext) interface {
			agent.LLMReplanAware
			agent.LLMFinalizationGuard
			agent.LLMRequestContextProvider
		}
	}{
		{
			name: "local",
			callbacks: func(loopCtx *LoopContext) interface {
				agent.LLMReplanAware
				agent.LLMFinalizationGuard
				agent.LLMRequestContextProvider
			} {
				return newCodingSubAgentCallbacks(&CodingSubAgent{loopCtx: loopCtx}, &TaskItem{Title: "steer"}, "", "", nil)
			},
		},
		{
			name: "remote",
			callbacks: func(loopCtx *LoopContext) interface {
				agent.LLMReplanAware
				agent.LLMFinalizationGuard
				agent.LLMRequestContextProvider
			} {
				return &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{loopCtx: loopCtx}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			loopCtx := NewLoopContext("coding-replan-"+test.name, 3, nil)
			cb := test.callbacks(loopCtx)
			requestCtx, finish, err := cb.LLMRequestContext(0)
			if err != nil {
				t.Fatalf("LLMRequestContext: %v", err)
			}
			defer finish(nil)
			if cb.LLMReplanRequested() {
				t.Fatal("unexpected replan before steering")
			}
			// The transform boundary—not the request context—declares which
			// revision is incorporated in the conversation that will be sent.
			// This mirrors RunLoop's ordering immediately before the request.
			switch typed := cb.(type) {
			case *codingSubAgentCallbacks:
				typed.llmReplanRevision.Store(loopCtx.ReplanRevision())
			case *remoteCodingCallbacks:
				typed.llmReplanRevision.Store(loopCtx.ReplanRevision())
			}
			loopCtx.RequestReplan()
			select {
			case <-requestCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("accepted steer did not cancel the current request context")
			}
			if !cb.LLMReplanRequested() {
				t.Fatal("callback did not expose the live replan")
			}
			if cb.TryFinalizeLLMResponse() {
				t.Fatal("final response committed despite a newer steer")
			}
		})
	}
}

func TestCodingSubAgentHooksConsumesOnlyTheCurrentReplanRevision(t *testing.T) {
	loopCtx := NewLoopContext("coding-replan-drain", 3, nil)
	handler := &IMMessageHandler{}
	userID := "desktop-user:coding-replan-drain"
	cb := newCodingSubAgentCallbacks(&CodingSubAgent{loopCtx: loopCtx, handler: handler}, &TaskItem{Title: "steer"}, "", "", nil)
	hooks := &codingSubAgentHooks{handler: handler, userID: userID, loopCtx: loopCtx, replanRevision: &cb.llmReplanRevision}
	handler.accumulateInjection(userID, "[用户补充] first steer")
	loopCtx.RequestReplan()
	hooks.TransformConversation(nil)
	if cb.LLMReplanRequested() {
		t.Fatal("the steer incorporated by this transform still invalidated its successor")
	}
	// A steer accepted after the transform boundary remains visible for the
	// next request instead of being absorbed into the predecessor watermark.
	loopCtx.RequestReplan()
	if !cb.LLMReplanRequested() {
		t.Fatal("steer after transform was consumed by the predecessor")
	}
}

func TestCodingCallbacksRelaySteeringDispositionRetiresBoundReservationExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		new  func(*codingBoundDynamicRequestLifecycleRelay) interface {
			agent.ToolSurfaceRequestChannelProvider
			agent.BoundModelRequestToolSurfaceRenderer
			agent.ToolSurfaceResponseBinder
			agent.ToolSurfaceDispositionObserver
		}
	}{
		{
			name: "local",
			new: func(relay *codingBoundDynamicRequestLifecycleRelay) interface {
				agent.ToolSurfaceRequestChannelProvider
				agent.BoundModelRequestToolSurfaceRenderer
				agent.ToolSurfaceResponseBinder
				agent.ToolSurfaceDispositionObserver
			} {
				return &codingSubAgentCallbacks{dynamicLifecycleRelay: relay}
			},
		},
		{
			name: "remote",
			new: func(relay *codingBoundDynamicRequestLifecycleRelay) interface {
				agent.ToolSurfaceRequestChannelProvider
				agent.BoundModelRequestToolSurfaceRenderer
				agent.ToolSurfaceResponseBinder
				agent.ToolSurfaceDispositionObserver
			} {
				return &remoteCodingCallbacks{dynamicLifecycleRelay: relay}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			ledger := newTestCodingReservationLedger()
			var adapters []*codingBoundDynamicRequestAdapter
			var reservation int
			relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				reservation++
				channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{
					Protocol: "test-provider/v1", ConnectionID: fmt.Sprintf("%s-steer-%d", tc.name, reservation),
				}}
				adapter, err := newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				if err == nil {
					adapters = append(adapters, adapter)
				}
				return adapter, err
			})
			cb := tc.new(relay)
			if _, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatal(err)
			}
			if len(adapters) != 1 {
				t.Fatalf("predecessor reservation count=%d", len(adapters))
			}
			predecessor := adapters[0]
			predecessorExec := predecessor.ExecutionContext()
			predecessorExec.SurfaceEpoch = "steer-predecessor-epoch"
			ledger.record("reserved", predecessorExec)
			if definitions := cb.BuildToolsForBoundModelRequest("", 0, predecessorExec); len(definitions) != 1 {
				t.Fatalf("predecessor did not publish a bound surface: %#v", definitions)
			}
			ledger.record("prepared", predecessorExec)
			predecessorExec.ResponseID = "steer-predecessor-response"
			if err := cb.BindToolSurfaceResponse(predecessorExec); err != nil {
				t.Fatalf("bind predecessor: %v", err)
			}
			ledger.record("bound:"+predecessorExec.ResponseID, predecessorExec)
			var alias string
			for name := range predecessor.surface.aliases {
				alias = name
				break
			}
			if alias == "" {
				t.Fatal("predecessor had no opaque alias")
			}

			// RunLoop is the only production emitter of this disposition. This
			// focused callback conformance supplies the exact tuple it would pass;
			// the callback may only forward it to the single active relay.
			cb.OnToolSurfaceDisposition(predecessorExec, agent.ToolSurfaceSteered)
			ledger.record("terminal:steered", predecessorExec)
			cb.OnToolSurfaceDisposition(predecessorExec, agent.ToolSurfaceSteered)
			if got, want := ledger.eventsFor(t, predecessorExec), []string{"reserved", "prepared", "bound:steer-predecessor-response", "terminal:steered"}; fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("predecessor must have exactly one terminal disposition: got=%v want=%v", got, want)
			}
			// The relay itself accepts only the first matching terminal fact: it
			// retires the holder and clears ownership, so the duplicate cannot
			// consume another grant or close a successor.
			if !predecessor.terminal || relay.active != nil {
				t.Fatalf("steered predecessor not terminal: terminal=%v active=%#v", predecessor.terminal, relay.active)
			}
			if got := predecessor.ExecuteToolCallWithContext(alias, `{"query":"late"}`, "late-call", predecessorExec); got.Result != "[system rejected] stale_surface" {
				t.Fatalf("steered predecessor alias dispatched: %#v", got)
			}
			if _, _, err := predecessor.surface.ResolveAlias(predecessorExec.ResponseID, alias); err == nil {
				t.Fatal("steered predecessor alias remained resolvable")
			}

			if _, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatalf("successor reservation: %v", err)
			}
			if len(adapters) != 2 {
				t.Fatalf("successor was not reserved: %d", len(adapters))
			}
			successor := adapters[1]
			successorExec := successor.ExecutionContext()
			successorExec.SurfaceEpoch = "steer-successor-epoch"
			if predecessorExec.ConnectionID == successorExec.ConnectionID || predecessorExec.SurfaceEpoch == successorExec.SurfaceEpoch {
				t.Fatalf("successor reused predecessor tuple: predecessor=%+v successor=%+v", predecessorExec, successorExec)
			}
			cb.OnToolSurfaceDisposition(predecessorExec, agent.ToolSurfaceResponseSettled)
			if relay.active != successor || successor.terminal {
				t.Fatal("late predecessor disposition disturbed successor ownership")
			}
		})
	}
}

func TestCodingCallbacksRelayRunLoopBatchDurabilityDisposesBoundReservationExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		newHost func(*codingBoundDynamicRequestLifecycleRelay, *testCodingReservationLedger, *testCodingDynamicRunLoopControl) agent.LoopCallbacks
	}{
		{
			name: "local",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger, control *testCodingDynamicRunLoopControl) agent.LoopCallbacks {
				return &testLocalDynamicLifecycleLoopHost{codingSubAgentCallbacks: &codingSubAgentCallbacks{
					dynamicLifecycleRelay: relay,
					subagent: &CodingSubAgent{cfg: corelib.MaclawLLMConfig{
						URL: "https://unused.example", Model: "test", Key: "test-key",
					}, loopCtx: NewLoopContext("d2c-local", 1, nil)},
				}, ledger: ledger, control: control}
			},
		},
		{
			name: "remote",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger, control *testCodingDynamicRunLoopControl) agent.LoopCallbacks {
				return &testRemoteDynamicLifecycleLoopHost{remoteCodingCallbacks: &remoteCodingCallbacks{
					dynamicLifecycleRelay: relay,
					agent: &RemoteCodingSubAgent{cfg: corelib.MaclawLLMConfig{
						URL: "https://unused.example", Model: "test", Key: "test-key",
					}, loopCtx: NewLoopContext("d2c-remote", 1, nil)},
				}, ledger: ledger, control: control}
			},
		},
	} {
		for _, scenario := range []struct {
			name             string
			failStart        bool
			failCommit       bool
			cancel           bool
			steerAfterCommit bool
			want             agent.ToolSurfaceDisposition
		}{
			{name: "starter failure abandons before execution", failStart: true, want: agent.ToolSurfaceResponseAbandoned},
			{name: "committer failure abandons paired batch", failCommit: true, want: agent.ToolSurfaceResponseAbandoned},
			{name: "complete batch settles exactly once", want: agent.ToolSurfaceToolBatchSettled},
			{name: "runtime cancellation prevents batch settlement", cancel: true, want: agent.ToolSurfaceRuntimeTerminal},
			{name: "steer after durable commit prevents settled terminal", steerAfterCommit: true, want: agent.ToolSurfaceSteered},
		} {
			t.Run(tc.name+"/"+scenario.name, func(t *testing.T) {
				handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
				ledger := newTestCodingReservationLedger()
				var adapter *codingBoundDynamicRequestAdapter
				var execution agent.ToolCallExecutionContext
				var alias string
				control := &testCodingDynamicRunLoopControl{}
				channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{
					Protocol: "test-provider/v1", ConnectionID: tc.name + "-batch-connection",
				}}
				channel.do = func(_ context.Context, _ []interface{}, definitions []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
					if len(definitions) != 1 || adapter == nil || adapter.surface == nil {
						t.Errorf("bound request was not rendered before send: definitions=%#v adapter=%#v", definitions, adapter)
						return nil, fmt.Errorf("bound request was not rendered before send")
					}
					for name := range adapter.surface.aliases {
						alias = name
						break
					}
					if alias == "" {
						return nil, fmt.Errorf("bound request has no opaque alias")
					}
					if scenario.cancel {
						control.stop = true
					}
					return &llm.Response{ResponseID: tc.name + "-batch-response", Choices: []llm.Choice{{
						Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
							ID: "batch-call", Type: "function", Function: llm.ToolCallFunction{Name: alias, Arguments: `{"query":"D2c"}`},
						}}},
						FinishReason: "tool_calls",
					}}}, nil
				}
				relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
					var err error
					adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
					return adapter, err
				})
				if relay == nil {
					t.Fatal("test relay was not created")
				}
				host := tc.newHost(relay, ledger, control)
				var requestReplan func()
				switch typed := host.(type) {
				case *testLocalDynamicLifecycleLoopHost:
					requestReplan = func() { typed.subagent.loopCtx.RequestReplan() }
				case *testRemoteDynamicLifecycleLoopHost:
					requestReplan = func() { typed.agent.loopCtx.RequestReplan() }
				default:
					t.Fatalf("unexpected D2c loop host %T", host)
				}
				hooks := &testCodingDynamicLifecycleBatchHooks{failStart: scenario.failStart, failCommit: scenario.failCommit}
				if scenario.steerAfterCommit {
					hooks.afterCommit = func() {
						requestReplan()
						// RunLoop will emit steered after the successful durability
						// commit, then observe this host terminal fence before it
						// can reserve an unrelated successor in this focused fixture.
						control.stop = true
					}
				}

				result := agent.RunLoop(host, "verify batch lifecycle", nil, nil, hooks)
				if adapter == nil {
					t.Fatal("RunLoop did not reserve a real holder")
				}
				adapter.mu.Lock()
				execution = agent.ToolCallExecutionContext{
					Protocol: adapter.protocol, ConnectionID: adapter.connection, SurfaceEpoch: adapter.epoch,
				}
				adapter.mu.Unlock()
				if execution.SurfaceEpoch == "" || execution.Protocol == "" || execution.ConnectionID == "" {
					t.Fatal("RunLoop did not assign a surface epoch")
				}
				execution.ResponseID = tc.name + "-batch-response"
				if hooks.started != 1 {
					t.Fatalf("batch starter calls=%d want=1; result=%+v", hooks.started, result)
				}
				if scenario.failStart {
					if hooks.committed != 0 || control.executions != 0 || result.Error != "recovery_checkpoint_failed" {
						t.Fatalf("starter failure result=%+v commits=%d executions=%d", result, hooks.committed, control.executions)
					}
				} else if scenario.cancel {
					if hooks.committed != 0 || control.executions != 0 || result.Error != "cancelled" {
						t.Fatalf("cancellation result=%+v commits=%d executions=%d", result, hooks.committed, control.executions)
					}
				} else if scenario.steerAfterCommit {
					if hooks.committed != 1 || control.executions != 1 || result.Error != "cancelled" {
						t.Fatalf("post-commit steer result=%+v commits=%d executions=%d", result, hooks.committed, control.executions)
					}
				} else if hooks.committed != 1 || control.executions != 1 {
					t.Fatalf("batch committer calls=%d executions=%d want=1; result=%+v", hooks.committed, control.executions, result)
				}
				got := ledger.eventsFor(t, execution)
				if len(got) != 5 || got[0] != "reserved" || got[1] != "prepared" || !strings.HasPrefix(got[2], "receipt:") || got[3] != "bound:"+execution.ResponseID || got[4] != "terminal:"+string(scenario.want) {
					t.Fatalf("reservation ledger got=%v result=%+v", got, result)
				}
				if relay.active != nil || !adapter.terminal {
					t.Fatalf("terminal disposition did not retire holder: active=%#v terminal=%v", relay.active, adapter.terminal)
				}
				if got := adapter.ExecuteToolCallWithContext(alias, `{"query":"late"}`, "late-call", execution); got.Result != "[system rejected] stale_surface" {
					t.Fatalf("terminal adapter accepted late alias: %#v", got)
				}
			})
		}
	}
}

func TestCodingCallbacksRelayRunLoopInteractivePauseAbandonsBoundReservationExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		newHost func(*codingBoundDynamicRequestLifecycleRelay, *testCodingReservationLedger, *testCodingDynamicRunLoopControl) agent.LoopCallbacks
	}{
		{
			name: "local",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger, control *testCodingDynamicRunLoopControl) agent.LoopCallbacks {
				return &testLocalDynamicLifecycleLoopHost{codingSubAgentCallbacks: &codingSubAgentCallbacks{
					dynamicLifecycleRelay: relay,
					subagent:              &CodingSubAgent{cfg: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test", Key: "test-key"}, loopCtx: NewLoopContext("d2-interactive-local", 1, nil)},
				}, ledger: ledger, control: control}
			},
		},
		{
			name: "remote",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger, control *testCodingDynamicRunLoopControl) agent.LoopCallbacks {
				return &testRemoteDynamicLifecycleLoopHost{remoteCodingCallbacks: &remoteCodingCallbacks{
					dynamicLifecycleRelay: relay,
					agent:                 &RemoteCodingSubAgent{cfg: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test", Key: "test-key"}, loopCtx: NewLoopContext("d2-interactive-remote", 1, nil)},
				}, ledger: ledger, control: control}
			},
		},
	} {
		for _, pause := range []struct {
			name       string
			result     string
			assertLoop func(*testing.T, agent.LoopResult)
		}{
			{
				name:   "ask user",
				result: agent.AskUserResultMarker(&agent.AskUserRequest{Question: "Need an explicit choice", Options: []string{"A", "B"}}),
				assertLoop: func(t *testing.T, result agent.LoopResult) {
					t.Helper()
					if result.AskUser == nil || result.RecordAudio != nil {
						t.Fatalf("interactive result=%+v", result)
					}
				},
			},
			{
				name:   "record audio",
				result: agent.FormatRecordAudioMarker(&agent.RecordAudioRequest{Title: "Record a short note"}),
				assertLoop: func(t *testing.T, result agent.LoopResult) {
					t.Helper()
					if result.RecordAudio == nil || result.AskUser != nil {
						t.Fatalf("interactive result=%+v", result)
					}
				},
			},
		} {
			t.Run(tc.name+"/"+pause.name, func(t *testing.T) {
				handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
				ledger := newTestCodingReservationLedger()
				control := &testCodingDynamicRunLoopControl{toolResult: pause.result}
				var adapter *codingBoundDynamicRequestAdapter
				var alias string
				channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: tc.name + "-interactive-connection"}}
				channel.do = func(_ context.Context, _ []interface{}, definitions []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
					if len(definitions) != 1 || adapter == nil || adapter.surface == nil {
						return nil, fmt.Errorf("bound request was not rendered")
					}
					for name := range adapter.surface.aliases {
						alias = name
						break
					}
					return &llm.Response{ResponseID: tc.name + "-interactive-response", Choices: []llm.Choice{{
						Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "interactive-call", Type: "function", Function: llm.ToolCallFunction{Name: alias, Arguments: `{}`}}}}, FinishReason: "tool_calls",
					}}}, nil
				}
				relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
					var err error
					adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
					return adapter, err
				})
				host := tc.newHost(relay, ledger, control)
				// The holder bridge normally executes a semantic provider selection;
				// this focused lifecycle probe replaces only its returned result so
				// it can cover RunLoop's interactive early-return branch.
				result := agent.RunLoop(host, "interactive lifecycle", nil, nil)
				pause.assertLoop(t, result)
				if control.executions != 1 || adapter == nil {
					t.Fatalf("interactive result did not execute exactly one bound call: result=%+v executions=%d adapter=%#v", result, control.executions, adapter)
				}
				adapter.mu.Lock()
				execution := agent.ToolCallExecutionContext{Protocol: adapter.protocol, ConnectionID: adapter.connection, SurfaceEpoch: adapter.epoch, ResponseID: tc.name + "-interactive-response"}
				adapter.mu.Unlock()
				got := ledger.eventsFor(t, execution)
				if len(got) != 5 || got[0] != "reserved" || got[1] != "prepared" || !strings.HasPrefix(got[2], "receipt:") || got[3] != "bound:"+execution.ResponseID || got[4] != "terminal:"+string(agent.ToolSurfaceResponseAbandoned) {
					t.Fatalf("interactive lifecycle ledger=%v result=%+v", got, result)
				}
				if relay.active != nil || !adapter.terminal {
					t.Fatalf("interactive disposition did not retire holder: active=%#v terminal=%v", relay.active, adapter.terminal)
				}
				if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "late", execution); got.Result != "[system rejected] stale_surface" {
					t.Fatalf("interactive terminal allowed late alias: %#v", got)
				}
			})
		}
	}
}

func TestCodingBoundDynamicLifecycleTerminalReasonsRetireOnlyTheirBoundReservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason codingBoundDynamicRequestTerminalReason
	}{
		{name: "nested exit", reason: codingBoundDynamicRequestNestedExit},
		{name: "route supersede", reason: codingBoundDynamicRequestRouteSuperseded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			var adapters []*codingBoundDynamicRequestAdapter
			var reservation int
			relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				reservation++
				channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{
					Protocol: "test-provider/v1", ConnectionID: fmt.Sprintf("terminal-%d", reservation),
				}}
				adapter, err := newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				if err == nil {
					adapters = append(adapters, adapter)
				}
				return adapter, err
			})
			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatal(err)
			}
			if len(adapters) != 1 {
				t.Fatalf("predecessor reservations=%d", len(adapters))
			}
			predecessor := adapters[0]
			predecessorExec := predecessor.ExecutionContext()
			predecessorExec.SurfaceEpoch = "terminal-predecessor"
			if definitions := relay.BuildToolsForBoundModelRequest("", 0, predecessorExec); len(definitions) != 1 {
				t.Fatalf("predecessor definitions=%#v", definitions)
			}
			predecessorExec.ResponseID = "terminal-response"
			if err := relay.BindToolSurfaceResponse(predecessorExec); err != nil {
				t.Fatal(err)
			}
			var alias string
			for name := range predecessor.surface.aliases {
				alias = name
				break
			}

			relay.CloseForLifecycle(tc.reason)
			if !predecessor.terminal || relay.active != nil {
				t.Fatalf("terminal reason did not retire predecessor: terminal=%v active=%#v", predecessor.terminal, relay.active)
			}
			if got := predecessor.ExecuteToolCallWithContext(alias, `{"query":"late"}`, "late-call", predecessorExec); got.Result != "[system rejected] stale_surface" {
				t.Fatalf("terminal predecessor dispatched: %#v", got)
			}

			if _, err := relay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
				t.Fatalf("successor reserve: %v", err)
			}
			if len(adapters) != 2 || relay.active != adapters[1] || adapters[1].terminal {
				t.Fatalf("successor ownership adapters=%d active=%#v", len(adapters), relay.active)
			}
			// A delayed predecessor terminal fact must never close a replacement
			// reservation. This is exact ownership, not a global callback close.
			relay.OnToolSurfaceDisposition(predecessorExec, agent.ToolSurfaceResponseAbandoned)
			if relay.active != adapters[1] || adapters[1].terminal {
				t.Fatal("late predecessor disposition disturbed successor")
			}
			relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

func TestCodingBoundDynamicLifecycleSeparatesConcurrentExecutorsOnOneCoordinator(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	handler := &IMMessageHandler{app: app}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	newPrepared := func(identity *trustedCodingInvocationIdentity) codingDynamicPlanPreparation {
		prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	firstIdentity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root-a", TurnID: "turn-a"}
	secondIdentity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root-b", TurnID: "turn-b"}

	newRelay := func(identity *trustedCodingInvocationIdentity, prepared codingDynamicPlanPreparation, connection string) (*codingBoundDynamicRequestLifecycleRelay, **codingBoundDynamicRequestAdapter) {
		var adapter *codingBoundDynamicRequestAdapter
		relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
			channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: connection}}
			var err error
			adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
			return adapter, err
		})
		return relay, &adapter
	}
	firstRelay, firstRef := newRelay(firstIdentity, newPrepared(firstIdentity), "executor-a")
	secondRelay, secondRef := newRelay(secondIdentity, newPrepared(secondIdentity), "executor-b")
	if _, err := firstRelay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, err := secondRelay.ReserveToolSurfaceRequestChannel(context.Background(), corelib.MaclawLLMConfig{}); err != nil {
		t.Fatal(err)
	}
	first, second := *firstRef, *secondRef
	firstExec := first.ExecutionContext()
	firstExec.SurfaceEpoch = "executor-a-epoch"
	secondExec := second.ExecutionContext()
	secondExec.SurfaceEpoch = "executor-b-epoch"
	if len(firstRelay.BuildToolsForBoundModelRequest("", 0, firstExec)) != 1 || len(secondRelay.BuildToolsForBoundModelRequest("", 0, secondExec)) != 1 {
		t.Fatal("concurrent executors did not publish isolated surfaces")
	}
	firstExec.ResponseID, secondExec.ResponseID = "executor-a-response", "executor-b-response"
	if err := firstRelay.BindToolSurfaceResponse(firstExec); err != nil {
		t.Fatal(err)
	}
	if err := secondRelay.BindToolSurfaceResponse(secondExec); err != nil {
		t.Fatal(err)
	}
	var firstAlias, secondAlias string
	for name := range first.surface.aliases {
		firstAlias = name
	}
	for name := range second.surface.aliases {
		secondAlias = name
	}
	firstRelay.CloseForLifecycle(codingBoundDynamicRequestRouteSuperseded)
	if !first.terminal || firstRelay.active != nil {
		t.Fatal("first executor was not retired")
	}
	if _, _, err := first.surface.ResolveAlias(firstExec.ResponseID, firstAlias); err == nil {
		t.Fatal("retired first executor retained its alias")
	}
	// Both routes share a coordinator and tenant, so this catches accidental
	// global route cancellation. The second executor remains independently
	// response-bound and must neither become stale nor lose its relay owner.
	if second.terminal || secondRelay.active != second {
		t.Fatalf("first terminal disturbed second executor: terminal=%v active=%#v", second.terminal, secondRelay.active)
	}
	if _, _, err := second.surface.ResolveAlias(secondExec.ResponseID, secondAlias); err != nil {
		t.Fatalf("second executor lost its bound alias: %v", err)
	}
	if got := second.ExecuteToolCallWithContext(secondAlias, `{"query":"still-live"}`, "second-call", secondExec); got.Result == "[system rejected] stale_surface" {
		t.Fatalf("second executor was cancelled by first terminal: %#v", got)
	}
	secondRelay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
}

func TestCodingBoundDynamicLifecycleRecoveryRetainsOnlyDurableBoundAuthority(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "recovery-connection"}}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "recovery-epoch"
	if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
		t.Fatalf("publish definitions=%#v", definitions)
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
	}
	execution.ResponseID = "recovery-response"
	if err := adapter.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after response bind. The new helper gets only the
	// verified identity and durable tuple; it deliberately has no old adapter,
	// definitions, alias map, channel, task text, or cached provider match.
	handler.app.closeSemanticInvocationStore()
	recovered, err := handler.app.recoverCodingDurableDynamicSurface(identity, execution.Protocol, execution.ConnectionID, execution.SurfaceEpoch)
	if err != nil {
		t.Fatalf("recover bound surface: %v", err)
	}
	if len(recovered.aliases) != 0 || len(recovered.definitions) != 0 {
		t.Fatalf("recovery restored process-local dispatch state: %#v", recovered)
	}
	if _, scope, err := recovered.ResolveAlias(execution.ResponseID, alias); err != nil || scope != adapter.surface.scope {
		t.Fatalf("recovery did not retain durable bound alias: scope=%#v err=%v", scope, err)
	}
	// The recovered fixed bridge has no rehydrated catalog or by-name fallback.
	// It therefore rejects before provider I/O rather than turning the durable
	// alias into an executable cache entry.
	got := recovered.ExecuteBoundSelection(context.Background(), identity, codingDynamicCatalogSnapshot{}, handler, execution.Protocol, execution.ConnectionID, execution.ResponseID, "recovered-call", alias, `{"query":"after restart"}`, time.Now().UTC())
	if got.ReasonCode != "catalog_incomplete" {
		t.Fatalf("recovered bridge result=%#v, want catalog_incomplete", got)
	}
	if err := recovered.Cancel(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := recovered.ResolveAlias(execution.ResponseID, alias); err == nil {
		t.Fatal("recovered terminal route retained alias")
	}
	if _, err := handler.app.recoverCodingDurableDynamicSurface(identity, execution.Protocol, execution.ConnectionID, execution.SurfaceEpoch); err == nil {
		t.Fatal("terminal route recovered after cancellation")
	}
}

func TestVerifiedIngressRunLoopBatchLifecycleUsesTheSameBoundRelay(t *testing.T) {
	// This is the D2 end-to-end shape, deliberately still test-only: verified
	// ingress creates the identity first, then the callback's actual RunLoop
	// composition receives one injected relay. Qualification remains disabled,
	// so production code cannot select this factory or materialize aliases.
	handler, _, _, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	relations, err := newCodingTaskRelationService(filepath.Join(t.TempDir(), "relations.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relations.Close() })
	subject, err := newVerifiedCodingSubject("desktop-d2-runloop", "principal-d2-runloop", "session-d2-runloop")
	if err != nil {
		t.Fatal(err)
	}
	handle, err := relations.CreateCodingTask(subject, time.Now().UTC(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := codingruntime.NewMemoryStore()
	loop := NewLoopContext("verified-d2-runloop", 1, nil)
	ctx, cancel := loop.Context()
	defer cancel()
	var (
		adapter *codingBoundDynamicRequestAdapter
		relay   *codingBoundDynamicRequestLifecycleRelay
		result  *CodingSubAgentResult
		attempt *codingruntime.Attempt
		runErr  error
	)
	result, attempt, runErr = runGUICodingTaskWithLedgerWithStart(ctx, store, "gui:verified-d2-runloop", "workflow", "phase", "D:/verified", "read", nil, func(request codingruntime.ExecutionRequest) {
		identity, ok := bindVerifiedCodingTaskHandle(relations, subject, &handle, store, request)
		if !ok || identity == nil {
			t.Fatal("verified ingress did not bind runtime identity")
		}
		prepared, prepErr := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Now().UTC())
		if prepErr != nil {
			t.Fatal(prepErr)
		}
		channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "verified-d2-runloop-connection"}}
		channel.do = func(_ context.Context, _ []interface{}, definitions []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
			if len(definitions) != 1 || adapter == nil || adapter.surface == nil {
				return nil, fmt.Errorf("verified ingress request was not bound-rendered")
			}
			var alias string
			for name := range adapter.surface.aliases {
				alias = name
			}
			return &llm.Response{ResponseID: "verified-d2-runloop-response", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "verified-d2-runloop-call", Type: "function", Function: llm.ToolCallFunction{Name: alias, Arguments: `{"query":"verified"}`}}}}, FinishReason: "tool_calls"}}}, nil
		}
		relay = newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
			var adapterErr error
			adapter, adapterErr = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
			return adapter, adapterErr
		})
		if relay == nil {
			t.Fatal("verified ingress did not construct relay")
		}
		subagent := &CodingSubAgent{
			handler: handler, loopCtx: loop, runtimeStore: store, runtimeAttempt: &request.Attempt,
			dynamicInvocationIdentity: identity,
			cfg:                       corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test", Key: "test-key"},
		}
		cb := newCodingSubAgentCallbacks(subagent, &TaskItem{Title: "verified RunLoop"}, "", "", nil)
		cb.dynamicLifecycleRelay = relay
		cb.registerDynamicLifecycleOwner()
		defer cb.closeDynamicLifecycleOwner(codingBoundDynamicRequestRuntimeClosed)
		hooks := &testCodingDynamicLifecycleBatchHooks{}
		loopResult := codingagent.Run(&testLocalDynamicLifecycleLoopHost{codingSubAgentCallbacks: cb, control: &testCodingDynamicRunLoopControl{}}, "verified lifecycle", nil, nil, nil, hooks)
		if loopResult.Error != "max iterations reached" || hooks.started != 1 || hooks.committed != 1 {
			t.Fatalf("verified ingress RunLoop result=%+v starts=%d commits=%d", loopResult, hooks.started, hooks.committed)
		}
		if adapter == nil || !adapter.terminal || relay.active != nil {
			t.Fatalf("verified RunLoop did not settle the bound holder: adapter=%#v active=%#v", adapter, relay.active)
		}
	}, func() *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "verified RunLoop lifecycle complete"}
	})
	if runErr != nil || result == nil || result.Status != TaskExecPassed || attempt == nil || attempt.Status != codingruntime.TaskCompleted {
		t.Fatalf("verified ingress lifecycle result=%#v attempt=%#v err=%v", result, attempt, runErr)
	}
	// The callback's full prompt construction opens app-owned knowledge storage;
	// close it before TempDir cleanup. This is unrelated to dynamic authority.
	handler.app.closeSemanticInvocationStore()
}

func TestVerifiedIngressRunLoopRejectsForgedReceiptBeforeBindingForLocalAndRemote(t *testing.T) {
	for _, tc := range []struct {
		name    string
		newHost func(*codingBoundDynamicRequestLifecycleRelay, *testCodingReservationLedger) agent.LoopCallbacks
	}{
		{
			name: "local",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger) agent.LoopCallbacks {
				return &testLocalDynamicLifecycleLoopHost{codingSubAgentCallbacks: &codingSubAgentCallbacks{
					dynamicLifecycleRelay: relay,
					subagent:              &CodingSubAgent{cfg: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test", Key: "test-key"}},
				}, ledger: ledger, control: &testCodingDynamicRunLoopControl{}}
			},
		},
		{
			name: "remote",
			newHost: func(relay *codingBoundDynamicRequestLifecycleRelay, ledger *testCodingReservationLedger) agent.LoopCallbacks {
				return &testRemoteDynamicLifecycleLoopHost{remoteCodingCallbacks: &remoteCodingCallbacks{
					dynamicLifecycleRelay: relay,
					agent:                 &RemoteCodingSubAgent{cfg: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test", Key: "test-key"}},
				}, ledger: ledger, control: &testCodingDynamicRunLoopControl{}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			ledger := newTestCodingReservationLedger()
			channel := &testCodingBoundDynamicRequestChannel{execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "forged-receipt-" + tc.name}}
			channel.do = func(_ context.Context, _ []interface{}, _ []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
				return &llm.Response{ResponseID: "forged-response", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "must not bind"}, FinishReason: "stop"}}}, nil
			}
			channel.mutateReceipt = func(receipt *agent.ToolSurfaceReceipt) { receipt.AuditDigest = "forged" }
			var adapter *codingBoundDynamicRequestAdapter
			relay := newCodingBoundDynamicRequestLifecycleRelay(handler, identity, func(_ context.Context, gotHandler *IMMessageHandler, gotIdentity *trustedCodingInvocationIdentity, _ corelib.MaclawLLMConfig) (*codingBoundDynamicRequestAdapter, error) {
				var err error
				adapter, err = newCodingBoundDynamicRequestAdapterForChannel(gotHandler, gotIdentity, prepared, dynamic, channel)
				return adapter, err
			})
			result := codingagent.Run(tc.newHost(relay, ledger), "reject forged receipt", "reject forged receipt", nil, nil, nil)
			if !strings.Contains(result.Error, "surface_integrity_failure") {
				t.Fatalf("result=%+v", result)
			}
			if adapter == nil || !adapter.terminal || relay.active != nil {
				t.Fatalf("forged receipt left holder live: adapter=%#v active=%#v", adapter, relay.active)
			}
			execution := agent.ToolCallExecutionContext{Protocol: channel.execution.Protocol, ConnectionID: channel.execution.ConnectionID, SurfaceEpoch: adapter.epoch}
			events := ledger.eventsFor(t, execution)
			if !slices.Contains(events, "terminal:surface_integrity_failure") || !slices.ContainsFunc(events, func(event string) bool { return strings.HasPrefix(event, "receipt:") }) || slices.ContainsFunc(events, func(event string) bool { return strings.HasPrefix(event, "bound:") }) {
				t.Fatalf("ledger=%#v", events)
			}
		})
	}
}

func TestCodingSubAgentRunLoopReplanCancelsPredecessorAndUsesSteeredSuccessor(t *testing.T) {
	loopCtx := NewLoopContext("coding-runloop-replan", 1, nil)
	userID := "desktop-user:coding-runloop-replan"
	loopCtx.UserID = userID
	handler := &IMMessageHandler{}
	cb := newCodingSubAgentCallbacks(&CodingSubAgent{
		handler:         handler,
		loopCtx:         loopCtx,
		cfg:             corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		fullEnvironment: false,
	}, &TaskItem{Title: "inspect", Description: "inspect the repository"}, "", "", nil)

	var requests int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		var payload struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 1 {
			handler.accumulateInjection(userID, "[用户补充] switch to SQLite")
			loopCtx.RequestReplan()
			select {
			case <-request.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("steer did not cancel predecessor HTTP request")
			}
			return nil, request.Context().Err()
		}
		foundSteer := false
		for _, message := range payload.Messages {
			if fmt.Sprint(message["content"]) != "" && bytes.Contains([]byte(fmt.Sprint(message["content"])), []byte("switch to SQLite")) {
				foundSteer = true
			}
		}
		if !foundSteer {
			t.Fatal("replacement request omitted the accepted steering message")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"role":"assistant","content":"steered result"}}]}`)),
		}, nil
	})}

	result := codingagent.Run(cb, "inspect", nil, nil, client, cb.subagent.buildLoopHooks(cb))
	if result.Error != "" || result.Text != "steered result" || requests != 2 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}

func TestRemoteCodingRunLoopReplanCancelsPredecessorAndUsesSteeredSuccessor(t *testing.T) {
	loopCtx := NewLoopContext("remote-coding-runloop-replan", 1, nil)
	userID := "desktop-user:remote-coding-runloop-replan"
	loopCtx.UserID = userID
	handler := &IMMessageHandler{}
	remote := &RemoteCodingSubAgent{
		handler: handler,
		loopCtx: loopCtx,
		cfg:     corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
	}
	cb := &remoteCodingCallbacks{agent: remote, task: "inspect the remote repository"}

	var requests int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		var payload struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if requests == 1 {
			handler.accumulateInjection(userID, "[用户补充] inspect the migration first")
			loopCtx.RequestReplan()
			select {
			case <-request.Context().Done():
			case <-time.After(time.Second):
				t.Fatal("steer did not cancel predecessor remote HTTP request")
			}
			return nil, request.Context().Err()
		}
		foundSteer := false
		for _, message := range payload.Messages {
			if bytes.Contains([]byte(fmt.Sprint(message["content"])), []byte("inspect the migration first")) {
				foundSteer = true
			}
		}
		if !foundSteer {
			t.Fatal("remote replacement request omitted the accepted steering message")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"choices":[{"message":{"role":"assistant","content":"remote steered result"}}]}`)),
		}, nil
	})}

	result := codingagent.Run(cb, "inspect", nil, nil, client, remote.buildRemoteCodingLoopHooks(cb))
	if result.Error != "" || result.Text != "remote steered result" || requests != 2 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}
