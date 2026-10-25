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
