package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestAutoCompactionHistoryIsRecoverableAfterRealTrim(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	t.Setenv("MACLAW_HANDOFF_MEMO", "")

	history := make([]agent.ConversationEntry, 0, 24)
	for i := 0; i < 12; i++ {
		history = append(history,
			agent.ConversationEntry{Role: "user", Content: "requirement " + string(rune('A'+i))},
			agent.ConversationEntry{Role: "assistant", Content: "progress " + string(rune('A'+i))},
		)
	}
	history[11].Content = "AUTO_TRIM_UNIQUE_IMPLEMENTATION_DETAIL"
	trimmed := trimHistoryWithSummary(history, nil, nil, 6, 0)
	if len(trimmed) >= len(history) {
		t.Fatalf("fixture did not trim: %d -> %d", len(history), len(trimmed))
	}
	h := &IMMessageHandler{}
	compacted, ok := h.preserveAutoCompactionHistory("desktop-user", history, trimmed)
	if !ok {
		t.Fatal("real trim was not recognized as a compaction")
	}
	checkpoint := ""
	for _, entry := range compacted {
		text, _ := entry.Content.(string)
		if strings.Contains(text, toolresult.HandleFooterMarker) {
			checkpoint = text
			break
		}
	}
	id := spilledHandleID(checkpoint)
	if id == "" {
		t.Fatalf("automatic compaction has no recovery handle: %#v", compacted)
	}
	result, err := toolresult.Read(toolresult.ReadOptions{ID: id, SessionKey: "desktop-user", Query: "AUTO_TRIM_UNIQUE_IMPLEMENTATION_DETAIL"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "AUTO_TRIM_UNIQUE_IMPLEMENTATION_DETAIL") {
		t.Fatalf("automatically dropped entry is not recoverable: %s", result.Content)
	}
}

func TestAutoCompactionDetectsSameLengthReplacement(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	call := llm.ToolCall{ID: "same-length", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"D:/work/SAME_LENGTH_UNIQUE.go"}`}}
	original := []agent.ConversationEntry{
		{Role: "user", Content: "implement"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}
	trimmed := []agent.ConversationEntry{
		original[0],
		{Role: "system", Content: compactionPlaceholder, CompactionID: "same-length-compaction"},
	}
	h := &IMMessageHandler{}
	got, ok := h.preserveAutoCompactionHistory("desktop-user", original, trimmed)
	if !ok || len(got) != len(original) {
		t.Fatalf("same-length compaction was missed: ok=%v entries=%#v", ok, got)
	}
	checkpoint, _ := got[1].Content.(string)
	id := spilledHandleID(checkpoint)
	result, err := toolresult.Read(toolresult.ReadOptions{ID: id, SessionKey: "desktop-user", Query: "SAME_LENGTH_UNIQUE"})
	if err != nil || !strings.Contains(result.Content, "SAME_LENGTH_UNIQUE") {
		t.Fatalf("same-length replacement lost tool arguments: result=%+v err=%v", result, err)
	}
}

func TestAutoCompactionDetectsEarlyHandoffTrim(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")

	history := []agent.ConversationEntry{
		{Role: "user", Content: "older goal"},
		{Role: "assistant", Content: "EARLY_HANDOFF_UNIQUE " + strings.Repeat("detail ", 800)},
		{Role: "user", Content: "ship the report"},
	}
	for i := 0; i < 6; i++ {
		history = append(history, agent.ConversationEntry{Role: "user", Content: "recent " + strings.Repeat("r", 30)})
	}
	const estimatedTokens = 850
	const tokenLimit = 1000
	if len(history) > 40 || estimatedTokens > tokenLimit {
		t.Fatal("fixture must be below the outer compaction trigger")
	}
	trimmed := trimHistoryWithSummaryPrecomputed(history, nil, nil, 40, tokenLimit, estimatedTokens, nil)
	if !historyMayHaveCompacted(history, trimmed) {
		t.Fatal("early handoff trim was invisible to the persistence boundary")
	}
	h := &IMMessageHandler{}
	got, ok := h.preserveAutoCompactionHistory("desktop-user", history, trimmed)
	if !ok {
		t.Fatal("early handoff trim did not preserve its dropped history")
	}
	checkpoint := ""
	for _, entry := range got {
		text, _ := entry.Content.(string)
		if strings.Contains(text, toolresult.HandleFooterMarker) {
			checkpoint = text
			break
		}
	}
	result, err := toolresult.Read(toolresult.ReadOptions{ID: spilledHandleID(checkpoint), SessionKey: "desktop-user", Query: "EARLY_HANDOFF_UNIQUE"})
	if err != nil || !strings.Contains(result.Content, "EARLY_HANDOFF_UNIQUE") {
		t.Fatalf("early handoff original is not recoverable: result=%+v err=%v", result, err)
	}
}

func TestAutoCompactionKeepsOriginalWhenSpillFails(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	badBase := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(badBase, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	maclawpath.SetBaseDir(badBase)
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	original := []agent.ConversationEntry{{Role: "user", Content: "keep"}, {Role: "assistant", Content: "DO_NOT_DROP_ON_SPILL_FAILURE"}}
	trimmed := []agent.ConversationEntry{original[0]}
	h := &IMMessageHandler{}
	got, ok := h.preserveAutoCompactionHistory("desktop-user", original, trimmed)
	if ok || len(got) != len(original) || got[1].Content != original[1].Content {
		t.Fatalf("spill failure discarded the original history: ok=%v entries=%#v", ok, got)
	}
}

func TestCompactionSummaryReplacementKeepsRecoveryFooter(t *testing.T) {
	prior := "old summary\n\n" + toolresult.HandleFooterMarker + "\nid: checkpoint-1\ntool: context_checkpoint\n"
	got := preserveContextCheckpointFooter("new summary", prior)
	if !strings.Contains(got, "new summary") || !strings.Contains(got, "id: checkpoint-1") {
		t.Fatalf("summary replacement dropped the recovery footer: %s", got)
	}
	if again := preserveContextCheckpointFooter(got, prior); again != got {
		t.Fatalf("footer preservation is not idempotent: %s", again)
	}

	memoPrior := agent.HandoffMemo{Task: "keep the constraint", Progress: "median 17.4"}.RenderBlock() + "\n\n" + toolresult.HandleFooterMarker + "\nid: memo-checkpoint\n"
	memoSummary := agent.HandoffMemo{Task: "keep the constraint", Progress: "new measurement"}.RenderBlock()
	block, ok := handoffAsyncReplacement(memoPrior, memoSummary)
	if !ok {
		t.Fatal("memo with a recovery footer became unparsable")
	}
	block = preserveContextCheckpointFooter(block, memoPrior)
	parsed, ok := agent.ParseHandoffMemo(block)
	if !ok || !strings.Contains(parsed.Progress, "median 17.4") || !strings.Contains(block, "id: memo-checkpoint") {
		t.Fatalf("memo summary lost progress or recovery handle: %+v %s", parsed, block)
	}
}
