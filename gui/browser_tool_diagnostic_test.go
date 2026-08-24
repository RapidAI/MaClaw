package main

import (
	"bytes"
	"log"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var browserDiagLogMu sync.Mutex

func captureBrowserDiagLog(t *testing.T, fn func()) string {
	t.Helper()
	browserDiagLogMu.Lock()
	defer browserDiagLogMu.Unlock()

	var buf bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(originalWriter)

	fn()
	return buf.String()
}

func TestBrowserDiagBaseNamesRedactsDirectories(t *testing.T) {
	got := browserDiagBaseNames([]string{
		`C:\Users\alice\Documents\deck.pptx`,
		"/home/alice/report.pdf",
		"",
		"plain.txt",
	})
	want := []string{"deck.pptx", "report.pdf", "plain.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("browserDiagBaseNames() = %#v, want %#v", got, want)
	}
}

func TestBrowserDiagRolePrefixDetectsBrowserAndTool(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantKind string
		wantOK   bool
	}{
		{name: "browser ascii", text: "Browser: deck generated", wantKind: "Browser", wantOK: true},
		{name: "browser fullwidth", text: "Browser\uFF1Adeck generated", wantKind: "Browser", wantOK: true},
		{name: "tool ascii", text: "Tool: wrote file", wantKind: "Tool", wantOK: true},
		{name: "tool fullwidth", text: "Tool\uFF1Awrote file", wantKind: "Tool", wantOK: true},
		{name: "first prefix wins", text: "intro\nTool: wrote file\nBrowser: opened page", wantKind: "Tool", wantOK: true},
		{name: "markdown quote", text: "> Browser: opened page", wantKind: "Browser", wantOK: true},
		{name: "numbered list", text: "1. Tool: wrote file", wantKind: "Tool", wantOK: true},
		{name: "inline mention ignored", text: "The Browser: tool already closed cleanly.", wantOK: false},
		{name: "later line prefix after inline mention", text: "The Browser: tool already closed cleanly.\nBrowser: leaked final line", wantKind: "Browser", wantOK: true},
		{name: "none", text: "PPT generated", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, _, gotOK := browserDiagRolePrefix(tt.text)
			if gotKind != tt.wantKind || gotOK != tt.wantOK {
				t.Fatalf("browserDiagRolePrefix(%q) = (%q, %v), want (%q, %v)", tt.text, gotKind, gotOK, tt.wantKind, tt.wantOK)
			}
		})
	}
}

func TestBrowserDiagFinalOutputDoesNotLogContentContext(t *testing.T) {
	got := captureBrowserDiagLog(t, func() {
		BrowserDiagCP7_FinalOutput("Browser: sensitive generated deck text", "test")
	})

	if strings.Contains(got, "sensitive generated deck text") {
		t.Fatalf("diagnostic log leaked content: %q", got)
	}
	if !strings.Contains(got, "index=0") {
		t.Fatalf("diagnostic log should include prefix index, got %q", got)
	}
}

func TestBrowserDiagFinalOutputIgnoresInlineMention(t *testing.T) {
	got := captureBrowserDiagLog(t, func() {
		BrowserDiagCP7_FinalOutput("The Browser: tool already closed cleanly.", "test")
	})

	if got != "" {
		t.Fatalf("inline Browser mention should not be logged, got %q", got)
	}
}

func TestBrowserDiagFileDeliveryMarksOnlyRolePrefixAsBrowserPrefix(t *testing.T) {
	got := captureBrowserDiagLog(t, func() {
		BrowserDiagFileDelivery("desktop-return", "The Browser: tool already closed cleanly.", []string{"deck.pptx"}, []string{`C:\tmp\deck.pptx`}, "file_delivery")
	})

	if !strings.Contains(got, "textHasBrowserPrefix=false") {
		t.Fatalf("inline Browser mention should not be marked as browser role prefix, got %q", got)
	}
	if !strings.Contains(got, "textHasRolePrefix=false") {
		t.Fatalf("inline Browser mention should not be marked as role prefix, got %q", got)
	}
}

func TestEmitFinalToolSurfaceDiagnosticsUsesExplainTraceOnManaged(t *testing.T) {
	surface := &semanticCallSurface{plan: tool.ToolPlan{
		ID: "plan-1", SnapshotDigest: "snap-1",
		Trace: tool.ExplainTrace{
			PlanID: "plan-1", SnapshotDigest: "snap-1",
			Events: []tool.TraceEvent{{
				Stage: tool.TraceStageSemantics, Subject: "information.search.web",
				Event: "recognized", ReasonCode: "need_required",
			}},
		},
	}}
	got := captureBrowserDiagLog(t, func() {
		emitFinalToolSurfaceDiagnostics([]map[string]interface{}{toolDef("invoke_lookup", "lookup", nil, nil)}, surface, 0)
	})
	if !strings.Contains(got, "[explain-trace] plan=plan-1") {
		t.Fatalf("managed surface must emit explain-trace: %q", got)
	}
	if strings.Contains(got, "[browser-diag] CP4") {
		t.Fatalf("managed surface without soup leak must not emit CP4: %q", got)
	}
	if strings.Contains(got, "invoke_lookup") {
		t.Fatalf("explain-trace leaked a function name: %q", got)
	}
}

func TestEmitFinalToolSurfaceDiagnosticsKeepsBrowserDiagOnLegacy(t *testing.T) {
	got := captureBrowserDiagLog(t, func() {
		emitFinalToolSurfaceDiagnostics([]map[string]interface{}{toolDef("browser", "browser", nil, nil)}, nil, 0)
	})
	if !strings.Contains(got, "[browser-diag] CP4_FinalToolList") {
		t.Fatalf("legacy path must keep CP4: %q", got)
	}
	if strings.Contains(got, "[explain-trace]") {
		t.Fatalf("legacy path should not emit explain-trace: %q", got)
	}
}

func TestEmitFinalToolSurfaceDiagnosticsSoupLeakKeepsCompatLayer(t *testing.T) {
	surface := &semanticCallSurface{plan: tool.ToolPlan{Trace: tool.ExplainTrace{
		PlanID: "plan-leak",
		Events: []tool.TraceEvent{{Stage: tool.TraceStageSemantics, Subject: "information.search.web", Event: "recognized", ReasonCode: "need_required"}},
	}}}
	got := captureBrowserDiagLog(t, func() {
		emitFinalToolSurfaceDiagnostics([]map[string]interface{}{toolDef("browser", "browser", nil, nil)}, surface, 0)
	})
	if !strings.Contains(got, "[explain-trace]") || !strings.Contains(got, "[browser-diag] CP4") {
		t.Fatalf("soup leak must keep explain-trace and CP4: %q", got)
	}
}

func TestBrowserDiagStreamFilterIgnoresInlineMention(t *testing.T) {
	got := captureBrowserDiagLog(t, func() {
		BrowserDiagCP5_StreamFilter(false, 0, false, 0, false, browserDiagHasBrowserRolePrefix("The Browser: tool already closed cleanly."), "The Browser: tool already closed cleanly.")
	})

	if got != "" {
		t.Fatalf("inline Browser mention should not trigger stream diagnostic, got %q", got)
	}
}
