package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestWorkflowDocDeliverySectionSkipsFileCompanion(t *testing.T) {
	const imCloser = "已生成 [阶段名称] 的 PDF 版本"
	if got := workflowDocDeliverySection("file-companion:s1", desktopLaunchFileCompanion); got != "" || strings.Contains(got, "PDF 版本") {
		t.Fatalf("companion platform section = %q", got)
	}
	if got := workflowDocDeliverySection("file-companion:s1", ""); got != "" {
		t.Fatalf("companion user id section = %q", got)
	}
	weixin := workflowDocDeliverySection("wx-user", "weixin")
	if !strings.Contains(weixin, imCloser) {
		t.Fatalf("weixin lost the IM closer:\n%s", weixin)
	}
	desktop := workflowDocDeliverySection("desktop-user", "desktop")
	if !strings.Contains(desktop, "仅工作流阶段文档") || strings.Contains(desktop, imCloser) {
		t.Fatalf("desktop section drifted:\n%s", desktop)
	}
	if got := workflowDocDeliverySection("user", ""); got != "" {
		t.Fatalf("empty platform section = %q", got)
	}
	bridge := workflowDocDeliverySection("user", "custom-bridge")
	if !strings.Contains(bridge, imCloser) {
		t.Fatalf("unknown non-empty platform lost the IM rule:\n%s", bridge)
	}
}

func TestFileCompanionPaperPromptKeepsTheHostPDF(t *testing.T) {
	paper := fileCompanionPaperPrompt("论文.pdf")
	if !strings.Contains(paper, "这篇解读的 PDF 由宿主根据正文生成") {
		t.Fatalf("paper prompt = %q", paper)
	}
	if strings.Contains(paper, "PDF 版本") || strings.Contains(paper, "不要声称已经生成") {
		t.Fatalf("paper prompt borrowed the ordinary ban: %q", paper)
	}
	instruction := fileCompanionPaperInstruction()
	if !strings.Contains(instruction, "已生成论文解读") || strings.Contains(instruction, "PDF 版本") {
		t.Fatalf("paper instruction = %q", instruction)
	}
	ordinary := fileCompanionPrompt("告知书.pdf", ".pdf")
	if !strings.Contains(ordinary, "不要声称已经生成或发送了 PDF") || strings.Contains(ordinary, "由宿主根据正文生成") {
		t.Fatalf("ordinary prompt = %q", ordinary)
	}
}

func TestFileCompanionBindingPromptIsInjectedWithoutBotProfile(t *testing.T) {
	want := fileCompanionPrompt("告知书.pdf", ".pdf")
	got := buildAssistantBindingPrompt(&agent.AssistantBinding{
		Mode:          desktopLaunchFileCompanion,
		InitialPrompt: want,
	})
	if got != want {
		t.Fatalf("companion binding prompt = %q", got)
	}
	if got := buildAssistantBindingPrompt(&agent.AssistantBinding{Mode: "review", InitialPrompt: want}); got != "" {
		t.Fatalf("mode-only review binding produced prompt: %q", got)
	}
}

func TestFileCompanionEntryPromptOmitsIMPDFCloser(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	userID := "file-companion:s1"
	instructions := fileCompanionPrompt("告知书.pdf", ".pdf")
	msg := IMUserMessage{
		UserID:   userID,
		Platform: desktopLaunchFileCompanion,
		Text:     "文件中有啥内容？",
		AssistantBinding: &agent.AssistantBinding{
			Mode:          desktopLaunchFileCompanion,
			InitialPrompt: instructions,
		},
	}
	for _, profile := range []ExecutionProfile{
		{Layer: string(executionLayerLight), PromptProfile: "light", Reason: "lookup"},
		fullExecutionProfile("companion question"),
	} {
		ctx := &LoopContext{
			Platform: desktopLaunchFileCompanion,
			UserID:   userID,
			Runtime:  RuntimeContext{Execution: profile},
		}
		prompt := h.buildIMEntrySystemPrompt(msg, nil, ctx, false, "", "", "", "")
		if !strings.Contains(prompt, instructions) {
			t.Fatalf("profile %s dropped the companion prompt", profile.Layer)
		}
		for _, banned := range []string{"PDF 版本", "IM 通道文档交付规则", "## 文档交付契约"} {
			if strings.Contains(prompt, banned) {
				t.Fatalf("profile %s still contains %q", profile.Layer, banned)
			}
		}
	}

	weixin := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "wx-user", Platform: "weixin", Text: "继续",
	}, nil, &LoopContext{
		Platform: "weixin",
		UserID:   "wx-user",
		Runtime:  RuntimeContext{Execution: fullExecutionProfile("im")},
	}, false, "", "", "", "")
	if !strings.Contains(weixin, "已生成 [阶段名称] 的 PDF 版本") {
		t.Fatal("weixin entry prompt lost the IM document rule")
	}
	desktop := h.buildIMEntrySystemPrompt(IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", Text: "继续",
	}, nil, &LoopContext{
		Platform: "desktop",
		UserID:   "desktop-user",
		Runtime:  RuntimeContext{Execution: fullExecutionProfile("desktop")},
	}, false, "", "", "", "")
	if !strings.Contains(desktop, "仅工作流阶段文档") || strings.Contains(desktop, "PDF 版本") {
		t.Fatal("desktop entry prompt lost its override or gained the IM closer")
	}
}
