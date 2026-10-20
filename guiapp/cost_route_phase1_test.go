package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	cllm "github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestApplyTurnModelRoute_AttachesCostTierShadow(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "shadow")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{
		URL: "https://example.invalid/v1", Model: "primary-model", ProviderName: "p1",
	}
	_, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteMode != "shadow" {
		t.Fatalf("mode=%s", d.CostRouteMode)
	}
	if d.CostRouteApplied {
		t.Fatal("shadow must not apply")
	}
	if d.Model != primary.Model {
		t.Fatalf("shadow must keep model: got %s", d.Model)
	}
	if d.Task == string(cllm.TaskFast) && d.CostTier != "c0" {
		t.Fatalf("fast should map c0, got %s", d.CostTier)
	}
}

func TestApplyTurnModelRoute_CostOnAppliesAuxForGreeting(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	app := &App{}
	app.ohModules.cachedAuxLLM = corelib.AuxiliaryLLMConfig{
		URL: "https://aux.example", Key: "ak", Model: "aux-flash",
	}
	h := &IMMessageHandler{app: app}
	primary := corelib.MaclawLLMConfig{URL: "https://p.example", Key: "pk", Model: "primary-m"}
	cfg, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteMode != "on" || !d.CostRouteApplied {
		t.Fatalf("decision=%+v", d)
	}
	if d.CostTier != "c0" {
		t.Fatalf("greeting tier=%s want c0", d.CostTier)
	}
	if cfg.Model != "aux-flash" || d.Model != "aux-flash" {
		t.Fatalf("expected aux model, cfg=%s decision=%s", cfg.Model, d.Model)
	}
	if d.Source != "aux" {
		t.Fatalf("source=%s", d.Source)
	}
	// Phase 3: c0 → thinking off
	if d.ThinkingPolicy != "off" {
		t.Fatalf("think=%s", d.ThinkingPolicy)
	}
	if cfg.ThinkingMode != "disabled" || cfg.ReasoningEffort != "none" {
		t.Fatalf("cfg thinking=%+v effort=%s", cfg.ThinkingMode, cfg.ReasoningEffort)
	}
}

func TestApplyTurnModelRoute_HubManagedSkipsCostApply(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	app := &App{}
	app.ohModules.cachedAuxLLM = corelib.AuxiliaryLLMConfig{
		URL: "https://aux.example", Key: "ak", Model: "aux-flash",
	}
	h := &IMMessageHandler{app: app}
	primary := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", ProviderName: "hub"}
	cfg, d := h.applyTurnModelRoute(primary, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if d.CostRouteApplied {
		t.Fatalf("hub-managed must not apply desktop cost-route: %+v", d)
	}
	if cfg.Model != "auto" || cfg.URL != primary.URL {
		t.Fatalf("hub-managed must keep auto: cfg=%+v", cfg)
	}
	if !strings.Contains(d.Reason, "hub-managed") {
		t.Fatalf("reason=%q", d.Reason)
	}
	if !cfg.HubManaged || cfg.TaskTypeHint == "" {
		t.Fatalf("hub-managed must attach task hint: %+v", cfg)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("desktop must not invent P0 class: %+v", cfg)
	}
}

func TestApplyTurnModelRoute_HubManagedSendsWorkflowHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"}
	cfg, _ := h.applyTurnModelRoute(primary, "实现登录接口", &LoopContext{
		Kind:              LoopKindChat,
		WorkflowAgentLoop: true,
		WorkflowType:      "coding",
		WorkflowPhaseKind: "execution",
		WorkflowPhaseID:   "implementation",
	}, nil)
	if cfg.Model != "auto" {
		t.Fatalf("must keep auto, got %s", cfg.Model)
	}
	if cfg.WorkflowTypeHint != "coding" || cfg.PhaseKindHint != "execution" {
		t.Fatalf("workflow hints: type=%q phase=%q", cfg.WorkflowTypeHint, cfg.PhaseKindHint)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("must not send P0 class, got %q", cfg.WorkloadClassHint)
	}
}

func TestBtwRouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	h := &IMMessageHandler{}
	btw := NewBtwSubAgent(h, corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"}, nil, "u1")
	cb := &btwCallbacks{subagent: btw}
	cfg, d, ok := cb.RouteTurn("你好")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("btw hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped btw hints: %+v", cb.GetLLMConfig())
	}
}

func TestVERouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	cb := &veAgentCallbacks{
		app:    &App{},
		llmCfg: corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"},
	}
	cfg, d, ok := cb.RouteTurn("你好")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("ve hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped ve hints: %+v", cb.GetLLMConfig())
	}
}

func TestLoopCycleRouteTurn_HubManagedPersistsHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	cb := &loopCycleCallbacks{parent: &guiLoopCommandCallbacks{
		handler: &IMMessageHandler{},
		llmCfg:  corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"},
	}}
	cfg, d, ok := cb.RouteTurn("fix the failing tests")
	if !ok || d.CostRouteApplied || cfg.Model != "auto" || cfg.TaskTypeHint == "" {
		t.Fatalf("loop hub hints: ok=%v cfg=%+v decision=%+v", ok, cfg, d)
	}
	if cb.GetLLMConfig().TaskTypeHint != cfg.TaskTypeHint {
		t.Fatalf("GetLLMConfig dropped loop hints: %+v", cb.GetLLMConfig())
	}
}

func TestApplyTurnModelRoute_ThirdPartyHasNoHints(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-4o"}
	cfg, _ := h.applyTurnModelRoute(primary, "你好", &LoopContext{
		Kind:              LoopKindChat,
		WorkflowAgentLoop: true,
		WorkflowType:      "coding",
	}, nil)
	if cfg.HubManaged || cfg.TaskTypeHint != "" || cfg.WorkflowTypeHint != "" {
		t.Fatalf("third-party must not carry Hub hints: %+v", cfg)
	}
}

func TestApplyTurnModelRoute_CostOffNoApply(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://x", Model: "m1"}
	_, d := h.applyTurnModelRoute(primary, "你好", nil, nil)
	if d.CostRouteMode != "off" || d.CostRouteApplied {
		t.Fatalf("%+v", d)
	}
	if d.CostTier == "" {
		t.Fatal("tier still computed when off")
	}
}

func TestApplyTurnModelRoute_HostImageNotesDoNotForceVision(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://x", Model: "m1"}
	text := "北京天气\n[Host note: selected image \"missing.jpg\" could not be read: file does not exist]"
	_, d := h.applyTurnModelRoute(primary, text, &LoopContext{Kind: LoopKindChat}, nil)
	if d.Task == string(cllm.TaskVision) {
		t.Fatalf("failed picker notes must not route weather as vision: %+v", d)
	}
	_, vision := h.applyTurnModelRoute(primary, "北京天气", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"},
	})
	if vision.Task != string(cllm.TaskVision) {
		t.Fatalf("loaded image bytes must still route vision: %+v", vision)
	}
	_, fileTurn := h.applyTurnModelRoute(primary, "请看这个", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "file", FileName: "notes.pdf", MimeType: "application/pdf", Data: "AAAA"},
	})
	if fileTurn.Task == string(cllm.TaskVision) {
		t.Fatalf("a PDF attachment must not route as vision: %+v", fileTurn)
	}
	if fileTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("a short PDF question should stay fast, got %+v", fileTurn)
	}
	picked := "建议几个标题\n" + agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	_, pathTurn := h.applyTurnModelRoute(primary, picked, &LoopContext{Kind: LoopKindChat}, nil)
	if pathTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("a short picked-PDF question should stay fast, got %+v", pathTurn)
	}
	coding := "帮我修一下这个 bug\n" + agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	_, codingTurn := h.applyTurnModelRoute(primary, coding, &LoopContext{Kind: LoopKindChat}, nil)
	if codingTurn.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a coding question with a PDF should stay reasoning, got %+v", codingTurn)
	}
	_, zipTurn := h.applyTurnModelRoute(primary, "请看这个", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "file", FileName: "pack.zip"},
	})
	if zipTurn.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a zip attachment should stay reasoning, got %+v", zipTurn)
	}
	_, voiceTurn := h.applyTurnModelRoute(primary, "请听一下", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "audio", FileName: "memo.mp3"},
	})
	if voiceTurn.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a voice attachment should stay reasoning, got %+v", voiceTurn)
	}
	_, titleOnly := h.applyTurnModelRoute(primary, "建议几个标题", &LoopContext{Kind: LoopKindChat}, nil)
	if titleOnly.Task != string(cllm.TaskFast) {
		t.Fatalf("title text without a document should stay fast, got %+v", titleOnly)
	}
	imagePicked := "你好\n" + agent.FilePathPromptPrefix + "\nC:\\scan.png\n"
	_, imagePath := h.applyTurnModelRoute(primary, imagePicked, &LoopContext{Kind: LoopKindChat}, nil)
	if imagePath.Task != string(cllm.TaskFast) {
		t.Fatalf("a picked image path is not a document turn, got %+v", imagePath)
	}
	historical := "建议几个标题\n" + agent.FilePathPromptPrefixHistorical + "\nC:\\old.pdf\n"
	_, oldPath := h.applyTurnModelRoute(primary, historical, &LoopContext{Kind: LoopKindChat}, nil)
	if oldPath.Task != string(cllm.TaskFast) {
		t.Fatalf("a historical PDF path must not keep the turn heavy, got %+v", oldPath)
	}
}

func TestApplyTurnModelRoute_DocumentQuestionPrefersFast(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "off")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://x", Model: "m1"}
	question := strings.Repeat("请根据规格书计算照度与电阻。", 4)
	_, plain := h.applyTurnModelRoute(primary, question, &LoopContext{Kind: LoopKindChat}, nil)
	if plain.Task != string(cllm.TaskDefault) {
		t.Fatalf("a medium question without a document should stay default, got %+v", plain)
	}
	picked := question + "\n" + agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	_, withPDF := h.applyTurnModelRoute(primary, picked, &LoopContext{Kind: LoopKindChat}, nil)
	if withPDF.Task != string(cllm.TaskFast) {
		t.Fatalf("a medium datasheet question should stay fast, got %+v", withPDF)
	}
	hist := question + "\n" + agent.FilePathPromptPrefixHistorical + "\nC:\\old.pdf\n"
	_, oldPath := h.applyTurnModelRoute(primary, hist, &LoopContext{Kind: LoopKindChat}, nil)
	if oldPath.Task != string(cllm.TaskFast) {
		t.Fatalf("a historical PDF question should stay on chat, got %+v", oldPath)
	}
	_, histCode := h.applyTurnModelRoute(primary, "帮我修一下这个 bug\n"+agent.FilePathPromptPrefixHistorical+"\nC:\\old.pdf\n", &LoopContext{Kind: LoopKindChat}, nil)
	if histCode.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a coding question with a historical PDF should stay reasoning, got %+v", histCode)
	}
	imgHist := question + "\n" + agent.FilePathPromptPrefixHistorical + "\nC:\\old.png\n"
	_, imgOld := h.applyTurnModelRoute(primary, imgHist, &LoopContext{Kind: LoopKindChat}, nil)
	if imgOld.Task != string(cllm.TaskDefault) {
		t.Fatalf("a historical image must not pin the question to chat, got %+v", imgOld)
	}
	_, pinTurn := h.applyTurnModelRoute(primary, "这个传感器的接口电压是多少\n"+agent.FilePathPromptPrefix+"\nC:\\paper.pdf\n", &LoopContext{Kind: LoopKindChat}, nil)
	if pinTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("接口 on a datasheet should stay fast, got %+v", pinTurn)
	}
	_, pinPlain := h.applyTurnModelRoute(primary, "这个传感器的接口电压是多少", &LoopContext{Kind: LoopKindChat}, nil)
	if pinPlain.Task != string(cllm.TaskReasoning) {
		t.Fatalf("接口 without a document should stay reasoning, got %+v", pinPlain)
	}
	_, follow := h.applyTurnModelRoute(primary, "这个传感器的接口电压是多少", &LoopContext{
		Kind: LoopKindChat,
		History: []agent.ConversationEntry{{
			Role:    "user",
			Content: agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n请根据规格书计算照度",
		}},
	}, nil)
	if follow.Task != string(cllm.TaskFast) {
		t.Fatalf("a datasheet follow-up should stay fast, got %+v", follow)
	}
	_, followCode := h.applyTurnModelRoute(primary, "帮我修一下这个 bug", &LoopContext{
		Kind: LoopKindChat,
		History: []agent.ConversationEntry{{
			Role:    "user",
			Content: agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n",
		}},
	}, nil)
	if followCode.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a coding follow-up should stay reasoning, got %+v", followCode)
	}
	codePick := question + "\n" + agent.FilePathPromptPrefix + "\nC:\\main.go\n"
	_, codeTurn := h.applyTurnModelRoute(primary, codePick, &LoopContext{
		Kind: LoopKindChat,
		History: []agent.ConversationEntry{{
			Role:    "user",
			Content: agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n",
		}},
	}, nil)
	if codeTurn.Task != string(cllm.TaskDefault) {
		t.Fatalf("a new source file must not keep the old datasheet chat pin, got %+v", codeTurn)
	}
	echoed := question + "\n" + agent.FilePathPromptPrefixHistorical + "\nC:\\old.pdf\n" + agent.FilePathPromptPrefix + "\nC:\\main.go\n"
	_, echoedTurn := h.applyTurnModelRoute(primary, echoed, &LoopContext{Kind: LoopKindChat}, nil)
	if echoedTurn.Task != string(cllm.TaskDefault) {
		t.Fatalf("a new source file must not keep an echoed datasheet on chat, got %+v", echoedTurn)
	}
	steps := question + "然后给出第一步和第二步\n" + agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	_, multi := h.applyTurnModelRoute(primary, steps, &LoopContext{Kind: LoopKindChat}, nil)
	if multi.Task != string(cllm.TaskFast) {
		t.Fatalf("a multi-step datasheet question should stay fast, got %+v", multi)
	}
	longQ := strings.Repeat("请根据规格书计算照度与电阻。", 15)
	_, longTurn := h.applyTurnModelRoute(primary, longQ+"\n"+agent.FilePathPromptPrefix+"\nC:\\paper.pdf\n", &LoopContext{Kind: LoopKindChat}, nil)
	if longTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("a long datasheet question should stay fast, got %+v", longTurn)
	}
	picture := "看一下规格书里的图片和曲线\n" + agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	_, pictureTurn := h.applyTurnModelRoute(primary, picture, &LoopContext{Kind: LoopKindChat}, nil)
	if pictureTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("图片 in a datasheet question must not become vision, got %+v", pictureTurn)
	}
	_, workflowTurn := h.applyTurnModelRoute(primary, picked, &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}, nil)
	if workflowTurn.Task != string(cllm.TaskReasoning) {
		t.Fatalf("a workflow document turn should stay reasoning, got %+v", workflowTurn)
	}
	_, mimeTurn := h.applyTurnModelRoute(primary, "请看这个", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "file", FileName: "blob", MimeType: "application/pdf"},
	})
	if mimeTurn.Task != string(cllm.TaskFast) {
		t.Fatalf("a PDF mime without an extension should stay fast, got %+v", mimeTurn)
	}
	_, unnamed := h.applyTurnModelRoute(primary, "请看这个", &LoopContext{Kind: LoopKindChat}, []MessageAttachment{
		{Type: "file"},
	})
	if unnamed.Task != string(cllm.TaskReasoning) {
		t.Fatalf("an unnamed non-document attachment should stay reasoning, got %+v", unnamed)
	}
	hub := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto"}
	cfg, hubTurn := h.applyTurnModelRoute(hub, picked, &LoopContext{Kind: LoopKindChat}, nil)
	if hubTurn.Task != string(cllm.TaskFast) || cfg.TaskTypeHint != string(cllm.TaskFast) || cfg.WorkloadClassHint != "" {
		t.Fatalf("hub datasheet hint: cfg=%+v decision=%+v", cfg, hubTurn)
	}
}

func TestApplyTurnModelRoute_HubPathOnlyPDFUsesFastHint(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	h := &IMMessageHandler{}
	primary := corelib.MaclawLLMConfig{URL: "https://hub.example.com/api/llm/v1", Model: "auto", ProviderName: "hub"}
	picked := agent.FilePathPromptPrefix + "\nC:\\paper.pdf\n"
	cfg, d := h.applyTurnModelRoute(primary, picked, &LoopContext{Kind: LoopKindChat}, nil)
	if d.Task != string(cllm.TaskFast) {
		t.Fatalf("path-only PDF should classify fast, got %+v", d)
	}
	if cfg.TaskTypeHint != string(cllm.TaskFast) {
		t.Fatalf("hub task hint=%q, want fast", cfg.TaskTypeHint)
	}
	if cfg.WorkloadClassHint != "" {
		t.Fatalf("desktop must not invent P0 class: %+v", cfg)
	}
	if cfg.Model != "auto" {
		t.Fatalf("hub-managed must keep auto, got %s", cfg.Model)
	}
}
