package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestHostKeepToolsForMessagePinsOfficeOnTruncatedExtract(t *testing.T) {
	h := &IMMessageHandler{}
	msg := agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	got := h.hostKeepToolsForMessage(msg)
	keep := map[string]bool{}
	for _, name := range got {
		keep[name] = true
	}
	for _, name := range agent.AutoExtractContinuationToolNames() {
		if !keep[name] {
			t.Fatalf("truncated extract must host-keep %s, got %v", name, got)
		}
	}
	if names := h.hostKeepToolsForMessage("今天天气怎么样"); len(names) != 0 {
		t.Fatalf("plain chat must not host-keep tools, got %v", names)
	}
	complete := agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=false ---`
	joined := strings.Join(h.hostKeepToolsForMessage(complete), ",")
	if strings.Contains(joined, "office") || strings.Contains(joined, "read_document") {
		t.Fatalf("complete extract must not host-keep continuation tools, got %v", joined)
	}
}

func TestDocumentContinuationOverlayPinsOneReaderWhenExtractTruncated(t *testing.T) {
	msg := agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "office", Description: "office", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler:         &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{grants: map[string]tool.InvocationGrant{}},
		userText:        msg,
	}
	cb.maybeOverlayDocumentContinuationForTurn(msg)
	if len(cb.legacyPetitionTools) != 1 {
		t.Fatalf("truncated extract must overlay exactly one continuation reader, got %v", cb.legacyPetitionTools)
	}
	if !cb.legacyPetitionTools["office"] && !cb.legacyPetitionTools["read_document"] {
		t.Fatalf("overlay must be office or read_document, got %v", cb.legacyPetitionTools)
	}
}

func TestDocumentContinuationOverlayPrefersReadDocumentOverOffice(t *testing.T) {
	msg := agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	registry := NewToolRegistry()
	for _, name := range []string{"office", "read_document"} {
		if err := registry.Register(RegisteredTool{Name: name, Description: name, Status: RegToolAvailable}); err != nil {
			t.Fatal(err)
		}
	}
	cb := &sharedAgentLoopCallbacks{
		handler:         &IMMessageHandler{registry: registry},
		semanticSurface: &semanticCallSurface{grants: map[string]tool.InvocationGrant{}},
		userText:        msg,
	}
	cb.maybeOverlayDocumentContinuationForTurn(msg)
	if !cb.legacyPetitionTools["read_document"] || cb.legacyPetitionTools["office"] || len(cb.legacyPetitionTools) != 1 {
		t.Fatalf("overlay must prefer read_document over write office, got %v", cb.legacyPetitionTools)
	}
}

func TestDocumentContinuationOverlaySeesExpandedExtractInHistory(t *testing.T) {
	raw := "评审本篇论文\n\n" + filePathPromptPrefix + "\nC:\\docs\\paper.pdf\n"
	expanded := raw + "\n" + agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "office", Description: "office", Status: RegToolAvailable}); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler:           &IMMessageHandler{registry: registry},
		semanticSurface:   &semanticCallSurface{grants: map[string]tool.InvocationGrant{}},
		userText:          raw,
		checkpointHistory: []agent.ConversationEntry{{Role: "user", Content: expanded}},
	}
	cb.maybeOverlayDocumentContinuationForTurn(raw)
	if !cb.legacyPetitionTools["office"] {
		t.Fatalf("overlay must see truncated extract in prepared user content, got %v", cb.legacyPetitionTools)
	}
	defs := cb.appendLegacyPetitionedDatabaseTools(nil)
	if len(defs) != 1 {
		t.Fatalf("overlay definition missing: %#v", defs)
	}
	fn, _ := defs[0]["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	action, _ := props["action"].(map[string]interface{})
	desc, _ := action["description"].(string)
	if strings.Contains(desc, "write_excel") || strings.Contains(desc, "generate_pdf") {
		t.Fatalf("overlaid office must be read-only, action=%q", desc)
	}
	if denied := cb.continuationOverlayDenial("office", `{"action":"write_excel"}`); denied == "" {
		t.Fatal("write_excel must be denied on continuation overlay")
	}
	if denied := cb.continuationOverlayDenial("office", `{"action":"read_document","offset":10}`); denied != "" {
		t.Fatalf("read_document must be allowed, got %q", denied)
	}
	if denied := cb.continuationOverlayDenial("office", `{"offset":10}`); denied != "" {
		t.Fatalf("missing action must default to read_document, got %q", denied)
	}
	if got := cb.continuationOverlayArgsJSON("office", `{"offset":10}`); !strings.Contains(got, `"action":"read_document"`) {
		t.Fatalf("missing action must be filled, got %q", got)
	}
}

func TestHostKeepMessageForTurnUsesExpandedExtract(t *testing.T) {
	raw := "评审本篇论文\n\n" + filePathPromptPrefix + "\nC:\\docs\\paper.pdf\n"
	expanded := raw + "\n" + agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	ctx := &LoopContext{autoExtractExpandedText: expanded}
	if got := hostKeepMessageForTurn(ctx, raw); got != expanded {
		t.Fatalf("HostKeep must use expanded extract, got %q", got)
	}
	h := &IMMessageHandler{}
	keep := h.hostKeepToolsForTurn(ctx, raw)
	found := false
	for _, name := range keep {
		if name == "office" || name == "read_document" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expanded extract must host-keep continuation readers, got %v", keep)
	}
	if names := h.hostKeepToolsForTurn(nil, raw); strings.Contains(strings.Join(names, ","), "office") {
		t.Fatalf("raw picker must not host-keep office, got %v", names)
	}
	ctx.autoExtractExpandedText = ""
	if names := h.hostKeepToolsForTurn(ctx, "今天天气怎么样"); strings.Contains(strings.Join(names, ","), "office") {
		t.Fatalf("cleared extract must not host-keep office, got %v", names)
	}
	grantCtx := &LoopContext{}
	grantCtx.rememberDiscoveredConditionalTool("write_file")
	foundWrite := false
	for _, name := range h.hostKeepToolsForTurn(grantCtx, "今天天气怎么样") {
		if name == "write_file" {
			foundWrite = true
			break
		}
	}
	if !foundWrite {
		t.Fatal("discover_tool grants must host-keep write_file for the next leftover route")
	}
	sqlPaper := raw + "\n" + agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---` + "\nSELECT * FROM schema.tables WHERE catalog='mysql'\n"
	sqlCtx := &LoopContext{autoExtractExpandedText: sqlPaper}
	for _, name := range h.hostKeepToolsForTurn(sqlCtx, raw) {
		if name == "database" || name == "database_query" {
			t.Fatalf("extract body must not host-keep SQL tools, got %v", h.hostKeepToolsForTurn(sqlCtx, raw))
		}
	}
}

func TestDocumentContinuationOverlaySkipsWhenTrustedReadIsBound(t *testing.T) {
	msg := agent.AutoExtractBeginMarker + `path="C:\docs\paper.pdf" format="pdf" truncated=true next_offset=10 ---`
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: NewToolRegistry()},
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{
				"invoke_doc": {Token: "tok", SelectionID: "sel-doc"},
			},
			plan: tool.ToolPlan{
				Selections: []tool.PlannedSelection{{
					ID:       "sel-doc",
					FitProof: tool.FitProof{MatchedCapability: "document.read.local"},
				}},
			},
		},
		userText: msg,
	}
	cb.maybeOverlayDocumentContinuationForTurn(msg)
	if len(cb.legacyPetitionTools) != 0 {
		t.Fatalf("bound document.read must not overlay write office, got %v", cb.legacyPetitionTools)
	}
}
