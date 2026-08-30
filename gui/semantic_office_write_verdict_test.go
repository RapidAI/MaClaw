package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func trustedOfficeWriteSheets() map[string]interface{} {
	return map[string]interface{}{
		"sheets": []interface{}{
			map[string]interface{}{
				"name": "Sheet1",
				"rows": []interface{}{
					[]interface{}{map[string]interface{}{"value": "cell"}},
				},
			},
		},
	}
}

// The managed office write used to decide the outcome by searching the tool's
// prose for 缺少/错误/失败. An empty sheet set fails with none of those words,
// so the adapter announced a spreadsheet it had never created.
func TestTrustedOfficeWriteDoesNotAnnounceASpreadsheetItNeverWrote(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace

	got, err := h.writeTrustedOffice(principal, "book.xlsx", map[string]interface{}{"sheets": []interface{}{}})
	if err == nil {
		t.Fatalf("empty sheet set reported success=%q", got)
	}
	if !strings.Contains(err.Error(), "trusted_office_write_failed") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "book.xlsx")); statErr == nil {
		t.Fatal("a failed write must not leave a spreadsheet behind")
	}
}

// The success line carries the path, so scanning it for those same words let a
// filename overrule a write that had already happened.
func TestTrustedOfficeWriteJudgesTheWriteAndNotTheFilename(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace

	got, err := h.writeTrustedOffice(principal, "失败统计.xlsx", trustedOfficeWriteSheets())
	if err != nil {
		t.Fatalf("write=%q err=%v", got, err)
	}
	if !strings.Contains(got, "失败统计.xlsx") || strings.Contains(got, workspace) {
		t.Fatalf("write=%q", got)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "失败统计.xlsx")); statErr != nil {
		t.Fatalf("spreadsheet missing: %v", statErr)
	}
}

func TestTrustedOfficeWriteArgsAllowedAdmitsExactlyOneDocumentForm(t *testing.T) {
	// Presentation form: path + slides (+ optional title/subtitle).
	path, data, err := semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path":  "deck.pptx",
		"title": "生日会",
		"slides": []interface{}{
			map[string]interface{}{"title": "封面", "bullets": []interface{}{"第一条"}},
		},
	})
	if err != nil || path != "deck.pptx" || data["slides"] == nil || data["title"] != "生日会" {
		t.Fatalf("presentation form: path=%q data=%#v err=%v", path, data, err)
	}
	// Spreadsheet form still admitted.
	if _, data, err = semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path": "book.xlsx", "sheets": []interface{}{},
	}); err != nil || data["sheets"] == nil {
		t.Fatalf("spreadsheet form: data=%#v err=%v", data, err)
	}
	// Mixed, missing, or smuggled forms are rejected before the adapter runs.
	for _, args := range []map[string]interface{}{
		{"path": "x", "sheets": []interface{}{}, "slides": []interface{}{}},
		{"path": "x"},
		{"path": "x", "slides": []interface{}{}, "action": "write_pptx"},
		{"path": 42, "slides": []interface{}{}},
	} {
		if _, _, err := semanticTrustedOfficeWriteArgsAllowed(args); err == nil {
			t.Fatalf("args %#v must be rejected", args)
		}
	}
}

// Regression for the 2026-08-26 birthday-deck turn: the rendered schema
// declares sheets and slides side by side, so the model sent the unused form
// as an empty array (and on retry, both forms as stringified JSON). The
// strict either/or admission burned the one-shot grant on that correctable
// shape, the tool vanished mid-turn, and the deck was never written. An
// empty unused form must be dropped, and stringified arrays unwrapped.
func TestTrustedOfficeWriteArgsAllowedDropsEmptyUnusedForm(t *testing.T) {
	slide := map[string]interface{}{"title": "封面", "bullets": []interface{}{"生日快乐"}}
	sheet := map[string]interface{}{"name": "s1", "rows": []interface{}{[]interface{}{"a"}}}

	// slides + empty sheets array → presentation.
	path, data, err := semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path": "deck.pptx", "slides": []interface{}{slide}, "sheets": []interface{}{},
	})
	if err != nil || path != "deck.pptx" || data["slides"] == nil {
		t.Fatalf("empty unused sheets: path=%q data=%#v err=%v", path, data, err)
	}
	// sheets + null slides → spreadsheet.
	if _, data, err = semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path": "book.xlsx", "sheets": []interface{}{sheet}, "slides": nil,
	}); err != nil || data["sheets"] == nil {
		t.Fatalf("null unused slides: data=%#v err=%v", data, err)
	}
	// Both forms as stringified JSON (empty sheets string) → presentation,
	// with the slides string unwrapped into an array.
	_, data, err = semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path": "deck.pptx", "slides": `[{"title":"封面","bullets":["快乐"]}]`, "sheets": "[]",
	})
	if err != nil {
		t.Fatalf("stringified forms: err=%v", err)
	}
	slides, ok := data["slides"].([]interface{})
	if !ok || len(slides) != 1 {
		t.Fatalf("stringified slides not unwrapped: %#v", data["slides"])
	}
	// Two non-empty forms stay a genuine mix and are rejected.
	if _, _, err = semanticTrustedOfficeWriteArgsAllowed(map[string]interface{}{
		"path": "x", "slides": []interface{}{slide}, "sheets": []interface{}{sheet},
	}); err == nil {
		t.Fatal("two non-empty forms must be rejected")
	}
}

// The pre-canonicalization wash must turn the two shapes observed on
// 2026-08-26 into schema-valid arguments before validation can burn the
// one-shot grant, and must leave genuinely invalid content untouched.
func TestSemanticOfficeWriteInvocationArgsWashesUnusedAndStringifiedForms(t *testing.T) {
	parse := func(s string) map[string]interface{} {
		out := map[string]interface{}{}
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatalf("washed args are not JSON: %v", err)
		}
		return out
	}
	// slides + empty sheets array → sheets key dropped.
	got := semanticOfficeWriteInvocationArgs(`{"path":"deck.pptx","slides":[{"title":"封面"}],"sheets":[]}`)
	parsed := parse(got)
	if _, ok := parsed["sheets"]; ok {
		t.Fatalf("empty unused sheets must be dropped: %s", got)
	}
	if slides, ok := parsed["slides"].([]interface{}); !ok || len(slides) != 1 {
		t.Fatalf("slides lost: %s", got)
	}
	// Both forms stringified → slides unwrapped, empty sheets dropped.
	got = semanticOfficeWriteInvocationArgs(`{"path":"deck.pptx","slides":"[{\"title\":\"封面\"}]","sheets":"[]","title":"生日"}`)
	parsed = parse(got)
	if _, ok := parsed["sheets"]; ok {
		t.Fatalf("stringified empty sheets must be dropped: %s", got)
	}
	if _, ok := parsed["slides"].([]interface{}); !ok {
		t.Fatalf("stringified slides must be unwrapped: %s", got)
	}
	// sheets + null slides → slides key dropped, sheets kept.
	got = semanticOfficeWriteInvocationArgs(`{"path":"book.xlsx","sheets":[{"name":"s","rows":[["a"]]}],"slides":null}`)
	parsed = parse(got)
	if _, ok := parsed["slides"]; ok {
		t.Fatalf("null unused slides must be dropped: %s", got)
	}
	// Genuinely mixed content passes through untouched (fail closed later).
	mixed := `{"path":"x","slides":[{"title":"a"}],"sheets":[{"name":"s","rows":[["a"]]}]}`
	if got := semanticOfficeWriteInvocationArgs(mixed); got != mixed {
		t.Fatalf("genuine mix must pass through unchanged: %s", got)
	}
	// Garbage passes through untouched.
	garbage := `not json`
	if got := semanticOfficeWriteInvocationArgs(garbage); got != garbage {
		t.Fatalf("garbage must pass through unchanged: %s", got)
	}
}

func TestTrustedOfficeWriteRendersPresentationNatively(t *testing.T) {
	h := &IMMessageHandler{}
	workspace := t.TempDir()
	principal := desktopUserID + ":" + workspace

	got, err := h.writeTrustedOffice(principal, "生日会.pptx", map[string]interface{}{
		"title": "庆祝布偶宝宝5岁生日",
		"slides": []interface{}{
			map[string]interface{}{"title": "关于布偶宝宝", "bullets": []interface{}{"温顺粘人", "蓝眼睛长毛"}},
		},
	})
	if err != nil {
		t.Fatalf("write=%q err=%v", got, err)
	}
	if !strings.Contains(got, "Wrote presentation") || !strings.Contains(got, "生日会.pptx") {
		t.Fatalf("write=%q", got)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "生日会.pptx")); statErr != nil {
		t.Fatalf("presentation missing: %v", statErr)
	}
}

// Regression for the 2026-08-25 production failure "office pptx 生成失败:
// trusted_file_write_path_unavailable" on the main local tab. The tab's
// 切换目录 choice is stored in the global working_directory config, not in the
// per-owner binding map, so a plain desktop-user principal resolved to an
// empty workspace and every managed office write was rejected.
func TestTrustedOfficeWriteUsesConfiguredMainTabWorkingDir(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workspace := filepath.Join(t.TempDir(), "个人介绍")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir main tab: %v", err)
	}
	h := &IMMessageHandler{app: app}

	if got := trustedPrincipalBoundWorkspace(h, desktopUserID); got != filepath.Clean(workspace) {
		t.Fatalf("main tab trusted workspace = %q, want %q", got, workspace)
	}
	got, err := h.writeTrustedOffice(desktopUserID, "布偶宝宝5岁生日.pptx", map[string]interface{}{
		"title": "我家布偶宝宝5岁生日",
		"slides": []interface{}{
			map[string]interface{}{"title": "封面", "bullets": []interface{}{"生日快乐"}},
		},
	})
	if err != nil {
		t.Fatalf("write=%q err=%v", got, err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "布偶宝宝5岁生日.pptx")); statErr != nil {
		t.Fatalf("presentation missing in the configured tab directory: %v", statErr)
	}
}

// The configured main-tab directory is the main tab's own explicit choice;
// isolated owners (project/expert/group sessions) must not inherit it.
func TestTrustedPrincipalBoundWorkspaceDoesNotLeakMainTabDirToIsolatedOwners(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workspace := filepath.Join(t.TempDir(), "个人介绍")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir main tab: %v", err)
	}
	h := &IMMessageHandler{app: app}

	if got := trustedPrincipalBoundWorkspace(h, expertSessionUserID("code-reviewer")); got != "" {
		t.Fatalf("expert owner must not inherit the main tab dir, got %q", got)
	}
	if got := trustedPrincipalBoundWorkspace(h, ""); got != "" {
		t.Fatalf("empty principal must stay unavailable, got %q", got)
	}
}

// The managed office write produces a real workspace file but no file_base64
// marker, so without host registration the chat showed nothing and the user
// asked "ppt在哪？" after the assistant announced the deck. A successful write
// must record the produced artifact as a delivered path.
func TestTrustedOfficeWriteRegistersProducedArtifactForDelivery(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workspace := filepath.Join(t.TempDir(), "个人介绍")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir main tab: %v", err)
	}
	h := &IMMessageHandler{app: app}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: desktopUserID}

	got := callbacks.executeTrustedOfficeWrite(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"path":"布偶宝宝5岁生日.pptx","title":"生日","slides":[{"title":"封面","bullets":["快乐"]}]}`),
	}, false)
	if !strings.Contains(got, "Wrote presentation") {
		t.Fatalf("write=%q", got)
	}
	want := filepath.Join(workspace, "布偶宝宝5岁生日.pptx")
	abs, err := filepath.Abs(want)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range callbacks.deliveredPaths {
		if p == abs {
			found = true
		}
	}
	if !found {
		t.Fatalf("produced deck not registered for delivery: deliveredPaths=%v want %q", callbacks.deliveredPaths, abs)
	}
}
