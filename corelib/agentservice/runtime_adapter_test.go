package agentservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type testSharedRuntimeModule struct{}

func (testSharedRuntimeModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "shared.test", Version: "1.0.0", HeadlessSupport: true}
}

type contributingRuntimeModule struct{}

func (contributingRuntimeModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "contrib.test", Version: "1.0.0", HeadlessSupport: true}
}
func (contributingRuntimeModule) ContributePrompt(context.Context, agentruntime.TurnRequest) (string, error) {
	return "shared prompt section", nil
}
func (contributingRuntimeModule) Tools(context.Context, agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	return []agentruntime.ToolDefinition{{Name: "shared_tool", Description: "shared tool", Parameters: map[string]any{"type": "object"}}}, nil
}
func (contributingRuntimeModule) InvokeTool(context.Context, agentruntime.TurnRequest, string, map[string]any) (string, error) {
	return "shared result", nil
}

type descriptorOnlyToolModule struct{}

func (descriptorOnlyToolModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "descriptor.only", Version: "1.0.0", HeadlessSupport: true}
}
func (descriptorOnlyToolModule) Tools(context.Context, agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	return []agentruntime.ToolDefinition{{Name: "descriptor_tool"}}, nil
}

type denyRuntimeModule struct{}

func (denyRuntimeModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "deny.test", Version: "1", HeadlessSupport: true}
}
func (denyRuntimeModule) EvaluatePolicy(context.Context, agentruntime.TurnRequest) error {
	return errors.New("blocked")
}

type runtimeCountingExecutor struct{ calls int }

func (e *runtimeCountingExecutor) Execute(context.Context, ExecuteRequest) (*ExecuteResult, error) {
	e.calls++
	return &ExecuteResult{Content: "unexpected"}, nil
}

type failingRuntimeEventSink struct{ err error }

func (s failingRuntimeEventSink) Emit(context.Context, agentruntime.Event) error { return s.err }

type failingRuntimeEventExecutor struct{ err error }

func (e failingRuntimeEventExecutor) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if req.OnToken != nil {
		req.OnToken("partial")
	}
	return nil, e.err
}

type workingStateResultExecutor struct{}

func (workingStateResultExecutor) Execute(context.Context, ExecuteRequest) (*ExecuteResult, error) {
	return &ExecuteResult{Content: "answer\n[任务状态]\ninternal"}, nil
}

func TestRuntimeAdapterDelegatesExecuteAndCapabilities(t *testing.T) {
	executor := &captureExecutor{}
	runtime := NewRuntimeExecutor(executor)
	input := ExecuteRequest{
		Message:                Message{Content: "hello"},
		RuntimePrompt:          "forged prompt",
		RuntimeTools:           []agentruntime.ToolDefinition{{Name: "forged"}},
		RuntimeExecutableTools: []agentruntime.ToolDefinition{{Name: "forged"}},
		RuntimeToolInvoker:     runtimeToolInvokerStub{},
	}
	result, err := runtime.Execute(context.Background(), agentruntime.TurnRequest{Input: input})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output, ok := result.Output.(*ExecuteResult); !ok || output.Content != "ok" {
		t.Fatalf("unexpected runtime output: %#v", result.Output)
	}
	if executor.req.Message.Content != "hello" {
		t.Fatalf("adapter did not delegate request: %#v", executor.req)
	}
	if executor.req.Host == nil || !executor.req.Host.Profile().Headless {
		t.Fatalf("nil host must normalize to headless capabilities: %#v", executor.req.Host)
	}
	if executor.req.RuntimePrompt != "" || len(executor.req.RuntimeTools) != 0 || len(executor.req.RuntimeExecutableTools) != 0 || executor.req.RuntimeToolInvoker != nil {
		t.Fatalf("runtime-owned fields were not cleared: %#v", executor.req)
	}
}

func TestRuntimeAdapterCapabilityDigestAcceptsNeutralTurnInput(t *testing.T) {
	runtime := NewRuntimeExecutor(&runtimeCountingExecutor{})
	prompt := "same prompt on GUI and srv"
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{
		Input: agentruntime.TurnInput{SystemPrompt: prompt},
	})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if snapshot.PromptDigest != agentruntime.DigestPrompt(prompt) {
		t.Fatalf("prompt digest = %q, want %q", snapshot.PromptDigest, agentruntime.DigestPrompt(prompt))
	}
}

func TestNewServiceClosesOwnedStoreWhenRuntimeRegistryFails(t *testing.T) {
	root := t.TempDir()
	_, err := NewService(Config{
		DataRoot:     root,
		StoreBackend: "sqlite",
		RuntimeModules: []agentruntime.Module{
			testSharedRuntimeModule{},
			testSharedRuntimeModule{}, // duplicate module id forces failure after store creation
		},
	}, nil, EchoExecutor{})
	if err == nil {
		t.Fatal("expected duplicate runtime module construction error")
	}
	storePath := filepath.Join(root, "state", "service.db")
	renamedPath := filepath.Join(root, "state", "service.db.closed")
	if err := os.Rename(storePath, renamedPath); err != nil {
		t.Fatalf("owned SQLite store remained open after failed NewService: %v", err)
	}
}

func TestRuntimeExecutorSurfacesDurableEventSinkFailure(t *testing.T) {
	sentinel := errors.New("outbox unavailable")
	runtime := NewRuntimeExecutor(callbackEventExecutor{})
	_, err := runtime.Execute(context.Background(), agentruntime.TurnRequest{
		Input:  ExecuteRequest{Message: Message{Content: "hello"}},
		Events: failingRuntimeEventSink{err: sentinel},
	})
	if err == nil || !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("event sink failure was not surfaced: %v", err)
	}
}

func TestRuntimeExecutorJoinsExecutorAndEventSinkFailures(t *testing.T) {
	executorErr := errors.New("model failed")
	sinkErr := errors.New("outbox unavailable")
	runtime := NewRuntimeExecutor(failingRuntimeEventExecutor{err: executorErr})
	_, err := runtime.Execute(context.Background(), agentruntime.TurnRequest{
		Input:  ExecuteRequest{Message: Message{Content: "hello"}},
		Events: failingRuntimeEventSink{err: sinkErr},
	})
	if err == nil || !errors.Is(err, executorErr) || !errors.Is(err, sinkErr) {
		t.Fatalf("joined executor/event sink failures = %v", err)
	}
}

func TestDescribeRuntimeCapabilitiesUsesHeadlessFallback(t *testing.T) {
	svc, principal, instance := setupCaptureAgentService(t, EchoExecutor{})
	defer svc.Close()
	snapshot, err := svc.DescribeRuntimeCapabilities(context.Background(), principal, instance.ID)
	if err != nil {
		t.Fatalf("DescribeRuntimeCapabilities: %v", err)
	}
	if snapshot.ContractVersion != agentruntime.ContractVersion {
		t.Fatalf("contract version=%q; want %q", snapshot.ContractVersion, agentruntime.ContractVersion)
	}
	if !snapshot.Profile.Headless || snapshot.Profile.Name != "echo" {
		t.Fatalf("expected explicit headless profile, got %#v", snapshot.Profile)
	}
	if snapshot.Metadata[agentruntime.CapabilityMetadataLifecyclePersistenceMode] != "atomic" || snapshot.Metadata[agentruntime.CapabilityMetadataDurableOutbox] != "true" {
		t.Fatalf("expected lifecycle persistence metadata, got %#v", snapshot.Metadata)
	}
	for _, key := range []string{agentruntime.CapabilityMetadataAdmissionOutboxAtomic, agentruntime.CapabilityMetadataCompletionOutboxAtomic, agentruntime.CapabilityMetadataTerminalOutboxAtomic} {
		if snapshot.Metadata[key] != "true" {
			t.Fatalf("expected %s=true, metadata=%#v", key, snapshot.Metadata)
		}
	}
	if snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitEnabled] != "false" || snapshot.Metadata[agentruntime.CapabilityMetadataRateLimitBurst] != "0" {
		t.Fatalf("expected disabled default rate-limit metadata, got %#v", snapshot.Metadata)
	}
}

func TestServicePostMessageUsesSharedVisibleTextProjection(t *testing.T) {
	svc, principal, instance := setupCaptureAgentService(t, workingStateResultExecutor{})
	defer svc.Close()
	session, err := svc.CreateSession(context.Background(), principal, instance.ID, CreateSessionInput{Title: "session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_, assistant, err := svc.PostMessage(context.Background(), principal, instance.ID, session.ID, PostMessageInput{Content: "hello"})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if assistant == nil || assistant.Content != "answer" {
		t.Fatalf("assistant content=%#v, want working-state block removed", assistant)
	}
}

func TestRuntimeExecutorPublishesCompositionRootModules(t *testing.T) {
	registry, err := agentruntime.NewModuleRegistry(testSharedRuntimeModule{})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	runtime := NewRuntimeExecutorWithModules(EchoExecutor{}, registry)
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{
		Host: agentruntime.HeadlessHostCapabilities{},
	})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if len(snapshot.Modules) != 1 || snapshot.Modules[0].ModuleID != "shared.test" {
		t.Fatalf("unexpected shared modules: %#v", snapshot.Modules)
	}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345", RuntimeModules: []agentruntime.Module{testSharedRuntimeModule{}}}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService with runtime modules: %v", err)
	}
	defer svc.Close()
	serviceSnapshot, err := svc.Runtime().DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: agentruntime.HeadlessHostCapabilities{}})
	if err != nil {
		t.Fatalf("service runtime snapshot: %v", err)
	}
	ids := map[string]bool{}
	for _, module := range serviceSnapshot.Modules {
		ids[module.ModuleID] = true
	}
	if !ids["shared.test"] || !ids["prompt.default_role"] {
		t.Fatalf("service runtime modules=%#v", serviceSnapshot.Modules)
	}
}

func TestRuntimeExecutorEvaluatesModulePoliciesBeforeExecution(t *testing.T) {
	registry, err := agentruntime.NewModuleRegistry(denyRuntimeModule{})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	executor := &runtimeCountingExecutor{}
	runtime := NewRuntimeExecutorWithModules(executor, registry)
	_, err = runtime.Execute(context.Background(), agentruntime.TurnRequest{Input: ExecuteRequest{Message: Message{Content: "blocked"}}})
	if err == nil {
		t.Fatal("expected module policy denial")
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls=%d; policy must run first", executor.calls)
	}
}

func TestRuntimeExecutorForwardsModulePromptAndTools(t *testing.T) {
	registry, err := agentruntime.NewModuleRegistry(contributingRuntimeModule{})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	executor := &captureExecutor{}
	runtime := NewRuntimeExecutorWithModules(executor, registry)
	_, err = runtime.Execute(context.Background(), agentruntime.TurnRequest{Input: ExecuteRequest{Message: Message{Content: "hello"}}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if executor.req.RuntimePrompt != "shared prompt section" {
		t.Fatalf("runtime prompt=%q", executor.req.RuntimePrompt)
	}
	if len(executor.req.RuntimeTools) != 1 || executor.req.RuntimeTools[0].Name != "shared_tool" {
		t.Fatalf("runtime tools=%#v", executor.req.RuntimeTools)
	}
	if len(executor.req.RuntimeExecutableTools) != 1 || executor.req.RuntimeExecutableTools[0].Name != "shared_tool" {
		t.Fatalf("runtime executable tools=%#v", executor.req.RuntimeExecutableTools)
	}
}

func TestRuntimeExecutorCapabilitySnapshotIncludesModuleSurface(t *testing.T) {
	registry, err := agentruntime.NewModuleRegistry(contributingRuntimeModule{})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	runtime := NewRuntimeExecutorWithModules(EchoExecutor{}, registry)
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: agentruntime.HeadlessHostCapabilities{Capabilities: map[string]bool{"http": true}}})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if snapshot.PromptDigest != agentruntime.DigestPrompt("shared prompt section") {
		t.Fatalf("prompt digest=%q", snapshot.PromptDigest)
	}
	if !snapshot.Profile.Capabilities["http"] {
		t.Fatalf("host capability was lost while merging executor flags: %#v", snapshot.Profile.Capabilities)
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Name != "shared_tool" || !snapshot.Tools[0].Enabled {
		t.Fatalf("snapshot tools=%#v", snapshot.Tools)
	}
	if snapshot.SurfaceDigest == "" || snapshot.SurfaceDigest != agentruntime.DigestCapabilitySurface(snapshot) {
		t.Fatalf("surface digest=%q does not match canonical snapshot", snapshot.SurfaceDigest)
	}
}

func TestLegacyCapabilitiesCarrySurfaceDigest(t *testing.T) {
	snapshot := agentruntime.CapabilitySnapshot{
		ContractVersion: agentruntime.ContractVersion,
		Profile:         agentruntime.CapabilityProfile{Name: "headless", Headless: true, Capabilities: map[string]bool{"sessions": true}},
		PromptDigest:    agentruntime.DigestPrompt("shared"),
		Tools:           []agentruntime.CapabilityTool{{Name: "lookup", Enabled: true}},
	}
	snapshot.SurfaceDigest = agentruntime.DigestCapabilitySurface(snapshot)
	legacy := LegacyCapabilitiesFromRuntime(snapshot)
	if legacy.SurfaceDigest != snapshot.SurfaceDigest {
		t.Fatalf("legacy surface digest=%q, want %q", legacy.SurfaceDigest, snapshot.SurfaceDigest)
	}
}

func TestSafeRuntimeCapabilityMetadataIsAllowlistedAndBounded(t *testing.T) {
	longValue := strings.Repeat("x", 300)
	metadata := safeRuntimeCapabilityMetadata(map[string]string{
		"workspace_dir":              "C:/secret/workspace",
		"bash_enabled":               longValue,
		"ssh_direct_connect_enabled": "true",
	})
	if _, leaked := metadata["workspace_dir"]; leaked {
		t.Fatal("workspace path must not be exposed in runtime metadata")
	}
	if got := len([]rune(metadata["bash_enabled"])); got != 256 {
		t.Fatalf("metadata value length=%d, want 256", got)
	}
	if metadata["ssh_direct_connect_enabled"] != "true" {
		t.Fatalf("allowlisted metadata was lost: %#v", metadata)
	}
}

func TestRegisterRunCancelCancelsLateRegistrationAfterClose(t *testing.T) {
	svc := &Service{}
	svc.runMu.Lock()
	svc.closed = true
	svc.runMu.Unlock()
	cancelled := make(chan struct{})
	svc.registerRunCancel("run-late", func() { close(cancelled) })
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("late run cancellation was not invoked")
	}
}

func TestRuntimeExecutorDoesNotExposeDescriptorOnlyToolForExecution(t *testing.T) {
	registry, err := agentruntime.NewModuleRegistry(descriptorOnlyToolModule{})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	executor := &captureExecutor{}
	runtime := NewRuntimeExecutorWithModules(executor, registry)
	_, err = runtime.Execute(context.Background(), agentruntime.TurnRequest{Input: ExecuteRequest{Message: Message{Content: "hello"}}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(executor.req.RuntimeTools) != 1 || executor.req.RuntimeTools[0].Name != "descriptor_tool" {
		t.Fatalf("runtime tools=%#v", executor.req.RuntimeTools)
	}
	if len(executor.req.RuntimeExecutableTools) != 0 {
		t.Fatalf("descriptor-only tool unexpectedly executable: %#v", executor.req.RuntimeExecutableTools)
	}
}

func TestCoreAgentCallbacksConsumesExecutableRuntimeTool(t *testing.T) {
	cb := &coreAgentCallbacks{
		runtimeTools:       []agentruntime.ToolDefinition{{Name: "shared_tool", Description: "shared tool"}},
		runtimeToolInvoker: runtimeToolInvokerStub{},
	}
	defs := cb.BuildTools("Implement this repository feature and modify the code")
	for _, def := range defs {
		if tooldef.Name(def) == "shared_tool" {
			result := cb.ExecuteToolCall("shared_tool", `{"value":1}`, "call-1")
			if result.Outcome != agent.ToolExecutionOutcomeOK || result.Result != "handled result" {
				t.Fatalf("runtime tool result=%#v", result)
			}
			return
		}
	}
	t.Fatalf("shared runtime tool missing from callback surface: %#v", defs)
}

func TestCoreAgentCallbacksKeepsBuiltinToolAuthority(t *testing.T) {
	cb := &coreAgentCallbacks{
		runtimeTools:       []agentruntime.ToolDefinition{{Name: "ask_user"}},
		runtimeToolInvoker: runtimeToolInvokerStub{},
	}
	if cb.runtimeToolExposed("ask_user") {
		t.Fatal("runtime module must not intercept a built-in tool name")
	}
}

func TestCoreAgentCallbacksInjectsRuntimePrompt(t *testing.T) {
	cb := &coreAgentCallbacks{
		appCfg:        corelib.AppConfig{},
		runtimePrompt: "shared runtime instruction",
	}
	prompt := cb.BuildSystemPrompt("Implement this repository feature and modify the code", true)
	if !strings.Contains(prompt, "shared runtime instruction") {
		t.Fatalf("runtime prompt contribution missing: %q", prompt)
	}
}

type runtimeToolInvokerStub struct{}

func (runtimeToolInvokerStub) InvokeRuntimeTool(context.Context, string, map[string]any) (string, bool, error) {
	return "handled result", true, nil
}
