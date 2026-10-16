package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestPreviewToolResultForToolComputerObserveKeepsRefs(t *testing.T) {
	var b strings.Builder
	b.WriteString("mode=text_primary screen=100x100\nwindows:\n  - Word\nelements (80):\n")
	for i := 0; i < 80; i++ {
		b.WriteString(fmt.Sprintf("  e%d [button] \"SaveAsDialog\" conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\n", i))
	}
	b.WriteString("ocr_excerpt: Document saved successfully\n")
	raw := b.String()
	if len(raw) <= MaxToolResultLen {
		t.Fatalf("fixture must exceed generic 4KiB cap, len=%d", len(raw))
	}
	got := PreviewToolResultForTool("computer_observe", raw)
	if !strings.Contains(got, "e0 [button]") || !strings.Contains(got, "e79 [button]") {
		t.Fatalf("latest observe preview must keep click refs")
	}
	if !strings.Contains(got, "ocr_excerpt: Document saved successfully") {
		t.Fatal("OCR excerpt missing from observe preview")
	}
}

func TestProjectToolResultKeepsSSHStatusInline(t *testing.T) {
	// 2026-09-27: a 6406-byte ssh status was cut at 4096, and uptime/memory/disk
	// sat in the discarded middle.
	raw := "SSH 连接成功\n" + strings.Repeat("m", 2000) + "\n=== UPTIME ===\nup 18 min\n=== MEMORY ===\n5.8Gi\n" + strings.Repeat("t", 4200)
	if len(raw) <= MaxToolResultLen || len(raw) > TerminalMaxToolResult {
		t.Fatalf("fixture len=%d, want between %d and %d", len(raw), MaxToolResultLen, TerminalMaxToolResult)
	}
	for _, toolName := range []string{"ssh", "bash"} {
		projection, err := ProjectToolResult(toolName, "owner", raw)
		if err != nil {
			t.Fatalf("%s: %v", toolName, err)
		}
		if projection.Spilled || projection.Preview != raw {
			t.Fatalf("%s status was truncated: spilled=%v preview=%d raw=%d", toolName, projection.Spilled, len(projection.Preview), len(raw))
		}
	}
}

func TestProjectToolResultKeepsDesktopFileAndCommand(t *testing.T) {
	file := "int main(){}\n" + strings.Repeat("a", 20_000)
	if len(file) <= MaxToolResultLen || len(file) > desktop.ToolResultMax {
		t.Fatalf("fixture len=%d", len(file))
	}
	projection, err := ProjectToolResult("desktop", "owner", file)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Spilled || projection.Preview != file {
		t.Fatalf("file was cut: spilled=%v preview=%d raw=%d", projection.Spilled, len(projection.Preview), len(file))
	}
	cmd := strings.Repeat("note\n", 900) + "a.c:1: error: expected ';'\n(exit 1)"
	if len(cmd) <= MaxToolResultLen || len(cmd) > desktop.ToolResultMax {
		t.Fatalf("command fixture len=%d", len(cmd))
	}
	projection, err = ProjectToolResult("desktop", "owner", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Spilled || projection.Preview != cmd || !strings.Contains(projection.Preview, "(exit 1)") {
		t.Fatalf("command was cut: spilled=%v preview=%d", projection.Spilled, len(projection.Preview))
	}
	huge := strings.Repeat("x", desktop.ToolResultMax) + "\na.c:1: error: expected ';'\n(exit 1)"
	got := PreviewToolResultForTool("desktop", huge)
	if !strings.Contains(got, "expected ';'") || !strings.Contains(got, "(exit 1)") {
		t.Fatalf("command tail dropped")
	}
	longFile := "int main(void) {\n" + strings.Repeat("a", desktop.ToolResultMax)
	if !strings.HasPrefix(PreviewToolResultForTool("desktop", longFile), "int main(void)") {
		t.Fatal("file head dropped")
	}
}

func TestProjectToolResultKeepsBoundedCJKDocumentPagesInline(t *testing.T) {
	// This mirrors a final page of a Chinese document: it is a safe, explicitly
	// bounded page, but its UTF-8 representation is larger than the generic
	// 4 KiB tool-result limit.
	page := strings.Repeat("文", 5_000)
	for _, toolName := range []string{
		"office", "read_document", "read_excel", "read_pptx",
		"read_file", "FileRead",
	} {
		t.Run(toolName, func(t *testing.T) {
			projection, err := ProjectToolResult(toolName, "", page)
			if err != nil {
				t.Fatalf("ProjectToolResult() error = %v", err)
			}
			if projection.Spilled || projection.Handle != nil {
				t.Fatalf("document page should stay inline, projection=%+v", projection)
			}
			if projection.Preview != page {
				t.Fatalf("document page was unexpectedly truncated: got %d bytes, want %d", len(projection.Preview), len(page))
			}
		})
	}
}

func TestProjectToolResultStillSpillsLargeOfficeDocument(t *testing.T) {
	document := strings.Repeat("文", 40_000)
	projection, err := ProjectToolResult("read_document", "session", document)
	if err != nil {
		t.Fatalf("ProjectToolResult() error = %v", err)
	}
	if !projection.Spilled || projection.Handle == nil {
		t.Fatalf("oversized office document must still spill, projection=%+v", projection)
	}
	if len(projection.Preview) > DocumentReadMaxToolResult {
		t.Fatalf("preview exceeds document limit: %d > %d", len(projection.Preview), DocumentReadMaxToolResult)
	}
}

func TestToolResultReadBackKeepsOnePageInline(t *testing.T) {
	page := toolresult.FormatReadResult(toolresult.ReadResult{
		ID: "20260927T033427_ssh_abc", Offset: 0, ReturnedBytes: 6000, TotalBytes: 20000,
		Truncated: true, NextOffset: 6000, Content: strings.Repeat("x", 6000),
	})
	if len(page) <= MaxToolResultLen {
		t.Fatalf("fixture len=%d, want above the generic 4KiB cap", len(page))
	}
	projection, err := ProjectToolResult("read_tool_result", "owner", page)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Spilled || projection.Preview != page {
		t.Fatalf("read page was cut: spilled=%v preview=%d page=%d", projection.Spilled, len(projection.Preview), len(page))
	}
	if !strings.Contains(projection.Preview, "next_offset: 6000") {
		t.Fatal("continuation fields missing")
	}
}

func TestToolResultReadBackStillSpillsPastMaxPage(t *testing.T) {
	page := strings.Repeat("x", toolresult.MaxReadLimit+8192)
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "read_tool_result",
		SessionKey: "owner",
		Content:    page,
		Limit:      ToolResultReadMaxToolResult,
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Spilled || projection.Handle == nil {
		t.Fatalf("oversized read-back must still spill: %+v", projection)
	}
	if len(projection.Preview) > ToolResultReadMaxToolResult {
		t.Fatalf("preview %d exceeds read budget %d", len(projection.Preview), ToolResultReadMaxToolResult)
	}
	if strings.Count(projection.Preview, "已截断") != 1 {
		t.Fatalf("read-back was truncated more than once: %s", projection.Preview[max(0, len(projection.Preview)-180):])
	}
}

func TestDocumentReadToolResultLimitScalesWithContextWindow(t *testing.T) {
	for _, tc := range []struct {
		context int
		want    int
	}{
		{context: 32_000, want: 32 * 1024},
		{context: 200_000, want: 125_000},
		{context: 400_000, want: 250_000},
		{context: 1_000_000, want: 256 * 1024},
	} {
		if got := DocumentReadToolResultLimit(tc.context); got != tc.want {
			t.Fatalf("DocumentReadToolResultLimit(%d) = %d, want %d", tc.context, got, tc.want)
		}
	}
}

func TestProjectToolResultWithContextKeepsLargerDocumentPageInline(t *testing.T) {
	page := strings.Repeat("文", 40_000) // 120KB: too large for the static default.
	projection, err := ProjectToolResultWithContext("read_document", "", page, 200_000)
	if err != nil {
		t.Fatalf("ProjectToolResultWithContext() error = %v", err)
	}
	if projection.Spilled || projection.Preview != page {
		t.Fatalf("context-aware document page should stay inline: %+v", projection)
	}
}

func TestInferFileDeliveryMessageUsesStructuredDocType(t *testing.T) {
	generic := InferFileDeliveryMessage("requirements_design_tasks.pdf")
	if strings.Contains(generic, "需求文档") || strings.Contains(generic, "技术设计") || strings.Contains(generic, "任务列表") {
		t.Fatalf("InferFileDeliveryMessage inferred workflow type from file name: %q", generic)
	}

	requirements := InferFileDeliveryMessageForDocType("requirements", "anything.pdf")
	if !strings.Contains(requirements, "需求文档") {
		t.Fatalf("requirements message = %q, want requirements prompt", requirements)
	}

	tasks := InferFileDeliveryMessageForDocType("task_plan", "anything.pdf")
	if !strings.Contains(tasks, "任务列表") {
		t.Fatalf("task_plan message = %q, want task-list prompt", tasks)
	}
}

func TestTrimConversationKeepsPendingAsk(t *testing.T) {
	msgs := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": strings.Repeat("x", 8000)},
		map[string]string{"role": "assistant", "content": "tool"},
		map[string]string{"role": "tool", "content": AskUserResultMarker(&AskUserRequest{Question: "continue?"})},
	}
	got := TrimConversation(msgs, 100, 0, nil)
	if len(got) != len(msgs) {
		t.Fatalf("pending ask was trimmed: %d -> %d", len(msgs), len(got))
	}
}

func TestConversationHasPendingAskOnlyAtTail(t *testing.T) {
	pending := []interface{}{
		map[string]string{"role": "user", "content": "hi"},
		map[string]string{"role": "tool", "content": AskUserResultMarker(&AskUserRequest{Question: "continue?"})},
	}
	if !conversationHasPendingAsk(pending) {
		t.Fatal("tail ask should be pending")
	}
	answered := []interface{}{
		map[string]string{"role": "tool", "content": AskUserResultMarker(&AskUserRequest{Question: "continue?"})},
		map[string]string{"role": "user", "content": "ok"},
	}
	if conversationHasPendingAsk(answered) {
		t.Fatal("ask followed by a user reply is not pending")
	}
}

func TestConversationHasPendingAskRewrittenAskedUser(t *testing.T) {
	pending := []interface{}{
		map[string]string{"role": "user", "content": "login"},
		map[string]string{"role": "tool", "content": FormatAskedUserHistoryResult(&AskUserRequest{
			Question: "solve captcha",
			Context:  "resume_task_id=bt-9",
		})},
	}
	if !conversationHasPendingAsk(pending) {
		t.Fatal("rewritten Asked user: tail should be pending")
	}
	msgs := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": strings.Repeat("x", 8000)},
		map[string]string{"role": "assistant", "content": "tool"},
		map[string]string{"role": "tool", "content": "Asked user: continue?\nresume_task_id=bt-9"},
	}
	got := TrimConversation(msgs, 100, 0, nil)
	if len(got) != len(msgs) {
		t.Fatalf("rewritten pending ask was trimmed: %d -> %d", len(msgs), len(got))
	}
	answered := []interface{}{
		map[string]string{"role": "tool", "content": "Asked user: continue?"},
		map[string]string{"role": "user", "content": "ok"},
	}
	if conversationHasPendingAsk(answered) {
		t.Fatal("rewritten ask followed by a user reply is not pending")
	}
	if !conversationHasPendingAsk([]interface{}{
		map[string]string{"role": "tool", "content": "Asked user"},
	}) {
		t.Fatal("bare Asked user should be pending")
	}
	if conversationHasPendingAsk([]interface{}{
		map[string]string{"role": "assistant", "content": "Asked user: continue?"},
	}) {
		t.Fatal("assistant echo must not freeze trim")
	}
}

func TestFormatAskedUserHistoryResultKeepsResumeTaskID(t *testing.T) {
	got := FormatAskedUserHistoryResult(&AskUserRequest{
		Question: "solve captcha",
		Context:  "resume_task_id=bt-9 challenge",
	})
	if !strings.Contains(got, "Asked user: solve captcha") || !strings.Contains(got, "resume_task_id=bt-9") {
		t.Fatalf("got=%q", got)
	}
}

func TestTrimConversationFallbackKeepsUserRequestAndFilePath(t *testing.T) {
	msgs := trimConversationOverflowFixture()
	got := TrimConversation(msgs, 4000, 0, nil)
	blob := flattenConversation(got)
	if !strings.Contains(blob, "请改 login.go") || !strings.Contains(blob, "login.go") {
		t.Fatalf("handoff missing dropped request or path:\n%s", blob)
	}
	if !strings.Contains(blob, "KEEP-TAIL") {
		t.Fatal("recent tail was dropped")
	}
	if strings.Contains(blob, "已被省略，请基于最近的上下文继续工作") {
		t.Fatal("generic placeholder replaced the handoff")
	}
}

func TestTrimConversationRejectsOversizedOrToolCallSummary(t *testing.T) {
	msgs := trimConversationOverflowFixture()
	oversized := TrimConversation(msgs, 4000, 0, func(string) string {
		return strings.Repeat("长", 6000)
	})
	if strings.Contains(flattenConversation(oversized), strings.Repeat("长", 40)) {
		t.Fatal("oversized summary was kept")
	}
	toolShaped := TrimConversation(msgs, 4000, 0, func(string) string {
		return `{"tool_calls":[{"name":"read_file"}]}`
	})
	if strings.Contains(flattenConversation(toolShaped), `"tool_calls"`) {
		t.Fatal("tool-call summary was kept")
	}
	if !strings.Contains(flattenConversation(toolShaped), "请改 login.go") {
		t.Fatal("rejected summary did not fall back to the handoff")
	}
}

func trimConversationOverflowFixture() []interface{} {
	msgs := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "请改 login.go 的登录校验"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "read",
			"tool_calls": []map[string]interface{}{
				{"id": "r1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"login.go"}`}},
			},
		},
		map[string]string{"role": "tool", "content": "package main"},
	}
	for i := 0; i < 4; i++ {
		msgs = append(msgs, map[string]string{"role": "assistant", "content": strings.Repeat("archived work ", 400)})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": "KEEP-TAIL"})
	return msgs
}

func flattenConversation(msgs []interface{}) string {
	var b strings.Builder
	for _, msg := range msgs {
		data, _ := json.Marshal(msg)
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestHistoryHandoffMergesEarlierCheckpoint(t *testing.T) {
	prior := historyHandoffEntry([]ConversationEntry{
		{Role: "user", Content: "先改 login.go 的过期时间"},
		{Role: "assistant", Content: "read", ToolCalls: []map[string]interface{}{
			{"function": map[string]interface{}{"name": "read_file", "arguments": map[string]interface{}{"path": "login.go"}}},
		}},
	})
	again := historyHandoffEntry([]ConversationEntry{
		prior,
		{Role: "user", Content: "再改 session.go"},
		{Role: "assistant", Content: "edit", ToolCalls: []map[string]interface{}{
			{"name": "edit_file", "arguments": `{"file_path":"session.go"}`},
		}},
	})
	text, _ := again.Content.(string)
	if strings.Count(text, contextHandoffPrefix) != 1 {
		t.Fatalf("previous handoff was nested: %s", text)
	}
	for _, want := range []string{"先改 login.go", "login.go", "再改 session.go", "session.go"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged handoff missing %q:\n%s", want, text)
		}
	}
}

func TestHistoryHandoffKeepsTaggedFileLists(t *testing.T) {
	entry := historyHandoffEntry([]ConversationEntry{{
		Role:    "user",
		Content: "[对话历史摘要]\n登录校验已修好\n\n<read-files>\nlogin.go\n</read-files>\n<modified-files>\nsession.go\n</modified-files>",
	}})
	text, _ := entry.Content.(string)
	if !strings.Contains(text, "login.go") || !strings.Contains(text, "session.go") {
		t.Fatalf("tagged file lists were dropped:\n%s", text)
	}
}

func TestHistoryHandoffKeepsMultilineRequestAndSummary(t *testing.T) {
	again := historyHandoffEntry([]ConversationEntry{
		{Role: "user", Content: "[Skill 优先要求]\n先用技能"},
		{Role: "user", Content: "请改 login.go\n不要动 session.go"},
		{Role: "user", Content: "[对话历史摘要]\n登录校验已修好，下一步补测试"},
	})
	text, _ := again.Content.(string)
	if strings.Contains(text, "Skill 优先要求") {
		t.Fatalf("framework prompt was stored as a user request:\n%s", text)
	}
	if !strings.Contains(text, "请改 login.go 不要动 session.go") {
		t.Fatalf("multiline request was split:\n%s", text)
	}
	if !strings.Contains(text, "登录校验已修好") {
		t.Fatalf("summary was dropped:\n%s", text)
	}
	merged := historyHandoffEntry([]ConversationEntry{again, {Role: "user", Content: "补上测试"}})
	mergedText, _ := merged.Content.(string)
	if !strings.Contains(mergedText, "登录校验已修好") || !strings.Contains(mergedText, "补上测试") {
		t.Fatalf("second handoff lost summary or new request:\n%s", mergedText)
	}
}

func TestTrimHistoryHandoffStaysWithinTokenBudget(t *testing.T) {
	const early = "请保留这条早期要求"
	handoffTokens := EstimateConversationEntryTokens(prependHistoryHandoff(
		[]ConversationEntry{{Role: "user", Content: early}}, nil))
	target := MaxMemoryTokenEstimate / 2
	body := ""
	lo, hi := 1, MaxMemoryTokenEstimate*3
	for lo <= hi {
		mid := (lo + hi) / 2
		tok := EstimateConversationEntryTokens([]ConversationEntry{{Role: "assistant", Content: strings.Repeat("a", mid)}})
		if tok <= target {
			body = strings.Repeat("a", mid)
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	unit := EstimateConversationEntryTokens([]ConversationEntry{{Role: "assistant", Content: body}})
	if unit*2 > MaxMemoryTokenEstimate || unit*2+handoffTokens <= MaxMemoryTokenEstimate || unit*3 <= MaxMemoryTokenEstimate {
		t.Fatalf("fixture gap is not handoff-sized: unit=%d handoff=%d", unit, handoffTokens)
	}
	entries := []ConversationEntry{{Role: "user", Content: early}}
	for i := 0; i < 3; i++ {
		entries = append(entries, ConversationEntry{Role: "assistant", Content: body})
	}
	trimmed := TrimHistory(entries)
	if got := EstimateConversationEntryTokens(trimmed); got > MaxMemoryTokenEstimate {
		t.Fatalf("trimmed tokens=%d budget=%d", got, MaxMemoryTokenEstimate)
	}
	text, _ := trimmed[0].Content.(string)
	if !strings.Contains(text, early) {
		t.Fatalf("handoff missing early request: %s", text)
	}
	large := 0
	for _, entry := range trimmed {
		body, _ := entry.Content.(string)
		if entry.Role == "assistant" && len(body) > 1000 {
			large++
		}
	}
	if large != 1 {
		t.Fatalf("kept %d large turns, want 1", large)
	}
}

func TestTrimConversationSummarizerSeesDroppedWork(t *testing.T) {
	var got string
	out := TrimConversation(trimConversationOverflowFixture(), 4000, 0, func(input string) string {
		got = input
		return "摘要：改了校验"
	})
	if !strings.Contains(got, "请改 login.go") || !strings.Contains(got, "login.go") || !strings.Contains(got, "package main") {
		t.Fatalf("summarizer did not see dropped work:\n%s", got)
	}
	if !strings.Contains(flattenConversation(out), "摘要：改了校验") {
		t.Fatal("accepted summary was not installed")
	}
}

func TestFormatPendingAskUserAnswerHintKeepsContext(t *testing.T) {
	got := FormatPendingAskUserAnswerHint("solve captcha", "continue", "resume_task_id=bt-9")
	if !strings.Contains(got, "resume_task_id=bt-9") || !strings.Contains(got, "continue") {
		t.Fatalf("got=%q", got)
	}
}
