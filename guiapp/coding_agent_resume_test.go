package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestCodingAgentResumeRoundTripKeepsTheLastLoop(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	src := filepath.Join(dir, "snake.cpp")
	body := []byte("int main(){return 0;}\n")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(src)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicalCodingPath(src)
	delta := []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result.md"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{"id": "call-1", "type": "function"},
		}},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at snake.cpp"},
		{Role: "assistant", Content: "结果已写入 result.md"},
	}
	recordCodingAgentResume(userID, dir, delta, map[string]codingFileSnapshot{key: snap})
	recordCodingAgentResume(userID, dir, nil, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) != 4 {
		t.Fatalf("history = %#v", got.History)
	}
	if got.History[0].Role != "user" || got.History[2].ToolCallID != "call-1" {
		t.Fatalf("history = %#v", got.History)
	}
	if _, ok := got.Snapshots[key]; !ok {
		t.Fatalf("snapshots = %#v", got.Snapshots)
	}

	cb := newCodingSubAgentCallbacks(&CodingSubAgent{projectPath: dir}, &TaskItem{}, "", "", nil)
	if n := cb.installRestoredReadSnapshots(dir, got.Snapshots); n != 1 {
		t.Fatalf("restored snapshots = %d", n)
	}
	if ok, msg := cb.validateReadSnapshot(src); !ok || msg != "" {
		t.Fatalf("unchanged file ok=%v msg=%q", ok, msg)
	}
	cb.trackFile(src)
	if !cb.exploredBeforeFirstEdit() {
		t.Fatal("restored read should count as exploration before this turn's edit")
	}

	if err := os.WriteFile(src, []byte("int main(){return 1;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := newCodingSubAgentCallbacks(&CodingSubAgent{projectPath: dir}, &TaskItem{}, "", "", nil)
	if n := changed.installRestoredReadSnapshots(dir, got.Snapshots); n != 0 {
		t.Fatalf("changed file restored %d snapshots", n)
	}
	if ok, _ := changed.validateReadSnapshot(src); ok {
		t.Fatal("changed file was treated as already read")
	}
}

func TestCodingAgentResumeSecondTurnKeepsEarlierEvidence(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	src := filepath.Join(dir, "snake.cpp")
	if err := os.WriteFile(src, []byte("int main(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(src)
	if err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "call-1"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "frame at snake.cpp"},
		{Role: "assistant", Content: "窗口版已经接上"},
		{Role: "user", Content: "keep going", HostContinuation: true},
	}, map[string]codingFileSnapshot{canonicalCodingPath(src): snap})
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result2.md"},
		{Role: "assistant", Content: "结果已写入 result2.md"},
	}, nil)

	got := loadCodingAgentResume(userID, dir)
	var text string
	for _, entry := range got.History {
		if entry.HostContinuation {
			t.Fatalf("host nudge was stored: %#v", entry)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
	}
	if strings.Contains(text, "keep going") {
		t.Fatalf("host nudge leaked into history: %s", text)
	}
	if !strings.Contains(text, "frame at snake.cpp") || !strings.Contains(text, "result2.md") {
		t.Fatalf("history = %s", text)
	}
	if _, ok := got.Snapshots[canonicalCodingPath(src)]; !ok {
		t.Fatalf("read snapshot was dropped by the short turn: %#v", got.Snapshots)
	}
}

func TestCodingAgentResumeDropsExpiredFile(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	path := codingAgentResumeStatePath(userID)
	raw := []byte(`{"project_path":"` + filepath.ToSlash(dir) + `","updated_at_unix":1,"history":[{"role":"user","content":"old"},{"role":"assistant","content":"old answer"}]}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadCodingAgentResume(userID, dir); len(got.History) != 0 {
		t.Fatalf("expired history loaded: %#v", got.History)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired resume file still present: %v", err)
	}
}

func TestCodingAgentResumeRejectsAnotherProject(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, nil)
	if got := loadCodingAgentResume(userID, other); len(got.History) != 0 {
		t.Fatalf("other project loaded history: %#v", got.History)
	}
	outside := filepath.Join(other, "secret.cpp")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotCodingFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, map[string]codingFileSnapshot{canonicalCodingPath(outside): snap})
	if got := loadCodingAgentResume(userID, dir); len(got.Snapshots) != 0 {
		t.Fatalf("snapshot outside the project was stored: %#v", got.Snapshots)
	}
}

func TestCodingAgentResumeDropsUnmatchedToolCalls(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", Content: "先确认范围", ToolCalls: []llm.ToolCall{
			{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "code_navigation", Arguments: "{}"}},
			{ID: "call-ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user", Arguments: "{}"}},
		}},
		{Role: "user", Content: "keep going", HostContinuation: true},
		{Role: "tool", ToolCallID: "call-1", ToolName: "code_navigation", Content: "frame at snake.cpp"},
		{Role: "tool", ToolCallID: "call-stray", Content: "unpaired result"},
	}, nil)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "将结果写入result2.md"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{
				"id":   "call-open",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "ask_user",
					"arguments": "{}",
				},
			},
		}},
	}, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) == 0 || got.History[0].Role != "user" {
		t.Fatalf("history = %#v", got.History)
	}
	var text string
	ids := map[string]bool{}
	for i, entry := range got.History {
		if entry.HostContinuation {
			t.Fatalf("host nudge was stored: %#v", entry)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
		if id := strings.TrimSpace(entry.ToolCallID); id != "" {
			ids[id] = true
		}
		callIDs := codingAgentResumeToolCallIDs(entry.ToolCalls)
		if len(callIDs) == 0 {
			continue
		}
		answered := map[string]bool{}
		for _, id := range callIDs {
			ids[id] = true
			answered[id] = false
		}
		for j := i + 1; j < len(got.History) && got.History[j].Role == "tool"; j++ {
			if _, ok := answered[got.History[j].ToolCallID]; ok {
				answered[got.History[j].ToolCallID] = true
			}
		}
		for id, ok := range answered {
			if !ok {
				t.Fatalf("open tool call %s in %#v", id, got.History)
			}
		}
	}
	if !strings.Contains(text, "frame at snake.cpp") || !strings.Contains(text, "先确认范围") {
		t.Fatalf("history = %s", text)
	}
	if strings.Contains(text, "keep going") || strings.Contains(text, "unpaired result") || strings.Contains(text, "result2.md") {
		t.Fatalf("dropped rows leaked: %s", text)
	}
	if got.History[len(got.History)-1].Role == "user" {
		t.Fatalf("resume ends on a user row: %#v", got.History)
	}
	if !ids["call-1"] || ids["call-ask"] || ids["call-open"] || ids["call-stray"] {
		t.Fatalf("tool call ids = %#v", ids)
	}
}

func TestCodingAgentResumeKeepsVerifyFixLoop(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	main := []agent.ConversationEntry{
		{Role: "user", Content: "开发贪吃蛇"},
		{Role: "assistant", Content: "已实现 snake.cpp"},
	}
	// A fix round that stored no assistant or tool output. The next model
	// loop starts with its own user message, so this row must not remain.
	bare := []agent.ConversationEntry{
		{Role: "user", Content: "验证失败但没有回复"},
	}
	fix := []agent.ConversationEntry{
		{Role: "user", Content: "验证命令失败，请修复 tick"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "call-fix", Type: "function", Function: llm.ToolCallFunction{Name: "edit_file", Arguments: "{}"}},
		}},
		{Role: "tool", ToolCallID: "call-fix", ToolName: "edit_file", Content: "patched kTick in snake.cpp"},
		{Role: "assistant", Content: "已修复 tick"},
	}
	if got := codingAgentResumeAppendLoop(main, nil); len(got) != len(main) {
		t.Fatalf("empty extra changed the base: %#v", got)
	}
	delta := codingAgentResumeAppendLoop(main, bare)
	delta = codingAgentResumeAppendLoop(delta, fix)
	recordCodingAgentResume(userID, dir, delta, nil)

	got := loadCodingAgentResume(userID, dir)
	if len(got.History) == 0 || got.History[0].Role != "user" {
		t.Fatalf("history = %#v", got.History)
	}
	var text string
	for i, entry := range got.History {
		if i > 0 && entry.Role == "user" && got.History[i-1].Role == "user" {
			t.Fatalf("adjacent user rows: %#v", got.History)
		}
		if body, ok := entry.Content.(string); ok {
			text += body + "\n"
		}
	}
	if !strings.Contains(text, "已实现 snake.cpp") || !strings.Contains(text, "patched kTick in snake.cpp") || !strings.Contains(text, "已修复 tick") {
		t.Fatalf("fix loop was dropped: %s", text)
	}
	if strings.Contains(text, "验证失败但没有回复") {
		t.Fatalf("bare fix round was stored: %s", text)
	}
	if got.History[len(got.History)-1].Role == "user" {
		t.Fatalf("resume ends on a user row: %#v", got.History)
	}
	sawFix := false
	for i, entry := range got.History {
		if entry.ToolCallID != "call-fix" {
			continue
		}
		sawFix = true
		if i == 0 || got.History[i-1].Role != "assistant" {
			t.Fatalf("fix tool result was split: %#v", got.History)
		}
	}
	if !sawFix {
		t.Fatalf("fix tool result missing: %#v", got.History)
	}
}

func TestCodingAgentResumeReadyStartsWithTheUserTurn(t *testing.T) {
	entries := []agent.ConversationEntry{{Role: "user", Content: "将结果写入result.md"}}
	for i := 0; i < agent.MaxConversationTurns+5; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: fmt.Sprintf("step %d", i)})
	}
	ready := codingAgentResumeReady(entries)
	if len(ready) == 0 || ready[0].Role != "user" {
		t.Fatalf("ready = %#v", ready)
	}
	text, _ := ready[0].Content.(string)
	if !strings.Contains(text, "result.md") {
		t.Fatalf("anchor = %q", text)
	}
	last, _ := ready[len(ready)-1].Content.(string)
	if !strings.Contains(last, fmt.Sprintf("step %d", agent.MaxConversationTurns+4)) {
		t.Fatalf("last = %q", last)
	}
}

func TestClearStickyCodingDropsAgentResume(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "build the game"},
		{Role: "assistant", Content: "done"},
	}, nil)
	if _, err := os.Stat(codingAgentResumeStatePath(userID)); err != nil {
		t.Fatal(err)
	}
	(&IMMessageHandler{}).clearStickyCodingWorkbenchMemory(userID)
	if _, err := os.Stat(codingAgentResumeStatePath(userID)); !os.IsNotExist(err) {
		t.Fatalf("resume file still present: %v", err)
	}
}
