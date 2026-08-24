package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestIMSemanticFileDownloadUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileDownload)}
	h.semanticTrustedArtifactAcquire = func(userID, rawURL string) (string, error) {
		if userID != "user-1" || rawURL != "https://example.com/report.pdf" {
			t.Fatalf("user=%q url=%q", userID, rawURL)
		}
		return "Acquired remote artifact into the workspace.\nName: report.pdf", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "下载这个文件", "lansenger", "root-acquire", "turn-acquire", &intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedAcquireRemoteAdapter {
		t.Fatalf("selection=%+v", surface.plan.Selections[0])
	}
	name := extractToolName(defs[0])
	if name != "download_file" {
		t.Fatalf("managed download name=%q, want download_file", name)
	}
	if surface.plan.Selections[0].AdapterName == "download_file" {
		t.Fatal("adapter leaked soup name")
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	// save_path, headers and via_browser were the legacy downloader's own
	// arguments. The managed surface must reject them before execution.
	if got := cb.ExecuteTool(name, `{"url":"https://example.com/report.pdf","save_path":"C:\\Windows\\evil.exe","headers":{"Authorization":"Bearer secret"},"via_browser":true}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_schema_invalid") {
		t.Fatalf("legacy arguments accepted: %q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "下载这个文件", "lansenger", "root-acquire-exec", "turn-acquire-exec", &intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("exec surface handled=%v err=%v", handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"url":"https://example.com/report.pdf"}`)
	if !strings.Contains(got, "Acquired remote artifact") || !strings.Contains(got, "report.pdf") {
		t.Fatalf("acquire=%q", got)
	}
}

func TestSemanticTrustedAcquireRemoteArgsRejectNonPublicShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"extra field", map[string]interface{}{"url": "https://example.com/a", "save_path": "x"}, "arguments_rejected"},
		{"missing url", map[string]interface{}{}, "arguments_rejected"},
		{"non string", map[string]interface{}{"url": 7}, "arguments_rejected"},
		{"empty", map[string]interface{}{"url": "   "}, "url_required"},
		{"file scheme", map[string]interface{}{"url": "file:///etc/passwd"}, "url_scheme_rejected"},
		{"no host", map[string]interface{}{"url": "https://"}, "url_invalid"},
		{"credentials", map[string]interface{}{"url": "https://user:secret@example.com/a"}, "url_credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := semanticTrustedAcquireRemoteArgsAllowed(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("value=%q err=%v, want %s", got, err, tc.want)
			}
		})
	}
	got, err := semanticTrustedAcquireRemoteArgsAllowed(map[string]interface{}{"url": " https://example.com/report.pdf "})
	if err != nil || got != "https://example.com/report.pdf" {
		t.Fatalf("accepted url=%q err=%v", got, err)
	}
}

// The projection is the only text the model sees, so a host location must never
// survive it even if the downloader echoes one back.
func TestSemanticTrustedAcquireRemoteResultProjectionRejectsHostLocations(t *testing.T) {
	for _, text := range []string{
		"Saved to C:\\Users\\me\\workspace\\report.pdf",
		"Saved to /home/me/workspace/report.pdf",
		"download_file finished",
		"use save_path next time",
		"[file_base64]AAAA",
	} {
		if out, err := semanticTrustedAcquireRemoteResultProjection(text); err == nil {
			t.Fatalf("projection accepted %q as %q", text, out)
		}
	}
	if _, err := semanticTrustedAcquireRemoteResultProjection("   "); err == nil {
		t.Fatal("empty projection accepted")
	}
	clean := "Acquired remote artifact into the workspace.\nName: report.pdf\nType: application/pdf\nSize: 12 bytes"
	out, err := semanticTrustedAcquireRemoteResultProjection(clean)
	if err != nil || out != clean {
		t.Fatalf("clean projection out=%q err=%v", out, err)
	}
	// A URL in the text is not a host location: the scheme's separators must
	// not be mistaken for a rooted path.
	if _, err := semanticTrustedAcquireRemoteResultProjection("Acquired https://example.com/report.pdf"); err != nil {
		t.Fatalf("url projection err=%v", err)
	}
}
