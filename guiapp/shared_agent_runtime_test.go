package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type fakeGUIRuntime struct {
	called bool
	input  agentruntime.TurnInput
}

func (r *fakeGUIRuntime) Execute(_ context.Context, request agentruntime.TurnRequest) (agentruntime.TurnResult, error) {
	r.called = true
	input, ok := request.Input.(agentruntime.TurnInput)
	r.input = input
	if !ok || input.UserText != "hello" || input.Platform != "desktop" {
		return agentruntime.TurnResult{}, context.Canceled
	}
	return agentruntime.TurnResult{Output: &IMAgentResponse{Text: "delegated"}}, nil
}

func TestRunAgentLoopSharedUsesMinimalGUIHostPayload(t *testing.T) {
	fake := &fakeGUIRuntime{}
	h := &IMMessageHandler{}
	h.SetSharedAgentRuntime(fake)
	loopCtx := &LoopContext{ID: "loop-1"}
	history := []agent.ConversationEntry{{Role: "user", Content: "prior"}}
	attachments := []MessageAttachment{{FileName: "note.txt", MimeType: "text/plain"}}
	response := h.runAgentLoopShared(loopCtx, "user-1", "prompt", history, "hello", attachments, nil, nil, nil, nil, 1, "desktop")
	if response == nil || response.Error != "" {
		t.Fatalf("runtime delegation failed: %#v", response)
	}
	host, ok := fake.input.HostPayload.(guiRuntimeContext)
	if !ok {
		t.Fatalf("host payload type = %T, want guiRuntimeContext", fake.input.HostPayload)
	}
	if host.ctx != loopCtx || host.userID != "user-1" {
		t.Fatalf("host payload leaked or lost context: %#v", host)
	}
	if got, ok := fake.input.History.([]agent.ConversationEntry); !ok || len(got) != 1 || got[0].Content != "prior" {
		t.Fatalf("history payload = %#v", fake.input.History)
	}
	if got, ok := fake.input.Attachments.([]MessageAttachment); !ok || len(got) != 1 || got[0].FileName != "note.txt" {
		t.Fatalf("attachments payload = %#v", fake.input.Attachments)
	}
}

func TestGUIRuntimeRejectsScopePayloadMismatch(t *testing.T) {
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}}
	_, err := runtime.Execute(context.Background(), agentruntime.TurnRequest{
		Scope: agentruntime.Scope{UserID: "other-user"},
		Input: agentruntime.TurnInput{HostPayload: guiRuntimeContext{userID: "user-1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "scope user") {
		t.Fatalf("scope/payload mismatch must be rejected, got %v", err)
	}
}

func TestGUIRuntimeExecuteAppliesRequestHost(t *testing.T) {
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}}
	loopCtx := &LoopContext{ID: "loop-host"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Execute(ctx, agentruntime.TurnRequest{
		Scope: agentruntime.Scope{UserID: "user-1"},
		Host:  guiHostCapabilities{},
		Input: agentruntime.TurnInput{HostPayload: guiRuntimeContext{ctx: loopCtx, userID: "user-1"}},
	})
	if err == nil {
		t.Fatal("cancelled context should fail before the loop")
	}
	if loopCtx.Host == nil {
		t.Fatal("request Host must be stored on the loop context")
	}
}

func TestGUIRuntimeExecuteAcceptsOmittedHistory(t *testing.T) {
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Execute(ctx, agentruntime.TurnRequest{
		Scope: agentruntime.Scope{UserID: "user-1"},
		Input: agentruntime.TurnInput{UserText: "hello", HostPayload: guiRuntimeContext{userID: "user-1"}},
	})
	if err == nil {
		t.Fatal("cancelled context should fail before the loop")
	}
}

// TestGUIAndSrvCapabilityParityUsesSharedFixture exercises both transport
// adapters against the same composition-root registry. Unlike the historical
// EchoExecutor-only test, this verifies the GUI adapter's module path and the
// service adapter's module path publish the same effective surface.
func TestGUIAndSrvCapabilityParityUsesSharedFixture(t *testing.T) {
	registry, err := agentruntime.RegisterBuiltinModules(parityFixtureModule{})
	if err != nil {
		t.Fatal(err)
	}
	host := agentruntime.HeadlessHostCapabilities{Capabilities: map[string]bool{"http": true}}
	guiSnapshot, err := (&guiSharedAgentRuntime{modules: registry}).DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("GUI snapshot: %v", err)
	}
	srvSnapshot, err := agentservice.NewRuntimeExecutorWithModules(parityExecutor{}, registry).DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("srv snapshot: %v", err)
	}
	if guiSnapshot.SurfaceDigest != srvSnapshot.SurfaceDigest {
		t.Fatalf("surface parity mismatch: GUI=%s srv=%s", guiSnapshot.SurfaceDigest, srvSnapshot.SurfaceDigest)
	}
}

type parityExecutor struct{}

func (parityExecutor) Execute(context.Context, agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	return &agentservice.ExecuteResult{Content: "ok"}, nil
}

type parityFixtureModule struct{}

func (parityFixtureModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "fixture.shared", Version: "1.0.0", HeadlessSupport: true}
}

func (parityFixtureModule) ContributePrompt(context.Context, agentruntime.TurnRequest) (string, error) {
	return "shared fixture prompt", nil
}

func (parityFixtureModule) Tools(context.Context, agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	return []agentruntime.ToolDefinition{{Name: "fixture.echo", Description: "shared fixture tool", Parameters: map[string]any{"type": "object"}}}, nil
}

func (parityFixtureModule) InvokeTool(context.Context, agentruntime.TurnRequest, string, map[string]any) (string, error) {
	return "ok", nil
}

func (*fakeGUIRuntime) DescribeCapabilities(context.Context, agentruntime.CapabilityRequest) (agentruntime.CapabilitySnapshot, error) {
	return agentruntime.CapabilitySnapshot{ContractVersion: agentruntime.ContractVersion}, nil
}

func (*fakeGUIRuntime) Close() error { return nil }

func TestRunAgentLoopSharedDelegatesThroughRuntimeContract(t *testing.T) {
	fake := &fakeGUIRuntime{}
	h := &IMMessageHandler{}
	h.SetSharedAgentRuntime(fake)
	response := h.runAgentLoopShared(nil, "user-1", "prompt", nil, "hello", nil, nil, nil, nil, nil, 0, "desktop")
	if !fake.called || response == nil || response.Text != "delegated" {
		t.Fatalf("runtime delegation failed: called=%v response=%#v", fake.called, response)
	}
}

func TestStandaloneCompositionCanInjectSharedRuntime(t *testing.T) {
	fake := &fakeGUIRuntime{}
	h := NewIMMessageHandlerStandalone(StandaloneConfig{SharedAgentRuntime: fake})
	if got := h.sharedAgentRuntime(); got != fake {
		t.Fatalf("standalone runtime = %T, want injected runtime", got)
	}
}

func TestGUIRuntimeMergesHandlerCatalogWithBuiltinModules(t *testing.T) {
	registry, err := agentruntime.RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}, modules: registry}
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: guiHostCapabilities{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) < 2 {
		t.Fatalf("merged GUI surface too small: %#v", snapshot.Tools)
	}
	ids := map[string]bool{}
	for _, module := range snapshot.Modules {
		ids[module.ModuleID] = true
	}
	if !ids["prompt.default_role"] {
		t.Fatalf("merged snapshot missing builtin modules: %#v", snapshot.Modules)
	}
}

func TestGUIRuntimeDescribesDeterministicToolSurface(t *testing.T) {
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}}
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tools) == 0 || snapshot.SurfaceDigest == "" {
		t.Fatalf("missing GUI capability surface: %#v", snapshot)
	}
	for i := 1; i < len(snapshot.Tools); i++ {
		if snapshot.Tools[i-1].Name > snapshot.Tools[i].Name {
			t.Fatalf("tool surface is not sorted: %q > %q", snapshot.Tools[i-1].Name, snapshot.Tools[i].Name)
		}
	}
}

func TestGUIRuntimeCapabilityDigestUsesTurnPrompt(t *testing.T) {
	runtime := &guiSharedAgentRuntime{handler: &IMMessageHandler{}}
	prompt := "shared prompt for this turn"
	snapshot, err := runtime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{
		Input: agentruntime.TurnInput{SystemPrompt: prompt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PromptDigest != agentruntime.DigestPrompt(prompt) {
		t.Fatalf("prompt digest = %q, want %q", snapshot.PromptDigest, agentruntime.DigestPrompt(prompt))
	}
}
