package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestHasCodingImageAttachment(t *testing.T) {
	if hasCodingImageAttachment(nil) {
		t.Fatal("nil")
	}
	if hasCodingImageAttachment([]agent.MessageAttachment{{Type: "file", MimeType: "text/plain"}}) {
		t.Fatal("plain file")
	}
	if !hasCodingImageAttachment([]agent.MessageAttachment{{Type: "image", MimeType: "image/png"}}) {
		t.Fatal("image type")
	}
	if !hasCodingImageAttachment([]agent.MessageAttachment{{MimeType: "image/jpeg"}}) {
		t.Fatal("jpeg mime")
	}
}

func TestShouldUseRemoteCodingIsolate(t *testing.T) {
	if shouldUseRemoteCodingIsolate(codingWorktreeModeOff, true, "implement", "write", nil) {
		t.Fatal("off")
	}
	if shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "explore", "探查代码", nil) {
		t.Fatal("explore")
	}
	// T1-style remote preflight: check env/workdir only — must not isolate.
	// Description mentions 编译器/构建工具 (false friends of 编译/构建).
	if shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "检查远端环境与目录",
		"连接到远程服务器，检查工作目录是否存在以及 C++ 编译器、构建工具版本", nil) {
		t.Fatal("env-check preflight should be explore-only and skip isolate")
	}
	if !isCodingPlanExploreOnlyStep("检查远端环境与目录", "检查工作目录与编译器、构建工具版本") {
		t.Fatal("检查 keywords should mark explore-only (编译器/构建工具 must not hard-block)")
	}
	if isCodingPlanExploreOnlyStep("编译并验收", "编译项目并运行验收测试") {
		t.Fatal("real compile/verify step must not be explore-only")
	}
	// Bare "目录/环境" without check/preflight phrases should not force explore-only.
	if isCodingPlanExploreOnlyStep("准备目录结构", "在环境中准备目录结构") {
		t.Fatal("bare 目录/环境 without check intent must not be explore-only")
	}
	if !shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "implement JWT", "写代码", nil) {
		t.Fatal("auto planned independent implement")
	}
	if shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "implement JWT", "写代码", []int{1}) {
		t.Fatal("auto chained should not isolate")
	}
	if !shouldUseRemoteCodingIsolate(codingWorktreeModeAlways, false, "implement", "x", []int{1}) {
		t.Fatal("always")
	}
}

func TestRemoteIsolateMergeGateRejectsUnsafeClaimsAndRequiresCompleteFrame(t *testing.T) {
	for _, writes := range [][]string{nil, {"."}, {"./internal/config.go"}, {"internal/../config.go"}, {"../outside.go"}, {"/etc/passwd"}, {"*.go"}, {"internal/${target}.go"}} {
		if err := validateRemoteIsolateWriteClaims(writes); err == nil {
			t.Fatalf("unsafe remote isolate write set accepted: %#v", writes)
		}
	}
	if err := validateRemoteIsolateWriteClaims([]string{"cmd/app/", "internal/config.go"}); err != nil {
		t.Fatalf("valid write set rejected: %v", err)
	}
	start, end := "__REMOTE_BEGIN__", "__REMOTE_END__"
	if remoteIsolateMergeFrameComplete("echo "+start+" "+end, start, end) {
		t.Fatal("single echoed marker line was accepted as an executed merge frame")
	}
	if remoteIsolateMergeFrameComplete("echo "+start+"\n"+start+"\nresult\n", start, end) {
		t.Fatal("incomplete merge frame was accepted")
	}
	if !remoteIsolateMergeFrameComplete("echo "+start+" "+end+"\n"+start+"\nmerged\n"+end+"\n", start, end) {
		t.Fatal("final complete merge frame was rejected")
	}
}

func TestRemoteIsolateFrozenWriteScopeContainsOnlyClaimedFiles(t *testing.T) {
	for _, tc := range []struct {
		claim string
		file  string
		want  bool
	}{
		{"internal/config.go", "internal/config.go", true},
		{"internal/config.go", "internal/other.go", false},
		{"cmd/", "cmd/app/main.go", true},
		{"cmd/", "cmdx/main.go", false},
		{"cmd", "cmd/main.go", false},
		{"cmd/", "cmd/../secret.go", false},
	} {
		if got := remoteIsolateClaimContainsFile(tc.claim, tc.file); got != tc.want {
			t.Fatalf("claim %q file %q = %v, want %v", tc.claim, tc.file, got, tc.want)
		}
	}
}

func TestRemoteIsolateAlwaysModeFailsClosed(t *testing.T) {
	if !remoteIsolateCreationMustFailClosed(codingWorktreeModeAlways) {
		t.Fatal("always mode must not fall back to the primary remote workspace")
	}
	if remoteIsolateCreationMustFailClosed(codingWorktreeModeAuto) || remoteIsolateCreationMustFailClosed(codingWorktreeModeOff) {
		t.Fatal("only always mode should require a remote isolate")
	}
}

func TestManagedRemoteCodingIsolatePathRejectsUntrustedDeleteTargets(t *testing.T) {
	for _, value := range []string{"/tmp/maclaw-wt-1", "/tmp/maclaw-coding-2"} {
		if !isManagedRemoteCodingIsolatePath(value) {
			t.Fatalf("managed isolate path rejected: %q", value)
		}
	}
	for _, value := range []string{"", "/tmp", "/tmp/../etc", "/srv/app", "/tmp/other/maclaw-wt-1", "/tmp/maclaw-wt-1/child"} {
		if isManagedRemoteCodingIsolatePath(value) {
			t.Fatalf("untrusted isolate path accepted: %q", value)
		}
	}
}

func TestRemoteConflictResolutionRequiresFrozenScope(t *testing.T) {
	c := codingWorkbenchConflict{Path: "/tmp/maclaw-wt-1", Files: []string{"internal/", "README.md"}}
	if got, err := validateRemoteConflictFileWithinFrozenScope(c, "internal/config.go"); err != nil || got != "internal/config.go" {
		t.Fatalf("valid scoped conflict file = %q, %v", got, err)
	}
	for _, file := range []string{"other.go", "internal/../secret.go", "/etc/passwd"} {
		if _, err := validateRemoteConflictFileWithinFrozenScope(c, file); err == nil {
			t.Fatalf("unscoped conflict file accepted: %q", file)
		}
	}
	if _, err := remoteConflictExactFilesForBulkResolution(c, nil); err == nil {
		t.Fatal("remote bulk resolution must require explicit files")
	}
	files, err := remoteConflictExactFilesForBulkResolution(c, []string{"internal/config.go", "README.md"})
	if err != nil || len(files) != 2 {
		t.Fatalf("selected scoped files = %#v, %v", files, err)
	}
}

func TestRemoteGitWorktreeMergeCommandIsFailClosed(t *testing.T) {
	command := remoteGitWorktreeMergeCommand("/tmp/maclaw-wt-1", "/srv/repo", 7, []string{"internal/", "cmd/app/main.go"}, "BEGIN", "END")
	for _, required := range []string{
		"test \"$#\" -gt 0", "undeclared isolate write", "git status --porcelain", "git cherry-pick --allow-empty", "git cherry-pick --abort", "BEGIN", "END",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("controlled remote merge command lacks %q: %s", required, command)
		}
	}
	for _, forbidden := range []string{"rsync -a", "cp -a"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("controlled remote merge command retains unsafe fallback %q: %s", forbidden, command)
		}
	}
}

func TestRecordStickyCodingRoute(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:route-test"
	h.recordStickyCodingRoute(userID, "gpt-4o", "route", "vision", "has image")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.LastRouteModel != "gpt-4o" || !strings.Contains(mem.LastRouteReason, "image") {
		t.Fatalf("%+v", mem)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingRouteCapabilitiesWithoutRouter(t *testing.T) {
	invalidateCodingRouteCapabilitiesCache()
	h := &IMMessageHandler{}
	caps := h.codingRouteCapabilities()
	if len(caps) != 4 {
		t.Fatalf("%+v", caps)
	}
	byPref := map[string]codingRouteCapability{}
	for _, c := range caps {
		byPref[c.Pref] = c
	}
	if !byPref[codingRoutePrefPrimary].Available {
		t.Fatal("primary always available")
	}
	if !strings.Contains(byPref[codingRoutePrefReasoning].Note, "reasoning") &&
		byPref[codingRoutePrefReasoning].Source != "primary" {
		// without router: note or primary source
		t.Logf("reasoning cap=%+v", byPref[codingRoutePrefReasoning])
	}
	// Cache hit returns equal length.
	caps2 := h.codingRouteCapabilities()
	if len(caps2) != 4 {
		t.Fatalf("cache %+v", caps2)
	}
	invalidateCodingRouteCapabilitiesCache()
	md := h.formatCodingRouteCapabilitiesMarkdown()
	if !strings.Contains(md, "ModelRouter") || !strings.Contains(md, "reasoning") {
		t.Fatalf("md=%s", md)
	}
}

func TestCodingRouteKeepsCodingProfileSnapshot(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {
			Model:         "coding-reasoning",
			URL:           "https://coding-route.example/v1",
			Provider:      "Coding route",
			ContextLength: 400_000,
		},
	})
	h := &IMMessageHandler{app: app}
	base := corelib.MaclawLLMConfig{
		URL: "https://coding-base.example/v1", Key: "coding-key", Model: "coding-base",
		ContextLength: 32_000, ProviderName: "Coding provider", ProviderID: "coding-provider-id", Profile: "coding", RouteSource: "base",
	}

	routed := h.applyCodingRoutePreference("coding-route-test", base, false)
	if routed.Model != "coding-reasoning" || routed.URL != "https://coding-route.example/v1" || routed.ProviderName != "Coding route" || routed.ContextLength != 400_000 {
		t.Fatalf("unexpected coding route: %+v", routed)
	}
	if routed.Profile != "coding" || routed.ProviderID != "coding-provider-id" || routed.RouteSource != "route" {
		t.Fatalf("route lost coding attribution: %+v", routed)
	}
}

func TestCodingRouteAppliesContextOnlyOverride(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: "coding-base", ContextLength: 400_000},
	})
	h := &IMMessageHandler{app: app}
	base := corelib.MaclawLLMConfig{
		URL: "https://coding.example/v1", Key: "coding-key", Model: "coding-base", ContextLength: 32_000,
		ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding", RouteSource: "base",
	}

	routed := h.routeCodingLLMConfig(llm.TaskReasoning, base)
	if routed.ContextLength != 400_000 || routed.RouteSource != "route" {
		t.Fatalf("context-only route was discarded: %+v", routed)
	}
}

func TestLocalCodingRouteTurnKeepsCodingProfileSnapshot(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: "routed-model"},
	})
	base := corelib.MaclawLLMConfig{Model: "coding-base", ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding", RouteSource: "base"}
	cb := &codingSubAgentCallbacks{subagent: NewCodingSubAgent(&IMMessageHandler{app: app}, base, nil, "", nil)}
	routed, decision, applied := cb.RouteTurn("implement")
	if !applied || decision.Source != "route" || routed.Model != "routed-model" {
		t.Fatalf("unexpected local coding route: cfg=%+v decision=%+v applied=%v", routed, decision, applied)
	}
	if routed.Profile != "coding" || routed.ProviderID != "coding-id" || routed.RouteSource != "route" {
		t.Fatalf("local route lost coding attribution: %+v", routed)
	}
}

func TestCodingRouteDoesNotInheritVisionFromReplacedModel(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: "text-only-route"},
	})
	h := &IMMessageHandler{app: app}
	base := corelib.MaclawLLMConfig{
		URL: "https://coding.example/v1", Model: "vision-base", SupportsVision: true,
		ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding",
	}
	routed := h.routeCodingLLMConfig(llm.TaskReasoning, base)
	if routed.SupportsVision {
		t.Fatalf("routed model inherited unverified vision capability: %+v", routed)
	}
}

func TestLocalCodingRouteTurn_HubManagedAttachesWorkflowHints(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: "routed-model", URL: "https://aux.example/v1"},
	})
	base := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", ProviderName: "hub", Profile: "coding"}
	loop := &LoopContext{WorkflowAgentLoop: true, WorkflowType: "coding", WorkflowPhaseKind: "execution", WorkflowPhaseID: "implementation"}
	cb := &codingSubAgentCallbacks{subagent: NewCodingSubAgent(&IMMessageHandler{app: app}, base, nil, "", loop)}
	routed, decision, applied := cb.RouteTurn("implement")
	if !applied || routed.Model != "auto" || routed.URL != base.URL {
		t.Fatalf("hub-managed coding must keep auto: cfg=%+v decision=%+v applied=%v", routed, decision, applied)
	}
	if routed.WorkflowTypeHint != "coding" || routed.PhaseKindHint != "execution" || routed.TaskTypeHint != string(llm.TaskReasoning) {
		t.Fatalf("hub-managed coding hints: %+v", routed)
	}
	if !strings.Contains(decision.Reason, "hub-managed") {
		t.Fatalf("reason=%q", decision.Reason)
	}
	kept := cb.GetLLMConfig()
	if kept.WorkflowTypeHint != "coding" || kept.PhaseKindHint != "execution" || kept.TaskTypeHint != string(llm.TaskReasoning) {
		t.Fatalf("GetLLMConfig dropped hints after RouteTurn: %+v", kept)
	}
}

func TestRemoteCodingRouteTurnKeepsCodingProfileSnapshot(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskReasoning): {Model: "remote-routed-model"},
	})
	base := corelib.MaclawLLMConfig{Model: "coding-base", ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding", RouteSource: "base"}
	cb := &remoteCodingCallbacks{agent: NewRemoteCodingSubAgent(&IMMessageHandler{app: app}, base, nil, "session", "work", "project", nil)}
	routed, decision, applied := cb.RouteTurn("implement")
	if !applied || decision.Source != "route" || routed.Model != "remote-routed-model" {
		t.Fatalf("unexpected remote coding route: cfg=%+v decision=%+v applied=%v", routed, decision, applied)
	}
	if routed.Profile != "coding" || routed.ProviderID != "coding-id" || routed.RouteSource != "route" {
		t.Fatalf("remote route lost coding attribution: %+v", routed)
	}
}

func TestCodingLightweightConfigUsesCodingProfile(t *testing.T) {
	app := &App{}
	app.ohModules.modelRouter = llm.NewModelRouter(map[string]llm.ModelRoute{
		string(llm.TaskFast): {Model: "coding-fast"},
	})
	base := corelib.MaclawLLMConfig{URL: "https://coding.example/v1", Key: "coding-key", Model: "coding-base", ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding", RouteSource: "base"}
	// A standalone handler lets the test isolate the coding accessor's source
	// config without creating a persisted AppConfig.
	h := &IMMessageHandler{app: app, standaloneConfig: &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig { return base }}}
	cfg := h.getCodingLightweightLLMConfig()
	if cfg.Model != "coding-fast" || cfg.Profile != "coding" || cfg.ProviderID != "coding-id" || cfg.RouteSource != "route" {
		t.Fatalf("coding lightweight config lost coding attribution: %+v", cfg)
	}
}

func TestLoopUsageUsesFinalRoutedCodingModel(t *testing.T) {
	base := corelib.MaclawLLMConfig{Model: "coding-base", ProviderName: "Coding", ProviderID: "coding-id", Profile: "coding", RouteSource: "base"}
	final := finalLLMConfigForLoopUsage(base, agent.LoopResult{Route: agent.RouteDecision{Model: "coding-reasoning", Provider: "Coding route", Source: "route"}})
	if final.Model != "coding-reasoning" || final.ProviderName != "Coding route" || final.Profile != "coding" || final.ProviderID != "coding-id" || final.RouteSource != "route" {
		t.Fatalf("final coding usage config = %+v", final)
	}
}

func TestCodingSubAgentUserContentNoAttachments(t *testing.T) {
	sa := &CodingSubAgent{}
	got := codingSubAgentUserContent(sa, "hello")
	if got != "hello" {
		t.Fatalf("%v", got)
	}
}

func TestCodingSubAgentUserContentWithImageSavesWhenNoVision(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", SupportsVision: false}, nil, "", nil)
	sa.SetAttachments([]agent.MessageAttachment{{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		// 1x1 transparent PNG
		Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	}})
	got := codingSubAgentUserContent(sa, "implement this UI")
	// Without vision support, BuildUserContent returns a string with saved path note.
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string content when vision unsupported, got %T", got)
	}
	if !strings.Contains(s, "implement this UI") {
		t.Fatalf("missing user text: %q", s)
	}
	if !strings.Contains(s, "图片") && !strings.Contains(strings.ToLower(s), "image") {
		t.Fatalf("expected image note: %q", s)
	}
}

func TestCodingSubAgentUserContentWithVisionMultimodal(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", SupportsVision: true}, nil, "", nil)
	sa.SetAttachments([]agent.MessageAttachment{{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	}})
	got := codingSubAgentUserContent(sa, "fix layout")
	// Multimodal content is typically []map or []interface{}.
	if _, ok := got.(string); ok {
		t.Fatalf("expected multimodal structure when vision enabled, got string %q", got)
	}
}

func TestCodingSubAgentUserContentScalesDocumentAttachmentToRoutedContext(t *testing.T) {
	body := strings.Repeat("文", 200_000)
	attachment := agent.MessageAttachment{
		Type:     "file",
		FileName: "notes.txt",
		MimeType: "text/plain",
		Data:     base64.StdEncoding.EncodeToString([]byte(body)),
	}
	low := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", ContextLength: 10_000}, nil, "", nil)
	low.SetAttachments([]agent.MessageAttachment{attachment})
	high := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", ContextLength: 400_000}, nil, "", nil)
	high.SetAttachments([]agent.MessageAttachment{attachment})

	lowText, ok := codingSubAgentUserContent(low, "inspect").(string)
	if !ok {
		t.Fatalf("low-context content type = %T, want string", codingSubAgentUserContent(low, "inspect"))
	}
	highText, ok := codingSubAgentUserContent(high, "inspect").(string)
	if !ok {
		t.Fatalf("high-context content type = %T, want string", codingSubAgentUserContent(high, "inspect"))
	}
	if lowLen, highLen := len(lowText), len(highText); highLen <= lowLen || highLen < 180_000 {
		t.Fatalf("routed attachment payload lengths = %d and %d; high context was not expanded", lowLen, highLen)
	}
}
