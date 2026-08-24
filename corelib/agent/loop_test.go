package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

var callCounter atomic.Int64

// mockCallbacks implements LoopCallbacks for testing.
type mockCallbacks struct {
	config      corelib.MaclawLLMConfig
	maxIter     int
	sysPrompt   string
	tools       []map[string]interface{}
	toolResult  string
	toolOutcome ToolExecutionOutcome
	allowed     map[string]bool
	callAllowed map[string]bool
	callReason  string
	tokens      []string
	toolCalls   []string
	toolArgs    []string
	toolEvents  []string
	stopped     bool
}

type invocationPolicyCallbacks struct {
	mockCallbacks
	policy ToolSurfaceInvocationPolicy
	err    error
}

func (m *invocationPolicyCallbacks) ToolSurfaceInvocationPolicy(int) (ToolSurfaceInvocationPolicy, error) {
	return m.policy, m.err
}

type rotatingInvocationPolicyCallbacks struct {
	mockCallbacks
	policies []ToolSurfaceInvocationPolicy
	calls    int
}

type fallbackPolicyFailureCallbacks struct {
	*rotatingInvocationPolicyCallbacks
	events *[]ToolSurfaceEvent
}

type fallbackLifecycleFailureCallbacks struct {
	*mockCallbacks
	calls  int
	events []ToolSurfaceEvent
}

type policyEventCallbacks struct {
	*invocationPolicyCallbacks
	events *[]ToolSurfaceEvent
}

type reserveFailureCallbacks struct {
	*mockCallbacks
	events       []ToolSurfaceEvent
	reserveCalls int
}

type contextFailureCallbacks struct {
	*mockCallbacks
	events       []ToolSurfaceEvent
	contextCalls int
}

func (m *rotatingInvocationPolicyCallbacks) ToolSurfaceInvocationPolicy(int) (ToolSurfaceInvocationPolicy, error) {
	if len(m.policies) == 0 {
		return ToolSurfaceInvocationPolicy{}, fmt.Errorf("no test invocation policies")
	}
	index := m.calls
	m.calls++
	if index >= len(m.policies) {
		index = len(m.policies) - 1
	}
	return m.policies[index], nil
}

func (m *fallbackPolicyFailureCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	*m.events = append(*m.events, event)
}

func (m *fallbackLifecycleFailureCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	m.calls++
	if m.calls == 1 {
		return []map[string]interface{}{tooldef.BuildToolDef("first_tool", "first", map[string]interface{}{"type": "object"})}
	}
	return []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "invalid_successor",
			"description": 42,
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}}
}

func (m *fallbackLifecycleFailureCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *policyEventCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	*m.events = append(*m.events, event)
}

func (m *reserveFailureCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	m.reserveCalls++
	return nil, fmt.Errorf("reservation unavailable")
}

func (m *reserveFailureCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *contextFailureCallbacks) LLMRequestContext(int) (context.Context, func(error), error) {
	m.contextCalls++
	return nil, nil, fmt.Errorf("request context unavailable")
}

func (m *contextFailureCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *mockCallbacks) GetLLMConfig() corelib.MaclawLLMConfig      { return m.config }
func (m *mockCallbacks) GetMaxIterations() int                      { return m.maxIter }
func (m *mockCallbacks) BuildSystemPrompt(string, bool) string      { return m.sysPrompt }
func (m *mockCallbacks) BuildTools(string) []map[string]interface{} { return m.tools }
func (m *mockCallbacks) ExecuteTool(name, args string) string {
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	return m.toolResult
}
func (m *mockCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	if m.toolOutcome == "" {
		return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: executionOutcomeFromToolOutcome(classifyToolResult(m.toolResult).kind)}
	}
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	return ToolExecutionResult{Result: m.toolResult, Outcome: m.toolOutcome}
}

type callIDCallbacks struct {
	mockCallbacks
	callID string
}

func (m *callIDCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	m.callID = callID
	return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: ToolExecutionOutcomeOK}
}

func TestExecuteAuthorizedLoopToolCallPreservesHostCallID(t *testing.T) {
	callbacks := &callIDCallbacks{mockCallbacks: mockCallbacks{toolResult: "ok"}}
	got := executeAuthorizedLoopToolCall(callbacks, "opaque_adapter", `{}`, "call-opaque-1")
	if got.Result != "ok" || callbacks.callID != "call-opaque-1" {
		t.Fatalf("result=%+v call id=%q", got, callbacks.callID)
	}
}

type epochAwareCallbacks struct {
	mockCallbacks
	epochs []string
	seen   ToolCallExecutionContext
}

// contextExecutorPreferredCallbacks has both executor forms on purpose. It
// proves a model response cannot accidentally fall through to the epoch-less
// structured compatibility API when the host supplies the request-bound one.
type contextExecutorPreferredCallbacks struct {
	mockCallbacks
	structuredCalls int
	seen            ToolCallExecutionContext
}

type requestSurfaceBreakdownCallbacks struct {
	mockCallbacks
	rendered  []map[string]interface{}
	breakdown []LoopInputBreakdown
	events    []ToolSurfaceEvent
}

type breakdownMutatingCallbacks struct {
	requestSurfaceBreakdownCallbacks
	mutated bool
}

type rotatingBreakdownMutatingCallbacks struct {
	mockCallbacks
	rendered  [][]map[string]interface{}
	current   []map[string]interface{}
	calls     int
	mutations int
}

type retryFreezeFailureCallbacks struct {
	mockCallbacks
	calls  int
	events []ToolSurfaceEvent
}

type rotatingRequestSurfaceCallbacks struct {
	mockCallbacks
	rendered [][]map[string]interface{}
	calls    int
}

// epochOrderedRequestSurfaceCallbacks records the epoch visible to the
// request-bound renderer. It proves every outbound successor gets its epoch
// before it replaces the callback's current tool surface.
type epochOrderedRequestSurfaceCallbacks struct {
	mockCallbacks
	rendered       [][]map[string]interface{}
	calls          int
	currentEpoch   string
	epochs         []string
	renderedEpochs []string
}

// replanEpochOrderedRequestSurfaceCallbacks makes live steering arrive only
// after the predecessor renderer has installed its request surface. It guards
// the ownership boundary that a replan shares with fallback and retry: the
// successor must invalidate the predecessor epoch before replacement render.
type replanEpochOrderedRequestSurfaceCallbacks struct {
	epochOrderedRequestSurfaceCallbacks
	replanPending atomic.Bool
	steered       atomic.Bool
	receipts      []ToolSurfaceReceipt
}

// rotatingLegacyStaticSurfaceCallbacks intentionally omits the request-local
// renderer. It models the S0.5 compatibility path, which must still rebuild
// its complete replacement surface for every real outbound request instead of
// reusing the loop's previous in-memory definitions.
type rotatingLegacyStaticSurfaceCallbacks struct {
	mockCallbacks
	rendered [][]map[string]interface{}
	calls    int
}

type rotatingReceiptSurfaceCallbacks struct {
	rotatingRequestSurfaceCallbacks
	receipts []ToolSurfaceReceipt
}

type receiptObservingCallbacks struct {
	mockCallbacks
	receipts []ToolSurfaceReceipt
	events   []ToolSurfaceEvent
}

type manifestMutatingCallbacks struct {
	receiptObservingCallbacks
	mutated bool
}

type boundManifestMutatingCallbacks struct {
	*boundChannelCallbacks
	mutated bool
}

type boundAuditEvidenceMutatingCallbacks struct {
	*boundChannelCallbacks
	evidence ToolSurfacePlanEvidence
	mutated  bool
}

func (m *receiptObservingCallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.receipts = append(m.receipts, receipt)
}

func (m *receiptObservingCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *manifestMutatingCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.receiptObservingCallbacks.OnToolSurfaceEvent(event)
	if m.mutated || event.Kind != ToolSurfaceEventManifestCreated || len(m.tools) == 0 {
		return
	}
	m.mutated = true
	function, _ := m.tools[0]["function"].(map[string]interface{})
	if function != nil {
		function["description"] = "mutated after manifest creation"
	}
}

func (m *boundManifestMutatingCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.boundChannelCallbacks.OnToolSurfaceEvent(event)
	if m.mutated || event.Kind != ToolSurfaceEventManifestCreated || len(m.tools) == 0 {
		return
	}
	m.mutated = true
	function, _ := m.tools[0]["function"].(map[string]interface{})
	if function != nil {
		function["description"] = "mutated after bound manifest creation"
	}
}

func (m *boundAuditEvidenceMutatingCallbacks) ToolSurfaceAuditEvidence(ToolCallExecutionContext) ToolSurfacePlanEvidence {
	return m.evidence
}

func (m *boundAuditEvidenceMutatingCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.boundChannelCallbacks.OnToolSurfaceEvent(event)
	if m.mutated || event.Kind != ToolSurfaceEventManifestCreated || len(m.evidence.Omitted) == 0 {
		return
	}
	m.mutated = true
	m.evidence.Omitted[0].ReasonCode = "mutated_after_manifest"
}

func (*manifestMutatingCallbacks) ContainToolSurfaceAmbiguousDelivery() bool { return true }

func (m *requestSurfaceBreakdownCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	return m.rendered
}

func (m *requestSurfaceBreakdownCallbacks) OnLoopInputBreakdown(breakdown LoopInputBreakdown) {
	m.breakdown = append(m.breakdown, breakdown)
}

func (m *requestSurfaceBreakdownCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *breakdownMutatingCallbacks) OnLoopInputBreakdown(breakdown LoopInputBreakdown) {
	m.requestSurfaceBreakdownCallbacks.OnLoopInputBreakdown(breakdown)
	if m.mutated || len(m.rendered) == 0 {
		return
	}
	m.mutated = true
	function, _ := m.rendered[0]["function"].(map[string]interface{})
	if function != nil {
		function["description"] = "mutated from input breakdown observer"
	}
}

func (m *rotatingBreakdownMutatingCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	if len(m.rendered) == 0 {
		return nil
	}
	index := m.calls
	m.calls++
	if index >= len(m.rendered) {
		index = len(m.rendered) - 1
	}
	m.current = m.rendered[index]
	return m.current
}

func (m *rotatingBreakdownMutatingCallbacks) OnLoopInputBreakdown(LoopInputBreakdown) {
	if len(m.current) == 0 {
		return
	}
	function, _ := m.current[0]["function"].(map[string]interface{})
	if function != nil {
		m.mutations++
		function["description"] = fmt.Sprintf("mutated after attempt %d", m.mutations)
	}
}

func (m *retryFreezeFailureCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	m.calls++
	if m.calls <= 2 {
		return []map[string]interface{}{tooldef.BuildToolDef("first_attempt", "first", map[string]interface{}{"type": "object"})}
	}
	return []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "unserializable_retry",
			"description": "must not send",
			"parameters":  map[string]interface{}{"type": "object", "invalid": func() {}},
		},
	}}
}

func (m *retryFreezeFailureCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *rotatingRequestSurfaceCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	if len(m.rendered) == 0 {
		return nil
	}
	index := m.calls
	m.calls++
	if index >= len(m.rendered) {
		index = len(m.rendered) - 1
	}
	return m.rendered[index]
}

func (m *epochOrderedRequestSurfaceCallbacks) BeginToolSurfaceEpoch(iteration int) string {
	epoch := fmt.Sprintf("epoch-%d-%d", iteration, len(m.epochs)+1)
	m.currentEpoch = epoch
	m.epochs = append(m.epochs, epoch)
	return epoch
}

func (m *epochOrderedRequestSurfaceCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	m.renderedEpochs = append(m.renderedEpochs, m.currentEpoch)
	if len(m.rendered) == 0 {
		return nil
	}
	index := m.calls
	m.calls++
	if index >= len(m.rendered) {
		index = len(m.rendered) - 1
	}
	return m.rendered[index]
}

func (m *replanEpochOrderedRequestSurfaceCallbacks) BuildToolsForModelRequest(userText string, iteration int) []map[string]interface{} {
	tools := m.epochOrderedRequestSurfaceCallbacks.BuildToolsForModelRequest(userText, iteration)
	if !m.steered.Swap(true) {
		m.replanPending.Store(true)
	}
	return tools
}

func (m *replanEpochOrderedRequestSurfaceCallbacks) LLMReplanRequested() bool {
	return m.replanPending.Load()
}

func (m *replanEpochOrderedRequestSurfaceCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	if !m.replanPending.Swap(false) {
		return nil
	}
	return append(conversation, map[string]string{"role": "user", "content": "steer"})
}

func (*replanEpochOrderedRequestSurfaceCallbacks) OnToolExecuted(string, string, string, bool) {}
func (*replanEpochOrderedRequestSurfaceCallbacks) OnEmptyResponse(int) bool                    { return false }

func (m *replanEpochOrderedRequestSurfaceCallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.receipts = append(m.receipts, receipt)
}

func (m *rotatingLegacyStaticSurfaceCallbacks) BuildTools(string) []map[string]interface{} {
	if len(m.rendered) == 0 {
		return nil
	}
	index := m.calls
	m.calls++
	if index >= len(m.rendered) {
		index = len(m.rendered) - 1
	}
	return m.rendered[index]
}

func (m *rotatingReceiptSurfaceCallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.receipts = append(m.receipts, receipt)
}

type deliveryObserverCallbacks struct {
	mockCallbacks
	containment bool
	starts      []ToolCallExecutionContext
	finishes    []ToolSurfaceDeliveryState
}

type responseBindingCallbacks struct {
	mockCallbacks
	boundResponseIDs []string
}

type boundChannelCallbacks struct {
	mockCallbacks
	channels            []*testToolSurfaceRequestChannel
	renderContexts      []ToolCallExecutionContext
	binderContexts      []ToolCallExecutionContext
	dispositions        []ToolSurfaceDisposition
	dispositionContexts []ToolCallExecutionContext
	executionContext    ToolCallExecutionContext
	receipts            []ToolSurfaceReceipt
	events              []ToolSurfaceEvent
}

type auditBoundChannelCallbacks struct {
	*boundChannelCallbacks
	evidence ToolSurfacePlanEvidence
}

// Bound-channel fixtures model correlation-bound dynamic surfaces, so their
// default evidence is available. Individual tests override this explicitly to
// prove missing/unavailable evidence fails before transport handoff.
func (*boundChannelCallbacks) ToolSurfaceAuditEvidence(ToolCallExecutionContext) ToolSurfacePlanEvidence {
	return ToolSurfacePlanEvidence{Available: true, PlanID: "bound-test-plan", PlanSnapshotDigest: "bound-test-snapshot"}
}

func (m *boundChannelCallbacks) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	m.receipts = append(m.receipts, receipt)
}

func (m *boundChannelCallbacks) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	m.events = append(m.events, event)
}

func (m *auditBoundChannelCallbacks) ToolSurfaceAuditEvidence(ToolCallExecutionContext) ToolSurfacePlanEvidence {
	return m.evidence
}

type missingEpochBoundChannelCallbacks struct{ *boundChannelCallbacks }

func (*missingEpochBoundChannelCallbacks) BeginToolSurfaceEpoch(int) string { return "" }

type missingRendererBoundChannelCallbacks struct {
	mockCallbacks
	channel      *testToolSurfaceRequestChannel
	dispositions []ToolSurfaceDisposition
}

func (*missingRendererBoundChannelCallbacks) BeginToolSurfaceEpoch(int) string {
	return "missing-renderer-epoch"
}
func (m *missingRendererBoundChannelCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	return m.channel, nil
}
func (m *missingRendererBoundChannelCallbacks) OnToolSurfaceDisposition(_ ToolCallExecutionContext, disposition ToolSurfaceDisposition) {
	m.dispositions = append(m.dispositions, disposition)
}

type invalidCorrelationBoundChannelCallbacks struct{ *boundChannelCallbacks }

func (m *invalidCorrelationBoundChannelCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	if len(m.channels) == 0 {
		return nil, fmt.Errorf("unexpected request channel reservation")
	}
	channel := m.channels[0]
	m.channels = m.channels[1:]
	return channel, nil
}

type unavailableAuditBoundChannelCallbacks struct{ *boundChannelCallbacks }

func (*unavailableAuditBoundChannelCallbacks) ToolSurfaceAuditEvidence(ToolCallExecutionContext) ToolSurfacePlanEvidence {
	return ToolSurfacePlanEvidence{}
}

type noAuditProviderBoundChannelCallbacks struct {
	mockCallbacks
	channel      *testToolSurfaceRequestChannel
	dispositions []ToolSurfaceDisposition
}

func (*noAuditProviderBoundChannelCallbacks) BeginToolSurfaceEpoch(int) string {
	return "no-audit-provider-epoch"
}
func (m *noAuditProviderBoundChannelCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	return m.channel, nil
}
func (m *noAuditProviderBoundChannelCallbacks) BuildToolsForBoundModelRequest(_ string, _ int, _ ToolCallExecutionContext) []map[string]interface{} {
	return m.tools
}
func (*noAuditProviderBoundChannelCallbacks) BindToolSurfaceResponse(ToolCallExecutionContext) error {
	return nil
}
func (m *noAuditProviderBoundChannelCallbacks) OnToolSurfaceDisposition(_ ToolCallExecutionContext, disposition ToolSurfaceDisposition) {
	m.dispositions = append(m.dispositions, disposition)
}

type noAuditEvidenceChannel struct {
	execution ToolCallExecutionContext
	response  *llm.Response
	sends     int
	closed    []error
}

func (c *noAuditEvidenceChannel) ExecutionContext() ToolCallExecutionContext { return c.execution }
func (c *noAuditEvidenceChannel) Do(_ context.Context, _ []interface{}, _ []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
	c.sends++
	return c.response, nil
}
func (c *noAuditEvidenceChannel) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (VerifiedToolSurfaceDispatch, error) {
	response, err := c.Do(ctx, conversation, tools, onToken, stream)
	receipt, receiptErr := VerifyToolSurfaceWirePayloadWithInvocationPolicy(tools, tools, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat))
	if receiptErr != nil {
		return VerifiedToolSurfaceDispatch{Receipt: receipt}, receiptErr
	}
	receipt.Handoff = ToolSurfaceHandoffStarted
	return VerifiedToolSurfaceDispatch{Response: response, Receipt: receipt}, err
}
func (c *noAuditEvidenceChannel) Close(cause error) { c.closed = append(c.closed, cause) }

type testToolSurfaceRequestChannel struct {
	execution                ToolCallExecutionContext
	response                 *llm.Response
	err                      error
	forceStartedNilResponse  bool
	returnEmptyReceipt       bool
	requiresPublicationProof bool
	sends                    int
	closed                   []error
	mutateTools              func([]map[string]interface{})
	mutateReceipt            func(*ToolSurfaceReceipt)
	auditEvidence            ToolSurfacePlanEvidence
	auditSet                 bool
	invocationPolicy         ToolSurfaceInvocationPolicy
	invocationPolicySet      bool
}

func (c *testToolSurfaceRequestChannel) ExecutionContext() ToolCallExecutionContext {
	return c.execution
}
func (c *testToolSurfaceRequestChannel) RequiresPublishedBoundToolSurface() bool {
	return c != nil && c.requiresPublicationProof
}
func (c *testToolSurfaceRequestChannel) Do(_ context.Context, _ []interface{}, tools []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
	c.sends++
	if c.mutateTools != nil {
		c.mutateTools(tools)
	}
	return c.response, c.err
}
func (c *testToolSurfaceRequestChannel) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (VerifiedToolSurfaceDispatch, error) {
	response, err := c.Do(ctx, conversation, tools, onToken, stream)
	if c.returnEmptyReceipt {
		return VerifiedToolSurfaceDispatch{Response: response}, err
	}
	policy := c.invocationPolicy
	if !c.invocationPolicySet {
		return VerifiedToolSurfaceDispatch{}, fmt.Errorf("surface_integrity_failure: test channel invocation policy was not set")
	}
	receipt, receiptErr := VerifyToolSurfaceWirePayloadWithAuditEvidence(tools, tools, policy, c.auditEvidence)
	if c.mutateReceipt != nil {
		c.mutateReceipt(&receipt)
	}
	if receiptErr != nil {
		return VerifiedToolSurfaceDispatch{Receipt: receipt}, receiptErr
	}
	if err != nil {
		receipt.Handoff = ToolSurfaceHandoffAmbiguous
		return VerifiedToolSurfaceDispatch{Response: response, Receipt: receipt}, err
	}
	if c.forceStartedNilResponse {
		receipt.Handoff = ToolSurfaceHandoffStarted
		return VerifiedToolSurfaceDispatch{Receipt: receipt}, nil
	}
	receipt.Handoff = ToolSurfaceHandoffStarted
	return VerifiedToolSurfaceDispatch{Response: response, Receipt: receipt}, nil
}
func (c *testToolSurfaceRequestChannel) SetToolSurfaceDispatchPreparation(preparation ToolSurfaceDispatchPreparation) error {
	evidence, err := NormalizeToolSurfacePlanEvidence(preparation.AuditEvidence)
	if err != nil {
		return err
	}
	policy, err := normalizeToolSurfaceInvocationPolicy(preparation.InvocationPolicy)
	if err != nil {
		return err
	}
	if c.auditSet && !toolSurfacePlanEvidenceEqualForTest(c.auditEvidence, evidence) {
		return fmt.Errorf("test channel dispatch preparation changed")
	}
	if c.invocationPolicySet && c.invocationPolicy != policy {
		return fmt.Errorf("test channel dispatch preparation changed")
	}
	c.auditEvidence, c.auditSet = evidence, true
	c.invocationPolicy, c.invocationPolicySet = policy, true
	return nil
}

func toolSurfacePlanEvidenceEqualForTest(left, right ToolSurfacePlanEvidence) bool {
	left, leftErr := NormalizeToolSurfacePlanEvidence(left)
	right, rightErr := NormalizeToolSurfacePlanEvidence(right)
	if leftErr != nil || rightErr != nil || left.Available != right.Available || left.PlanID != right.PlanID || left.PlanSnapshotDigest != right.PlanSnapshotDigest || left.CatalogGeneration != right.CatalogGeneration || len(left.Omitted) != len(right.Omitted) {
		return false
	}
	for index := range left.Omitted {
		if left.Omitted[index] != right.Omitted[index] {
			return false
		}
	}
	return true
}

func TestRunLoopBoundRequestChannelRejectsReceiptMismatchBeforeBinder(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-receipt-mismatch"},
		response:  &llm.Response{ResponseID: "response-should-not-bind", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "should not bind"}, FinishReason: "stop"}}},
		mutateReceipt: func(receipt *ToolSurfaceReceipt) {
			receipt.WirePayloadHash = "forged-wire-digest"
		},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.binderContexts) != 0 {
		t.Fatalf("binder received a response without a matching receipt: %#v", cb.binderContexts)
	}
	if !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("dispositions=%#v", cb.dispositions)
	}
	if channel.sends != 1 || len(channel.closed) != 1 {
		t.Fatalf("channel lifecycle=%#v", channel)
	}
}

func TestRunLoopBoundRequestChannelSynthesizesFailureReceiptWhenDispatchOmitsIt(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution:          ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-empty-dispatch-receipt"},
		response:           &llm.Response{ResponseID: "response-should-not-bind", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "should not bind"}, FinishReason: "stop"}}},
		returnEmptyReceipt: true,
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.binderContexts) != 0 || channel.sends != 1 || len(channel.closed) != 1 {
		t.Fatalf("binder/channel=%#v/%#v", cb.binderContexts, channel)
	}
	if !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("dispositions=%#v", cb.dispositions)
	}
	if len(cb.receipts) != 1 || cb.receipts[0].Verified || cb.receipts[0].Handoff != ToolSurfaceHandoffAmbiguous || cb.receipts[0].PayloadDigest != "" || !strings.Contains(cb.receipts[0].Failure, "returned no receipt") {
		t.Fatalf("receipt=%#v", cb.receipts)
	}
}

func TestRunLoopBoundRequestChannelRejectsStartedDispatchWithoutResponseBeforeBinder(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution:               ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-started-nil-response"},
		forceStartedNilResponse: true,
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") || !strings.Contains(result.Error, "without response or error") {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.binderContexts) != 0 || channel.sends != 1 || len(channel.closed) != 1 || !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("binder/channel/dispositions=%#v/%#v/%#v", cb.binderContexts, channel, cb.dispositions)
	}
}

func TestRequireLLMDispatchResponseRejectsNilResponse(t *testing.T) {
	if err := requireLLMDispatchResponse(nil); err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
		t.Fatalf("nil response error=%v", err)
	}
	if err := requireLLMDispatchResponse(&llm.Response{}); err != nil {
		t.Fatalf("response rejected: %v", err)
	}
}

func TestRunLoopBoundRequestChannelRejectsForgedAuditDigestBeforeBinder(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-audit-mismatch"},
		response:  &llm.Response{ResponseID: "response-should-not-bind", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "should not bind"}, FinishReason: "stop"}}},
		mutateReceipt: func(receipt *ToolSurfaceReceipt) {
			receipt.AuditDigest = "forged-audit-digest"
		},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &auditBoundChannelCallbacks{boundChannelCallbacks: base, evidence: ToolSurfacePlanEvidence{
		Available: true, PlanID: "plan-audit", PlanSnapshotDigest: "snapshot-audit", CatalogGeneration: 1,
		Omitted: []ToolSurfaceOmission{{NeedID: "optional", ReasonCode: "budget_exhausted"}},
	}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") {
		t.Fatalf("result=%+v", result)
	}
	if len(base.binderContexts) != 0 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) || channel.sends != 1 {
		t.Fatalf("binder/disposition/channel=%#v/%#v/%#v", base.binderContexts, base.dispositions, channel)
	}
}

type unpublishedBoundChannelCallbacks struct {
	*boundChannelCallbacks
	failure string
}

func (m *unpublishedBoundChannelCallbacks) RenderPublishedBoundToolSurface(_ string, _ int, execution ToolCallExecutionContext) BoundToolSurfaceRender {
	m.renderContexts = append(m.renderContexts, execution)
	return BoundToolSurfaceRender{Failure: m.failure}
}

func TestRunLoopBoundRequestChannelFailsClosedWhenRendererCannotPublishSurface(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-empty-bound-render"},
		response:  &llm.Response{ResponseID: "must-not-bind"},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &unpublishedBoundChannelCallbacks{boundChannelCallbacks: base, failure: "durable surface publication failed"}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "durable surface publication failed") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(base.binderContexts) != 0 || len(base.renderContexts) != 1 {
		t.Fatalf("unpublished bound surface dispatched or bound: channel=%#v binder=%#v renderer=%#v", channel, base.binderContexts, base.renderContexts)
	}
	if len(base.receipts) != 1 || base.receipts[0].Verified || base.receipts[0].Handoff != ToolSurfaceHandoffNotStarted || !strings.Contains(base.receipts[0].Failure, "publication failed") {
		t.Fatalf("receipts=%#v", base.receipts)
	}
	if len(channel.closed) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, base.dispositions)
	}
}

func TestRunLoopBoundRequestChannelRequiresPublishedSurfaceProofWhenReservationDemandsIt(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution:                ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-required-publication-proof"},
		response:                 &llm.Response{ResponseID: "must-not-bind"},
		requiresPublicationProof: true,
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(base, "test", nil, nil)
	if !strings.Contains(result.Error, "requires published surface proof") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(base.binderContexts) != 0 || len(base.renderContexts) != 0 {
		t.Fatalf("proof-required reservation sent or rendered legacy surface: channel=%#v binder=%#v renderer=%#v", channel, base.binderContexts, base.renderContexts)
	}
	if len(base.receipts) != 1 || base.receipts[0].Verified || !strings.Contains(base.receipts[0].Failure, "published surface proof") {
		t.Fatalf("receipts=%#v", base.receipts)
	}
	if len(channel.closed) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, base.dispositions)
	}
}

func TestRunLoopBoundRequestChannelFailsClosedWhenAuthorizerWouldClipPublishedSurface(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-authorizer-clips-bound-surface"},
		response:  &llm.Response{ResponseID: "must-not-bind"},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{
			tooldef.BuildToolDef("opaque_alias_allowed", "allowed", map[string]interface{}{"type": "object"}),
			tooldef.BuildToolDef("opaque_alias_rejected", "rejected", map[string]interface{}{"type": "object"}),
		},
		allowed: map[string]bool{"opaque_alias_allowed": true},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "bound tool surface definition is rejected") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(cb.binderContexts) != 0 || len(cb.renderContexts) != 1 {
		t.Fatalf("bound surface was clipped or bound: channel=%#v binder=%#v renderer=%#v", channel, cb.binderContexts, cb.renderContexts)
	}
	if len(cb.receipts) != 1 || cb.receipts[0].Verified || cb.receipts[0].Handoff != ToolSurfaceHandoffNotStarted || !strings.Contains(cb.receipts[0].Failure, "authorizer") {
		t.Fatalf("receipts=%#v", cb.receipts)
	}
	if len(channel.closed) != 1 || !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, cb.dispositions)
	}
}

func TestRunLoopBoundRequestChannelFailsClosedWhenAuditProviderCannotSetEvidence(t *testing.T) {
	channel := &noAuditEvidenceChannel{execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-no-audit-setter"}, response: &llm.Response{ResponseID: "must-not-send"}}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}}
	// The channel slice uses the embedded test seam for reservation, but its
	// wrapper deliberately hides SetToolSurfaceAuditEvidence at the interface
	// boundary below.
	base.channels = nil
	cb := &auditBoundChannelCallbacks{boundChannelCallbacks: base, evidence: ToolSurfacePlanEvidence{Available: true, PlanID: "plan", PlanSnapshotDigest: "snapshot"}}
	// A bespoke provider avoids mutating the normal test channel's interface.
	provider := &auditNoSetterCallbacks{auditBoundChannelCallbacks: cb, channel: channel}
	result := RunLoop(provider, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") || channel.sends != 0 || len(base.binderContexts) != 0 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("result=%+v channel=%#v binder=%#v dispositions=%#v", result, channel, base.binderContexts, base.dispositions)
	}
	manifestEvents, integrityEvents, terminalEvents := 0, 0, 0
	var manifest, terminal ToolSurfaceEvent
	for _, event := range base.events {
		switch event.Kind {
		case ToolSurfaceEventManifestCreated:
			manifestEvents++
			manifest = event
		case ToolSurfaceEventIntegrityFailure:
			integrityEvents++
		case ToolSurfaceEventTerminalReason:
			terminalEvents++
			terminal = event
		}
	}
	if manifestEvents != 1 || integrityEvents != 1 || terminalEvents != 1 {
		t.Fatalf("pre-dispatch lifecycle events=%+v", base.events)
	}
	if terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.PayloadDigest != manifest.PayloadDigest || terminal.AuditDigest != manifest.AuditDigest || terminal.ExpectedToolCount != manifest.ExpectedToolCount || terminal.ReplacementMode != manifest.ReplacementMode {
		t.Fatalf("terminal must stay anchored to the emitted manifest: manifest=%+v terminal=%+v", manifest, terminal)
	}
}

func TestRunLoopBoundRequestChannelDisposesMissingEpochBeforeRender(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-missing-epoch"},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &missingEpochBoundChannelCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "bound tool surface epoch is required" {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(channel.closed) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, base.dispositions)
	}
	if len(base.receipts) != 1 || base.receipts[0].Verified || base.receipts[0].Handoff != ToolSurfaceHandoffNotStarted || !strings.Contains(base.receipts[0].Failure, "epoch is required") {
		t.Fatalf("receipts=%#v", base.receipts)
	}
	var terminal ToolSurfaceEvent
	for _, event := range base.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminal = event
		}
	}
	if terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("pre-manifest terminal must not borrow a surface digest: %+v", terminal)
	}
}

func TestRunLoopBoundRequestChannelDisposesMissingCorrelationBeforeRender(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{execution: ToolCallExecutionContext{Protocol: "reviewed-protocol"}}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &invalidCorrelationBoundChannelCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "tool surface channel correlation is required" {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(channel.closed) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, base.dispositions)
	}
}

func TestRunLoopBoundRequestChannelDisposesMissingRendererBeforeSend(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-missing-renderer"},
	}
	cb := &missingRendererBoundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channel: channel}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "bound tool surface renderer is required" {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(channel.closed) != 1 || !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, cb.dispositions)
	}
}

func TestRunLoopBoundRequestChannelFailsClosedWithoutAuditEvidenceProvider(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-no-audit-provider"},
	}
	cb := &noAuditProviderBoundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channel: channel}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "lacks audit evidence provider") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(channel.closed) != 1 || !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, cb.dispositions)
	}
}

func TestRunLoopBoundRequestChannelFailsClosedWithUnavailableAuditEvidence(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-unavailable-audit"},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &unavailableAuditBoundChannelCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "has unavailable audit evidence") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 0 || len(channel.closed) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/dispositions=%#v/%#v", channel, base.dispositions)
	}
	if len(base.receipts) != 1 || base.receipts[0].Verified || base.receipts[0].Handoff != ToolSurfaceHandoffNotStarted || !strings.Contains(base.receipts[0].Failure, "unavailable audit evidence") {
		t.Fatalf("receipts=%#v", base.receipts)
	}
}

type auditNoSetterCallbacks struct {
	*auditBoundChannelCallbacks
	channel *noAuditEvidenceChannel
}

func (m *auditNoSetterCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	return m.channel, nil
}
func (c *testToolSurfaceRequestChannel) Close(cause error) { c.closed = append(c.closed, cause) }

func (m *boundChannelCallbacks) BeginToolSurfaceEpoch(iteration int) string {
	return fmt.Sprintf("bound-surface-%d", iteration)
}

func (m *boundChannelCallbacks) ReserveToolSurfaceRequestChannel(context.Context, corelib.MaclawLLMConfig) (ToolSurfaceRequestChannel, error) {
	if len(m.channels) == 0 {
		return nil, fmt.Errorf("unexpected request channel reservation")
	}
	channel := m.channels[0]
	m.channels = m.channels[1:]
	return channel, nil
}

func (m *boundChannelCallbacks) BuildToolsForBoundModelRequest(_ string, _ int, execution ToolCallExecutionContext) []map[string]interface{} {
	m.renderContexts = append(m.renderContexts, execution)
	return m.tools
}

func (m *boundChannelCallbacks) BindToolSurfaceResponse(execution ToolCallExecutionContext) error {
	m.binderContexts = append(m.binderContexts, execution)
	return nil
}

func (m *boundChannelCallbacks) OnToolSurfaceDisposition(execution ToolCallExecutionContext, disposition ToolSurfaceDisposition) {
	m.dispositionContexts = append(m.dispositionContexts, execution)
	m.dispositions = append(m.dispositions, disposition)
}

func (m *boundChannelCallbacks) ExecuteToolCallWithContext(name, args, _ string, execution ToolCallExecutionContext) ToolExecutionResult {
	m.executionContext = execution
	return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: ToolExecutionOutcomeOK}
}

func (m *responseBindingCallbacks) BindToolSurfaceResponse(execution ToolCallExecutionContext) error {
	m.boundResponseIDs = append(m.boundResponseIDs, execution.ResponseID)
	return nil
}

func TestRunLoopBoundRequestChannelUsesOneTransportReservationForRenderBindAndDispatch(t *testing.T) {
	first := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-a"},
		response:  &llm.Response{ResponseID: "response-a", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-a", Type: "function", Function: llm.ToolCallFunction{Name: "opaque_alias", Arguments: `{}`}}}}, FinishReason: "tool_calls"}}},
	}
	second := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-b"},
		response:  &llm.Response{ResponseID: "response-b", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}}},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 2, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})}, toolResult: "ok",
	}, channels: []*testToolSurfaceRequestChannel{first, second}}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if first.sends != 1 || second.sends != 1 || len(first.closed) != 1 || len(second.closed) != 1 {
		t.Fatalf("channel lifecycle first=%#v second=%#v", first, second)
	}
	if len(cb.renderContexts) != 2 || len(cb.binderContexts) != 2 {
		t.Fatalf("render=%#v binder=%#v", cb.renderContexts, cb.binderContexts)
	}
	if got := cb.renderContexts[0]; got.Protocol != "reviewed-protocol" || got.ConnectionID != "connection-a" || got.SurfaceEpoch == "" {
		t.Fatalf("first render context=%+v", got)
	}
	if got := cb.binderContexts[0]; got.ResponseID != "response-a" || got.ConnectionID != "connection-a" || got.SurfaceEpoch != cb.renderContexts[0].SurfaceEpoch {
		t.Fatalf("first binder context=%+v", got)
	}
	if got := cb.executionContext; got.ResponseID != "response-a" || got.ConnectionID != "connection-a" || got.Protocol != "reviewed-protocol" || got.SurfaceEpoch != cb.renderContexts[0].SurfaceEpoch {
		t.Fatalf("tool execution context=%+v", got)
	}
	if cb.renderContexts[1].ConnectionID != "connection-b" || cb.renderContexts[1].SurfaceEpoch == cb.renderContexts[0].SurfaceEpoch {
		t.Fatalf("successor reservation was not isolated: %#v", cb.renderContexts)
	}
	if got := cb.dispositions; !slices.Equal(got, []ToolSurfaceDisposition{ToolSurfaceToolBatchSettled, ToolSurfaceResponseSettled}) {
		t.Fatalf("dispositions=%#v", got)
	}
	if len(cb.dispositionContexts) != 2 || cb.dispositionContexts[0].ResponseID != "response-a" || cb.dispositionContexts[1].ResponseID != "response-b" {
		t.Fatalf("disposition contexts=%#v", cb.dispositionContexts)
	}
	manifestEvents, verifiedEvents, terminalEvents := 0, 0, 0
	for _, event := range cb.events {
		switch event.Kind {
		case ToolSurfaceEventManifestCreated:
			manifestEvents++
			if event.AuditDigest == "" || event.PayloadDigest == "" || event.ExpectedToolCount != 1 {
				t.Fatalf("manifest event=%+v", event)
			}
		case ToolSurfaceEventPayloadVerified:
			verifiedEvents++
		case ToolSurfaceEventTerminalReason:
			terminalEvents++
			if event.TerminalReason == "" || event.PayloadDigest == "" || event.AuditDigest == "" || event.ExpectedToolCount != 1 || event.ReplacementMode != "replace" {
				t.Fatalf("terminal event=%+v", event)
			}
		}
	}
	if manifestEvents != 2 || verifiedEvents != 2 || terminalEvents != 2 {
		t.Fatalf("lifecycle events=%+v", cb.events)
	}
}

func TestRunLoopVerifiesStaticSurfaceForEachOutboundRequest(t *testing.T) {
	var wireTools [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		wireTools = append(wireTools, names)
		if len(wireTools) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &receiptObservingCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:    2,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"}), tooldef.BuildToolDef("search", "search", map[string]interface{}{"type": "object"})},
		toolResult: "result",
	}}
	result := RunLoop(cb, "inspect", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(wireTools) != 2 || !slices.Equal(wireTools[0], []string{"read_file", "search"}) || !slices.Equal(wireTools[1], []string{"read_file", "search"}) {
		t.Fatalf("wire tool surfaces=%#v", wireTools)
	}
	if len(cb.receipts) != 2 {
		t.Fatalf("receipts=%+v", cb.receipts)
	}
	for index, receipt := range cb.receipts {
		if !receipt.Verified || receipt.ExpectedToolCount != 2 || receipt.WireToolCount != 2 || receipt.PayloadDigest == "" || receipt.PayloadDigest != receipt.WirePayloadHash {
			t.Fatalf("receipt[%d]=%+v", index, receipt)
		}
	}
	manifestEvents, verifiedEvents := 0, 0
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventManifestCreated {
			manifestEvents++
			if event.PayloadDigest == "" || event.ExpectedToolCount != 2 || event.ReplacementMode != "replace" {
				t.Fatalf("manifest event=%+v", event)
			}
		}
		if event.Kind == ToolSurfaceEventPayloadVerified {
			verifiedEvents++
			if event.PayloadDigest == "" || event.WireToolCount != 2 || event.FailureKind != "" {
				t.Fatalf("verified event=%+v", event)
			}
		}
	}
	if manifestEvents != 2 || verifiedEvents != 2 {
		t.Fatalf("lifecycle events=%+v", cb.events)
	}
	terminalEvents := 0
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminalEvents++
			if event.TerminalReason == "" {
				t.Fatalf("terminal event=%+v", event)
			}
		}
	}
	if terminalEvents != 2 {
		t.Fatalf("terminal events=%+v", cb.events)
	}
}

func TestRunLoopRebuildsLegacyStaticSurfaceForEachOutboundRequest(t *testing.T) {
	var wireTools [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		wireTools = append(wireTools, names)
		if len(wireTools) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-legacy-static","type":"function","function":{"name":"round_one","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &rotatingLegacyStaticSurfaceCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 2, sysPrompt: "sys", toolResult: "ok",
		},
		rendered: [][]map[string]interface{}{
			{tooldef.BuildToolDef("round_one", "first outbound request", map[string]interface{}{"type": "object"})},
			{tooldef.BuildToolDef("round_two", "second outbound request", map[string]interface{}{"type": "object"})},
		},
	}
	result := RunLoop(cb, "inspect", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(wireTools) != 2 || !slices.Equal(wireTools[0], []string{"round_one"}) || !slices.Equal(wireTools[1], []string{"round_two"}) {
		t.Fatalf("legacy static requests reused stale surface: got=%v", wireTools)
	}
	if cb.calls != 2 {
		t.Fatalf("legacy static surface was not rebuilt at each request boundary: calls=%d", cb.calls)
	}
}

func TestRunLoopProjectsHostOwnedInvocationPolicyToChatWireAndReceipt(t *testing.T) {
	var wire map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	parallel := false
	cb := &invocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
			tools: []map[string]interface{}{tooldef.BuildToolDef("only_tool", "test", map[string]interface{}{"type": "object"})},
		},
		policy: ToolSurfaceInvocationPolicy{
			Envelope:          ToolSurfaceEnvelopeOpenAIChat,
			ToolChoice:        ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "only_tool"},
			ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: parallel},
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	choice, _ := wire["tool_choice"].(map[string]interface{})
	function, _ := choice["function"].(map[string]interface{})
	if choice["type"] != "function" || function["name"] != "only_tool" || wire["parallel_tool_calls"] != false {
		t.Fatalf("wire invocation policy=%#v", wire)
	}
}

func TestRunLoopFallbackReprojectsSuccessorChatInvocationPolicy(t *testing.T) {
	var wires []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		wires = append(wires, wire)
		if len(wires) == 1 {
			http.Error(w, "streaming format unsupported", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &rotatingInvocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
			tools: []map[string]interface{}{tooldef.BuildToolDef("only_tool", "test", map[string]interface{}{"type": "object"})},
		},
		policies: []ToolSurfaceInvocationPolicy{
			{Envelope: ToolSurfaceEnvelopeOpenAIChat, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceAuto}, ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: true}},
			{Envelope: ToolSurfaceEnvelopeOpenAIChat, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "only_tool"}, ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: false}},
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" || len(wires) != 2 {
		t.Fatalf("result=%+v wires=%#v", result, wires)
	}
	firstChoice, _ := wires[0]["tool_choice"].(string)
	if firstChoice != "auto" || wires[0]["parallel_tool_calls"] != true {
		t.Fatalf("predecessor wire policy=%#v", wires[0])
	}
	secondChoice, _ := wires[1]["tool_choice"].(map[string]interface{})
	secondFunction, _ := secondChoice["function"].(map[string]interface{})
	if secondChoice["type"] != "function" || secondFunction["name"] != "only_tool" || wires[1]["parallel_tool_calls"] != false {
		t.Fatalf("fallback did not use successor policy: %#v", wires[1])
	}
}

func TestRunLoopFallbackReprojectsSuccessorResponsesInvocationPolicy(t *testing.T) {
	var wires []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		wires = append(wires, wire)
		if len(wires) == 1 {
			http.Error(w, "streaming format unsupported", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"id":"resp-successor-policy","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	defer server.Close()
	cb := &rotatingInvocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"}, maxIter: 1, sysPrompt: "sys",
			tools: []map[string]interface{}{tooldef.BuildToolDef("only_tool", "test", map[string]interface{}{"type": "object"})},
		},
		policies: []ToolSurfaceInvocationPolicy{
			{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceAuto}, ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: true}},
			{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "only_tool"}, ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: false}},
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" || len(wires) != 2 {
		t.Fatalf("result=%+v wires=%#v", result, wires)
	}
	if firstChoice, _ := wires[0]["tool_choice"].(string); firstChoice != "auto" || wires[0]["parallel_tool_calls"] != true {
		t.Fatalf("predecessor wire policy=%#v", wires[0])
	}
	secondChoice, _ := wires[1]["tool_choice"].(map[string]interface{})
	if secondChoice["type"] != "function" || secondChoice["name"] != "only_tool" || wires[1]["parallel_tool_calls"] != false {
		t.Fatalf("fallback did not use successor policy: %#v", wires[1])
	}
}

func TestRunLoopFallbackSuccessorPolicyFailureHasOwnIntegrityTerminal(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "streaming format unsupported", http.StatusBadRequest)
	}))
	defer server.Close()
	cb := &rotatingInvocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
			tools: []map[string]interface{}{tooldef.BuildToolDef("only_tool", "test", map[string]interface{}{"type": "object"})},
		},
		policies: []ToolSurfaceInvocationPolicy{
			{Envelope: ToolSurfaceEnvelopeOpenAIChat, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceAuto}},
			{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceAuto}},
		},
	}
	var events []ToolSurfaceEvent
	observing := &fallbackPolicyFailureCallbacks{rotatingInvocationPolicyCallbacks: cb, events: &events}
	result := RunLoop(observing, "test", nil, server.Client())
	if !strings.Contains(result.Error, "surface_integrity_failure") || requests.Load() != 1 {
		t.Fatalf("result=%+v requests=%d", result, requests.Load())
	}
	var terminals []ToolSurfaceEvent
	for _, event := range events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminals = append(terminals, event)
		}
	}
	if len(terminals) != 2 {
		t.Fatalf("terminal events=%+v, want predecessor plus failed successor", terminals)
	}
	if predecessor := terminals[0]; predecessor.TerminalReason != ToolSurfaceTransportFailure || predecessor.PayloadDigest == "" {
		t.Fatalf("predecessor terminal=%+v", predecessor)
	}
	if successor := terminals[1]; successor.TerminalReason != ToolSurfaceIntegrityFailure || successor.PayloadDigest != "" || successor.AuditDigest != "" || successor.ExpectedToolCount != 0 || successor.ReplacementMode != "" || successor.FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("failed successor terminal=%+v", successor)
	}
}

func TestRunLoopFallbackSuccessorLifecycleFailureHasOwnIntegrityTerminal(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "streaming format unsupported", http.StatusBadRequest)
	}))
	defer server.Close()
	cb := &fallbackLifecycleFailureCallbacks{mockCallbacks: &mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if !strings.Contains(result.Error, "surface_integrity_failure") || requests.Load() != 1 || cb.calls != 2 {
		t.Fatalf("result=%+v requests=%d renders=%d", result, requests.Load(), cb.calls)
	}
	var terminals []ToolSurfaceEvent
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminals = append(terminals, event)
		}
	}
	if len(terminals) != 2 {
		t.Fatalf("terminal events=%+v, want predecessor plus failed successor", terminals)
	}
	if predecessor := terminals[0]; predecessor.TerminalReason != ToolSurfaceTransportFailure || predecessor.PayloadDigest == "" {
		t.Fatalf("predecessor terminal=%+v", predecessor)
	}
	if successor := terminals[1]; successor.TerminalReason != ToolSurfaceIntegrityFailure || successor.PayloadDigest != "" || successor.AuditDigest != "" || successor.ExpectedToolCount != 0 || successor.ReplacementMode != "" || successor.FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("failed successor terminal=%+v", successor)
	}
}

func TestRunLoopProjectsHostOwnedInvocationPolicyToResponsesWireAndReceipt(t *testing.T) {
	var wire map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"id":"resp-policy","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	defer server.Close()
	cb := &invocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"}, maxIter: 1, sysPrompt: "sys",
			tools: []map[string]interface{}{tooldef.BuildToolDef("only_tool", "test", map[string]interface{}{"type": "object"})},
		},
		policy: ToolSurfaceInvocationPolicy{
			Envelope:          ToolSurfaceEnvelopeResponses,
			ToolChoice:        ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "only_tool"},
			ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: false},
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	choice, _ := wire["tool_choice"].(map[string]interface{})
	if choice["type"] != "function" || choice["name"] != "only_tool" || wire["parallel_tool_calls"] != false {
		t.Fatalf("wire invocation policy=%#v", wire)
	}
}

func TestRunLoopRejectsHostInvocationPolicyForAnthropicBeforeSend(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	cb := &invocationPolicyCallbacks{
		mockCallbacks: mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Protocol: "anthropic"}, maxIter: 1, sysPrompt: "sys"},
		policy: ToolSurfaceInvocationPolicy{
			Envelope:   ToolSurfaceEnvelopeAnthropic,
			ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceRequired},
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if !strings.Contains(result.Error, "surface_integrity_failure") || requests.Load() != 0 {
		t.Fatalf("result=%+v requests=%d", result, requests.Load())
	}
}

func TestRunLoopRejectsHostInvocationPolicyForWrongEnvelopeBeforeSend(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	cb := &receiptObservingCallbacks{mockCallbacks: mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys"}}
	policy := &invocationPolicyCallbacks{
		mockCallbacks: cb.mockCallbacks,
		policy:        DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeResponses),
	}
	observing := &policyEventCallbacks{invocationPolicyCallbacks: policy, events: &cb.events}
	result := RunLoop(observing, "test", nil, server.Client())
	if !strings.Contains(result.Error, "surface_integrity_failure") || !strings.Contains(result.Error, "does not match request envelope") || requests.Load() != 0 {
		t.Fatalf("result=%+v requests=%d", result, requests.Load())
	}
	if len(cb.events) != 2 || cb.events[0].Kind != ToolSurfaceEventIntegrityFailure || cb.events[0].FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("events=%+v, want redacted integrity failure plus terminal", cb.events)
	}
	if terminal := cb.events[1]; terminal.Kind != ToolSurfaceEventTerminalReason || terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.FailureKind != ToolSurfaceFailureIntegrity || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.ExpectedToolCount != 0 || terminal.ReplacementMode != "" {
		t.Fatalf("policy preflight terminal=%+v", terminal)
	}
}

func TestRunLoopReserveFailureClosesUncorrelatedIntegrityLifecycle(t *testing.T) {
	var sends atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, fmt.Errorf("unexpected transport send")
	})}
	cb := &reserveFailureCallbacks{mockCallbacks: &mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, client)
	if !strings.Contains(result.Error, "surface_integrity_failure") || !strings.Contains(result.Error, "reservation unavailable") || cb.reserveCalls != 1 || sends.Load() != 0 {
		t.Fatalf("result=%+v reserves=%d sends=%d", result, cb.reserveCalls, sends.Load())
	}
	if len(cb.events) != 2 || cb.events[0].Kind != ToolSurfaceEventIntegrityFailure || cb.events[0].FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("events=%+v, want redacted integrity failure plus terminal", cb.events)
	}
	if terminal := cb.events[1]; terminal.Kind != ToolSurfaceEventTerminalReason || terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.FailureKind != ToolSurfaceFailureIntegrity || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.ExpectedToolCount != 0 || terminal.ReplacementMode != "" {
		t.Fatalf("reserve-failure terminal=%+v", terminal)
	}
}

func TestRunLoopRequestContextFailureClosesUncorrelatedIntegrityLifecycle(t *testing.T) {
	var sends atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, fmt.Errorf("unexpected transport send")
	})}
	cb := &contextFailureCallbacks{mockCallbacks: &mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, client)
	if !strings.Contains(result.Error, "surface_integrity_failure") || !strings.Contains(result.Error, "request context unavailable") || cb.contextCalls != 1 || sends.Load() != 0 {
		t.Fatalf("result=%+v contexts=%d sends=%d", result, cb.contextCalls, sends.Load())
	}
	if len(cb.events) != 2 || cb.events[0].Kind != ToolSurfaceEventIntegrityFailure || cb.events[0].FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("events=%+v, want redacted integrity failure plus terminal", cb.events)
	}
	if terminal := cb.events[1]; terminal.Kind != ToolSurfaceEventTerminalReason || terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.FailureKind != ToolSurfaceFailureIntegrity || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.ExpectedToolCount != 0 || terminal.ReplacementMode != "" {
		t.Fatalf("context-failure terminal=%+v", terminal)
	}
}

func TestRunLoopStaticTerminalUsesImmutableManifestAfterObserverMutation(t *testing.T) {
	var transportCalls atomic.Int64
	var wireDescription string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		transportCalls.Add(1)
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		function, _ := payload.Tools[0]["function"].(map[string]interface{})
		wireDescription, _ = function["description"].(string)
		return nil, fmt.Errorf("simulated post-handoff transport failure")
	})}
	definitions := []map[string]interface{}{tooldef.BuildToolDef("read_file", "original", map[string]interface{}{"type": "object"})}
	cb := &manifestMutatingCallbacks{receiptObservingCallbacks: receiptObservingCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys", tools: definitions,
	}}}

	result := RunLoop(cb, "inspect", nil, client)
	if !strings.Contains(result.Error, "simulated post-handoff transport failure") || transportCalls.Load() != 1 {
		t.Fatalf("result=%+v transport=%d", result, transportCalls.Load())
	}
	if wireDescription != "original" {
		t.Fatalf("wire description=%q, want frozen pre-observer definition", wireDescription)
	}
	if len(cb.receipts) != 1 || !cb.receipts[0].Verified || cb.receipts[0].ExpectedToolCount != 1 || cb.receipts[0].ManifestDigest == "" || cb.receipts[0].Handoff != ToolSurfaceHandoffAmbiguous {
		t.Fatalf("receipt must preserve and send the pre-mutation immutable manifest: %+v", cb.receipts)
	}
	var manifest, terminal ToolSurfaceEvent
	for _, event := range cb.events {
		switch event.Kind {
		case ToolSurfaceEventManifestCreated:
			manifest = event
		case ToolSurfaceEventTerminalReason:
			terminal = event
		}
	}
	if manifest.PayloadDigest == "" || cb.receipts[0].ManifestDigest != manifest.PayloadDigest || terminal.TerminalReason != ToolSurfaceTransportFailure || terminal.PayloadDigest != manifest.PayloadDigest || terminal.AuditDigest != manifest.AuditDigest || terminal.ExpectedToolCount != manifest.ExpectedToolCount || terminal.ReplacementMode != manifest.ReplacementMode {
		t.Fatalf("terminal must retain the original immutable manifest after observer mutation: manifest=%+v terminal=%+v", manifest, terminal)
	}
}

func TestRunLoopStaticUsesFrozenSurfaceBeforeInputBreakdownObserver(t *testing.T) {
	var wireDescription string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			return nil, err
		}
		function, _ := payload.Tools[0]["function"].(map[string]interface{})
		wireDescription, _ = function["description"].(string)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))}, nil
	})}
	rendered := []map[string]interface{}{tooldef.BuildToolDef("read_file", "original", map[string]interface{}{"type": "object"})}
	cb := &breakdownMutatingCallbacks{requestSurfaceBreakdownCallbacks: requestSurfaceBreakdownCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, rendered: rendered}}

	result := RunLoop(cb, "inspect", nil, client)
	if result.Error != "" || result.Text != "done" || !cb.mutated {
		t.Fatalf("result=%+v mutated=%v", result, cb.mutated)
	}
	if wireDescription != "original" {
		t.Fatalf("wire description=%q, want pre-breakdown snapshot", wireDescription)
	}
	if len(cb.breakdown) != 1 || len(cb.events) == 0 {
		t.Fatalf("breakdown/events=%#v/%#v", cb.breakdown, cb.events)
	}
}

func TestRunLoopBoundDispatchUsesFrozenManifestAfterObserverMutation(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-bound-manifest-mutation"},
		response:  &llm.Response{ResponseID: "response-bound-manifest-mutation", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}}},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "original", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &boundManifestMutatingCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "" || result.Text != "done" || channel.sends != 1 {
		t.Fatalf("result=%+v channel=%#v", result, channel)
	}
	if !cb.mutated {
		t.Fatal("test observer did not mutate callback-owned definitions")
	}
	if len(base.binderContexts) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceResponseSettled}) {
		t.Fatalf("binder/dispositions=%#v/%#v", base.binderContexts, base.dispositions)
	}
	if len(base.receipts) != 1 || !base.receipts[0].Verified {
		t.Fatalf("receipts=%#v", base.receipts)
	}
	var manifest, terminal ToolSurfaceEvent
	for _, event := range base.events {
		switch event.Kind {
		case ToolSurfaceEventManifestCreated:
			manifest = event
		case ToolSurfaceEventTerminalReason:
			terminal = event
		}
	}
	if manifest.PayloadDigest == "" || base.receipts[0].PayloadDigest != manifest.PayloadDigest || terminal.PayloadDigest != manifest.PayloadDigest || terminal.AuditDigest != manifest.AuditDigest {
		t.Fatalf("bound lifecycle diverged after observer mutation: manifest=%+v receipt=%+v terminal=%+v", manifest, base.receipts[0], terminal)
	}
}

func TestRunLoopBoundDispatchUsesFrozenAuditEvidenceBeforeManifestObserver(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-bound-audit-mutation"},
		response:  &llm.Response{ResponseID: "response-bound-audit-mutation", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}}},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "original", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &boundAuditEvidenceMutatingCallbacks{
		boundChannelCallbacks: base,
		evidence: ToolSurfacePlanEvidence{
			Available:          true,
			PlanID:             "bound-audit-freeze-plan",
			PlanSnapshotDigest: "bound-audit-freeze-snapshot",
			Omitted:            []ToolSurfaceOmission{{NeedID: "network", ReasonCode: "policy_denied"}},
		},
	}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "" || result.Text != "done" || channel.sends != 1 || !cb.mutated {
		t.Fatalf("result=%+v channel=%#v mutated=%v", result, channel, cb.mutated)
	}
	if len(base.binderContexts) != 1 || !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceResponseSettled}) {
		t.Fatalf("binder/dispositions=%#v/%#v", base.binderContexts, base.dispositions)
	}
	var manifest ToolSurfaceEvent
	for _, event := range base.events {
		if event.Kind == ToolSurfaceEventManifestCreated {
			manifest = event
		}
	}
	if manifest.AuditDigest == "" || len(base.receipts) != 1 || base.receipts[0].AuditDigest != manifest.AuditDigest || channel.auditEvidence.Omitted[0].ReasonCode != "policy_denied" {
		t.Fatalf("audit evidence diverged after observer mutation: manifest=%+v receipts=%+v channel=%+v", manifest, base.receipts, channel.auditEvidence)
	}
}

func TestRunLoopBoundDispatchRejectsChannelMutationOfItsInputSnapshot(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-bound-channel-mutation"},
		response:  &llm.Response{ResponseID: "response-bound-channel-mutation", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "must-not-bind"}, FinishReason: "stop"}}},
		mutateTools: func(tools []map[string]interface{}) {
			function, _ := tools[0]["function"].(map[string]interface{})
			function["description"] = "channel mutated its private input"
		},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "original", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure") {
		t.Fatalf("result=%+v", result)
	}
	if channel.sends != 1 || len(cb.binderContexts) != 0 || !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceIntegrityFailure}) {
		t.Fatalf("channel/binder/dispositions=%#v/%#v/%#v", channel, cb.binderContexts, cb.dispositions)
	}
	function, _ := cb.tools[0]["function"].(map[string]interface{})
	got, _ := function["description"].(string)
	if got != "original" {
		t.Fatalf("channel mutation escaped its request-local copy: description=%q", got)
	}
}

func TestRunLoopBoundRequestChannelDoesNotCreateHiddenFallbackOrRetry(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-a"},
		err:       fmt.Errorf("temporary transport failure"),
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error == "" {
		t.Fatalf("failed channel result=%+v", result)
	}
	if channel.sends != 1 || len(channel.closed) != 1 || len(cb.renderContexts) != 1 {
		t.Fatalf("bound channel created successor attempt: channel=%#v renders=%#v", channel, cb.renderContexts)
	}
	if !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceTransportFailure}) {
		t.Fatalf("failed request disposition=%#v", cb.dispositions)
	}
}

func TestRunLoopBoundRequestChannelAbandonsEmptyResponseExactlyOnce(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-empty"},
		response:  &llm.Response{ResponseID: "response-empty"},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "LLM returned no choices" {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceResponseAbandoned}) {
		t.Fatalf("empty response dispositions=%#v", cb.dispositions)
	}
	if len(cb.dispositionContexts) != 1 || cb.dispositionContexts[0].ResponseID != "response-empty" {
		t.Fatalf("empty response context=%#v", cb.dispositionContexts)
	}
}

type finalizationDispositionCallbacks struct {
	*boundChannelCallbacks
	finalizeCalls int
}

type failingBoundChannelCallbacks struct {
	*boundChannelCallbacks
}

func (m *failingBoundChannelCallbacks) BindToolSurfaceResponse(execution ToolCallExecutionContext) error {
	m.binderContexts = append(m.binderContexts, execution)
	return fmt.Errorf("durable response bind failed")
}

func (m *finalizationDispositionCallbacks) TryFinalizeLLMResponse() bool {
	m.finalizeCalls++
	return m.finalizeCalls > 1
}

func TestRunLoopBoundRequestChannelDisposesSteeredResponseThenSettlesSuccessor(t *testing.T) {
	first := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-steered"},
		response:  &llm.Response{ResponseID: "response-steered", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "stale"}, FinishReason: "stop"}}},
	}
	second := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-final"},
		response:  &llm.Response{ResponseID: "response-final", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "final"}, FinishReason: "stop"}}},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, channels: []*testToolSurfaceRequestChannel{first, second}}
	cb := &finalizationDispositionCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "" || result.Text != "final" {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceSteered, ToolSurfaceResponseSettled}) {
		t.Fatalf("steer dispositions=%#v", base.dispositions)
	}
	if len(base.dispositionContexts) != 2 || base.dispositionContexts[0].ResponseID != "response-steered" || base.dispositionContexts[1].ResponseID != "response-final" {
		t.Fatalf("steer contexts=%#v", base.dispositionContexts)
	}
}

func TestRunLoopBoundRequestChannelDisposesBindFailureAsAbandoned(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-bind-failure"},
		response:  &llm.Response{ResponseID: "response-bind-failure", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-a", Type: "function", Function: llm.ToolCallFunction{Name: "opaque_alias", Arguments: `{}`}}}}, FinishReason: "tool_calls"}}},
	}
	base := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}
	cb := &failingBoundChannelCallbacks{boundChannelCallbacks: base}

	result := RunLoop(cb, "test", nil, nil)
	if !strings.Contains(result.Error, "surface_integrity_failure: response binding failed") {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Equal(base.dispositions, []ToolSurfaceDisposition{ToolSurfaceResponseAbandoned}) {
		t.Fatalf("bind-failure dispositions=%#v", base.dispositions)
	}
	if len(base.toolCalls) != 0 {
		t.Fatalf("binding failure executed tool calls=%#v", base.toolCalls)
	}
	if len(base.binderContexts) != 1 || base.binderContexts[0].ResponseID != "response-bind-failure" {
		t.Fatalf("binder contexts=%#v", base.binderContexts)
	}
	if base.executionContext != (ToolCallExecutionContext{}) {
		t.Fatalf("tool execution context=%#v", base.executionContext)
	}
}

func TestRunLoopBoundRequestChannelAbandonsInvalidToolArgumentsBeforeRecovery(t *testing.T) {
	channel := &testToolSurfaceRequestChannel{
		execution: ToolCallExecutionContext{Protocol: "reviewed-protocol", ConnectionID: "connection-invalid-args"},
		response:  &llm.Response{ResponseID: "response-invalid-args", Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-invalid", Type: "function", Function: llm.ToolCallFunction{Name: "opaque_alias", Arguments: `{`}}}}, FinishReason: "tool_calls"}}},
	}
	cb := &boundChannelCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "https://unused.example", Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_alias", "test", map[string]interface{}{"type": "object"})},
	}, channels: []*testToolSurfaceRequestChannel{channel}}

	result := RunLoop(cb, "test", nil, nil)
	if result.Error != "max iterations reached" {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Equal(cb.dispositions, []ToolSurfaceDisposition{ToolSurfaceResponseAbandoned}) {
		t.Fatalf("invalid-argument dispositions=%#v", cb.dispositions)
	}
	if len(cb.binderContexts) != 1 || cb.binderContexts[0].ResponseID != "response-invalid-args" {
		t.Fatalf("invalid-argument binder contexts=%#v", cb.binderContexts)
	}
}

func (m *deliveryObserverCallbacks) ContainToolSurfaceAmbiguousDelivery() bool {
	return m.containment
}

func (m *deliveryObserverCallbacks) OnToolSurfaceAttemptStarted(execution ToolCallExecutionContext) {
	m.starts = append(m.starts, execution)
}

func (m *deliveryObserverCallbacks) OnToolSurfaceAttemptFinished(_ ToolCallExecutionContext, delivery ToolSurfaceDeliveryState) {
	m.finishes = append(m.finishes, delivery)
}

func (m *epochAwareCallbacks) BeginToolSurfaceEpoch(iteration int) string {
	epoch := fmt.Sprintf("surface-%d-%d", iteration, len(m.epochs)+1)
	m.epochs = append(m.epochs, epoch)
	return epoch
}

func (m *epochAwareCallbacks) ExecuteToolCallWithContext(name, args, callID string, execution ToolCallExecutionContext) ToolExecutionResult {
	m.seen = execution
	return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: ToolExecutionOutcomeOK}
}

func (m *contextExecutorPreferredCallbacks) BeginToolSurfaceEpoch(int) string {
	return "context-executor-surface"
}

func (m *contextExecutorPreferredCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	m.structuredCalls++
	return ToolExecutionResult{Result: "structured compatibility fallback was used", Outcome: ToolExecutionOutcomeError}
}

func (m *contextExecutorPreferredCallbacks) ExecuteToolCallWithContext(name, args, callID string, execution ToolCallExecutionContext) ToolExecutionResult {
	m.seen = execution
	return ToolExecutionResult{Result: m.ExecuteTool(name, args), Outcome: ToolExecutionOutcomeOK}
}

func TestRunLoopBindsToolCallsToTheRequestSurfaceEpoch(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"epoch-call","type":"function","function":{"name":"opaque_adapter","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &epochAwareCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 2, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_adapter", "test", map[string]interface{}{"type": "object"})}, toolResult: "ok",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.epochs) != 2 || cb.seen.SurfaceEpoch == "" || cb.seen.SurfaceEpoch != cb.epochs[0] {
		t.Fatalf("epochs=%v execution=%+v", cb.epochs, cb.seen)
	}
}

func TestRunLoopPrefersContextExecutorOverEpochlessStructuredFallback(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"context-call","type":"function","function":{"name":"opaque_adapter","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &contextExecutorPreferredCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 2, sysPrompt: "sys",
		tools: []map[string]interface{}{tooldef.BuildToolDef("opaque_adapter", "test", map[string]interface{}{"type": "object"})}, toolResult: "ok",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" || result.ToolCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if cb.structuredCalls != 0 || cb.seen.SurfaceEpoch != "context-executor-surface" {
		t.Fatalf("model dispatch bypassed context executor: structured=%d execution=%+v", cb.structuredCalls, cb.seen)
	}
}

func TestRunLoopRejectsToolCallsOutsideTheRenderedRequestSurface(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"hallucinated-call","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:    2,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"})},
		toolResult: "dispatcher must not run",
	}
	result := RunLoop(cb, "inspect", nil, server.Client())
	if result.Error != "" || result.Text != "done" || result.ToolCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("unrendered model tool reached host dispatcher: %v", cb.toolCalls)
	}
}

func TestRunLoopInputBreakdownUsesActualRequestToolSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	wideTool := tooldef.BuildToolDef("wide_compatibility_tool", strings.Repeat("wide ", 220), map[string]interface{}{"type": "object"})
	actualTool := tooldef.BuildToolDef("actual_request_tool", "small", map[string]interface{}{"type": "object"})
	cb := &requestSurfaceBreakdownCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		tools: []map[string]interface{}{wideTool},
	}, rendered: []map[string]interface{}{actualTool}}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.breakdown) != 1 {
		t.Fatalf("breakdowns=%#v", cb.breakdown)
	}
	want := EstimateLoopInputBreakdown([]interface{}{map[string]string{"role": "system", "content": "sys"}, map[string]interface{}{"role": "user", "content": "test"}}, []map[string]interface{}{actualTool})
	if cb.breakdown[0].ToolDefinitionTokens != want.ToolDefinitionTokens {
		t.Fatalf("tool definition tokens=%d want actual rendered=%d (wide=%d)", cb.breakdown[0].ToolDefinitionTokens, want.ToolDefinitionTokens, EstimateLoopInputBreakdown(nil, []map[string]interface{}{wideTool}).ToolDefinitionTokens)
	}
}

func TestRunLoopBindsResponsesStreamProviderResponseID(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, "event: response.completed\n"+
				"data: {\"response\":{\"id\":\"resp-tool\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-tool\",\"name\":\"read_file\",\"arguments\":\"{}\"}]}}\n\n")
			return
		}
		_, _ = io.WriteString(w, "event: response.completed\n"+
			"data: {\"response\":{\"id\":\"resp-final\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
	}))
	defer server.Close()

	cb := &responseBindingCallbacks{mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", WireAPI: "responses"},
		maxIter:    2,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_file", "Read", map[string]interface{}{"type": "object"})},
		toolResult: "file contents",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := cb.boundResponseIDs, []string{"resp-tool", "resp-final"}; !slices.Equal(got, want) {
		t.Fatalf("bound response IDs=%v, want %v", got, want)
	}
}

func TestRunLoopInputBreakdownRecordsEveryActualRequestAttempt(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "streaming upstream failure", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	actualTool := tooldef.BuildToolDef("actual_request_tool", "small", map[string]interface{}{"type": "object"})
	cb := &requestSurfaceBreakdownCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, rendered: []map[string]interface{}{actualTool}}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests=%d, want stream attempt plus fallback", got)
	}
	if len(cb.breakdown) != 2 {
		t.Fatalf("breakdowns=%#v, want one record per actual request", cb.breakdown)
	}
	want := EstimateLoopInputBreakdown([]interface{}{map[string]string{"role": "system", "content": "sys"}, map[string]interface{}{"role": "user", "content": "test"}}, []map[string]interface{}{actualTool})
	for i, breakdown := range cb.breakdown {
		if breakdown.ToolDefinitionTokens != want.ToolDefinitionTokens {
			t.Fatalf("attempt %d tool definition tokens=%d, want %d", i+1, breakdown.ToolDefinitionTokens, want.ToolDefinitionTokens)
		}
	}
	var terminals []ToolSurfaceDisposition
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			if event.PayloadDigest == "" || event.AuditDigest == "" || event.ExpectedToolCount != 1 || event.ReplacementMode != "replace" {
				t.Fatalf("terminal event lacks the rendered surface summary: %+v", event)
			}
			terminals = append(terminals, event.TerminalReason)
		}
	}
	if want := []ToolSurfaceDisposition{ToolSurfaceTransportFailure, ToolSurfaceResponseSettled}; !slices.Equal(terminals, want) {
		t.Fatalf("terminal events=%v, want %v (one per wire attempt)", terminals, want)
	}
}

func TestRunLoopInputBreakdownRecordsOuterRetryRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	actualTool := tooldef.BuildToolDef("actual_request_tool", "small", map[string]interface{}{"type": "object"})
	cb := &requestSurfaceBreakdownCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, rendered: []map[string]interface{}{actualTool}}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests=%d, want initial stream plus outer retry", got)
	}
	if len(cb.breakdown) != 2 {
		t.Fatalf("breakdowns=%#v, want one record per actual request", cb.breakdown)
	}
	want := EstimateLoopInputBreakdown([]interface{}{map[string]string{"role": "system", "content": "sys"}, map[string]interface{}{"role": "user", "content": "test"}}, []map[string]interface{}{actualTool})
	for i, breakdown := range cb.breakdown {
		if breakdown.ToolDefinitionTokens != want.ToolDefinitionTokens {
			t.Fatalf("attempt %d tool definition tokens=%d, want %d", i+1, breakdown.ToolDefinitionTokens, want.ToolDefinitionTokens)
		}
	}
	var terminals []ToolSurfaceDisposition
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			if event.PayloadDigest == "" || event.AuditDigest == "" || event.ExpectedToolCount != 1 || event.ReplacementMode != "replace" {
				t.Fatalf("terminal event lacks the rendered surface summary: %+v", event)
			}
			terminals = append(terminals, event.TerminalReason)
		}
	}
	if want := []ToolSurfaceDisposition{ToolSurfaceTransportFailure, ToolSurfaceResponseSettled}; !slices.Equal(terminals, want) {
		t.Fatalf("terminal events=%v, want %v (one per wire attempt)", terminals, want)
	}
}

func TestRunLoopOuterRetryUsesFrozenSurfaceBeforeInputBreakdownObserver(t *testing.T) {
	var requests atomic.Int64
	wireDescriptions := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		function, _ := payload.Tools[0]["function"].(map[string]interface{})
		description, _ := function["description"].(string)
		wireDescriptions = append(wireDescriptions, description)
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	first := []map[string]interface{}{tooldef.BuildToolDef("retry_tool_first", "original first", map[string]interface{}{"type": "object"})}
	second := []map[string]interface{}{tooldef.BuildToolDef("retry_tool_second", "original second", map[string]interface{}{"type": "object"})}
	cb := &rotatingBreakdownMutatingCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}, rendered: [][]map[string]interface{}{first, second}}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" || cb.mutations != 2 {
		t.Fatalf("result=%+v mutations=%d", result, cb.mutations)
	}
	if got, want := wireDescriptions, []string{"original first", "original second"}; !slices.Equal(got, want) {
		t.Fatalf("wire descriptions=%v, want %v", got, want)
	}
}

func TestRunLoopOuterRetryFreezeFailureHasOneUncorrelatedIntegrityTerminal(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cb := &retryFreezeFailureCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}

	result := RunLoop(cb, "test", nil, server.Client())
	if !strings.Contains(result.Error, "surface_integrity_failure") {
		t.Fatalf("result=%+v", result)
	}
	if requests.Load() != 2 || cb.calls != 3 {
		t.Fatalf("requests=%d renders=%d, want stream/fallback predecessor sends and one failed retry render", requests.Load(), cb.calls)
	}
	var terminals []ToolSurfaceEvent
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminals = append(terminals, event)
		}
	}
	if len(terminals) != 3 {
		t.Fatalf("terminal events=%+v, want stream/fallback predecessors plus failed successor", terminals)
	}
	for _, predecessor := range terminals[:2] {
		if predecessor.TerminalReason != ToolSurfaceTransportFailure || predecessor.PayloadDigest == "" {
			t.Fatalf("predecessor terminal=%+v", predecessor)
		}
	}
	if terminal := terminals[2]; terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.ExpectedToolCount != 0 || terminal.ReplacementMode != "" || terminal.FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("retry freeze-failure terminal=%+v", terminal)
	}
}

func TestRunLoopOuterRetryRebuildsToolSurfaceInsteadOfReusingPredecessor(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		surfaces = append(surfaces, names)
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	first := tooldef.BuildToolDef("predecessor_only", "first request", map[string]interface{}{"type": "object"})
	second := tooldef.BuildToolDef("successor_only", "retry request", map[string]interface{}{"type": "object"})
	cb := &rotatingReceiptSurfaceCallbacks{rotatingRequestSurfaceCallbacks: rotatingRequestSurfaceCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		},
		rendered: [][]map[string]interface{}{{first}, {second}},
	}}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests=%d, want stream attempt plus retry", got)
	}
	if len(surfaces) != 2 {
		t.Fatalf("tool surfaces=%#v, want two requests", surfaces)
	}
	if got, want := surfaces, [][]string{{"predecessor_only"}, {"successor_only"}}; !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("tool surfaces=%#v, want %#v", got, want)
	}
	if len(cb.receipts) != 2 || !cb.receipts[0].Verified || !cb.receipts[1].Verified || cb.receipts[0].PayloadDigest == cb.receipts[1].PayloadDigest {
		t.Fatalf("receipts=%#v, want two distinct verified request receipts", cb.receipts)
	}
}

func TestRunLoopIssuesSuccessorEpochBeforeReplacementRender(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		surfaces = append(surfaces, names)
		if requests.Add(1) == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &epochOrderedRequestSurfaceCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		},
		rendered: [][]map[string]interface{}{
			{tooldef.BuildToolDef("predecessor_only", "first request", map[string]interface{}{"type": "object"})},
			{tooldef.BuildToolDef("successor_only", "second request", map[string]interface{}{"type": "object"})},
		},
	}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := surfaces, [][]string{{"predecessor_only"}, {"successor_only"}}; len(got) != len(want) || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("tool surfaces=%#v, want %#v", got, want)
	}
	if got, want := cb.renderedEpochs, []string{"epoch-0-1", "epoch-0-2"}; !slices.Equal(got, want) {
		t.Fatalf("renderer saw epochs=%v, want %v", got, want)
	}
	if !slices.Equal(cb.epochs, cb.renderedEpochs) {
		t.Fatalf("epoch issuance/render order diverged: issued=%v rendered=%v", cb.epochs, cb.renderedEpochs)
	}
}

func TestRunLoopFallbackIssuesSuccessorEpochBeforeReplacementRender(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		surfaces = append(surfaces, names)
		if requests.Add(1) == 1 {
			http.Error(w, "streaming format unsupported", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &epochOrderedRequestSurfaceCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		},
		rendered: [][]map[string]interface{}{
			{tooldef.BuildToolDef("stream_predecessor_only", "stream request", map[string]interface{}{"type": "object"})},
			{tooldef.BuildToolDef("fallback_successor_only", "fallback request", map[string]interface{}{"type": "object"})},
		},
	}

	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := surfaces, [][]string{{"stream_predecessor_only"}, {"fallback_successor_only"}}; len(got) != len(want) || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("tool surfaces=%#v, want %#v", got, want)
	}
	if got, want := cb.renderedEpochs, []string{"epoch-0-1", "epoch-0-2"}; !slices.Equal(got, want) {
		t.Fatalf("renderer saw epochs=%v, want %v", got, want)
	}
	if !slices.Equal(cb.epochs, cb.renderedEpochs) {
		t.Fatalf("epoch issuance/render order diverged: issued=%v rendered=%v", cb.epochs, cb.renderedEpochs)
	}
}

func TestRunLoopReplanIssuesSuccessorEpochBeforeReplacementRender(t *testing.T) {
	var requests atomic.Int64
	var surfaces [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		names := make([]string, 0, len(payload.Tools))
		for _, definition := range payload.Tools {
			names = append(names, tooldef.Name(definition))
		}
		surfaces = append(surfaces, names)
		text := "discarded predecessor"
		if requests.Add(1) > 1 {
			text = "steered successor"
		}
		_, _ = io.WriteString(w, fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, text))
	}))
	defer server.Close()

	cb := &replanEpochOrderedRequestSurfaceCallbacks{epochOrderedRequestSurfaceCallbacks: epochOrderedRequestSurfaceCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		},
		rendered: [][]map[string]interface{}{
			{tooldef.BuildToolDef("replan_predecessor_only", "predecessor request", map[string]interface{}{"type": "object"})},
			{tooldef.BuildToolDef("replan_successor_only", "successor request", map[string]interface{}{"type": "object"})},
		},
	}}

	result := RunLoop(cb, "test", nil, server.Client(), cb)
	if result.Error != "" || result.Text != "steered successor" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := surfaces, [][]string{{"replan_predecessor_only"}, {"replan_successor_only"}}; len(got) != len(want) || !slices.Equal(got[0], want[0]) || !slices.Equal(got[1], want[1]) {
		t.Fatalf("tool surfaces=%#v, want %#v", got, want)
	}
	if got, want := cb.renderedEpochs, []string{"epoch-0-1", "epoch-0-2"}; !slices.Equal(got, want) {
		t.Fatalf("renderer saw epochs=%v, want %v", got, want)
	}
	if !slices.Equal(cb.epochs, cb.renderedEpochs) {
		t.Fatalf("epoch issuance/render order diverged: issued=%v rendered=%v", cb.epochs, cb.renderedEpochs)
	}
	if len(cb.receipts) != 2 || !cb.receipts[0].Verified || !cb.receipts[1].Verified || cb.receipts[0].PayloadDigest == cb.receipts[1].PayloadDigest {
		t.Fatalf("receipts=%#v, want distinct verified predecessor/successor receipts", cb.receipts)
	}
}

func TestRunLoopRequestOwnerDisablesHiddenOpenAICompatibilityRetries(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"compat reject"}}`)),
		}, nil
	})}
	actualTool := tooldef.BuildToolDef("actual_request_tool", "small", map[string]interface{}{"type": "object"})
	cb := &requestSurfaceBreakdownCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: "http://codegen.qianxin-inc.cn/api/v1", Model: "qax-codegen/Auto", ProviderName: "CodeGen"}, maxIter: 1, sysPrompt: "sys",
	}, rendered: []map[string]interface{}{actualTool}}
	result := RunLoop(cb, "test", nil, client)
	if result.Error == "" {
		t.Fatalf("result=%+v, want compat failure", result)
	}
	// The loop did not opt into the legacy static containment interface, so it
	// performs its one request-level non-stream fallback. Crucially, the SDK
	// does not add tool-less/compact successor requests inside either call.
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests=%d, want stream plus RunLoop-owned fallback only", got)
	}
	if got := len(cb.breakdown); got != 2 {
		t.Fatalf("breakdowns=%d, want one record for each owned request", got)
	}
}

func TestRunLoopReportsNormallyConsumedToolSurfaceAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &deliveryObserverCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.starts) != 1 || len(cb.finishes) != 1 || cb.finishes[0] != ToolSurfaceResponseConsumed {
		t.Fatalf("starts=%#v finishes=%#v", cb.starts, cb.finishes)
	}
}

func TestRunLoopReportsEmptyChoicesAsConsumedBeforeAbandoningSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer server.Close()

	cb := &deliveryObserverCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "LLM returned no choices" {
		t.Fatalf("result=%+v", result)
	}
	if len(cb.starts) != 1 || len(cb.finishes) != 1 || cb.finishes[0] != ToolSurfaceResponseConsumed {
		t.Fatalf("starts=%#v finishes=%#v", cb.starts, cb.finishes)
	}
}

func TestRunLoopEmptyAssistantTurnRetiresSurfaceBeforeRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &receiptObservingCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	var terminals []ToolSurfaceDisposition
	for _, event := range cb.events {
		if event.Kind == ToolSurfaceEventTerminalReason {
			terminals = append(terminals, event.TerminalReason)
		}
	}
	if result.Error != "max iterations reached" || !slices.Equal(terminals, []ToolSurfaceDisposition{ToolSurfaceResponseAbandoned}) {
		t.Fatalf("result=%+v terminals=%#v", result, terminals)
	}
}

func TestRunLoopAmbiguousDeliveryContainmentBlocksFallbackAndRetry(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "upstream failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	cb := &deliveryObserverCallbacks{
		containment: true,
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
		},
	}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error == "" {
		t.Fatalf("result=%+v, want terminal ambiguous-delivery failure", result)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, containment must block non-stream fallback/retry", requests.Load())
	}
	if len(cb.starts) != 1 || len(cb.finishes) != 1 || cb.finishes[0] != ToolSurfaceAmbiguousDelivery {
		t.Fatalf("starts=%#v finishes=%#v", cb.starts, cb.finishes)
	}
}

func TestRunLoopReportsAmbiguousPredecessorBeforeGenericFallback(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "upstream failure", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &deliveryObserverCallbacks{mockCallbacks: mockCallbacks{
		config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "test", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d, want streaming predecessor plus fallback", requests.Load())
	}
	if len(cb.starts) != 2 || len(cb.finishes) != 2 || cb.finishes[0] != ToolSurfaceAmbiguousDelivery || cb.finishes[1] != ToolSurfaceResponseConsumed {
		t.Fatalf("starts=%#v finishes=%#v", cb.starts, cb.finishes)
	}
}
func (m *mockCallbacks) IsToolAllowed(name string) bool {
	if m.allowed == nil {
		return true
	}
	return m.allowed[name]
}
func (m *mockCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	_ = argsJSON
	if m.callAllowed == nil {
		return true, ""
	}
	if m.callAllowed[name] {
		return true, ""
	}
	return false, m.callReason
}
func (m *mockCallbacks) OnToken(delta string)     { m.tokens = append(m.tokens, delta) }
func (m *mockCallbacks) OnProgress(text string)   {}
func (m *mockCallbacks) OnToolCall(name string)   { m.toolEvents = append(m.toolEvents, name) }
func (m *mockCallbacks) OnToolResult(name string) {}
func (m *mockCallbacks) ShouldStop() bool         { return m.stopped }

type contextProviderCallbacks struct {
	*mockCallbacks
	started  int32
	finished int32
}

type replanCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	revision        atomic.Int64
	requestRevision atomic.Int64
	steerPending    atomic.Bool
	contexts        atomic.Int32
	finished        atomic.Int32
	newRounds       atomic.Int32
}

type finalizationRaceCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	finalizeCalls atomic.Int32
	steerPending  atomic.Bool
}

type retryBackoffReplanCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	revision        atomic.Int64
	requestRevision atomic.Int64
	steerPending    atomic.Bool
	contexts        atomic.Int32
}

type retryBackoffStopCallbacks struct {
	*deliveryObserverCallbacks
	requests atomic.Int32
}

type toolCommitReplanCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	revision        atomic.Int64
	requestRevision atomic.Int64
	steerPending    atomic.Bool
	finalChecks     atomic.Int32
}

func (m *toolCommitReplanCallbacks) LLMReplanRequested() bool {
	if m.finalChecks.Add(1) == 3 && m.revision.Load() == m.requestRevision.Load() {
		m.steerPending.Store(true)
		m.revision.Add(1)
	}
	return m.revision.Load() > m.requestRevision.Load()
}

func (m *toolCommitReplanCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	m.requestRevision.Store(m.revision.Load())
	if !m.steerPending.CompareAndSwap(true, false) {
		return nil
	}
	next := append([]interface{}(nil), conversation...)
	return append(next, map[string]string{"role": "user", "content": "do not run the stale tool"})
}

func (m *retryBackoffReplanCallbacks) LLMRequestContext(int) (context.Context, func(error), error) {
	m.requestRevision.Store(m.revision.Load())
	m.contexts.Add(1)
	return context.Background(), func(error) {}, nil
}

func (m *retryBackoffReplanCallbacks) LLMReplanRequested() bool {
	return m.revision.Load() > m.requestRevision.Load()
}

func (m *retryBackoffReplanCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	if !m.steerPending.CompareAndSwap(true, false) {
		return nil
	}
	next := append([]interface{}(nil), conversation...)
	return append(next, map[string]string{"role": "user", "content": "steer during retry backoff"})
}

func (m *retryBackoffStopCallbacks) ShouldStop() bool {
	return m.requests.Load() > 0
}

func (m *finalizationRaceCallbacks) TryFinalizeLLMResponse() bool {
	if m.finalizeCalls.Add(1) == 1 {
		m.steerPending.Store(true)
		return false
	}
	return true
}

func (m *finalizationRaceCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	if !m.steerPending.CompareAndSwap(true, false) {
		return nil
	}
	next := append([]interface{}(nil), conversation...)
	return append(next, map[string]string{"role": "user", "content": "late steering won finalization race"})
}

func (m *replanCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	_ = iteration
	revision := m.revision.Load()
	m.requestRevision.Store(revision)
	m.contexts.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	if revision == 0 {
		m.steerPending.Store(true)
		m.revision.Add(1)
		cancel()
	}
	return ctx, func(error) {
		cancel()
		m.finished.Add(1)
	}, nil
}

func (m *replanCallbacks) LLMReplanRequested() bool {
	return m.revision.Load() > m.requestRevision.Load()
}

func (m *replanCallbacks) OnLLMNewRound() { m.newRounds.Add(1) }

func (m *replanCallbacks) TransformConversation(conversation []interface{}) []interface{} {
	if !m.steerPending.CompareAndSwap(true, false) {
		return nil
	}
	next := append([]interface{}(nil), conversation...)
	return append(next, map[string]string{"role": "user", "content": "use SQLite instead"})
}

type stopAfterFirstToolCallbacks struct {
	*mockCallbacks
}

type projectingCallbacks struct {
	*mockCallbacks
	rawSeen       string
	projectedSeen string
}

// escalatingToolCallbacks models a host that starts a turn on a lightweight
// route and switches to a reasoning route as soon as the model uses a tool.
// It records the config observed at each actual request boundary.
type escalatingToolCallbacks struct {
	*mockCallbacks
	requestContexts  []int
	projectedContext int
	refreshes        int
}

type countingEscalatingToolCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	escalations int
	refreshes   int
	executed    int
}

type perToolSurfaceRefreshCallbacks struct {
	*mockCallbacks
	refreshed []string
}

type postCommitSurfaceRefreshCallbacks struct {
	*mockCallbacks
	DefaultLoopHooks
	committed       bool
	refreshed       []string
	buildToolsCalls int
}

type toolBatchCommitCallbacks struct {
	DefaultLoopHooks
	batches   [][]ConversationEntry
	metas     []ToolBatchMetadata
	starts    []ToolBatchMetadata
	fail      bool
	failStart bool
	abandons  []ToolBatchMetadata
}

func (m *toolBatchCommitCallbacks) OnToolBatchStarting(delta []ConversationEntry, meta ToolBatchMetadata) error {
	if len(delta) != 1 || delta[0].Role != "assistant" || !entryHasToolCalls(delta[0]) {
		return fmt.Errorf("pre-execution checkpoint did not receive a complete assistant tool-call declaration: %#v", delta)
	}
	m.starts = append(m.starts, meta)
	if m.failStart {
		return fmt.Errorf("disk unavailable")
	}
	return nil
}

func (m *toolBatchCommitCallbacks) OnToolBatchAbandoned(meta ToolBatchMetadata) {
	m.abandons = append(m.abandons, meta)
}

func (m *toolBatchCommitCallbacks) OnToolBatchCommitted(delta []ConversationEntry, meta ToolBatchMetadata) error {
	m.batches = append(m.batches, append([]ConversationEntry(nil), delta...))
	m.metas = append(m.metas, meta)
	if m.fail {
		return fmt.Errorf("disk unavailable")
	}
	return nil
}

func (m *projectingCallbacks) OnToolExecuted(name, argsJSON, result string, success bool) {
	_ = name
	_ = argsJSON
	_ = success
	m.rawSeen = result
}

func (m *projectingCallbacks) TransformConversation([]interface{}) []interface{} { return nil }
func (m *projectingCallbacks) OnEmptyResponse(int) bool                          { return false }

func (m *projectingCallbacks) ProjectToolResult(name string, result ToolExecutionResult) string {
	_ = name
	m.projectedSeen = result.Result
	return "MODEL_PREVIEW_WITH_HANDLE"
}

func (m *escalatingToolCallbacks) EscalateAfterToolExecution(string) {
	m.config.ContextLength = 400_000
	m.config.TimeoutSec = corelib.MaxAgentTimeoutSec
}

func (m *escalatingToolCallbacks) LLMRequestContext(int) (context.Context, func(error), error) {
	m.requestContexts = append(m.requestContexts, m.config.EffectiveContextTokens())
	return context.Background(), func(error) {}, nil
}

func (m *escalatingToolCallbacks) ProjectToolResult(_ string, result ToolExecutionResult) string {
	m.projectedContext = m.config.EffectiveContextTokens()
	return result.Result
}

func (m *escalatingToolCallbacks) RefreshAfterToolExecution(string) bool {
	m.refreshes++
	m.sysPrompt = "REASONING SYSTEM"
	m.tools = []map[string]interface{}{tooldef.BuildToolDef("read_document", "Read", map[string]interface{}{"type": "object"}), tooldef.BuildToolDef("bash", "Run", map[string]interface{}{"type": "object"})}
	return true
}

func (m *countingEscalatingToolCallbacks) EscalateAfterToolExecution(string) {
	m.escalations++
}

func (m *countingEscalatingToolCallbacks) OnToolExecuted(string, string, string, bool) {
	m.executed++
}

func (m *countingEscalatingToolCallbacks) RefreshAfterToolExecution(string) bool {
	m.refreshes++
	return false
}

func (m *perToolSurfaceRefreshCallbacks) RefreshAfterToolExecution(name string) bool {
	m.refreshed = append(m.refreshed, name)
	if len(m.refreshed) == 2 {
		m.tools = []map[string]interface{}{tooldef.BuildToolDef("after_batch", "After batch", map[string]interface{}{"type": "object"})}
	}
	return name == "second"
}

func (m *postCommitSurfaceRefreshCallbacks) OnToolBatchCommitted([]ConversationEntry, ToolBatchMetadata) error {
	m.committed = true
	m.tools = []map[string]interface{}{tooldef.BuildToolDef("after_commit", "After commit", map[string]interface{}{"type": "object"})}
	return nil
}

func (m *postCommitSurfaceRefreshCallbacks) RefreshAfterToolExecution(name string) bool {
	if !m.committed {
		panic("tool surface refresh ran before the tool batch committed")
	}
	m.refreshed = append(m.refreshed, name)
	return true
}

func (m *postCommitSurfaceRefreshCallbacks) BuildTools(userText string) []map[string]interface{} {
	m.buildToolsCalls++
	return m.mockCallbacks.BuildTools(userText)
}

func (m *stopAfterFirstToolCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	result := m.mockCallbacks.ExecuteToolStructured(name, args)
	if len(m.toolCalls) == 1 {
		m.stopped = true
	}
	return result
}

// budgetStopCallbacks trips EarlyStop after the first LLM usage charge.
type budgetStopCallbacks struct {
	*mockCallbacks
	usageRounds int
	earlyAfter  int // stop after this many OnLLMUsage calls
}

func (m *budgetStopCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	_ = model
	_ = inputTokens
	_ = outputTokens
	m.usageRounds++
}

func (m *budgetStopCallbacks) EarlyStop() (bool, string, string) {
	if m.usageRounds >= m.earlyAfter && m.earlyAfter > 0 {
		return true, "daily_llm_budget_exceeded", "budget test stop"
	}
	return false, "", ""
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (m *contextProviderCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	if iteration != 0 {
		return nil, nil, fmt.Errorf("unexpected iteration %d", iteration)
	}
	atomic.AddInt32(&m.started, 1)
	return context.WithValue(context.Background(), "loop-test-context", "ok"), func(error) {
		atomic.AddInt32(&m.finished, 1)
	}, nil
}

func responsesInputHasType(input []interface{}, typ string) bool {
	for _, item := range input {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == typ {
			return true
		}
	}
	return false
}

func TestRunLoop_UsesHostLLMRequestContext(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Context().Value("loop-test-context"); got != "ok" {
			return nil, fmt.Errorf("request context marker = %#v, want ok", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"done"}}]}`)),
		}, nil
	})}

	cb := &contextProviderCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "sys",
	}}
	result := RunLoop(cb, "hi", nil, client)
	if result.Error != "" || strings.TrimSpace(result.Text) != "done" {
		t.Fatalf("RunLoop result = %+v, want done without error", result)
	}
	if atomic.LoadInt32(&cb.started) != 1 || atomic.LoadInt32(&cb.finished) != 1 {
		t.Fatalf("context lifecycle started=%d finished=%d, want 1/1", cb.started, cb.finished)
	}
}

func TestRunLoop_ReplanReplacesCancelledRequestWithoutSpendingIteration(t *testing.T) {
	var requests atomic.Int32
	var sawSteer atomic.Bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		for _, msg := range req.Messages {
			if msg["role"] == "user" && strings.Contains(fmt.Sprint(msg["content"]), "use SQLite instead") {
				sawSteer.Store(true)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"switched to SQLite"}}]}`)),
		}, nil
	})}

	cb := &replanCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "sys",
	}}
	result := RunLoop(cb, "build a database", nil, client, cb)
	if result.Error != "" || result.Text != "switched to SQLite" {
		t.Fatalf("RunLoop result = %+v, want replacement response", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("HTTP requests = %d, want cancelled attempt plus replacement", requests.Load())
	}
	if cb.contexts.Load() != 2 || cb.finished.Load() != 2 {
		t.Fatalf("context lifecycle started=%d finished=%d, want 2/2", cb.contexts.Load(), cb.finished.Load())
	}
	if cb.newRounds.Load() != 1 {
		t.Fatalf("new-round notifications = %d, want 1", cb.newRounds.Load())
	}
	if !sawSteer.Load() {
		t.Fatal("replacement request did not include live steering")
	}
}

func TestRunLoop_FinalizationGuardRegeneratesWithoutSpendingIteration(t *testing.T) {
	var requests atomic.Int32
	var sawLateSteer atomic.Bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		for _, msg := range req.Messages {
			if strings.Contains(fmt.Sprint(msg["content"]), "late steering won finalization race") {
				sawLateSteer.Store(true)
			}
		}
		text := "stale final"
		if sawLateSteer.Load() {
			text = "steered final"
		}
		body := fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}]}`, text)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	cb := &finalizationRaceCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter: 1, sysPrompt: "sys",
	}}
	result := RunLoop(cb, "start", nil, client, cb)
	if result.Error != "" || result.Text != "steered final" {
		t.Fatalf("RunLoop result = %+v, want steered final", result)
	}
	if requests.Load() != 2 || cb.finalizeCalls.Load() != 2 || !sawLateSteer.Load() {
		t.Fatalf("requests=%d finalize=%d sawSteer=%v", requests.Load(), cb.finalizeCalls.Load(), sawLateSteer.Load())
	}
}

func TestRunLoop_ReplanInterruptsTransientRetryBackoff(t *testing.T) {
	var requests atomic.Int32
	var sawSteer atomic.Bool
	cb := &retryBackoffReplanCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key", WireAPI: "responses"},
		maxIter: 1, sysPrompt: "sys",
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		if n == 1 {
			if n == 1 {
				go func() {
					time.Sleep(100 * time.Millisecond)
					cb.steerPending.Store(true)
					cb.revision.Add(1)
				}()
			}
			return nil, newLLMHTTPError(http.StatusServiceUnavailable, "temporary")
		}
		var req struct {
			Input []map[string]interface{} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		for _, msg := range req.Input {
			if strings.Contains(fmt.Sprint(msg["content"]), "steer during retry backoff") {
				sawSteer.Store(true)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_steered","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"steered retry"}]}]}`))}, nil
	})}
	started := time.Now()
	result := RunLoop(cb, "start", nil, client, cb)
	if result.Error != "" || result.Text != "steered retry" || !sawSteer.Load() {
		t.Fatalf("RunLoop result=%+v sawSteer=%v", result, sawSteer.Load())
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("replan waited through retry backoff: %v", elapsed)
	}
	if requests.Load() != 2 || cb.contexts.Load() != 2 {
		t.Fatalf("requests=%d contexts=%d, want 2/2", requests.Load(), cb.contexts.Load())
	}
}

func TestRunLoop_StopDuringRetryBackoffDoesNotCreateSuccessorSurface(t *testing.T) {
	cb := &retryBackoffStopCallbacks{deliveryObserverCallbacks: &deliveryObserverCallbacks{mockCallbacks: mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key", WireAPI: "responses"},
		maxIter: 1, sysPrompt: "sys",
	}}}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		cb.requests.Add(1)
		return nil, newLLMHTTPError(http.StatusServiceUnavailable, "temporary")
	})}

	result := RunLoop(cb, "start", nil, client)
	if result.Error != "cancelled during LLM retry" {
		t.Fatalf("result=%+v", result)
	}
	if cb.requests.Load() != 1 || len(cb.starts) != 1 || len(cb.finishes) != 1 || cb.finishes[0] != ToolSurfaceAmbiguousDelivery {
		t.Fatalf("requests=%d starts=%#v finishes=%#v", cb.requests.Load(), cb.starts, cb.finishes)
	}
}

func TestRunLoop_ReplanBeforeToolCommitPreventsStaleSideEffect(t *testing.T) {
	var requests atomic.Int32
	var sawSteer atomic.Bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		for _, msg := range req.Messages {
			if strings.Contains(fmt.Sprint(msg["content"]), "do not run the stale tool") {
				sawSteer.Store(true)
			}
		}
		body := `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"stale","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo stale\"}"}}]},"finish_reason":"tool_calls"}]}`
		if n > 1 {
			body = `{"choices":[{"message":{"role":"assistant","content":"changed course"},"finish_reason":"stop"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	cb := &toolCommitReplanCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter: 1, sysPrompt: "sys", toolResult: "stale side effect ran",
	}}
	result := RunLoop(cb, "start", nil, client, cb)
	if result.Error != "" || result.Text != "changed course" || !sawSteer.Load() {
		t.Fatalf("RunLoop result=%+v sawSteer=%v", result, sawSteer.Load())
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("stale tool executed despite steering: %v", cb.toolCalls)
	}
}

func TestRunLoop_ReplanInterruptsEmptyResponseBackoff(t *testing.T) {
	var requests atomic.Int32
	cb := &retryBackoffReplanCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "test", Key: "test-key"},
		maxIter: 1, sysPrompt: "sys",
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		if n == 1 {
			go func() {
				time.Sleep(100 * time.Millisecond)
				cb.steerPending.Store(true)
				cb.revision.Add(1)
			}()
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"steered after empty"}}]}`))}, nil
	})}
	started := time.Now()
	result := RunLoop(cb, "start", nil, client, cb)
	if result.Error != "" || result.Text != "steered after empty" {
		t.Fatalf("RunLoop result=%+v", result)
	}
	if elapsed := time.Since(started); elapsed >= 800*time.Millisecond {
		t.Fatalf("replan waited through empty-response backoff: %v", elapsed)
	}
}

func TestRunLoop_NoToolCalls_ReturnsFinalText(t *testing.T) {
	// Mock LLM server that returns a simple text response (no tool calls).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! How can I help?",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "hi", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Text != "Hello! How can I help?" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.Iterations != 1 {
		t.Fatalf("expected 1 iteration, got %d", result.Iterations)
	}
	if len(cb.tokens) != 1 {
		t.Fatalf("OnToken should be called once with full text via streaming, got: %v", cb.tokens)
	}
	if cb.tokens[0] != "Hello! How can I help?" {
		t.Fatalf("OnToken delta mismatch: %q", cb.tokens[0])
	}
}

func TestRunLoop_InvalidToolArgumentsAreNotExecutedAndRecover(t *testing.T) {
	var requestCount atomic.Int64
	var sawRecoveryPrompt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if n == 1 {
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_bad",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "write_file",
								"arguments": `{"path":"a.txt","content":"unterminated`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		for _, msg := range req.Messages {
			if msg["role"] == "user" && strings.Contains(fmt.Sprint(msg["content"]), "Previous tool call arguments were incomplete") {
				sawRecoveryPrompt.Store(true)
			}
			if _, ok := msg["tool_call_id"]; ok {
				t.Fatalf("invalid JSON tool call should not create tool-result history: %#v", req.Messages)
			}
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "recovered"},
				"finish_reason": "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "You are a coding agent.",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write file", map[string]interface{}{"type": "object"})},
		toolResult: "should not run",
	}

	result := RunLoop(cb, "write file", nil, nil)
	if result.Error != "" || result.Text != "recovered" {
		t.Fatalf("RunLoop result = %+v, want recovered without error", result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool was executed despite invalid JSON: %v", cb.toolCalls)
	}
	if !sawRecoveryPrompt.Load() {
		t.Fatalf("second request did not receive invalid-JSON recovery prompt")
	}
}

func TestInvalidLoopToolArgumentNamesCatchesUnmarkedBadJSON(t *testing.T) {
	calls := []llm.ToolCall{
		{
			ID:   "call_bad",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "edit_file", Arguments: `{"new_string":"unterminated`},
		},
		{
			ID:   "call_array",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "bash", Arguments: `[]`},
		},
		{
			ID:   "call_null",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "write_file", Arguments: `null`},
		},
		{
			ID:   "call_ok",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: `{"path":"main.go"}`},
		},
	}
	got := invalidLoopToolArgumentNames(calls)
	if len(got) != 3 || got[0] != "edit_file" || got[1] != "bash" || got[2] != "write_file" {
		t.Fatalf("invalidLoopToolArgumentNames = %#v, want edit_file, bash, write_file", got)
	}
}

func TestRunLoop_TruncatedToolCallInjectsRecoveryWithoutExecuting(t *testing.T) {
	var requestCount atomic.Int64
	var sawRecoveryPrompt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"a.txt\",\"content\":\"unterminated"}}]},"finish_reason":null}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"length"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		for _, msg := range req.Messages {
			content := fmt.Sprint(msg["content"])
			if msg["role"] == "user" && strings.Contains(content, "Previous tool call arguments were incomplete") && strings.Contains(content, "For write_file: no per-call content length limit") {
				sawRecoveryPrompt.Store(true)
			}
			if _, ok := msg["tool_call_id"]; ok {
				t.Fatalf("truncated tool call should not create tool-result history: %#v", req.Messages)
			}
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"recovered after truncation"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "You are a coding agent.",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write file", map[string]interface{}{"type": "object"})},
		toolResult: "should not run",
	}

	result := RunLoop(cb, "write file", nil, server.Client())
	if result.Error != "" || result.Text != "recovered after truncation" {
		t.Fatalf("RunLoop result = %+v, want recovered without error", result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool was executed despite truncation: %v", cb.toolCalls)
	}
	if !sawRecoveryPrompt.Load() {
		t.Fatalf("second request did not receive truncation recovery prompt")
	}
}

func TestRunLoop_EmptyToolArgumentsNormalizeToObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]interface{}{{
						"id":   "call_empty",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "noop",
							"arguments": "",
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    1,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("noop", "Noop", map[string]interface{}{"type": "object"})},
		toolResult: "ok",
	}

	result := RunLoop(cb, "call noop", nil, nil)
	if result.Error != "max iterations reached" {
		t.Fatalf("RunLoop error = %q, want max iterations after tool execution", result.Error)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "noop" {
		t.Fatalf("tool calls = %#v, want noop executed", cb.toolCalls)
	}
	if len(cb.toolArgs) != 1 || cb.toolArgs[0] != "{}" {
		t.Fatalf("tool args = %#v, want normalized empty args to {}", cb.toolArgs)
	}
}

func TestRunLoopProjectsAfterRawHooksAndCommitsProjection(t *testing.T) {
	raw := strings.Repeat("raw-output-", 1000)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_project","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		var toolContent string
		for _, msg := range req.Messages {
			if msg["role"] == "tool" && msg["tool_call_id"] == "tc_project" {
				toolContent = fmt.Sprint(msg["content"])
			}
		}
		if toolContent != "MODEL_PREVIEW_WITH_HANDLE" {
			t.Fatalf("model tool content = %q", toolContent)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &projectingCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("bash", "Run", map[string]interface{}{"type": "object"})},
		toolResult: raw,
	}}
	result := RunLoop(cb, "run", nil, server.Client(), cb)
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if cb.rawSeen != raw || cb.projectedSeen != raw {
		t.Fatalf("raw consumers lost result: hook=%d projector=%d raw=%d", len(cb.rawSeen), len(cb.projectedSeen), len(raw))
	}
	var committed *ConversationEntry
	for i := range result.HistoryDelta {
		if result.HistoryDelta[i].Role == "tool" && result.HistoryDelta[i].ToolCallID == "tc_project" {
			committed = &result.HistoryDelta[i]
			break
		}
	}
	if committed == nil || committed.Content != "MODEL_PREVIEW_WITH_HANDLE" {
		t.Fatalf("history delta did not commit paired projection: %#v", result.HistoryDelta)
	}
}

func TestRunLoopToolEscalationUpdatesProjectionAndNextRequestBudget(t *testing.T) {
	var requests atomic.Int32
	var requestTimeouts []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_escalate","type":"function","function":{"name":"read_document","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
			Tools    []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Messages) == 0 || !strings.HasPrefix(fmt.Sprint(req.Messages[0]["content"]), "REASONING SYSTEM") {
			t.Fatalf("refreshed system prompt missing: %#v", req.Messages)
		}
		if len(req.Tools) != 2 || tooldef.Name(req.Tools[1]) != "bash" {
			t.Fatalf("refreshed tools = %#v, want reasoning surface", req.Tools)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &escalatingToolCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key", ContextLength: 32_000, TimeoutSec: corelib.MinAgentTimeoutSec},
		maxIter:    3,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_document", "Read", map[string]interface{}{"type": "object"})},
		toolResult: "document body",
	}}
	client := server.Client()
	baseTransport := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok {
			return nil, fmt.Errorf("LLM request missing deadline")
		}
		requestTimeouts = append(requestTimeouts, time.Until(deadline))
		return baseTransport.RoundTrip(req)
	})
	result := RunLoop(cb, "read the attached document", nil, client)
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := cb.projectedContext, 320_000; got != want {
		t.Fatalf("projection context = %d, want %d after escalation", got, want)
	}
	if got, want := cb.requestContexts, []int{25_600, 320_000}; !slices.Equal(got, want) {
		t.Fatalf("request contexts = %v, want %v", got, want)
	}
	if len(requestTimeouts) != 2 {
		t.Fatalf("request timeout observations = %d, want 2", len(requestTimeouts))
	}
	for i, want := range []time.Duration{time.Duration(corelib.MinAgentTimeoutSec) * time.Second, time.Duration(corelib.MaxAgentTimeoutSec) * time.Second} {
		if got := requestTimeouts[i]; got > want || got < want-2*time.Second {
			t.Fatalf("request %d timeout = %s, want approximately %s", i+1, got, want)
		}
	}
	if cb.refreshes != 1 {
		t.Fatalf("surface refreshes = %d, want 1", cb.refreshes)
	}
	if result.Route.Source != "escalate" || result.Route.TaskType != string(llm.TaskReasoning) || result.Route.Model != "test" {
		t.Fatalf("final route = %+v, want reasoning escalation metadata", result.Route)
	}
	if result.Usage.Model != "test" {
		t.Fatalf("usage model = %q, want final upgraded model", result.Usage.Model)
	}
}

func TestRunLoopSurfaceRefreshWithoutConfigChangeDoesNotMarkEscalate(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		n := requests.Add(1)
		if n == 1 {
			if len(req.Tools) != 1 || tooldef.Name(req.Tools[0]) != "invoke_search" {
				t.Fatalf("first round tools = %#v", req.Tools)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_search","type":"function","function":{"name":"invoke_search","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if len(req.Tools) != 0 {
			t.Fatalf("second round still advertised consumed tool: %#v", req.Tools)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"北京晴"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &grantRetireCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "light fence",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("invoke_search", "Search", map[string]interface{}{"type": "object"})},
		toolResult: "beijing weather",
	}}
	result := RunLoop(cb, "北京天气", nil, server.Client())
	if result.Error != "" || result.Text != "北京晴" {
		t.Fatalf("result=%+v", result)
	}
	if result.Route.Source == "escalate" {
		t.Fatalf("consumed lookup marked as model escalate: %+v", result.Route)
	}
	if cb.refreshes != 1 {
		t.Fatalf("surface refreshes = %d, want 1", cb.refreshes)
	}
}

func TestRunLoopRefreshesEveryExecutedToolAfterBatchCommit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_first","type":"function","function":{"name":"first","arguments":"{}"}},{"id":"tc_second","type":"function","function":{"name":"second","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if len(req.Tools) != 1 || tooldef.Name(req.Tools[0]) != "after_batch" {
			t.Fatalf("next request tools = %#v, want committed batch surface", req.Tools)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &perToolSurfaceRefreshCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter: 3,
		tools: []map[string]interface{}{
			tooldef.BuildToolDef("first", "First", map[string]interface{}{"type": "object"}),
			tooldef.BuildToolDef("second", "Second", map[string]interface{}{"type": "object"}),
		},
		toolResult: "ok",
	}}
	result := RunLoop(cb, "run both", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if got, want := cb.refreshed, []string{"first", "second"}; !slices.Equal(got, want) {
		t.Fatalf("refresh callbacks=%v, want %v", got, want)
	}
}

func TestRunLoopRefreshesOnlyAfterHooksCommit(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_commit","type":"function","function":{"name":"work","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if len(req.Tools) != 1 || tooldef.Name(req.Tools[0]) != "after_commit" {
			t.Fatalf("next request tools = %#v, want post-commit surface", req.Tools)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &postCommitSurfaceRefreshCallbacks{mockCallbacks: &mockCallbacks{
		config:  corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter: 3,
		tools: []map[string]interface{}{
			tooldef.BuildToolDef("work", "Work", map[string]interface{}{"type": "object"}),
		},
		toolResult: "ok",
	}}
	// Deliberately pass a different hooks object. This mirrors the public API
	// contract: batch durability belongs to hooks, while tool-surface ownership
	// belongs to callbacks.
	hooks := cb
	result := RunLoop(cb, "run", nil, server.Client(), hooks)
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if !cb.committed || !slices.Equal(cb.refreshed, []string{"work"}) {
		t.Fatalf("committed=%v refreshed=%v", cb.committed, cb.refreshed)
	}
	// One BuildTools call belongs to each of the two wire requests. In
	// particular, RefreshAfterToolExecution must not render a third, unowned
	// intermediate surface after the batch commit and before the successor
	// request boundary.
	if cb.buildToolsCalls != 2 {
		t.Fatalf("BuildTools calls = %d, want exactly 2 request-owned renders", cb.buildToolsCalls)
	}
}

type grantRetireCallbacks struct {
	*mockCallbacks
	refreshes int
}

func (m *grantRetireCallbacks) RefreshAfterToolExecution(string) bool {
	m.refreshes++
	m.tools = nil
	return true
}

func TestDoLLMRequestWithToolsStreamDoesNotFallbackAfterRequestCancellation(t *testing.T) {
	var requests atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		cancel()
		return nil, fmt.Errorf("stream failure")
	})}
	_, err := doLLMRequestWithToolsStream(ctx, corelib.MaclawLLMConfig{
		URL: "https://llm.test", Model: "test", Key: "test-key",
	}, []interface{}{map[string]string{"role": "user", "content": "hi"}}, nil, client, nil)
	if !strings.Contains(fmt.Sprint(err), "stream failure") {
		t.Fatalf("error = %v, want the original streaming error after cancellation", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want only the streaming request", requests.Load())
	}
}

func TestRunLoopMoAReferenceUsesModelRequestTimeout(t *testing.T) {
	var observed time.Duration
	var requestModels []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok {
			return nil, fmt.Errorf("MoA reference request missing deadline")
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		requestModels = append(requestModels, body.Model)
		if body.Model == "advisor" {
			observed = time.Until(deadline)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"advice"}}]}`)),
		}, nil
	})}
	cb := &moaMockCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "primary", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "sys",
	}, preset: moa.ResolvedPreset{
		Name:                "timeout-test",
		Enabled:             true,
		ReferenceTimeoutSec: corelib.MaxAgentTimeoutSec,
		References: []moa.ResolvedRef{{
			Label:  "advisor",
			Config: corelib.MaclawLLMConfig{URL: "https://llm.test", Model: "advisor", Key: "test-key", TimeoutSec: corelib.MinAgentTimeoutSec},
		}},
	}}

	result := RunLoop(cb, "review", nil, client)
	if result.Error != "" || result.Text != "advice" {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Contains(requestModels, "advisor") {
		t.Fatalf("requests = %v, expected advisor request", requestModels)
	}
	want := time.Duration(corelib.MinAgentTimeoutSec) * time.Second
	if observed > want || observed < want-2*time.Second {
		t.Fatalf("MoA reference deadline = %s, want approximately %s", observed, want)
	}
}

func TestRunLoopMoAReferenceDisablesHiddenCompatibilitySuccessor(t *testing.T) {
	var advisorRequests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.Model != "advisor" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"primary"}}]}`)),
			}, nil
		}
		advisorRequests.Add(1)
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"compat reject"}}`)),
		}, nil
	})}
	cb := &moaMockCallbacks{mockCallbacks: &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: "http://codegen.qianxin-inc.cn/api/v1", Model: "primary", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "sys",
	}, preset: moa.ResolvedPreset{
		Name: "no-hidden-advisor-successor", Enabled: true, FanoutMaxIterations: 1,
		References: []moa.ResolvedRef{{
			Label:  "advisor",
			Config: corelib.MaclawLLMConfig{URL: "http://codegen.qianxin-inc.cn/api/v1", Model: "advisor", Key: "test-key"},
		}},
	}}

	result := RunLoop(cb, "review", nil, client)
	if result.Error != "" || result.Text != "primary" {
		t.Fatalf("result=%+v", result)
	}
	if advisorRequests.Load() != 1 {
		t.Fatalf("advisor requests=%d, want exactly one owner-visible request", advisorRequests.Load())
	}
}

func TestRunLoopDoesNotEscalateForRejectedOrSyntheticToolCalls(t *testing.T) {
	tests := []struct {
		name        string
		args        string
		callAllowed map[string]bool
		wantCalls   int
	}{
		{
			name:      "invalid arguments",
			args:      `{`,
			wantCalls: 0,
		},
		{
			name:        "policy denied",
			args:        `{}`,
			callAllowed: map[string]bool{"write_file": false},
			wantCalls:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc","type":"function","function":{"name":"write_file","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, tt.args)
			}))
			defer server.Close()
			cb := &countingEscalatingToolCallbacks{mockCallbacks: &mockCallbacks{
				config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
				maxIter:     1,
				sysPrompt:   "sys",
				toolResult:  "should not execute",
				callAllowed: tt.callAllowed,
			}}
			result := RunLoop(cb, "write", nil, server.Client(), cb)
			if result.Error != "max iterations reached" {
				t.Fatalf("result=%+v", result)
			}
			if cb.escalations != 0 || cb.refreshes != 0 || cb.executed != 0 {
				t.Fatalf("escalations=%d refreshes=%d executed=%d, want no execution-side hooks", cb.escalations, cb.refreshes, cb.executed)
			}
			if len(cb.toolCalls) != tt.wantCalls {
				t.Fatalf("tool calls=%v, want %d", cb.toolCalls, tt.wantCalls)
			}
		})
	}
}

func TestRunLoopFailedExecutionRefreshesSurfaceWithoutEscalation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_fail","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"reported failure"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &countingEscalatingToolCallbacks{mockCallbacks: &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     3,
		sysPrompt:   "sys",
		toolResult:  "Error: write failed",
		toolOutcome: ToolExecutionOutcomeError,
	}}
	result := RunLoop(cb, "write", nil, server.Client(), cb)
	if result.Error != "" || result.Text != "reported failure" {
		t.Fatalf("result=%+v", result)
	}
	if cb.executed != 1 || cb.refreshes != 1 || cb.escalations != 0 {
		t.Fatalf("executed=%d refreshes=%d escalations=%d", cb.executed, cb.refreshes, cb.escalations)
	}
}

func TestRunLoopProjectsEveryParallelToolCallWithoutOrphans(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_a","type":"function","function":{"name":"bash","arguments":"{}"}},{"id":"tc_b","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		seen := map[string]string{}
		for _, msg := range req.Messages {
			if msg["role"] == "tool" {
				seen[fmt.Sprint(msg["tool_call_id"])] = fmt.Sprint(msg["content"])
			}
		}
		for _, id := range []string{"tc_a", "tc_b"} {
			if seen[id] != "MODEL_PREVIEW_WITH_HANDLE" {
				t.Fatalf("missing or unprojected tool result %s: %#v", id, seen)
			}
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &projectingCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("bash", "Run", map[string]interface{}{"type": "object"})},
		toolResult: strings.Repeat("raw-output-", 1000),
	}}
	result := RunLoop(cb, "run", nil, server.Client(), cb)
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	toolIDs := map[string]bool{}
	var assistantCallIDs map[string]bool
	for _, entry := range result.HistoryDelta {
		switch entry.Role {
		case "assistant":
			data, _ := json.Marshal(entry.ToolCalls)
			var calls []struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(data, &calls) == nil && len(calls) > 0 {
				assistantCallIDs = map[string]bool{}
				for _, call := range calls {
					assistantCallIDs[call.ID] = true
				}
			}
		case "tool":
			toolIDs[entry.ToolCallID] = true
			if entry.Content != "MODEL_PREVIEW_WITH_HANDLE" {
				t.Fatalf("history committed raw result for %s", entry.ToolCallID)
			}
		}
	}
	if len(assistantCallIDs) != 2 || len(toolIDs) != 2 {
		t.Fatalf("parallel group mismatch calls=%v tools=%v", assistantCallIDs, toolIDs)
	}
	for id := range assistantCallIDs {
		if !toolIDs[id] {
			t.Fatalf("orphaned assistant tool call %s", id)
		}
	}
}

func TestRunLoopCommitsOnlyCompleteToolBatch(t *testing.T) {
	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		w.Header().Set("Content-Type", "application/json")
		if serverCalls == 1 {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"a","type":"function","function":{"name":"read_file","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 3, sysPrompt: "sys", toolResult: "ok"}
	hooks := &toolBatchCommitCallbacks{}
	result := RunLoop(cb, "task", nil, server.Client(), hooks)
	if result.Error != "" || len(hooks.batches) != 1 {
		t.Fatalf("result=%#v batches=%d", result, len(hooks.batches))
	}
	batch := hooks.batches[0]
	if len(batch) != 3 || batch[0].Role != "assistant" || batch[1].Role != "tool" || batch[2].Role != "tool" {
		t.Fatalf("checkpoint received incomplete/invalid batch: %#v", batch)
	}
	if hooks.metas[0].Sequence != 1 || hooks.metas[0].LastToolName != "write_file" || hooks.metas[0].SideEffectState != "local_committed" {
		t.Fatalf("unexpected metadata: %#v", hooks.metas[0])
	}
	if len(hooks.starts) != 1 || hooks.starts[0].Sequence != 1 || hooks.starts[0].LastToolName != "read_file" || hooks.starts[0].SideEffectState != "external_uncertain" {
		t.Fatalf("unexpected pre-execution metadata: %#v", hooks.starts)
	}
}

func TestRunLoopStopsWhenToolBatchCheckpointFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"a","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 3, sysPrompt: "sys", toolResult: "ok"}
	hooks := &toolBatchCommitCallbacks{fail: true}
	result := RunLoop(cb, "task", nil, server.Client(), hooks)
	if result.Error != "recovery_checkpoint_failed" || !result.HardExit || len(cb.toolCalls) != 1 || len(hooks.batches) != 1 {
		t.Fatalf("result=%#v calls=%#v batches=%d", result, cb.toolCalls, len(hooks.batches))
	}
}

func TestRunLoopStopsBeforeToolWhenPreExecutionCheckpointFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"a","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 3, sysPrompt: "sys", toolResult: "ok"}
	hooks := &toolBatchCommitCallbacks{failStart: true}
	result := RunLoop(cb, "task", nil, server.Client(), hooks)
	if result.Error != "recovery_checkpoint_failed" || !result.HardExit || len(cb.toolCalls) != 0 || len(hooks.starts) != 1 || len(hooks.batches) != 0 {
		t.Fatalf("result=%#v calls=%#v starts=%d batches=%d", result, cb.toolCalls, len(hooks.starts), len(hooks.batches))
	}
}

func TestRunLoopAbandonsPreExecutionCheckpointForInteractivePause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","tool_calls":[{"id":"ask","type":"function","function":{"name":"ask_user","arguments":"{}"}},{"id":"sibling","type":"function","function":{"name":"write_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &mockCallbacks{config: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, maxIter: 3, sysPrompt: "sys", toolResult: ToolAskUser(map[string]interface{}{"question": "continue?"})}
	hooks := &toolBatchCommitCallbacks{}
	result := RunLoop(cb, "task", nil, server.Client(), hooks)
	if result.AskUser == nil || len(hooks.starts) != 1 || len(hooks.abandons) != 1 || len(hooks.batches) != 0 {
		t.Fatalf("result=%#v starts=%#v abandons=%#v batches=%#v", result, hooks.starts, hooks.abandons, hooks.batches)
	}
	if hooks.abandons[0].Sequence != hooks.starts[0].Sequence {
		t.Fatalf("abandoned wrong batch: start=%#v abandon=%#v", hooks.starts[0], hooks.abandons[0])
	}
}

func TestSideEffectStateForToolBatchFailsClosedForUnknownTools(t *testing.T) {
	tests := []struct {
		name  string
		tools []string
		want  string
	}{
		{name: "local reads", tools: []string{"read_file", "ripgrep"}, want: "none"},
		{name: "local mutation", tools: []string{"read_file", "apply_patch"}, want: "local_committed"},
		{name: "known external", tools: []string{"web_search"}, want: "external_uncertain"},
		{name: "dynamic client tool", tools: []string{"alarm_set"}, want: "external_uncertain"},
		{name: "empty name", tools: []string{""}, want: "external_uncertain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]llm.ToolCall, 0, len(tt.tools))
			for _, name := range tt.tools {
				calls = append(calls, llm.ToolCall{Function: llm.ToolCallFunction{Name: name}})
			}
			if got := sideEffectStateForToolBatch(calls); got != tt.want {
				t.Fatalf("sideEffectStateForToolBatch(%v) = %q, want %q", tt.tools, got, tt.want)
			}
		})
	}
}

func TestRunLoop_StripsRolePrefixFromFinalTextAndReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":              "assistant",
						"content":           "Answer kept.\n\nBrowser: duplicated browser instruction",
						"reasoning_content": "thinking kept\nBrowser: hidden browser instruction",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "test", nil, nil)
	if result.Text != "Answer kept." {
		t.Fatalf("Text = %q, want sanitized answer", result.Text)
	}
	if strings.Contains(result.Text, "Browser:") {
		t.Fatalf("role prefix leaked in final text: %q", result.Text)
	}
}

func TestRunLoop_StripsRolePrefixFromStreamingTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Answer kept.\n"},"finish_reason":""}]}`+"\n\n")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Browser: duplicated browser instruction"},"finish_reason":"stop"}]}`+"\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:   1,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "test", nil, nil)
	if result.Text != "Answer kept." {
		t.Fatalf("Text = %q, want sanitized answer", result.Text)
	}
	streamed := strings.Join(cb.tokens, "")
	if strings.Contains(streamed, "Browser:") {
		t.Fatalf("role prefix leaked in streaming tokens: %q", streamed)
	}
	if strings.TrimSpace(streamed) != "Answer kept." {
		t.Fatalf("streamed = %q, want sanitized answer", streamed)
	}
}

func TestRunLoop_WithToolCall_ExecutesAndContinues(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			// First call: return a tool call.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"echo hello"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			// Second call: return final text.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Done! The command output: hello",
						},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "hello\n",
	}

	result := RunLoop(cb, "run echo hello", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Text, "Done!") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.Iterations != 2 {
		t.Fatalf("expected 2 iterations, got %d", result.Iterations)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCalls)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "bash" {
		t.Fatalf("unexpected tool calls: %v", cb.toolCalls)
	}
}

func TestRunLoop_EarlyStopBudgetAfterFirstRound(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"usage": map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 20,
					"total_tokens":      120,
				},
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"echo x"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			// Should not be reached when EarlyStop fires after first usage.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "should not run",
						},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &budgetStopCallbacks{
		mockCallbacks: &mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:    10,
			sysPrompt:  "sys",
			toolResult: "ok",
		},
		earlyAfter: 1,
	}
	result := RunLoop(cb, "budget mid-loop", nil, nil)
	if result.Error != "daily_llm_budget_exceeded" {
		t.Fatalf("error=%q text=%q", result.Error, result.Text)
	}
	if result.Text != "budget test stop" {
		t.Fatalf("text=%q", result.Text)
	}
	if !result.HardExit {
		t.Fatal("expected HardExit")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 LLM call, got %d", callCount)
	}
	if cb.usageRounds != 1 {
		t.Fatalf("usageRounds=%d", cb.usageRounds)
	}
}

func TestRunLoop_UsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:     server.URL + "/v1",
			Model:   "test",
			Key:     "test-key",
			WireAPI: "responses",
		},
		maxIter:   3,
		sysPrompt: "You are a helpful assistant.",
	}

	result := RunLoop(cb, "hi", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("RunLoop result = %+v, want done without error", result)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("request body missing Responses input: %#v", gotBody)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("request body leaked chat messages: %#v", gotBody)
	}
	if strings.Join(cb.tokens, "") != "done" {
		t.Fatalf("stream fallback tokens = %#v, want done", cb.tokens)
	}
}

func TestRunLoop_ResponsesWireAPIExecutesTools(t *testing.T) {
	callCount := 0
	var secondBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"id":"resp_tool","output":[{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"command\":\"echo hi\"}"}]}`))
			return
		}
		secondBody = body
		_, _ = w.Write([]byte(`{"id":"resp_done","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"tool done"}]}]}`))
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:     server.URL + "/v1",
			Model:   "test",
			Key:     "test-key",
			WireAPI: "responses",
		},
		maxIter:    5,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: "hi\n",
		tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "bash",
				"description": "run command",
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}},
				},
			},
		}},
	}

	result := RunLoop(cb, "run echo hi", nil, server.Client())
	if result.Error != "" || result.Text != "tool done" {
		t.Fatalf("RunLoop result = %+v, want tool done without error", result)
	}
	if result.ToolCalls != 1 || len(cb.toolCalls) != 1 || cb.toolCalls[0] != "bash" {
		t.Fatalf("tool calls result=%d executed=%v, want one bash", result.ToolCalls, cb.toolCalls)
	}
	input, ok := secondBody["input"].([]interface{})
	if !ok {
		t.Fatalf("second request input = %#v, want array", secondBody["input"])
	}
	if !responsesInputHasType(input, "function_call") || !responsesInputHasType(input, "function_call_output") {
		t.Fatalf("second request missing function_call/function_call_output: %#v", input)
	}
	if _, ok := secondBody["messages"]; ok {
		t.Fatalf("second request leaked chat messages: %#v", secondBody)
	}
}

func TestRunLoop_ToolAuthorizerBlocksExecution(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "task",
										"arguments": `{"action":"run"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "blocked",
						},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "You are a helpful assistant.",
		allowed:   map[string]bool{"ssh": true},
	}

	result := RunLoop(cb, "run a task", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("blocked tool should not reach ExecuteTool, got calls: %v", cb.toolCalls)
	}
	if len(cb.toolEvents) != 0 {
		t.Fatalf("blocked tool should not emit OnToolCall, got events: %v", cb.toolEvents)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected blocked tool call to be counted, got %d", result.ToolCalls)
	}
}

type lightProfileCallbacks struct {
	*mockCallbacks
	profile PromptProfile
}

func (c *lightProfileCallbacks) CurrentPromptProfile() PromptProfile { return c.profile }

type capabilityLightProfileCallbacks struct {
	*lightProfileCallbacks
	allowed map[string]bool
}

func (c *capabilityLightProfileCallbacks) IsToolAllowedForPromptProfile(name string, profile PromptProfile) bool {
	if !profile.IsLight() {
		return true
	}
	return c.allowed[name]
}

func TestAuthorizeLoopTool_HostAuthorizerDenyDoesNotSuggestLightUpgrade(t *testing.T) {
	ResetPromptProfileStatsForTest()
	cb := &lightProfileCallbacks{
		mockCallbacks: &mockCallbacks{
			allowed: map[string]bool{"web_search": true},
		},
		profile: PromptProfileLight,
	}
	res, denied := authorizeLoopTool(cb, "write_file", `{"path":"out.md"}`)
	if !denied {
		t.Fatal("expected host authorizer deny")
	}
	if strings.Contains(res.Result, "light prompt profile") || strings.Contains(res.Result, "full for full tools") || strings.Contains(res.Result, PromptProfileEnvKey) {
		t.Fatalf("host grant deny leaked light-upgrade guidance: %q", res.Result)
	}
	if strings.Contains(res.Result, "lookup already returned evidence") {
		t.Fatalf("generic host policy deny used lookup-only copy: %q", res.Result)
	}
	if !strings.Contains(res.Result, "do not ask the user to re-authorize") {
		t.Fatalf("result=%q", res.Result)
	}
	if st := GetPromptProfileStats(); st.LightToolDenies != 0 {
		t.Fatalf("host authorizer deny counted as light misroute: %+v", st)
	}
	fetch, deniedFetch := authorizeLoopTool(cb, "web_fetch", `{"url":"https://example.com"}`)
	if !deniedFetch || strings.Contains(fetch.Result, "light prompt profile") {
		t.Fatalf("light-safe name still on a closed host surface must not upgrade: denied=%v result=%q", deniedFetch, fetch.Result)
	}
}

type lookupDenialPresenterCallbacks struct {
	*lightProfileCallbacks
}

func (c *lookupDenialPresenterCallbacks) ToolDenialMessage(name string) string {
	return fmt.Sprintf("custom deny %s", name)
}

func TestAuthorizeLoopTool_HostPresenterOverridesGenericDeny(t *testing.T) {
	cb := &lookupDenialPresenterCallbacks{lightProfileCallbacks: &lightProfileCallbacks{
		mockCallbacks: &mockCallbacks{allowed: map[string]bool{}},
		profile:       PromptProfileLight,
	}}
	res, denied := authorizeLoopTool(cb, "write_file", `{}`)
	if !denied || res.Result != "custom deny write_file" {
		t.Fatalf("denied=%v result=%q", denied, res.Result)
	}
}

func TestAuthorizeLoopTool_LightProfileDeniesBash(t *testing.T) {
	ResetPromptProfileStatsForTest()
	cb := &lightProfileCallbacks{
		mockCallbacks: &mockCallbacks{
			// Host policy allows everything; light profile still blocks bash.
			toolResult: "should not run",
		},
		profile: PromptProfileLight,
	}
	res, denied := authorizeLoopTool(cb, "bash", `{"command":"ls"}`)
	if !denied {
		t.Fatal("expected light deny")
	}
	if res.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("outcome=%s", res.Outcome)
	}
	if !strings.Contains(res.Result, "light prompt profile") {
		t.Fatalf("result=%q", res.Result)
	}
	st := GetPromptProfileStats()
	if st.LightToolDenies != 1 || st.LastDeniedTool != "bash" {
		t.Fatalf("stats=%+v", st)
	}
	// Allowlisted light tool should pass light guard (no host authorizer limits).
	res2, denied2 := authorizeLoopTool(cb, "web_search", `{}`)
	if denied2 {
		t.Fatalf("web_search should not be light-denied: %+v", res2)
	}
}

func TestAuthorizeLoopTool_LightProfileUsesHostCapabilityBoundaryForOpaqueNames(t *testing.T) {
	ResetPromptProfileStatsForTest()
	cb := &capabilityLightProfileCallbacks{
		lightProfileCallbacks: &lightProfileCallbacks{
			mockCallbacks: &mockCallbacks{toolResult: "ok"},
			profile:       PromptProfileLight,
		},
		allowed: map[string]bool{
			"invoke_readonly_grant": true,
			"invoke_external_grant": false,
			"web_search":            true,
		},
	}
	if _, denied := authorizeLoopTool(cb, "invoke_readonly_grant", `{}`); denied {
		t.Fatal("host-approved opaque read-only grant was rejected by its transport name")
	}
	if res, denied := authorizeLoopTool(cb, "invoke_external_grant", `{}`); !denied || !strings.Contains(res.Result, "light prompt profile") {
		t.Fatalf("host-denied external grant bypassed light policy: denied=%v result=%q", denied, res.Result)
	}
	if _, denied := authorizeLoopTool(cb, "web_search", `{}`); denied {
		t.Fatal("static light-safe tool should retain its host policy decision")
	}
}

type lightUpgradeCallbacks struct {
	*mockCallbacks
	profile  PromptProfile
	upgraded bool
	sys      string
	toolDefs []map[string]interface{}
}

func (c *lightUpgradeCallbacks) CurrentPromptProfile() PromptProfile { return c.profile }
func (c *lightUpgradeCallbacks) UpgradeLightPromptToFull(reason string) bool {
	if !c.profile.IsLight() {
		return false
	}
	c.profile = PromptProfileFull
	c.upgraded = true
	c.sys = "FULL SYSTEM " + reason
	c.toolDefs = []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "bash"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
	}
	return true
}
func (c *lightUpgradeCallbacks) BuildSystemPrompt(string, bool) string { return c.sys }
func (c *lightUpgradeCallbacks) BuildTools(string) []map[string]interface{} {
	return c.toolDefs
}

func TestTryLightProfileToolRetry_UpgradesWithoutRewritingCurrentRequestSurface(t *testing.T) {
	t.Setenv(PromptLightRetryEnvKey, "")
	ResetPromptProfileStatsForTest()
	cb := &lightUpgradeCallbacks{
		mockCallbacks: &mockCallbacks{toolResult: "ok"},
		profile:       PromptProfileLight,
		sys:           "LIGHT",
		toolDefs: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		},
	}
	tools := FilterToolDefinitionsByAuthorizer(cb, cb.BuildTools("x"))
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "LIGHT"},
		map[string]interface{}{"role": "user", "content": "run ls"},
	}
	// First authorize denies.
	res, denied := authorizeLoopTool(cb, "bash", `{}`)
	if !denied || !isLightToolDenyResult(res) {
		t.Fatalf("expected light deny: denied=%v res=%+v", denied, res)
	}
	ok := tryLightProfileToolRetry(cb, "run ls", true, "bash", &tools, conversation)
	if !ok || !cb.upgraded {
		t.Fatalf("upgrade failed ok=%v upgraded=%v", ok, cb.upgraded)
	}
	if cb.profile != PromptProfileFull {
		t.Fatalf("profile=%s", cb.profile)
	}
	// The current provider response was bound to this exact surface. An upgrade
	// may affect the successor request's renderer, but must not retrospectively
	// add bash to the already-sent request.
	names := map[string]bool{}
	for _, d := range tools {
		names[toolDefName(d)] = true
	}
	if names["bash"] || !names["web_search"] {
		t.Fatalf("current request surface changed after upgrade: %v", names)
	}
	sys, _ := conversation[0].(map[string]string)
	if !strings.Contains(sys["content"], "FULL SYSTEM") {
		t.Fatalf("system not refreshed: %q", sys["content"])
	}
	// Re-authorize should pass light guard.
	_, denied2 := authorizeLoopTool(cb, "bash", `{}`)
	if denied2 {
		t.Fatal("bash should be allowed after upgrade")
	}
	st := GetPromptProfileStats()
	if st.LightUpgrades < 1 {
		t.Fatalf("expected upgrade count: %+v", st)
	}
	if st.LastUpgradeReason == "" || !strings.Contains(st.LastUpgradeReason, "tool_deny_retry") {
		t.Fatalf("upgrade reason=%q", st.LastUpgradeReason)
	}
}

type managedLightUpgradeCallbacks struct {
	*lightUpgradeCallbacks
}

func (c *managedLightUpgradeCallbacks) ManagedSemanticTurn() bool { return true }

func TestTryLightProfileToolRetry_ManagedSemanticTurnDoesNotRebuildTools(t *testing.T) {
	t.Setenv(PromptLightRetryEnvKey, "")
	cb := &managedLightUpgradeCallbacks{lightUpgradeCallbacks: &lightUpgradeCallbacks{
		mockCallbacks: &mockCallbacks{},
		profile:       PromptProfileLight,
		toolDefs: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "invoke_lookup"}},
		},
	}}
	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "invoke_lookup"}},
	}
	conversation := []interface{}{map[string]string{"role": "system", "content": "LIGHT"}}
	if tryLightProfileToolRetry(cb, "lookup", true, "bash", &tools, conversation) {
		t.Fatal("managed semantic turn must not light→full rebuild tools")
	}
	if cb.upgraded {
		t.Fatal("managed semantic turn must not call the upgrader")
	}
	if len(tools) != 1 || toolDefName(tools[0]) != "invoke_lookup" {
		t.Fatalf("managed surface changed: %#v", tools)
	}
}

func TestTryLightProfileToolRetry_DisabledByEnv(t *testing.T) {
	t.Setenv(PromptLightRetryEnvKey, "off")
	cb := &lightUpgradeCallbacks{
		mockCallbacks: &mockCallbacks{},
		profile:       PromptProfileLight,
	}
	tools := []map[string]interface{}{}
	conversation := []interface{}{map[string]string{"role": "system", "content": "L"}}
	if tryLightProfileToolRetry(cb, "x", true, "bash", &tools, conversation) {
		t.Fatal("expected disabled")
	}
	if cb.upgraded {
		t.Fatal("should not upgrade when disabled")
	}
}

func TestExecuteLoopTool_ToolCallAuthorizerBlocksArguments(t *testing.T) {
	cb := &mockCallbacks{
		allowed:     map[string]bool{"bash": true},
		callAllowed: map[string]bool{"bash": false},
		callReason:  "high-risk command must be reviewed",
		toolResult:  "should not run",
	}

	result := executeLoopTool(cb, "bash", `{"command":"rm -rf /"}`)

	if result.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeError)
	}
	if !strings.Contains(result.Result, "high-risk command") {
		t.Fatalf("unexpected result: %q", result.Result)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("tool executed despite argument-level rejection: %#v", cb.toolCalls)
	}
}

func TestRunLoop_ToolAuthorizerFiltersExposedTools(t *testing.T) {
	var exposed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		for _, def := range req.Tools {
			exposed = append(exposed, tooldef.Name(def))
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "You are a helpful assistant.",
		tools: []map[string]interface{}{
			ToolDef("ssh", "remote shell", nil, nil),
			ToolDef("task", "spawn task", nil, nil),
		},
		allowed: map[string]bool{"ssh": true},
	}

	result := RunLoop(cb, "inspect server", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(exposed) != 1 || exposed[0] != "ssh" {
		t.Fatalf("expected only ssh to be exposed, got %v", exposed)
	}
}

func TestRunLoop_AskUserReturnsEarly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_ask_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "ask_user",
									"arguments": `{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: `__ASK_USER__{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
	}

	result := RunLoop(cb, "need help", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.AskUser == nil || result.AskUser.Question != "Choose one" {
		t.Fatalf("unexpected ask_user result: %#v", result.AskUser)
	}
	if result.PauseToolCallID != "call_ask_1" {
		t.Fatalf("PauseToolCallID = %q, want call_ask_1", result.PauseToolCallID)
	}
	if !strings.Contains(result.Text, "Choose one") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCalls)
	}
}

func TestRunLoop_RecordAudioReturnsEarly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_rec_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "record_audio",
									"arguments": `{"title":"项目例会","purpose":"讨论排期"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	marker := ToolRecordAudio(map[string]interface{}{
		"title":   "项目例会",
		"purpose": "讨论排期",
	})
	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: marker,
	}

	result := RunLoop(cb, "开始会议录音", nil, nil)
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.RecordAudio == nil {
		t.Fatal("expected RecordAudio pause result")
	}
	if result.RecordAudio.Title != "项目例会" {
		t.Fatalf("title = %q", result.RecordAudio.Title)
	}
	if result.RecordAudio.Purpose != "讨论排期" {
		t.Fatalf("purpose = %q", result.RecordAudio.Purpose)
	}
	if result.PauseToolCallID != "call_rec_1" {
		t.Fatalf("PauseToolCallID = %q, want call_rec_1", result.PauseToolCallID)
	}
	if !strings.Contains(result.Text, "项目例会") {
		t.Fatalf("display text missing title: %q", result.Text)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", result.ToolCalls)
	}
	// Loop must stop (no further iterations after interactive pause).
	if result.Iterations != 1 {
		t.Fatalf("iterations = %d, want 1", result.Iterations)
	}
}

func TestRunLoop_RecordAudioPauseUsesCurrentToolCallIDInBatch(t *testing.T) {
	// Multi-tool batch: record_audio is first; PauseToolCallID must be its id,
	// not the later sibling call that never executes after early-stop.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_rec_first",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "record_audio",
									"arguments": `{"title":"A"}`,
								},
							},
							{
								"id":   "call_bash_second",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "bash",
									"arguments": `{"command":"echo hi"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    10,
		sysPrompt:  "You are a helpful assistant.",
		toolResult: ToolRecordAudio(map[string]interface{}{"title": "A"}),
	}

	result := RunLoop(cb, "录音", nil, nil)
	if result.RecordAudio == nil {
		t.Fatal("expected RecordAudio pause")
	}
	if result.PauseToolCallID != "call_rec_first" {
		t.Fatalf("PauseToolCallID = %q, want call_rec_first (not last batch id)", result.PauseToolCallID)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1 (early-stop before sibling tools)", result.ToolCalls)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "record_audio" {
		t.Fatalf("executed tools = %v, want only record_audio", cb.toolCalls)
	}
}

func TestRunLoop_LLMNotConfigured_ReturnsError(t *testing.T) {
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{}, // empty
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error == "" {
		t.Fatal("expected error for unconfigured LLM")
	}
}

func TestRunLoop_Cancelled_ReturnsError(t *testing.T) {
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   "http://localhost:1",
			Model: "test",
			Key:   "test-key",
		},
		maxIter: 10,
		stopped: true, // immediately cancelled
	}
	result := RunLoop(cb, "hi", nil, nil)
	if result.Error != "cancelled" {
		t.Fatalf("expected 'cancelled' error, got %q", result.Error)
	}
}

func TestRunLoop_CancelBetweenToolCallsSkipsRemainingTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "first",
									"arguments": `{}`,
								},
							},
							{
								"id":   "call_2",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "second",
									"arguments": `{}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &stopAfterFirstToolCallbacks{mockCallbacks: &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    3,
		sysPrompt:  "test",
		toolResult: "ok",
	}}

	hooks := &toolBatchCommitCallbacks{}
	result := RunLoop(cb, "do tools", nil, nil, hooks)
	if result.Error != "cancelled" {
		t.Fatalf("RunLoop error = %q, want cancelled", result.Error)
	}
	if got := strings.Join(cb.toolCalls, ","); got != "first" {
		t.Fatalf("executed tools = %q, want first only", got)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("ToolCalls = %d, want 1", result.ToolCalls)
	}
	if len(hooks.starts) != 1 || len(hooks.abandons) != 1 || len(hooks.batches) != 0 {
		t.Fatalf("cancelled batch lifecycle starts=%d abandons=%d commits=%d", len(hooks.starts), len(hooks.abandons), len(hooks.batches))
	}
	if hooks.abandons[0].Sequence != hooks.starts[0].Sequence {
		t.Fatalf("cancel abandoned the wrong checkpoint: start=%#v abandon=%#v", hooks.starts[0], hooks.abandons[0])
	}
}

func TestRunLoop_MaxIterations_ReturnsError(t *testing.T) {
	// Server always returns tool calls, never a final answer.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   fmt.Sprintf("call_%d", callCounter.Add(1)),
								"type": "function",
								"function": map[string]interface{}{
									"name":      "bash",
									"arguments": `{"command":"echo loop"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    3,
		sysPrompt:  "test",
		toolResult: "ok",
	}

	result := RunLoop(cb, "loop forever", nil, nil)
	if !strings.Contains(result.Error, "max iterations") {
		t.Fatalf("expected max iterations error, got %q", result.Error)
	}
	if result.Iterations != 3 {
		t.Fatalf("expected 3 iterations, got %d", result.Iterations)
	}
	if result.ToolCalls != 3 {
		t.Fatalf("expected 3 tool calls, got %d", result.ToolCalls)
	}
}

func TestRunLoop_ConsecutiveEmptyResponses_HardExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return empty content, no tool calls.
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"role": "assistant", "content": ""},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   10,
		sysPrompt: "test",
	}

	result := RunLoop(cb, "hi", nil, nil)

	if !result.HardExit {
		t.Fatal("expected HardExit=true for consecutive empty responses")
	}
	// Should exit after 5 consecutive empty responses (maxConsecutiveEmpty).
	if result.Iterations > 6 {
		t.Fatalf("expected <=6 iterations for hard exit, got %d", result.Iterations)
	}
}

func TestRunLoop_DriftDetection_SameToolSameResult(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var respData map[string]interface{}
		if callCount <= 8 {
			// Keep returning the same tool call.
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"echo test"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			// After drift injection, return final answer.
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "I detected a loop and stopped."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
	}))
	defer server.Close()

	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:    20,
		sysPrompt:  "test",
		toolResult: "same output every time", // same result = drift
	}

	result := RunLoop(cb, "do something", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The drift detection should have injected a system message that caused
	// the LLM to stop. The loop should complete before maxIter.
	if result.Iterations >= 20 {
		t.Fatalf("expected drift detection to stop loop early, got %d iterations", result.Iterations)
	}
	if result.ToolCalls < 4 {
		t.Fatalf("expected at least 4 tool calls before drift detection, got %d", result.ToolCalls)
	}
}

func TestRunLoop_NoDrift_WhenResultsChange(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var respData map[string]interface{}
		if callCount <= 5 {
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]interface{}{
								{
									"id":   fmt.Sprintf("call_%d", callCount),
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"check_status"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		} else {
			respData = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "Task completed."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respData)
	}))
	defer server.Close()

	// Tool results change each time — this is polling, not drift.
	pollCount := 0
	cb := &mockCallbacks{
		config: corelib.MaclawLLMConfig{
			URL:   server.URL,
			Model: "test",
			Key:   "test-key",
		},
		maxIter:   20,
		sysPrompt: "test",
	}
	// Override ExecuteTool to return changing results.
	origExecute := cb.ExecuteTool
	_ = origExecute
	changingCb := &changingResultCallbacks{mockCallbacks: cb}
	changingCb.pollCount = &pollCount

	result := RunLoop(changingCb, "poll status", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Should complete normally without drift detection interfering.
	if result.Text != "Task completed." {
		t.Fatalf("unexpected text: %q", result.Text)
	}
}

// changingResultCallbacks wraps mockCallbacks but returns different results each time.
type changingResultCallbacks struct {
	*mockCallbacks
	pollCount *int
}

func (c *changingResultCallbacks) ExecuteTool(name, args string) string {
	*c.pollCount++
	return fmt.Sprintf("status: running (%d seconds)", *c.pollCount*5)
}

func TestRunLoop_EmptyResponseAfterToolTimeout_Recovers(t *testing.T) {
	// Simulates the exact scenario from the bug: tool returns timeout error,
	// then LLM returns empty response, but the recovery prompt helps it resume.
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		switch {
		case callCount == 1:
			// First call: LLM calls bash with a long-running command.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "Let me run the command.",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_1",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"sleep 120","timeout":125}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		case callCount == 2:
			// Second call: LLM returns empty response (the bug).
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": ""},
						"finish_reason": "stop",
					},
				},
			}
		case callCount == 3:
			// Third call: after recovery prompt, LLM resumes with a tool call.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "The command timed out. Let me check the status.",
							"tool_calls": []map[string]interface{}{
								{
									"id":   "call_2",
									"type": "function",
									"function": map[string]interface{}{
										"name":      "bash",
										"arguments": `{"command":"ps aux | grep sleep"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			}
		default:
			// Final call: LLM provides final answer.
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message":       map[string]interface{}{"role": "assistant", "content": "The operation completed successfully."},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	toolCallCount := 0
	cb := &timeoutToolCallbacks{
		mockCallbacks: &mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   10,
			sysPrompt: "You are a helpful assistant.",
		},
		toolCallCount: &toolCallCount,
	}

	result := RunLoop(cb, "run a long command on the server", nil, nil)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.HardExit {
		t.Fatal("should NOT hard exit — recovery prompt should help LLM resume")
	}
	if !strings.Contains(result.Text, "completed successfully") {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.ToolCalls != 2 {
		t.Fatalf("expected 2 tool calls, got %d", result.ToolCalls)
	}
}

// timeoutToolCallbacks simulates a tool that returns a timeout error on first call.
type timeoutToolCallbacks struct {
	*mockCallbacks
	toolCallCount *int
}

func (c *timeoutToolCallbacks) ExecuteTool(name, args string) string {
	*c.toolCallCount++
	c.mockCallbacks.toolCalls = append(c.mockCallbacks.toolCalls, name)
	if *c.toolCallCount == 1 {
		return "\n[错误] 命令超时（240 秒）"
	}
	return "process running"
}

func TestBuildEmptyResponseRecovery_Timeout(t *testing.T) {
	outcome := classifyToolResult("[错误] 命令超时（240 秒）")
	if outcome.kind != toolOutcomeTimeout {
		t.Fatalf("expected toolOutcomeTimeout, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "bash", outcome, "backup docker containers")
	if !strings.Contains(prompt, "超时") {
		t.Fatal("recovery prompt should mention timeout")
	}
	if !strings.Contains(prompt, "bash") {
		t.Fatal("recovery prompt should mention the tool name")
	}
	if !strings.Contains(prompt, "不要放弃") {
		t.Fatal("recovery prompt should encourage continuation")
	}
}

func TestBuildEmptyResponseRecovery_Error(t *testing.T) {
	outcome := classifyToolResult("Error: connection refused")
	if outcome.kind != toolOutcomeError {
		t.Fatalf("expected toolOutcomeError, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "ssh", outcome, "deploy to server")
	if !strings.Contains(prompt, "错误") || !strings.Contains(prompt, "ssh") {
		t.Fatal("recovery prompt should mention error and tool name")
	}
}

func TestBuildEmptyResponseRecovery_NoFalsePositiveError(t *testing.T) {
	// Normal output that contains "error" as a substring should NOT trigger
	// the error branch — classifyToolResult checks structured prefixes only.
	outcome := classifyToolResult("cat /var/log/error_log\nsome normal output")
	if outcome.kind != toolOutcomeOK {
		t.Fatalf("expected toolOutcomeOK for normal output, got %d", outcome.kind)
	}

	prompt := buildEmptyResponseRecovery(1, "bash", outcome, "check logs")
	if strings.Contains(prompt, "返回了错误") {
		t.Fatal("should not detect 'error' in normal output as an error condition")
	}
	if !strings.Contains(prompt, "请根据其结果继续") {
		t.Fatal("should use generic continuation prompt for normal output")
	}
}

func TestBuildEmptyResponseRecovery_Escalation(t *testing.T) {
	okOutcome := classifyToolResult("ok")
	emptyOutcome := toolOutcome{kind: toolOutcomeOK}

	// First empty: mild prompt.
	prompt1 := buildEmptyResponseRecovery(1, "", emptyOutcome, "test goal")
	if strings.Contains(prompt1, "警告") {
		t.Fatal("first empty should not contain warning")
	}

	// Third empty: escalated prompt with goal reminder.
	prompt3 := buildEmptyResponseRecovery(3, "bash", okOutcome, "test goal")
	if !strings.Contains(prompt3, "警告") {
		t.Fatal("third empty should contain warning")
	}
	if !strings.Contains(prompt3, "test goal") {
		t.Fatal("third empty should include user goal")
	}
}

func TestTruncateRunesSuffix(t *testing.T) {
	// ASCII: take last 5 chars.
	if got := truncateRunesSuffix("hello world", 5); got != "world" {
		t.Fatalf("expected 'world', got %q", got)
	}
	// Chinese: should not break multi-byte characters.
	if got := truncateRunesSuffix("你好世界测试", 3); got != "界测试" {
		t.Fatalf("expected '界测试', got %q", got)
	}
	// Short string: return as-is.
	if got := truncateRunesSuffix("hi", 10); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestTruncateRunesPrefix(t *testing.T) {
	// ASCII: take first 5 chars + "...".
	if got := truncateRunesPrefix("hello world", 5); got != "hello..." {
		t.Fatalf("expected 'hello...', got %q", got)
	}
	// Chinese: should not break multi-byte characters.
	if got := truncateRunesPrefix("你好世界测试", 3); got != "你好世..." {
		t.Fatalf("expected '你好世...', got %q", got)
	}
	// Short string: return as-is.
	if got := truncateRunesPrefix("hi", 10); got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestClassifyToolResult(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   toolOutcomeKind
	}{
		// Timeout cases — our tools produce these exact markers.
		{"bash timeout", "\n[错误] 命令超时（240 秒）", toolOutcomeTimeout},
		{"bash timeout with output", "partial output\n[错误] 命令超时（30 秒）", toolOutcomeTimeout},

		// Error cases — structured prefixes from our tool code.
		{"bash exit code", "\n[错误] 退出码: 1", toolOutcomeError},
		{"bash start failed", "[错误] 命令启动失败: exec: not found", toolOutcomeError},
		{"unknown tool", "未知工具: foobar", toolOutcomeError},
		{"tool panic", "工具执行异常: runtime error", toolOutcomeError},
		{"parse failed", "参数解析失败: unexpected end of JSON", toolOutcomeError},
		{"chinese error prefix", "错误: something went wrong", toolOutcomeError},
		{"english error prefix", "Error: connection refused", toolOutcomeError},
		{"mid-result error", "some output\n[错误] 后台任务启动失败: no space", toolOutcomeError},

		// OK cases — normal output should not be misclassified.
		{"normal output", "hello world", toolOutcomeOK},
		{"output with error substring", "cat /var/log/error_log\nsome data", toolOutcomeOK},
		{"output with Error in middle", "the Error was handled gracefully", toolOutcomeOK},
		{"empty result", "", toolOutcomeOK},
		{"json output", `{"status":"ok","errors":0}`, toolOutcomeOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyToolResult(tt.result)
			if got.kind != tt.want {
				t.Errorf("classifyToolResult(%q) = %d, want %d", tt.result, got.kind, tt.want)
			}
		})
	}
}

func TestExecuteLoopToolUsesStructuredOutcome(t *testing.T) {
	cb := &mockCallbacks{
		toolResult:  "Error: legacy-looking text",
		toolOutcome: ToolExecutionOutcomeOK,
	}
	result := executeLoopTool(cb, "demo", "{}")
	if result.Outcome != ToolExecutionOutcomeOK {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeOK)
	}
	outcome := toolOutcomeFromExecutionResult(result)
	if outcome.kind != toolOutcomeOK {
		t.Fatalf("toolOutcome kind = %d, want %d", outcome.kind, toolOutcomeOK)
	}
}

func TestExecuteLoopToolFallsBackWhenStructuredOutcomeUnset(t *testing.T) {
	cb := &mockCallbacks{
		toolResult: "Error: exit code: 1",
	}
	result := executeLoopTool(cb, "demo", "{}")
	if result.Outcome != ToolExecutionOutcomeError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, ToolExecutionOutcomeError)
	}
}
