package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestDigestCapabilitySurfaceIsDeterministicAndMetadataIndependent(t *testing.T) {
	base := CapabilitySnapshot{
		ContractVersion: ContractVersion,
		Profile: CapabilityProfile{
			Name: "headless", Headless: true,
			Capabilities: map[string]bool{"ssh": true, "bash": false},
		},
		Modules: []ModuleDescriptor{
			{ModuleID: "z-module", Version: "2", HeadlessSupport: true, RequiredHostCapabilities: []string{"ssh", "bash"}},
			{ModuleID: "a-module", Version: "1", HeadlessSupport: true},
		},
		Tools: []CapabilityTool{
			{Name: "z_tool", Enabled: true, Parameters: map[string]any{"required": []any{"path", "mode"}}},
			{Name: "a_tool", Enabled: false, DisabledReason: "headless", Parameters: map[string]any{"type": "object"}},
		},
		PromptDigest: "prompt-digest",
		Metadata:     map[string]string{"data_root": `C:\secret`, "tenant": "tenant-a"},
	}
	// Reorder every collection and mutate metadata: behaviour digest must stay
	// stable because the helper canonicalizes order and excludes metadata.
	reordered := base
	reordered.Profile.Capabilities = map[string]bool{"bash": false, "ssh": true}
	reordered.Modules = []ModuleDescriptor{base.Modules[0], base.Modules[1]}
	reordered.Modules[0].RequiredHostCapabilities = []string{"bash", "ssh"}
	reordered.Tools = []CapabilityTool{base.Tools[0], base.Tools[1]}
	reordered.Metadata = map[string]string{"data_root": `D:\other`, "tenant": "tenant-b", "rate_limit": "1"}

	first := DigestCapabilitySurface(base)
	second := DigestCapabilitySurface(reordered)
	if first == "" || len(first) != 64 {
		t.Fatalf("unexpected digest %q", first)
	}
	if first != second {
		t.Fatalf("digest changed with ordering/metadata: %s != %s", first, second)
	}

	changed := base
	changed.Tools = append([]CapabilityTool(nil), base.Tools...)
	changed.Tools[0].Description = "changed"
	if DigestCapabilitySurface(changed) == first {
		t.Fatal("digest did not change when a tool contract changed")
	}
	duplicateA := base
	duplicateA.Tools = append([]CapabilityTool(nil), base.Tools...)
	duplicateA.Tools = append(duplicateA.Tools, CapabilityTool{Name: "lookup", Description: "b"}, CapabilityTool{Name: "lookup", Description: "a"})
	duplicateB := duplicateA
	duplicateB.Tools = append([]CapabilityTool(nil), duplicateA.Tools...)
	duplicateB.Tools[len(duplicateB.Tools)-2], duplicateB.Tools[len(duplicateB.Tools)-1] = duplicateB.Tools[len(duplicateB.Tools)-1], duplicateB.Tools[len(duplicateB.Tools)-2]
	if DigestCapabilitySurface(duplicateA) != DigestCapabilitySurface(duplicateB) {
		t.Fatal("duplicate-name tool tie-break is order dependent")
	}
}

func TestHeadlessHostCapabilitiesAreExplicitlyUnavailable(t *testing.T) {
	host := HeadlessHostCapabilities{Capabilities: map[string]bool{"bash": true}}
	profile := host.Profile()
	if !profile.Headless || profile.Name != "headless" || !profile.Capabilities["bash"] {
		t.Fatalf("unexpected headless profile: %#v", profile)
	}
	if _, ok := host.DesktopCapture(); ok {
		t.Fatal("desktop capture must be unavailable on headless host")
	}
	if _, ok := host.DocumentLauncher(); ok {
		t.Fatal("document launcher must be unavailable on headless host")
	}
	if _, ok := host.URLLauncher(); ok {
		t.Fatal("URL launcher must be unavailable on headless host")
	}
	if _, ok := host.SpeechRenderer(); ok {
		t.Fatal("speech renderer must be unavailable on headless host")
	}
}

func TestNormalizeEventEnvelopeDefaultsAndCorrelatesRun(t *testing.T) {
	occurred := time.Date(2026, 9, 1, 1, 2, 3, 456000000, time.FixedZone("test", 8*60*60))
	event, err := NormalizeEventEnvelope(Event{
		EventID:    " evt-1 ",
		Type:       " assistant.delta ",
		Scope:      Scope{TenantID: "tenant", UserID: "user"},
		RunID:      " run-1 ",
		OccurredAt: occurred,
		Payload:    map[string]any{"delta": "hello"},
	})
	if err != nil {
		t.Fatalf("NormalizeEventEnvelope: %v", err)
	}
	if event.SchemaVersion != ContractVersion || event.EventID != "evt-1" || event.Type != "assistant.delta" {
		t.Fatalf("normalized envelope=%#v", event)
	}
	if event.Scope.RunID != "run-1" || event.RunID != "run-1" {
		t.Fatalf("run correlation was not filled: %#v", event)
	}
	if !event.OccurredAt.Equal(occurred.UTC()) {
		t.Fatalf("occurred_at=%s; want %s", event.OccurredAt, occurred.UTC())
	}
}

func TestNormalizeEventEnvelopeRejectsAmbiguousOrOversizedInput(t *testing.T) {
	if _, err := NormalizeEventEnvelope(Event{Type: "tool.call", RunID: "run-a", Scope: Scope{RunID: "run-b"}}); err == nil {
		t.Fatal("mismatched run ids must be rejected")
	}
	if _, err := NormalizeEventEnvelope(Event{Type: "tool.call", EventID: strings.Repeat("x", MaxEventIDBytes+1)}); err == nil {
		t.Fatal("oversized event id must be rejected")
	}
	for _, event := range []Event{{Type: "tool.call", EventID: "evt\n1"}, {Type: "tool\n.call"}} {
		if _, err := NormalizeEventEnvelope(event); err == nil {
			t.Fatalf("line-break event envelope must be rejected: %#v", event)
		}
	}
	if _, err := NormalizeEventEnvelope(Event{Payload: map[string]any{"bad": func() {}}}); err == nil {
		t.Fatal("non-JSON payload must be rejected")
	}
}

func TestNormalizeEventEnvelopeClonesNestedPayload(t *testing.T) {
	nested := map[string]any{"value": "before"}
	payload := map[string]any{"nested": nested, "items": []any{nested}}
	event, err := NormalizeEventEnvelope(Event{Type: "tool.result", Payload: payload})
	if err != nil {
		t.Fatalf("NormalizeEventEnvelope: %v", err)
	}
	nested["value"] = "after"
	payload["new"] = true
	gotNested, ok := event.Payload["nested"].(map[string]any)
	if !ok || gotNested["value"] != "before" {
		t.Fatalf("normalized payload aliases caller map: %#v", event.Payload)
	}
	if _, exists := event.Payload["new"]; exists {
		t.Fatal("normalized payload unexpectedly changed with caller map")
	}
}

func TestBuildClientToolPromptIsDeterministicAndAlarmScoped(t *testing.T) {
	prompt := BuildClientToolPrompt(ClientToolPromptRequest{ToolNames: []string{" alarm_create ", "alarm_clear"}, ClientID: "device-1"})
	for _, want := range []string{
		"Device-local alarm contract (mandatory)",
		"not on the computer or MaClaw GUI",
		"call alarm_create",
		"device-1",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("shared client prompt missing %q: %s", want, prompt)
		}
	}
	if got := BuildClientToolPrompt(ClientToolPromptRequest{ToolNames: []string{"device_led"}}); got != "" {
		t.Fatalf("non-alarm client tool produced prompt: %q", got)
	}
}

func TestBuildAssistantBindingPromptIsScopedAndDeterministic(t *testing.T) {
	binding := &agent.AssistantBinding{
		BotProfileID:        " bot-1 ",
		Mode:                "review",
		WorkingDirectory:    `D:\repo`,
		DocumentDirectories: []string{"docs", "specs"},
		InitialPrompt:       "只读取授权目录",
	}
	prompt := BuildAssistantBindingPrompt(binding)
	for _, want := range []string{"bot-1", "助手模式: review", `工作目录: D:\repo`, "文档检索目录: docs, specs", "只读取授权目录", "不得声称读取"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("binding prompt missing %q: %s", want, prompt)
		}
	}
	if got := BuildAssistantBindingPrompt(nil); got != "" {
		t.Fatalf("nil binding produced prompt: %q", got)
	}
	if got := BuildAssistantBindingPrompt(&agent.AssistantBinding{Mode: "review"}); got != "" {
		t.Fatalf("unidentified binding produced prompt: %q", got)
	}
}

func TestBuildLightPromptUsesSharedExecutionFence(t *testing.T) {
	prompt := BuildLightPrompt(LightPromptRequest{
		RoleName:            "MaClaw",
		RoleDescription:     "lookup assistant",
		UserText:            "天气",
		ExecutionLayer:      "light",
		ExecutionTask:       "fast",
		ExecutionConfidence: 0.91,
		ExecutionReason:     "simple lookup",
		CurrentTime:         time.Date(2026, 9, 1, 12, 34, 56, 0, time.FixedZone("CST", 8*60*60)),
		ClientTools:         []agent.ClientToolDefinition{{Name: "alarm_create"}},
		AssistantBinding:    &agent.AssistantBinding{BotProfileID: "bot-1"},
	})
	for _, want := range []string{"lookup assistant", "Use the smallest sufficient action", "Do not inspect local files", "Current local time: 2026-09-01 12:34:56 +0800", "Execution profile: layer=light task=fast confidence=0.91", "bot-1"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("shared light prompt missing %q: %s", want, prompt)
		}
	}
}

func TestWorkflowDocumentDeliveryPromptsRemainDistinct(t *testing.T) {
	desktop := DesktopWorkflowDocumentDeliveryPrompt()
	im := IMWorkflowDocumentDeliveryPrompt()
	if desktop == "" || im == "" || desktop == im {
		t.Fatal("desktop and IM workflow delivery contracts must be non-empty and distinct")
	}
	if !strings.Contains(desktop, "不要使用 office") || !strings.Contains(desktop, "右侧的预览面板") {
		t.Fatalf("desktop delivery contract lost markdown preview rules: %s", desktop)
	}
	if !strings.Contains(im, "必须") || !strings.Contains(im, "generate_pdf") {
		t.Fatalf("IM delivery contract lost PDF rule: %s", im)
	}
}

func TestEnsureLightSemanticGrantPromptFenceIsBoundedAndIdempotent(t *testing.T) {
	prompt := EnsureLightSemanticGrantPromptFence("base prompt")
	if !strings.Contains(prompt, "Governed tools: the live tool list is the ground truth") || !strings.Contains(prompt, "tools_search") {
		t.Fatalf("compact light fence missing routing contract: %s", prompt)
	}
	if len(prompt) > 512 {
		t.Fatalf("compact light fence unexpectedly large: %d", len(prompt))
	}
	if got := EnsureLightSemanticGrantPromptFence(prompt); got != prompt {
		t.Fatal("light fence is not idempotent")
	}
}

func TestDisplayReasoningPrefersResultThenAssistantHistory(t *testing.T) {
	if got := DisplayReasoning(agent.LoopResult{Reasoning: " result ", HistoryDelta: []agent.ConversationEntry{{Role: "assistant", ReasoningContent: "history"}}}); got != "result" {
		t.Fatalf("DisplayReasoning result = %q, want result", got)
	}
	if got := DisplayReasoning(agent.LoopResult{HistoryDelta: []agent.ConversationEntry{{Role: "user", ReasoningContent: "ignored"}, {Role: "assistant", ReasoningContent: " history "}}}); got != "history" {
		t.Fatalf("DisplayReasoning history = %q, want history", got)
	}
}

type testRuntime struct{}

func (testRuntime) Execute(context.Context, TurnRequest) (TurnResult, error) {
	return TurnResult{}, nil
}
func (testRuntime) DescribeCapabilities(context.Context, CapabilityRequest) (CapabilitySnapshot, error) {
	return CapabilitySnapshot{ContractVersion: ContractVersion}, nil
}
func (testRuntime) Close() error { return nil }

func TestRuntimeContractVersionIsStable(t *testing.T) {
	var _ Runtime = testRuntime{}
	if ContractVersion == "" {
		t.Fatal("contract version must not be empty")
	}
}

func TestCapabilityUnavailableErrorUsesStableCode(t *testing.T) {
	err := CapabilityUnavailableError{Capability: "desktop_capture"}
	if got, want := err.Error(), "capability_unavailable: desktop_capture"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

type testModule struct{ id string }

func (m testModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{ModuleID: m.id, Version: "1"}
}

func TestModuleRegistryRejectsDuplicateIDsAndSortsSnapshot(t *testing.T) {
	var typedNil *testModule
	registry, err := NewModuleRegistry(typedNil)
	if err != nil || len(registry.Snapshot()) != 0 {
		t.Fatalf("typed nil module should be ignored: registry=%#v err=%v", registry, err)
	}
	if _, err := NewModuleRegistry(testModule{id: "same"}, testModule{id: "same"}); err == nil {
		t.Fatal("duplicate module IDs must be rejected")
	}
	registry, err = NewModuleRegistry(testModule{id: "z"}, testModule{id: "a"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	snapshot := registry.Snapshot()
	if len(snapshot) != 2 || snapshot[0].ModuleID != "a" || snapshot[1].ModuleID != "z" {
		t.Fatalf("unexpected deterministic snapshot: %#v", snapshot)
	}
}

func TestModuleRegistryNormalizesWhitespaceModuleIDs(t *testing.T) {
	registry, err := NewModuleRegistry(contractTestModule{id: "  spaced.module  ", prompt: "prompt"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	prompt, err := registry.ContributePrompt(context.Background(), TurnRequest{})
	if err != nil || prompt != "prompt" {
		t.Fatalf("whitespace module was not executable after registration: prompt=%q err=%v", prompt, err)
	}
	snapshot := registry.Snapshot()
	if len(snapshot) != 1 || snapshot[0].ModuleID != "spaced.module" {
		t.Fatalf("module id was not normalized in snapshot: %#v", snapshot)
	}
}

type contractTestModule struct {
	id     string
	prompt string
	tool   string
	deny   bool
}

func (m contractTestModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{ModuleID: m.id, Version: "1", HeadlessSupport: true}
}
func (m contractTestModule) ContributePrompt(context.Context, TurnRequest) (string, error) {
	return m.prompt, nil
}
func (m contractTestModule) Tools(context.Context, TurnRequest) ([]ToolDefinition, error) {
	if m.tool == "" {
		return nil, nil
	}
	return []ToolDefinition{{Name: m.tool}}, nil
}
func (m contractTestModule) EvaluatePolicy(context.Context, TurnRequest) error {
	if m.deny {
		return errors.New("denied by test policy")
	}
	return nil
}

func TestModuleRegistryAggregatesPromptToolsAndPoliciesDeterministically(t *testing.T) {
	registry, err := NewModuleRegistry(
		contractTestModule{id: "z", prompt: "second", tool: "z.tool"},
		contractTestModule{id: "a", prompt: "first", tool: "a.tool"},
	)
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	prompt, err := registry.ContributePrompt(context.Background(), TurnRequest{})
	if err != nil || prompt != "first\n\nsecond" {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
	tools, err := registry.Tools(context.Background(), TurnRequest{})
	if err != nil || len(tools) != 2 || tools[0].Name != "a.tool" || tools[1].Name != "z.tool" {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
	if err := registry.EvaluatePolicies(context.Background(), TurnRequest{}); err != nil {
		t.Fatalf("EvaluatePolicies: %v", err)
	}
}

func TestModuleRegistryRejectsDuplicateToolNames(t *testing.T) {
	registry, err := NewModuleRegistry(contractTestModule{id: "a", tool: "same"}, contractTestModule{id: "b", tool: "same"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	if _, err := registry.Tools(context.Background(), TurnRequest{}); err == nil {
		t.Fatal("duplicate tool names must be rejected")
	}
}

func TestDigestCapabilitySurfaceFailsClosedForNonJSONToolSchema(t *testing.T) {
	snapshot := CapabilitySnapshot{
		ContractVersion: ContractVersion,
		Profile:         CapabilityProfile{Name: "headless", Headless: true},
		Tools: []CapabilityTool{{
			Name:       "invalid_schema",
			Parameters: map[string]any{"validator": func() {}},
		}},
	}
	want := DigestPrompt("invalid capability surface")
	if got := DigestCapabilitySurface(snapshot); got != want {
		t.Fatalf("invalid schema digest=%q, want fail-closed digest %q", got, want)
	}
}

func TestModuleRegistryStopsAtFirstDeniedPolicy(t *testing.T) {
	registry, err := NewModuleRegistry(contractTestModule{id: "a", deny: true}, contractTestModule{id: "b", deny: true})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	if err := registry.EvaluatePolicies(context.Background(), TurnRequest{}); err == nil {
		t.Fatal("expected denied policy")
	}
}

type hostScopedModule struct {
	headless bool
	required string
}

func (m hostScopedModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{ModuleID: "host.scoped", Version: "1", HeadlessSupport: m.headless, RequiredHostCapabilities: []string{m.required}}
}
func (hostScopedModule) ContributePrompt(context.Context, TurnRequest) (string, error) {
	return "host prompt", nil
}
func (hostScopedModule) Tools(context.Context, TurnRequest) ([]ToolDefinition, error) {
	return []ToolDefinition{{Name: "host.tool"}}, nil
}
func (hostScopedModule) InvokeTool(context.Context, TurnRequest, string, map[string]any) (string, error) {
	return "host result", nil
}

func TestModuleRegistryFiltersModulesByHostCapabilities(t *testing.T) {
	registry, err := NewModuleRegistry(hostScopedModule{headless: false, required: "desktop_capture"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	headless := HeadlessHostCapabilities{}
	if got := registry.SnapshotForHost(headless); len(got) != 0 {
		t.Fatalf("headless snapshot unexpectedly includes GUI module: %#v", got)
	}
	prompt, err := registry.ContributePrompt(context.Background(), TurnRequest{Host: headless})
	if err != nil {
		t.Fatalf("ContributePrompt: %v", err)
	}
	if prompt != "" {
		t.Fatalf("headless prompt unexpectedly includes GUI module: %q", prompt)
	}
	tools, err := registry.Tools(context.Background(), TurnRequest{Host: headless})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("headless tools unexpectedly include GUI module: %#v", tools)
	}
}

func TestModuleRegistryAllowsRequiredCapabilityAdvertisedByHost(t *testing.T) {
	registry, err := NewModuleRegistry(hostScopedModule{headless: true, required: "desktop_capture"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	host := HeadlessHostCapabilities{Capabilities: map[string]bool{"desktop_capture": true}}
	tools, err := registry.Tools(context.Background(), TurnRequest{Host: host})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "host.tool" {
		t.Fatalf("advertised host capability should enable module: %#v", tools)
	}
}

func TestModuleRegistryReturnsCapabilityUnavailableForKnownTool(t *testing.T) {
	registry, err := NewModuleRegistry(hostScopedModule{headless: true, required: "desktop_capture"})
	if err != nil {
		t.Fatalf("NewModuleRegistry: %v", err)
	}
	_, executable, invokeErr := registry.InvokeTool(context.Background(), TurnRequest{Host: HeadlessHostCapabilities{}}, "host.tool", nil)
	if !executable {
		t.Fatal("known tool must remain distinguishable from an unknown tool")
	}
	var unavailable CapabilityUnavailableError
	if !errors.As(invokeErr, &unavailable) || unavailable.Capability != "desktop_capture" {
		t.Fatalf("invoke error=%v; want capability_unavailable for desktop_capture", invokeErr)
	}
}
