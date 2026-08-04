package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func checkpointReaderTool() map[string]interface{} {
	return tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})
}

func TestCheckpointConversationIsLosslessAndPreservesGroups(t *testing.T) {
	conversation := []interface{}{map[string]string{"role": "system", "content": "system policy"}}
	for i := 0; i < 8; i++ {
		id := "tc" + string(rune('a'+i))
		conversation = append(conversation,
			map[string]string{"role": "user", "content": "user constraint " + strings.Repeat("x", 500)},
			map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{map[string]interface{}{"id": id, "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}}}},
			map[string]interface{}{"role": "tool", "tool_call_id": id, "content": strings.Repeat("result ", 400)},
		)
	}
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 5000,
		SessionKey:   "owner-a",
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   6,
		Root:         t.TempDir(),
	})
	if !result.Applied || result.Handle == nil {
		t.Fatalf("checkpoint not applied: %+v", result)
	}
	if result.AfterTokens >= result.BeforeTokens {
		t.Fatalf("checkpoint did not save tokens: %+v", result)
	}
	if !strings.Contains(result.Conversation[1].(map[string]string)["content"], "[tool_result_handle]") {
		t.Fatal("checkpoint omitted lossless handle")
	}
	if MsgRole(result.Conversation[1]) != "user" {
		t.Fatalf("checkpoint role = %q, want user to avoid creating a second system prompt", MsgRole(result.Conversation[1]))
	}
	data, err := os.ReadFile(result.Handle.Path)
	if err != nil {
		t.Fatal(err)
	}
	var dropped []interface{}
	if err := json.Unmarshal(data, &dropped); err != nil || len(dropped) != result.DroppedCount {
		t.Fatalf("stored dropped messages invalid: err=%v count=%d want=%d", err, len(dropped), result.DroppedCount)
	}
	assertNoOrphanedCheckpointToolMessages(t, result.Conversation)
}

func TestCheckpointConversationFailsClosedWithoutReaderOrWritableStore(t *testing.T) {
	conversation := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	for i := 0; i < 20; i++ {
		conversation = append(conversation, map[string]string{"role": "user", "content": strings.Repeat("payload", 200)})
	}
	withoutReader := CheckpointConversation(conversation, ContextCheckpointOptions{ContextLimit: 4000, Tools: nil, KeepGroups: 2})
	if withoutReader.Applied || len(withoutReader.Conversation) != len(conversation) {
		t.Fatalf("checkpoint changed conversation without reader: %+v", withoutReader)
	}
	rootFile := t.TempDir() + "/occupied"
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	failedStore := CheckpointConversation(conversation, ContextCheckpointOptions{ContextLimit: 4000, Tools: []map[string]interface{}{checkpointReaderTool()}, KeepGroups: 2, Root: rootFile})
	if failedStore.Applied || len(failedStore.Conversation) != len(conversation) {
		t.Fatalf("checkpoint changed conversation after spill failure: %+v", failedStore)
	}
}

func TestCheckpointConversationKeepsOpaqueMultimodalContentInContext(t *testing.T) {
	conversation := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	conversation = append(conversation, map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("inspect ", 1000)},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAA"}},
		},
	})
	for i := 0; i < 20; i++ {
		conversation = append(conversation, map[string]string{"role": "user", "content": strings.Repeat("payload ", 200)})
	}
	result := CheckpointConversation(conversation, ContextCheckpointOptions{ContextLimit: 4000, Tools: []map[string]interface{}{checkpointReaderTool()}, KeepGroups: 2, Root: t.TempDir()})
	if result.Applied || result.Reason != "opaque_content" || len(result.Conversation) != len(conversation) {
		t.Fatalf("multimodal content should fail closed: %+v", result)
	}
}

func TestCheckpointConversationFailsClosedOnMismatchedToolIDs(t *testing.T) {
	conversation := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	conversation = append(conversation,
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
			map[string]interface{}{"id": "declared", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "different", "content": strings.Repeat("result ", 700)},
	)
	for i := 0; i < 15; i++ {
		conversation = append(conversation, map[string]string{"role": "user", "content": strings.Repeat("payload ", 200)})
	}
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 4000,
		SessionKey:   "owner-a",
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   2,
		Root:         t.TempDir(),
	})
	if result.Applied || result.Reason != "invalid_tool_group" || len(result.Conversation) != len(conversation) {
		t.Fatalf("mismatched tool IDs must fail closed: %+v", result)
	}
}

func TestCheckpointConversationPreservesNestedToolResultHandle(t *testing.T) {
	root := t.TempDir()
	const owner = "owner-nested"
	original := "exact nested tool output\n" + strings.Repeat("detail ", 700)
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "bash",
		SessionKey: owner,
		Content:    original,
		Limit:      700,
		Root:       root,
		ForceSpill: true,
	})
	if err != nil || projection.Handle == nil {
		t.Fatalf("project nested tool result: handle=%#v err=%v", projection.Handle, err)
	}

	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system policy"},
		map[string]string{"role": "user", "content": "retain every exact tool detail"},
	}
	for i := 0; i < 6; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("old requirement %d %s", i, strings.Repeat("constraint ", 180)),
		})
	}
	conversation = append(conversation,
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
			map[string]interface{}{"id": "nested-call", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "nested-call", "content": projection.Preview},
	)
	for i := 0; i < 8; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("later progress %d %s", i, strings.Repeat("payload ", 120)),
		})
	}

	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 4000,
		SessionKey:   owner,
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   8,
		Root:         root,
	})
	if !result.Applied || result.Handle == nil {
		t.Fatalf("checkpoint not applied: %+v", result)
	}
	checkpoint := result.Conversation[1].(map[string]string)["content"]
	if !strings.Contains(checkpoint, "older_lossless_tool_handles:") || !strings.Contains(checkpoint, projection.Handle.ID) {
		t.Fatalf("checkpoint preview omitted nested handle %q:\n%s", projection.Handle.ID, checkpoint)
	}

	stored, err := toolresult.Read(toolresult.ReadOptions{
		ID:         result.Handle.ID,
		SessionKey: owner,
		Root:       root,
		Limit:      toolresult.MaxReadLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Truncated || !strings.Contains(stored.Content, "[tool_result_handle]") || !strings.Contains(stored.Content, projection.Handle.ID) {
		t.Fatalf("stored checkpoint did not preserve complete nested footer: %+v", stored)
	}
	nested, err := toolresult.Read(toolresult.ReadOptions{
		ID:         projection.Handle.ID,
		SessionKey: owner,
		Root:       root,
		Limit:      toolresult.MaxReadLimit,
	})
	if err != nil || nested.Truncated || nested.Content != original {
		t.Fatalf("nested handle is not exactly readable: err=%v truncated=%v content_match=%v", err, nested.Truncated, nested.Content == original)
	}
}

func TestCheckpointConversationCanRepeatWithoutNestingCheckpointAsUserGoal(t *testing.T) {
	root := t.TempDir()
	const owner = "owner-repeat"
	conversation := []interface{}{map[string]string{"role": "system", "content": "system policy"}}
	for i := 0; i < 18; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("original goal %d %s", i, strings.Repeat("约束🙂 ", 120)),
		})
	}
	opts := ContextCheckpointOptions{
		ContextLimit: 4000,
		SessionKey:   owner,
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   3,
		Root:         root,
	}
	first := CheckpointConversation(conversation, opts)
	if !first.Applied || first.Handle == nil {
		t.Fatalf("first checkpoint not applied: %+v", first)
	}
	secondInput := append([]interface{}{}, first.Conversation...)
	for i := 0; i < 12; i++ {
		secondInput = append(secondInput, map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("new progress %d %s", i, strings.Repeat("payload ", 180)),
		})
	}
	second := CheckpointConversation(secondInput, opts)
	if !second.Applied || second.Handle == nil {
		t.Fatalf("second checkpoint not applied: %+v", second)
	}
	preview, ok := second.Conversation[1].(map[string]string)
	if !ok {
		t.Fatalf("second checkpoint type=%T", second.Conversation[1])
	}
	if strings.Contains(preview["content"], "- [context_checkpoint]") {
		t.Fatalf("prior checkpoint was nested as a user goal:\n%s", preview["content"])
	}
	if !strings.Contains(preview["content"], first.Handle.ID) {
		t.Fatalf("prior lossless checkpoint handle %q not retained:\n%s", first.Handle.ID, preview["content"])
	}
	for _, handle := range []*toolresult.Handle{first.Handle, second.Handle} {
		page, err := toolresult.Read(toolresult.ReadOptions{ID: handle.ID, SessionKey: owner, Root: root, Limit: toolresult.MaxReadLimit})
		if err != nil || page.Content == "" {
			t.Fatalf("checkpoint %q is not readable: err=%v", handle.ID, err)
		}
	}
	assertNoOrphanedCheckpointToolMessages(t, second.Conversation)
}

func TestCompactCheckpointTextPreservesUTF8AndLimit(t *testing.T) {
	content := strings.Repeat("甲🙂乙", 100)
	for _, limit := range []int{1, 2, 3, 4, 7, 31, 128} {
		got := compactCheckpointText(content, limit)
		if !utf8.ValidString(got) {
			t.Fatalf("limit=%d returned invalid UTF-8: %x", limit, []byte(got))
		}
		if len(got) > limit {
			t.Fatalf("limit=%d returned %d bytes", limit, len(got))
		}
	}
}

func TestCheckpointConversationDryRunDoesNotPersistOrFlush(t *testing.T) {
	root := t.TempDir()
	conversation := []interface{}{map[string]string{"role": "system", "content": "system policy"}}
	for i := 0; i < 20; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("goal %d %s", i, strings.Repeat("payload ", 160)),
		})
	}
	flushes := 0
	before := CurrentContextCheckpointStats()
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 4000,
		SessionKey:   "owner-shadow",
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   3,
		Root:         root,
		DryRun:       true,
		BeforeCompress: func() error {
			flushes++
			return nil
		},
	})
	if result.Applied || !result.WouldApply || result.Handle != nil || result.Reason != "dry_run" {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	if flushes != 0 {
		t.Fatalf("dry run flushed durable memory %d times", flushes)
	}
	if len(result.Conversation) != len(conversation) {
		t.Fatalf("dry run changed conversation: before=%d after=%d", len(conversation), len(result.Conversation))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry run persisted %d entries", len(entries))
	}
	after := CurrentContextCheckpointStats()
	if after.ShadowEvaluated != before.ShadowEvaluated+1 || after.ShadowEligible != before.ShadowEligible+1 {
		t.Fatalf("shadow telemetry did not advance: before=%+v after=%+v", before, after)
	}
}

func TestCheckpointConversationNoSavingsRemovesUnexposedHandle(t *testing.T) {
	root := t.TempDir()
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "system policy"},
		map[string]string{"role": "user", "content": strings.Repeat("a", 100)},
		map[string]string{"role": "assistant", "content": strings.Repeat("b", 100)},
		map[string]string{"role": "user", "content": strings.Repeat("c", 100)},
		map[string]string{"role": "assistant", "content": strings.Repeat("d", 100)},
	}
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 4000,
		ThresholdPct: 1,
		SessionKey:   "owner-no-savings",
		Tools:        []map[string]interface{}{checkpointReaderTool()},
		KeepGroups:   1,
		PreviewLimit: 4096,
		Root:         root,
	})
	if result.Applied || result.Reason != "no_savings" || result.Handle != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	var files int
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("no-savings fallback left %d orphan handles", files)
	}
}

func assertNoOrphanedCheckpointToolMessages(t *testing.T, conversation []interface{}) {
	t.Helper()
	for i := 0; i < len(conversation); i++ {
		msg := conversation[i]
		if MsgRole(msg) == "tool" {
			t.Fatalf("orphaned tool at %d: %#v", i, msg)
		}
		if MsgRole(msg) != "assistant" || !MsgHasToolCalls(msg) {
			continue
		}
		declared := checkpointToolCallIDs(t, msg)
		actual := make(map[string]int, len(declared))
		j := i + 1
		for ; j < len(conversation) && MsgRole(conversation[j]) == "tool"; j++ {
			toolMessage, ok := conversation[j].(map[string]interface{})
			if !ok {
				t.Fatalf("tool result at %d has unexpected type %T", j, conversation[j])
			}
			id, _ := toolMessage["tool_call_id"].(string)
			if id == "" {
				t.Fatalf("tool result at %d has no tool_call_id: %#v", j, toolMessage)
			}
			actual[id]++
		}
		if len(declared) != len(actual) {
			t.Fatalf("assistant tool IDs and result IDs differ at %d: declared=%v actual=%v", i, declared, actual)
		}
		for id, count := range declared {
			if count != 1 || actual[id] != 1 {
				t.Fatalf("tool ID %q pairing mismatch at %d: declared=%d actual=%d", id, i, count, actual[id])
			}
		}
		i = j - 1
	}
}

func checkpointToolCallIDs(t *testing.T, msg interface{}) map[string]int {
	t.Helper()
	mm, ok := msg.(map[string]interface{})
	if !ok {
		t.Fatalf("assistant tool-call message has unexpected type %T", msg)
	}
	calls, ok := mm["tool_calls"].([]interface{})
	if !ok || len(calls) == 0 {
		t.Fatalf("assistant has invalid tool_calls: %#v", mm["tool_calls"])
	}
	ids := make(map[string]int, len(calls))
	for _, call := range calls {
		cm, ok := call.(map[string]interface{})
		if !ok {
			t.Fatalf("tool call has unexpected type %T", call)
		}
		id, _ := cm["id"].(string)
		if id == "" {
			t.Fatalf("tool call has no id: %#v", cm)
		}
		ids[id]++
	}
	return ids
}
