package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPersistentConversationMemoryRedactsSecretsBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{Role: "assistant", Content: "calling ssh", ToolCalls: []interface{}{map[string]interface{}{
			"id":   "call-1",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "ssh",
				"arguments": `{"host":"api2.maclaw.top","password":"secret-password","api_key_secret":"json-secret"}`,
			},
		}}},
		{Role: "tool", Content: "JWT_SECRET=jwt-secret\nAuthorization: Bearer bearer-secret", ToolCallID: "call-1"},
	})
	cm.Stop()

	data, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"secret-password", "json-secret", "jwt-secret", "bearer-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked %q: %s", secret, serialized)
		}
	}
}

func TestBackgroundPersistentConversationMemoryLoadsBeforeRead(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	raw := []byte(`{"sessions":{"desktop-user":{"entries":[{"role":"user","content":"hello"}],"last_access":"2026-01-01T00:00:00Z"}}}`)
	if err := os.WriteFile(storePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cm := NewBackgroundPersistentConversationMemory(storePath)
	defer cm.Stop()
	entries := cm.Load("desktop-user")
	if len(entries) != 1 {
		t.Fatalf("background load = %#v", entries)
	}
	text, _ := entries[0].Content.(string)
	if text != "hello" {
		t.Fatalf("background load content = %#v", entries[0].Content)
	}
}

func TestBackgroundLoadHookFinishesBeforeRead(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	raw := []byte(`{"sessions":{"desktop-user":{"entries":[{"role":"user","content":"hello"}],"last_access":"2026-01-01T00:00:00Z"}}}`)
	if err := os.WriteFile(storePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	loaded := make(chan []ConversationEntry, 1)
	cm := NewBackgroundPersistentConversationMemoryAfterLoad(storePath, func(*ConversationMemory) {
		close(entered)
		<-release
	})
	defer cm.Stop()
	go func() {
		loaded <- cm.Load("desktop-user")
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("load hook did not run")
	}
	select {
	case entries := <-loaded:
		t.Fatalf("read returned before the load hook finished: %#v", entries)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case entries := <-loaded:
		if len(entries) != 1 {
			t.Fatalf("entries after hook = %#v", entries)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read stayed blocked after the load hook")
	}
}

func TestBackgroundPersistentConversationMemoryServesRecoverySlot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	raw := []byte(`{"sessions":{"desktop-user":{"entries":[{"role":"user","content":"hello"}],"last_access":"2026-01-01T00:00:00Z","active_slot_id":"slot-1","unfinished_slot":{"slot_id":"slot-1","user_id":"desktop-user","status":"interrupted","source":"inflight_recovery","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}}`)
	if err := os.WriteFile(storePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cm := NewBackgroundPersistentConversationMemory(storePath)
	defer cm.Stop()

	done := make(chan error, 1)
	go func() {
		cm.Save("other-user", []ConversationEntry{{Role: "user", Content: "next"}})
		done <- nil
	}()

	slots := cm.UnfinishedSlots()
	if len(slots) != 1 || slots[0] == nil || slots[0].SlotID != "slot-1" {
		t.Fatalf("background unfinished slots = %#v", slots)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("checkpoint during background load: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("save blocked behind the background load")
	}
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	got, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "hello") || !strings.Contains(string(got), "next") {
		t.Fatalf("transcript lost a session: %s", got)
	}
}

func TestPersistentConversationMemoryKeepsCleanToolTranscript(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	raw := []byte(`{"sessions":{"desktop-user":{"entries":[{"role":"assistant","content":"calling","tool_calls":[{"id":"call-1","type":"function","function":{"name":"bash","arguments":"{}"}}]},{"role":"tool","content":"ok","tool_call_id":"call-1","tool_name":"bash","tool_outcome":"ok"}],"last_access":"2026-01-01T00:00:00Z"}}}`)
	if err := os.WriteFile(storePath, raw, 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	cm := NewPersistentConversationMemory(storePath)
	cm.Stop()

	got, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	// The entries must come back untouched. The file may gain the one-time
	// schema_version stamp that lets the next load skip the sanitize pass, but
	// no transcript content may change.
	assertTranscriptEntriesUnchanged(t, raw, got)
}

func TestPersistentConversationMemoryKeepsSessionIDText(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	raw := []byte(`{"sessions":{"desktop-user":{"entries":[{"role":"user","content":"the payload key is \"session_id\""}],"last_access":"2026-01-01T00:00:00Z"}}}`)
	if err := os.WriteFile(storePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cm := NewPersistentConversationMemory(storePath)
	cm.Stop()
	got, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatal(err)
	}
	assertTranscriptEntriesUnchanged(t, raw, got)
}

// assertTranscriptEntriesUnchanged compares persisted sessions after parsing, so
// the schema_version stamp is allowed while any change to an entry still fails.
func assertTranscriptEntriesUnchanged(t *testing.T, before, after []byte) {
	t.Helper()
	var want, got memorySnapshot
	if err := json.Unmarshal(before, &want); err != nil {
		t.Fatalf("parse expected transcript: %v", err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatalf("parse rewritten transcript: %v\nraw: %s", err, after)
	}
	if len(got.Sessions) != len(want.Sessions) {
		t.Fatalf("sessions after rewrite = %d, want %d", len(got.Sessions), len(want.Sessions))
	}
	for userID, wantSession := range want.Sessions {
		gotSession, ok := got.Sessions[userID]
		if !ok {
			t.Fatalf("session %q disappeared after rewrite", userID)
		}
		if len(gotSession.Entries) != len(wantSession.Entries) {
			t.Fatalf("session %q entries = %d, want %d", userID, len(gotSession.Entries), len(wantSession.Entries))
		}
		for i := range wantSession.Entries {
			if wantSession.Entries[i].ID == "" {
				gotSession.Entries[i].ID = ""
				gotSession.Entries[i].ParentID = wantSession.Entries[i].ParentID
				gotSession.Entries[i].Timestamp = 0
			}
			if !reflect.DeepEqual(gotSession.Entries[i], wantSession.Entries[i]) {
				t.Fatalf("entry %d of session %q was rewritten:\n got %#v\nwant %#v",
					i, userID, gotSession.Entries[i], wantSession.Entries[i])
			}
		}
		if !gotSession.LastAccess.Equal(wantSession.LastAccess) {
			t.Fatalf("session %q last_access = %v, want %v", userID, gotSession.LastAccess, wantSession.LastAccess)
		}
	}
}

func TestPersistentConversationMemoryRedactsLegacySecretsOnLoad(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	legacy := `{"sessions":{"desktop-user":{"entries":[{"role":"assistant","content":"calling","tool_calls":[{"id":"call-1","type":"function","function":{"name":"ssh","arguments":"{\"password\":\"legacy-password\"}"}}]},{"role":"tool","content":"Cookie: legacy-cookie","tool_call_id":"call-1"}],"last_access":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(storePath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy memory: %v", err)
	}

	cm := NewPersistentConversationMemory(storePath)
	cm.Stop()

	data, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatalf("read rewritten memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"legacy-cookie", "legacy-password"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("rewritten conversation leaked %q: %s", secret, serialized)
		}
	}
	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	entries := reloaded.Load("desktop-user")
	if len(entries) != 2 || entries[0].Role != "assistant" || entries[1].Role != "tool" || entries[1].ToolCallID != "call-1" {
		t.Fatalf("redaction should preserve valid tool-call group, got %#v", entries)
	}
}

func TestPersistentConversationMemoryStripsRolePrefixLeakBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{
			Role:    "assistant",
			Content: "safe answer\nBrowser: SECRET_BROWSER_CONTENT",
			// Mid-text "Tool:" lines are legitimate agent planning and must
			// survive persistence; only a leading prefix token is stripped.
			ReasoningContent: "thinking\nTool: bash df -h\n然后根据磁盘占用判断",
		},
		{
			Role:             "assistant",
			Content:          "ok",
			ReasoningContent: "Tool: 先检查状态\n再决定下一步",
		},
	})
	cm.Stop()

	data, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	// Content cleaning keeps Case 2: a mid-text role prefix truncates the
	// hallucinated duplicate that follows it.
	for _, secret := range []string{"Browser:", "SECRET_BROWSER_CONTENT"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked role-prefixed content %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "safe answer") {
		t.Fatalf("expected visible content prefix to be preserved: %s", serialized)
	}
	// Reasoning must not be truncated at mid-text "Tool:" planning lines.
	if !strings.Contains(serialized, "Tool: bash df -h") || !strings.Contains(serialized, "然后根据磁盘占用判断") {
		t.Fatalf("reasoning mid-text Tool: lines must be preserved: %s", serialized)
	}
	// A leading role-prefix token in reasoning is still stripped.
	if strings.Contains(serialized, "Tool: 先检查状态") {
		t.Fatalf("leading reasoning role prefix must be stripped: %s", serialized)
	}
	if !strings.Contains(serialized, "先检查状态") {
		t.Fatalf("reasoning body after a leading prefix must be preserved: %s", serialized)
	}
}

func TestPersistentConversationMemoryStripsPlainToolCallLeakBeforeDisk(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.Save("desktop-user", []ConversationEntry{
		{
			Role:    "assistant",
			Content: "先执行远程检查\nTOOL_CALL\n{\"function\":\"ssh_execute_command\",\"args\":{\"password\":\"<redacted>\",\"command\":\"df -h\"}}",
		},
	})
	cm.Stop()

	data, err := readConversationStoreForTest(t, storePath)
	if err != nil {
		t.Fatalf("read persisted memory: %v", err)
	}
	serialized := string(data)
	for _, secret := range []string{"TOOL_CALL", "<redacted>", "ssh_execute_command"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("persisted conversation leaked plain tool call %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "先执行远程检查") {
		t.Fatalf("expected visible prefix to be preserved: %s", serialized)
	}
}

func TestSanitizeConversationEntryDropsLeakedThinkingProtocol(t *testing.T) {
	entry, changed := sanitizeConversationEntryForPersistence(ConversationEntry{
		Role:             "assistant",
		Content:          "让我调用web_search来获取北京天气。\n</tool_value>\n</thinking>",
		ReasoningContent: "让我调用web_search来获取北京天气。\n</tool_value>\n</thinking>",
	})
	if !changed {
		t.Fatal("expected protocol closers to change the stored turn")
	}
	content, _ := entry.Content.(string)
	for _, got := range []string{content, entry.ReasoningContent} {
		if strings.Contains(got, "tool_value") || strings.Contains(got, "</thinking>") {
			t.Fatalf("persisted turn still has protocol tags: %q", got)
		}
		if got != "让我调用web_search来获取北京天气。" {
			t.Fatalf("persisted text = %q", got)
		}
	}
}

func TestStripPlainToolCallPersistenceLeakKeepsBareMentions(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare word in reasoning kept", "I will issue a tool_call to bash and inspect output", "I will issue a tool_call to bash and inspect output"},
		{"bare word with period kept", "the tool_call failed. retrying now", "the tool_call failed. retrying now"},
		{"plural bare word kept", "check the tool_calls field later", "check the tool_calls field later"},
		{"xml tag truncated", "visible\n<tool_call>{\"name\":\"ssh\"}</tool_call>", "visible"},
		{"longcat xml truncated", "visible\n<longcat_tool_call>ssh_read_file</longcat_tool_call>", "visible"},
		{"bare longcat word kept", "LongCat writes longcat_tool_call in its prompt", "LongCat writes longcat_tool_call in its prompt"},
		{"bare word followed by json truncated", "先执行远程检查\nTOOL_CALL\n{\"function\":\"ssh\"}", "先执行远程检查"},
		{"json key truncated", "result\n{\"tool_call\": {\"name\": \"ssh\"}}", "result\n{"},
		{"sentinel truncated", "answer\n<|tool_call_begin|>x", "answer"},
		{"no occurrence unchanged", "plain text", "plain text"},
		{"later markup after bare mention", "the tool_call word then <tool_call>x", "the tool_call word then"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripPlainToolCallPersistenceLeak(tc.input); got != tc.want {
				t.Fatalf("stripPlainToolCallPersistenceLeak(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
