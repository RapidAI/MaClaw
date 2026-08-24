package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The handler needs an app: several name-routing sections are emitted only
// when one is present, and asserting their absence against a fixture that
// could never produce them proves nothing.
func guiPostSSHRules(t *testing.T, managed bool) string {
	t.Helper()
	var b strings.Builder
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	h.appendGUIPostSSHRules(&b, true, "tester", corelib.AppConfig{}, "owner-1", managed)
	return b.String()
}

// The debt this closes: a managed turn's tools are opaque per-grant tokens, but
// the prompt kept routing the model to registry names. Every such instruction
// is a guaranteed-refused call the model was told to make.
func TestAManagedTurnIsNotTaughtToCallToolsByName(t *testing.T) {
	managed := guiPostSSHRules(t, true)
	legacy := guiPostSSHRules(t, false)

	nameRouting := []string{
		"## Skill 优先策略（重要）",
		"## MIS Dynamic AgentView",
		"## MaClaw Group Discussion",
		"## 高级能力",
	}
	for _, section := range nameRouting {
		if strings.Contains(managed, section) {
			t.Errorf("managed prompt still carries name-routing section %q", section)
		}
		// Without this the test would pass just as well against a prompt
		// builder that never emitted the section in the first place.
		if !strings.Contains(legacy, section) {
			t.Errorf("unmanaged prompt lost section %q; the assertion above is now vacuous", section)
		}
	}
}

// Facts are not instructions. Dropping the whole block would have cost the
// model its device, time, and slash-command context for no safety gain.
func TestAManagedTurnKeepsTheFactualState(t *testing.T) {
	managed := guiPostSSHRules(t, true)
	for _, kept := range []string{"## 当前设备状态", "- 平台: ", "- 当前时间: ", "## 对话管理"} {
		if !strings.Contains(managed, kept) {
			t.Errorf("managed prompt dropped factual state %q", kept)
		}
	}
}

// The replacement block must warn about exactly the names that are banned, and
// stay tied to the ban list rather than to a copy of it made once.
func TestTheManagedSurfaceBlockNamesEveryBannedGateway(t *testing.T) {
	var b strings.Builder
	appendManagedSemanticSurfaceRules(&b)
	block := b.String()

	for _, banned := range tool.LegacyDynamicGatewayNames() {
		if !strings.Contains(block, banned) {
			t.Errorf("the managed surface block does not warn about %q, which the managed surface refuses", banned)
		}
	}
	// Telling the model what is gone without telling it what to do instead
	// moves the failure from a refused call to an invented workaround.
	if !strings.Contains(block, "工具列表就是全部可用工具") {
		t.Error("the block does not state that the tool list is exhaustive")
	}
	if !strings.Contains(block, "预算用尽") {
		t.Error("the block does not explain why a tool disappears mid-turn")
	}
	if !strings.Contains(block, "按顺序解锁") || !strings.Contains(block, "没有 PDF 工具") {
		t.Error("the block does not warn that later grants unlock after the current step")
	}
	if !strings.Contains(block, "请稍候") || !strings.Contains(block, "立刻调用") {
		t.Error("the block does not forbid stopping with a please-wait promise after search")
	}
	if !strings.Contains(block, "previous_turn_tool") {
		t.Error("the block does not ban the invented previous_turn_tool alias")
	}
	if !strings.Contains(block, "不要传 path") || !strings.Contains(block, "send_to_im(path=") {
		t.Error("the block does not warn that bound delivery rejects the soup send_to_im signature")
	}
}

func guiPostCorePrinciples(t *testing.T, managed bool) string {
	t.Helper()
	var b strings.Builder
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	h.appendGUIPostCorePrinciples(&b, true, false, false, "desktop", managed)
	return b.String()
}

func TestAManagedTurnIsNotTaughtSoupDeliverySignatures(t *testing.T) {
	managed := guiPostCorePrinciples(t, true)
	legacy := guiPostCorePrinciples(t, false)
	for _, soup := range []string{"send_to_im(path=", "send_file+destination", "## 文件发到微信", "## Local Coding Tools Boundary", "compress_context"} {
		if strings.Contains(managed, soup) {
			t.Errorf("managed post-core principles still teach soup routing %q", soup)
		}
		if !strings.Contains(legacy, soup) {
			t.Errorf("unmanaged prompt lost section %q; the assertion above is now vacuous", soup)
		}
	}
}

func TestBuildIMEntrySystemPromptDoesNotEmbedPickerFilename(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			Execution: ExecutionProfile{
				Layer:         string(executionLayerLight),
				PromptProfile: "light",
				Reason:        "lookup",
			},
		},
	}
	prompt := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID:   "user-1",
		Text:     "北京天气\n\n" + filePathPromptPrefix + "\nC:\\tmp\\weather-report.jpg",
		Platform: "desktop",
	}, nil, ctx, false, "", "", "", "")
	if strings.Contains(prompt, "weather-report.jpg") {
		t.Fatal("entry prompt must not embed a picker filename into memory/skill/browser context")
	}
	if strings.Contains(prompt, filePathPromptPrefix) {
		t.Fatal("entry prompt must not embed the picker staging marker")
	}
}

func TestAManagedWeatherPDFEntryPromptOmitsSoupAndWorkflowOverrides(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	ctx := &LoopContext{
		Runtime: RuntimeContext{
			Execution: ExecutionProfile{
				Layer:         string(executionLayerFull),
				PromptProfile: "full",
				Reason:        "semantic capability-managed mutating intent",
			},
			SemanticIntent: liveDataGenerateClassification(),
		},
	}
	prompt := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "user-1", Text: "南京天气，生成pdf报告", Platform: "desktop",
	}, nil, ctx, false, "", "", "", "")
	for _, soup := range []string{
		"再调用 send_to_im(path=",
		"直接 send_file 该 MP3",
		"不要使用 generate_pdf",
		"CodingSubAgent",
		"manage_skill(action=\"run\"",
		"write_file, edit_file, ripgrep",
		"## Skill 使用文档",
		"## 最近自动修复的 Skill",
	} {
		if strings.Contains(prompt, soup) {
			t.Errorf("managed weather+PDF prompt still teaches %q", soup)
		}
	}
	if !strings.Contains(prompt, "不要传 path") {
		t.Fatal("managed weather+PDF prompt missing bound-delivery warning")
	}
	if !strings.Contains(prompt, "## 本回合的工具面") {
		t.Fatal("managed weather+PDF prompt lost the surface block")
	}
}

// The block belongs only on turns whose surface actually works that way.
func TestTheManagedSurfaceBlockStaysOffLegacyTurns(t *testing.T) {
	if strings.Contains(guiPostSSHRules(t, false), "## 本回合的工具面") {
		t.Fatal("a legacy turn was told its tool list is exhaustive; it is not")
	}
	if !strings.Contains(guiPostSSHRules(t, true), "## 本回合的工具面") {
		t.Fatal("a managed turn did not get the surface description")
	}
}
