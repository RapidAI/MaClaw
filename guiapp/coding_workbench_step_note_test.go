package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shape that put a whole report body in one checklist row: a one-line
// conclusion, then a report with headings, emphasis and code spans. The row
// note is the conclusion; the rest belongs to the transcript.
const codingStepNoteReportFixture = "T4 完成：剖析结论写在 `_probe/T4_PROFILE.md`，本轮没有改动 `sources/` 下的任何引擎代码，因为这属于纯定位任务。\n\n" +
	"## 差距的来源\n\n" +
	"单线程热态下，qm26 为 **3.94 ms**，ncnn 为 **3.21 ms**，相差 **+0.73 ms（22.7%）**。" +
	"任务描述中的 29% 是冷态数据，量级一致，而且归因只依赖比值。\n\n" +
	"按算子类别拆分（`_probe/t4opclass.py` 从 dump 自动反解析迭代除数，避免手填）：\n"

func TestCodingStepChecklistNoteKeepsOnlyTheOutcomeLine(t *testing.T) {
	got := codingStepChecklistNote(codingStepNoteReportFixture)
	want := "T4 完成：剖析结论写在 _probe/T4_PROFILE.md，本轮没有改动 sources/ 下的任何引擎代码，因为这属于纯定位任务。"
	if got != want {
		t.Fatalf("note = %q\nwant %q", got, want)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("note must be a single line: %q", got)
	}
}

func TestCodingStepChecklistNoteSkipsReportStructure(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		want    string
	}{
		{
			name:    "heading before the outcome",
			summary: "## 完成情况\n\n修复了登录页面的重定向。",
			want:    "修复了登录页面的重定向。",
		},
		{
			name:    "bullet list",
			summary: "## 完成情况\n\n- 修复了登录页面的重定向\n- 补充了回归测试",
			want:    "修复了登录页面的重定向",
		},
		{
			name:    "task checkbox and ordered marker",
			summary: "- [x] 跑通 qm26 基准\n2. 生成对比表",
			want:    "跑通 qm26 基准",
		},
		{
			name:    "fenced code is not an outcome",
			summary: "```bash\ncmake --build build\n```\n\n构建通过，产物在 build/ 下。",
			want:    "构建通过，产物在 build/ 下。",
		},
		{
			name:    "table row is not an outcome",
			summary: "| 指标 | qm26 | ncnn |\n| --- | --- | --- |\n| 单线程 | 3.94 ms | 3.21 ms |",
			want:    "",
		},
		{
			name:    "headings only",
			summary: "## 差距的来源\n\n### 细节",
			want:    "",
		},
		{
			name:    "crlf report",
			summary: "T4 完成：已定位热区。\r\n\r\n## 差距的来源\r\n\r\nqm26 为 3.94 ms。",
			want:    "T4 完成：已定位热区。",
		},
		{
			name:    "chinese ordered marker",
			summary: "1、跑通 qm26 基准\n2、生成对比表",
			want:    "跑通 qm26 基准",
		},
		{
			name:    "leading bom",
			summary: "\uFEFFT4 完成：已定位热区。",
			want:    "T4 完成：已定位热区。",
		},
		{
			name:    "blockquote",
			summary: "> 修复了登录页面的重定向。",
			want:    "修复了登录页面的重定向。",
		},
		{
			name:    "comparison is not a blockquote",
			summary: ">= 5 倍的差距，>90% 时间在卷积。",
			want:    ">= 5 倍的差距，>90% 时间在卷积。",
		},
		{
			name:    "dangling introducer colon",
			summary: "已完成以下改动：\n\n- 修复重定向\n- 补测试",
			want:    "已完成以下改动",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codingStepChecklistNote(tc.summary); got != tc.want {
				t.Fatalf("note = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodingStepChecklistNoteIsIdempotent(t *testing.T) {
	// Writers, the event snapshot and the disk loader all project. Running the
	// projection again must not shorten or corrupt an already compact note.
	inputs := []string{
		codingStepNoteReportFixture,
		strings.Repeat("单线程热态下 qm26 比 ncnn 慢 22.7%，差异集中在 depthwise 卷积。", 12),
		"step verify PASS: go test ./...",
		"这是**重点**内容",
	}
	for _, input := range inputs {
		once := codingStepChecklistNote(input)
		if twice := codingStepChecklistNote(once); twice != once {
			t.Fatalf("projection is not idempotent:\nonce  = %q\ntwice = %q", once, twice)
		}
	}
}

func TestCodingStepChecklistNoteStripsInlineMarkdown(t *testing.T) {
	got := codingStepChecklistNote("**结论**：热点在 `sources/net.cpp`，见 [剖析记录](file:///tmp/T4.md) 与 ~~旧版~~ 数据。")
	want := "结论：热点在 sources/net.cpp，见 剖析记录 与 旧版 数据。"
	if got != want {
		t.Fatalf("note = %q, want %q", got, want)
	}
	// A bare `**` is a glob, a pointer, or an exponent; leave it alone.
	if got := codingStepChecklistNote("只改了 **/*.go，power 用 x**2 表示"); !strings.Contains(got, "**/*.go") || !strings.Contains(got, "x**2") {
		t.Fatalf("code marker was stripped: %q", got)
	}
	// CJK prose has no space between emphasis and the surrounding sentence.
	if got := codingStepChecklistNote("这是**重点**内容"); got != "这是重点内容" {
		t.Fatalf("CJK bold not stripped: %q", got)
	}
	// Emphasis markers inside code spans are data, not markup.
	if got := codingStepChecklistNote("热点见 `a**b` 与 `c**d`"); got != "热点见 a**b 与 c**d" {
		t.Fatalf("code span content was treated as markup: %q", got)
	}
	if got := codingStepChecklistNote("热点见 `a**b` 与 `c**d`"); strings.ContainsRune(got, '\x00') {
		t.Fatalf("code-span placeholder leaked: %q", got)
	}
}

func TestCodingStepNoteSentenceBoundaryIgnoresDecimalsAndPaths(t *testing.T) {
	runes := []rune("版本 v2.5 的差距约为 22.7%，路径 a/b.go。")
	for i, r := range runes {
		if r != '.' {
			continue
		}
		if codingStepNoteIsSentenceEnd(runes, i) {
			t.Fatalf("index %d in %q must not be a sentence end", i, string(runes))
		}
	}
	last := len(runes) - 1
	if !codingStepNoteIsSentenceEnd(runes, last) {
		t.Fatalf("CJK full stop at the end must be a sentence end: %q", string(runes[last]))
	}
}

func TestCodingStepChecklistNoteBoundsLengthWithoutReportMarker(t *testing.T) {
	long := strings.Repeat("单线程热态下 qm26 比 ncnn 慢 22.7%，差异集中在 depthwise 卷积。", 12)
	got := codingStepChecklistNote(long)
	runes := []rune(got)
	if len(runes) > codingStepNoteMaxRunes+1 {
		t.Fatalf("note has %d runes, budget %d: %q", len(runes), codingStepNoteMaxRunes, got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "已截断") {
		t.Fatalf("note must stay one plain line: %q", got)
	}
	last := runes[len(runes)-1]
	if last != '…' && !strings.ContainsRune("。！？；.!?;", last) {
		t.Fatalf("truncated note should end on a boundary or ellipsis: %q", got)
	}
	// Short notes are untouched.
	if got := codingStepChecklistNote("step verify PASS: go test ./..."); got != "step verify PASS: go test ./..." {
		t.Fatalf("short note changed: %q", got)
	}
}

func TestUpdateStickyCodingStepStatusStoresANoteNotTheReport(t *testing.T) {
	userID := "test-step-note-status"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{{Index: 4, Title: "剖析单线程路径", Status: codingStepRunning}})

	h.updateStickyCodingStepStatus(userID, 4, codingStepPassed, codingStepNoteReportFixture)

	mem := h.getStickyCodingWorkbenchMemory(userID)
	if len(mem.StepStatuses) != 1 {
		t.Fatalf("steps = %+v", mem.StepStatuses)
	}
	note := mem.StepStatuses[0].Summary
	if strings.Contains(note, "##") || strings.Contains(note, "3.21 ms") || strings.Contains(note, "\n") {
		t.Fatalf("report body leaked into the checklist note: %q", note)
	}
	if !strings.Contains(note, "T4 完成") {
		t.Fatalf("outcome line missing: %q", note)
	}
}

func TestSetStickyCodingStepStatusesProjectsNotes(t *testing.T) {
	userID := "test-step-note-set"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{{
		Index:   1,
		Title:   "剖析单线程路径",
		Status:  codingStepPassed,
		Summary: codingStepNoteReportFixture,
	}})

	note := h.getStickyCodingWorkbenchMemory(userID).StepStatuses[0].Summary
	if strings.Contains(note, "##") || strings.Contains(note, "\n") || !strings.Contains(note, "T4 完成") {
		t.Fatalf("seeded note was not projected: %q", note)
	}
}

func TestUpdateStickyCodingStepVerifyStoresANote(t *testing.T) {
	userID := "test-step-note-verify"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{{Index: 2, Title: "验证构建", Status: codingStepRunning}})

	output := "src/game.cpp(114): error C3861: 'spawnFood': identifier not found\n" +
		strings.Repeat("more compiler output\n", 40)
	h.updateStickyCodingStepVerify(userID, 2, "cmake --build build", false, "step verify FAIL: cmake --build build\n"+output)

	note := h.getStickyCodingWorkbenchMemory(userID).StepStatuses[0].Summary
	if note != "step verify FAIL: cmake --build build" {
		t.Fatalf("verify note = %q", note)
	}
}

func TestMarkRemainingCodingStepsSkippedStoresANote(t *testing.T) {
	userID := "test-step-note-skip"
	h := &IMMessageHandler{}
	h.clearStickyCodingWorkbenchMemory(userID)
	t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })
	h.setStickyCodingStepStatuses(userID, []codingWorkbenchStepStatus{
		{Index: 1, Title: "T1", Status: codingStepFailed},
		{Index: 2, Title: "T2", Status: codingStepPending},
	})

	h.markRemainingCodingStepsSkipped(userID, 1, "## 跳过原因\n\n- prior step failed")

	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.StepStatuses[1].Status != codingStepSkipped {
		t.Fatalf("T2 status = %q", mem.StepStatuses[1].Status)
	}
	if note := mem.StepStatuses[1].Summary; note != "prior step failed" {
		t.Fatalf("skip note = %q", note)
	}
}

func TestCodingWorkbenchStepStatusMarshalProjectsTheNote(t *testing.T) {
	// Even a writer that bypassed the setters cannot put a report body on the
	// wire: the JSON gate projects the note.
	raw := codingWorkbenchStepStatus{Index: 4, Title: "剖析单线程路径", Status: codingStepPassed, Summary: codingStepNoteReportFixture}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decoded.Summary, "##") || strings.Contains(decoded.Summary, "\n") || !strings.Contains(decoded.Summary, "T4 完成") {
		t.Fatalf("marshaled note kept the report body: %q", decoded.Summary)
	}
}

func TestCodingWorkbenchStepsSnapshotHealsLegacyReportNotes(t *testing.T) {
	steps := []codingWorkbenchStepStatus{
		{Index: 1, Status: codingStepPassed, Summary: codingStepNoteReportFixture},
		{Index: 2, Status: codingStepFailed, Summary: "src/game.cpp(114): error C3861: 'spawnFood': identifier not found"},
	}
	out := codingWorkbenchStepsSnapshot(steps)
	if strings.Contains(out[0].Summary, "##") || strings.Contains(out[0].Summary, "\n") {
		t.Fatalf("event payload kept the report body: %q", out[0].Summary)
	}
	if out[1].Summary != steps[1].Summary {
		t.Fatalf("a real one-line error note must survive: %q", out[1].Summary)
	}
	// The snapshot is a display projection; the stored value is not rewritten.
	if steps[0].Summary != codingStepNoteReportFixture {
		t.Fatalf("snapshot must copy, not mutate: %q", steps[0].Summary)
	}
}

func TestStickyLoadHealsLegacyReportNotes(t *testing.T) {
	dir := t.TempDir()
	userID := "desktop-user:" + dir
	path := filepath.Join(dir, stickyCodingMemoryFileName)
	payload := map[string]any{
		"kind": "local",
		"step_statuses": []map[string]any{
			{"index": 4, "title": "剖析单线程路径", "status": codingStepPassed, "summary": codingStepNoteReportFixture},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	mem, found, missing := loadStickyCodingWorkbenchMemoryFromDisk(userID)
	if !found || missing {
		t.Fatalf("sticky load found=%v missing=%v", found, missing)
	}
	if len(mem.StepStatuses) != 1 {
		t.Fatalf("steps = %+v", mem.StepStatuses)
	}
	if note := mem.StepStatuses[0].Summary; strings.Contains(note, "##") || strings.Contains(note, "\n") || !strings.Contains(note, "T4 完成") {
		t.Fatalf("restored note was not healed: %q", note)
	}
}
