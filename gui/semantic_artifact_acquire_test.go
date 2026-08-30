package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
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
		{"reserved tld", map[string]interface{}{"url": "https://example.invalid/skip"}, "url_host_rejected"},
		{"reserved schemeless", map[string]interface{}{"url": "example.invalid/skip"}, "url_host_rejected"},
		{"localhost", map[string]interface{}{"url": "http://localhost/file"}, "url_host_rejected"},
		{"loopback", map[string]interface{}{"url": "http://127.0.0.1/file"}, "url_host_rejected"},
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
	lan, err := semanticTrustedAcquireRemoteArgsAllowed(map[string]interface{}{"url": "http://192.168.1.10/photo.jpg"})
	if err != nil || lan != "http://192.168.1.10/photo.jpg" {
		t.Fatalf("desktop acquire must still admit a LAN URL: url=%q err=%v", lan, err)
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

// The shared downloader's anti-crawl hint names legacy-only parameters
// (save_path, via_browser) that the closed managed schema rejects. On the
// managed surface the suggestion must be rewritten against what actually
// exists (§4.13): petition browser, then retry with the same arguments —
// otherwise the model follows the hint into parameter_schema_invalid
// (2026-08-27 birthday-deck turn).
func TestSemanticTrustedAcquireRemoteErrorRewritesLegacySuggestion(t *testing.T) {
	legacy := "HTTP 403: 403 Forbidden（目标站点存在反爬验证。请先用 browser 工具打开 https://example.com/x.jpg 完成人机验证后重试；仍失败则用 download_file(url, save_path, via_browser=true) 让浏览器直接下载）"
	got := semanticTrustedAcquireRemoteError(assertError{legacy}).Error()
	if strings.Contains(got, "via_browser") || strings.Contains(got, "save_path") {
		t.Fatalf("legacy-only parameters must not survive: %s", got)
	}
	if !strings.Contains(got, "HTTP 403") || !strings.Contains(got, "browser") || !strings.Contains(got, "请愿") {
		t.Fatalf("managed guidance must keep status and name the petition path: %s", got)
	}
	// Errors without legacy suggestions pass through untouched.
	plain := assertError{"HTTP 500: boom"}
	if got := semanticTrustedAcquireRemoteError(plain); got != plain {
		t.Fatalf("unrelated error rewritten: %v", got)
	}
}

type assertError struct{ text string }

func (e assertError) Error() string { return e.text }

// An extension-less URL tail must gain the extension its Content-Type
// implies: downstream consumers (office images, MIME sniffers) key on the
// suffix, and the 2026-08-27 birthday-deck turn burned three image_missing
// rejections because the artifact landed as "cat" while the model guessed
// "cat.jpg".
func TestSemanticAcquireNormalizeExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cat"), []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := semanticAcquireNormalizeExtension(dir, "cat", &websearch.FetchResult{ContentType: "image/jpeg"})
	if got != "cat.jpg" {
		t.Fatalf("name=%q, want cat.jpg", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "cat.jpg"))
	if err != nil || string(body) != "jpeg-bytes" {
		t.Fatalf("renamed file content lost: %q err=%v", body, err)
	}
	// An existing extension is never rewritten, an unknown MIME is a no-op,
	// and a rename collision keeps the original name.
	if got := semanticAcquireNormalizeExtension(dir, "photo.png", &websearch.FetchResult{ContentType: "image/jpeg"}); got != "photo.png" {
		t.Fatalf("existing extension rewritten: %q", got)
	}
	if got := semanticAcquireNormalizeExtension(dir, "data", &websearch.FetchResult{ContentType: "application/octet-stream"}); got != "data" {
		t.Fatalf("unknown MIME renamed: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "cat2.jpg"), []byte("taken"), 0o644); err == nil {
		_ = os.Remove(filepath.Join(dir, "cat2"))
		if got := semanticAcquireNormalizeExtension(dir, "cat2", &websearch.FetchResult{ContentType: "image/jpeg"}); got != "cat2" {
			t.Fatalf("collision must keep original: %q", got)
		}
	}
}

// Production 2026-08-28 PPT turn: download_file's petitioned family spent its
// three invocations on HTTP 404/403 FAILURES, and the retired-name denial then
// told the model the tool "already ran successfully ... that earlier result
// still stands ... do not retry". No successful result existed; the model was
// ordered to treat a failed download as valid evidence. A retired grant whose
// selection never completed must be presented as the failure it was.
func TestSemanticFailedRetiredToolDenialDoesNotClaimSuccess(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileDownload)}
	h.semanticTrustedArtifactAcquire = func(userID, rawURL string) (string, error) {
		return "", assertError{"HTTP 403 Forbidden"}
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "下载这个文件", "desktop", "root-acquire-fail", "turn-acquire-fail",
		&intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	// The family budget is three invocations; all three fail.
	for i := 1; i <= 3; i++ {
		if got := cb.ExecuteTool("download_file", `{"url":"https://example.com/r.jpg"}`); !strings.Contains(got, "HTTP 403") {
			t.Fatalf("failed acquire %d must surface the adapter failure: %q", i, got)
		}
	}
	allowed, reason := cb.IsToolCallAllowed("download_file", `{"url":"https://example.com/r.jpg"}`)
	if allowed {
		t.Fatal("an exhausted family must keep denying")
	}
	if strings.Contains(reason, "already ran successfully") || strings.Contains(reason, "still stands") {
		t.Fatalf("a failed tool was reported as a standing success: %q", reason)
	}
	if !strings.Contains(reason, "download_file") {
		t.Fatalf("denial must name the tool: %q", reason)
	}
}

// Production 2026-08-29 cloud-workspace PPT turn: the office face listed
// download_file as an optional offer, the renderer said "call whenever listed",
// and the model probed https://example.invalid/skip. DNS failed as "invalid
// host", the grant burned, and the loop petitioned bash/write_file instead of
// using office native charts. Placeholder hosts are Intake: they must not
// consume the acquire budget a later real image URL still needs.
func TestSemanticAcquirePlaceholderHostIsIntakeNotAdmission(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileDownload)}
	h.semanticTrustedArtifactAcquire = func(userID, rawURL string) (string, error) {
		if userID != "user-1" || rawURL != "https://example.com/report.pdf" {
			t.Fatalf("user=%q url=%q", userID, rawURL)
		}
		return "Acquired remote artifact into the workspace.\nName: report.pdf", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "下载这个文件", "desktop", "root-acquire-placeholder", "turn-acquire-placeholder",
		&intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	allowed, reason := cb.IsToolCallAllowed(name, `{"url":"https://example.invalid/skip"}`)
	if allowed {
		t.Fatal("placeholder host must be intake-rejected")
	}
	if !strings.Contains(reason, "real HTTP") || !strings.Contains(reason, "skip token") {
		t.Fatalf("intake reason=%q", reason)
	}
	if allowed, _ := cb.IsToolCallAllowed(name, `{"url":"example.invalid/skip"}`); allowed {
		t.Fatal("schemeless placeholder host must be intake-rejected")
	}
	if strings.Contains(reason, "no such host") {
		t.Fatalf("placeholder host must not reach DNS: %q", reason)
	}
	if got := cb.ExecuteTool(name, `{"url":"https://example.invalid/skip"}`); !strings.Contains(got, "skip token") {
		t.Fatalf("execute of placeholder must stay at intake: %q", got)
	}
	allowed, reason = cb.IsToolCallAllowed(name, `{"url":"https://example.com/report.pdf"}`)
	if !allowed {
		t.Fatalf("placeholder probe burned the grant: %q", reason)
	}
	got := cb.ExecuteTool(name, `{"url":"https://example.com/report.pdf"}`)
	if !strings.Contains(got, "Acquired remote artifact") {
		t.Fatalf("real url after placeholder probe=%q", got)
	}
}
