package guiapp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func capabilityExtensionPicker(paths ...string) string {
	return agent.FilePathPromptPrefix + "\n" + strings.Join(paths, "\n") + "\n"
}

func TestDiscoveryPictureCreationUtterance(t *testing.T) {
	longTail := "生成老鼠戏猫图" + strings.Repeat("补充说明", 8) + "图"
	farPicture := "生成" + strings.Repeat("补充说明", 8) + "图片"
	for _, text := range []string{
		"生成老鼠戏猫图", "生成一张图片", "生成一张图", "生成图片", "生成图像", "把图片生成出来", "把图片生成出来发给我", "把图像生成出来", "把这张图生成出来", "将一幅图生成出来",
		"生成一张如图所示的猫图", "生成图中的猫图", "生成一下图", "请生成一张图", "生成图片交给我", "生成一张猫图的高清图",
		"画个猫，方案另说", "画一个方案，再画个猫", "不画图，请画图", "do not generate a table, generate an image",
		longTail, "画图", "画个猫", "画个图", "文生图", "draw a picture", "generate an image", "generate an image of a password", "generate an excellent image", "text-to-image",
	} {
		if !discoveryPictureCreationUtterance(text) {
			t.Fatalf("%q should be picture creation", text)
		}
	}
	for _, text := range []string{
		"", "核对期刊", "北京天气，生成pdf", "生成pdf", "下载一张猫的图片", "截一张图", "找一张猫的图片", "生成流程图", "画一个流程图", "寻找现成图片", "imagine a cat",
		"生成一张清单", "生成一张表", "画一个方案", "画个方案", "画一个方案图", farPicture,
		"如图所示，请生成一段说明", "如上图，生成一段说明", "根据图片生成说明", "参考这张图片，请生成一段说明", "把图片里的字生成出来", "generate a word image",
		"把方案确定后，参考图片生成说明", "根据将来的图片生成说明", "生成一段如图所示的说明", "生成说明见图", "生成上图的说明", "生成下图", "生成图中的文字",
		"不画图", "不要画图", "别画一张猫", "不要生成一张图", "不是画图", "不是生成一张图", "这不是动画图", "把这张图生成给我一段说明", "生成图片的说明", "生成一张图片的说明", "do not generate an image", "do not text-to-image",
	} {
		if discoveryPictureCreationUtterance(text) {
			t.Fatalf("%q should not be picture creation", text)
		}
	}
}

func TestSemanticCapabilityExtensionOpensUnsupportedFilesAndUnmappedNeeds(t *testing.T) {
	reason, paths, ok := semanticCapabilityExtension(tool.UniqueTrustedInputCount(0), capabilityExtensionPicker(`C:\books\note.fb2`), nil)
	if !ok || reason == "" || len(paths) != 1 || !strings.HasSuffix(paths[0], `note.fb2`) {
		t.Fatalf("fb2 extension reason=%q paths=%v ok=%v", reason, paths, ok)
	}

	reason, paths, ok = semanticCapabilityExtension(tool.UniqueTrustedInputCount(0), "读这个", []MessageAttachment{{Type: "file", FileName: "notes.fb2"}})
	if !ok || len(paths) != 1 || paths[0] != "notes.fb2" {
		t.Fatalf("attachment extension reason=%q paths=%v ok=%v", reason, paths, ok)
	}

	reason, _, ok = semanticCapabilityExtension(semanticUnmappedCapabilityError{Label: intent.LabelCoding}, "做一个日历", nil)
	if !ok || !strings.Contains(reason, string(intent.LabelCoding)) {
		t.Fatalf("unmapped coding reason=%q ok=%v", reason, ok)
	}

	reason, _, ok = semanticCapabilityExtension(fmt.Errorf("semantic route has no governed capability needs"), "转成语音", nil)
	if !ok || reason == "" {
		t.Fatalf("ungoverned reason=%q ok=%v", reason, ok)
	}

	reason, _, ok = semanticCapabilityExtension(semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "speak", ReasonCode: "no_feasible_provider"}}}, "朗读", nil)
	if !ok || reason == "" {
		t.Fatalf("no provider reason=%q ok=%v", reason, ok)
	}

	_, _, ok = semanticCapabilityExtension(semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{
		{NeedID: "a", ReasonCode: tool.CatalogCoverageReasonNotReady},
		{NeedID: "b", ReasonCode: "no_feasible_provider"},
	}}, "朗读", nil)
	if !ok {
		t.Fatal("mixed availability and missing provider should open the extension")
	}
}

func TestSemanticCapabilityExtensionKeepsSafetyRefusals(t *testing.T) {
	cases := []struct {
		name string
		err  error
		text string
		atts []MessageAttachment
	}{
		{name: "nil", err: nil},
		{name: "ceiling", err: errSemanticSessionCeilingSpent},
		{name: "awaiting", err: errSemanticAwaitingConfirmation},
		{name: "conflict", err: errSemanticGenerateDeliveryConflict},
		{name: "stale", err: fmt.Errorf("trusted_document_context_stale")},
		{name: "produced", err: fmt.Errorf("produced_document_stale")},
		{name: "policy", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: "policy_denied"}}}},
		{name: "artifact", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: "artifact_dependency_missing"}}}},
		{name: "workflow", err: semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask}},
		{name: "pinned", err: semanticPinnedProviderUnavailableError{Providers: []string{"calibre"}}},
		{name: "pinned-unmet", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: tool.UnmetReasonPinnedProviderUnavailable}}}},
		{name: "budget", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: "budget_exceeded"}}}},
		{name: "not-ready", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: tool.CatalogCoverageReasonNotReady}}}},
		{name: "incomplete", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: tool.CatalogCoverageReasonIncomplete}}}},
		{name: "stale-catalog", err: semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "x", ReasonCode: tool.CatalogCoverageReasonStale}}}},
		{name: "no-file", err: tool.UniqueTrustedInputCount(0), text: "书中讲什么"},
		{name: "native-mobi", err: tool.UniqueTrustedInputCount(0), text: capabilityExtensionPicker(`E:\book.mobi`)},
		{name: "two-pdf", err: tool.UniqueTrustedInputCount(2), text: capabilityExtensionPicker(`C:\a.pdf`, `C:\b.pdf`)},
		{name: "mixed", err: tool.UniqueTrustedInputCount(0), text: capabilityExtensionPicker(`C:\a.pdf`, `C:\a.fb2`)},
		{name: "image", err: tool.UniqueTrustedInputCount(0), text: capabilityExtensionPicker(`C:\shot.png`)},
		{name: "native-attachment", err: tool.UniqueTrustedInputCount(0), text: "读一下", atts: []MessageAttachment{{Type: "file", FileName: "a.pdf"}}},
		{name: "image-attachment", err: tool.UniqueTrustedInputCount(0), text: "看图", atts: []MessageAttachment{{Type: "image", FileName: "a.png"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, paths, ok := semanticCapabilityExtension(tc.err, tc.text, tc.atts)
			if ok || reason != "" || len(paths) != 0 {
				t.Fatalf("extension opened reason=%q paths=%v", reason, paths)
			}
		})
	}
}

func TestCatalogGapPlansTheCapabilityExtension(t *testing.T) {
	h := &IMMessageHandler{}
	gap := &intent.ClassificationResult{
		Primary: intent.LabelNonCoding, Confidence: 0.93, Layer: 3,
		Reason:        "catalog gap: tree non_coding(0.930) refused local leader office(0.869)",
		RunnerUp:      intent.LabelOffice,
		RunnerUpScore: 0.87,
	}
	_, handled, err := h.semanticPlanForTurnWithClassification("desktop-user", "生成一张图片", "desktop", "root", "turn", gap)
	if !handled || err == nil || !strings.Contains(err.Error(), "no governed capability needs") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	reason, _, ok := semanticCapabilityExtension(err, "生成一张图片", nil)
	if !ok || !strings.Contains(reason, "内置工具没有覆盖") || !strings.Contains(reason, "不要用幻灯片、Word 或 PDF 顶替") {
		t.Fatalf("extension reason=%q ok=%v", reason, ok)
	}
	prompt := capabilityExtensionPrompt(reason, nil, []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "search_and_install_skill"}},
		{"type": "function", "function": map[string]interface{}{"name": "craft_tool"}},
	})
	if !strings.Contains(prompt, "不要改用幻灯片、Word 或 PDF 交差") || !strings.Contains(prompt, "直接回答") || strings.Contains(prompt, "先调用") {
		t.Fatalf("catalog gap prompt must forbid a document substitute:\n%s", prompt)
	}
	if !strings.Contains(prompt, "不要生成 pptx、docx 或 pdf") {
		t.Fatalf("catalog gap prompt must name the substitute files:\n%s", prompt)
	}
	continued := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.90, Reason: "catalog gap: tree continuation(0.900) refused local leader office(0.869)"}
	if intent.ClassificationHasCatalogGap(continued) {
		t.Fatal("continuation is not a catalog gap")
	}
	plain := &intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: 0.90, Layer: 3, Reason: "tree-after-embedding: non_coding (0.900)"}
	if intent.ClassificationHasCatalogGap(*plain) {
		t.Fatal("ordinary non_coding is not a catalog gap")
	}
}

func TestDiscoveryGrantFenceDoesNotOfferOfficeAsTheFallback(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	prompt := ensureTurnGrantPromptFence("base", discovery)
	if !strings.Contains(prompt, "【发现模式】") || !strings.Contains(prompt, "不要调用 office、generate_pdf、write_file 或 bash 来顶替") {
		t.Fatalf("discovery fence missing:\n%s", prompt)
	}
	if !strings.Contains(prompt, "search_and_install_skill") || !strings.Contains(prompt, "craft_tool") || !strings.Contains(prompt, "文字问题直接回答") {
		t.Fatalf("discovery fence must keep skill search and a direct answer:\n%s", prompt)
	}
	if !strings.Contains(prompt, "放进 needs") || strings.Contains(prompt, "直接调用它来请愿") {
		t.Fatalf("discovery fence must load a directory name through needs:\n%s", prompt)
	}
	if strings.Contains(prompt, "may be called once even though unlisted") || strings.Contains(prompt, "An untried petitionable name") {
		t.Fatalf("discovery fence still tells the model to call an unlisted office name:\n%s", prompt)
	}
	if again := ensureTurnGrantPromptFence(prompt, discovery); again != prompt {
		t.Fatal("discovery fence must be idempotent")
	}
	ordinary := ensureTurnGrantPromptFence("base", intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.9})
	if !strings.Contains(ordinary, "may be called once even though unlisted") || strings.Contains(ordinary, "【发现模式】") {
		t.Fatal("a classified turn must keep the ordinary grant fence")
	}
}

func TestCapabilityExtensionPromptNamesBothTools(t *testing.T) {
	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "search_and_install_skill"}},
		{"type": "function", "function": map[string]interface{}{"name": "craft_tool"}},
	}
	text := capabilityExtensionPrompt("内置工具没有覆盖这项请求", []string{`C:\books\note.fb2`}, tools)
	for _, want := range []string{"search_and_install_skill", "craft_tool", `C:\books\note.fb2`, "不要拒绝", "encrypted"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "search_and_install_skill") > strings.Index(text, "craft_tool") {
		t.Fatalf("search should be listed before craft:\n%s", text)
	}
}

func TestCapabilityExtensionBuildToolsStaysClosed(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"bash", "write_file", "ssh", "craft_tool", "search_and_install_skill", "tools_search"} {
		if err := registry.Register(RegisteredTool{Name: name, Description: name, Status: RegToolAvailable}); err != nil {
			t.Fatal(err)
		}
	}
	handler := &IMMessageHandler{registry: registry}
	defs := capabilityExtensionToolDefs(handler)
	cb := &sharedAgentLoopCallbacks{
		handler:       handler,
		loopCtx:       &LoopContext{capabilityExtensionReason: "内置工具没有覆盖这项请求"},
		tools:         []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "bash"}}},
		legacySurface: newLegacyToolSurfaceWithClientTools(defs, nil),
	}
	for i := 0; i < 2; i++ {
		got := cb.BuildToolsForModelRequest("把这个 fb2 读出来", i)
		names := map[string]bool{}
		for _, def := range got {
			names[extractToolName(def)] = true
		}
		if !names["craft_tool"] || !names["search_and_install_skill"] {
			t.Fatalf("iteration %d names=%v", i, names)
		}
		for _, banned := range []string{"bash", "write_file", "ssh", "tools_search"} {
			if names[banned] || cb.legacySurface.Allows(banned) {
				t.Fatalf("iteration %d published %s names=%v", i, banned, names)
			}
		}
	}
	if !cb.IsToolAllowedForPromptProfile("craft_tool", agent.PromptProfileLight) || !cb.IsToolAllowedForPromptProfile("search_and_install_skill", agent.PromptProfileLight) {
		t.Fatal("extension tools must run on a light profile")
	}
	if cb.IsToolAllowedForPromptProfile("bash", agent.PromptProfileLight) {
		t.Fatal("bash must stay off the light extension turn")
	}
	closed := &sharedAgentLoopCallbacks{loopCtx: &LoopContext{}}
	if closed.IsToolAllowedForPromptProfile("craft_tool", agent.PromptProfileLight) {
		t.Fatal("craft_tool is not light-safe outside an extension turn")
	}
}
