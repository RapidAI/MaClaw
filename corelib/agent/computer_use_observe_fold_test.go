package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func sampleComputerObserveDump(label string) string {
	return fmt.Sprintf("mode=text_primary screen=100x100 scale=1.00 screen_index=0 crop=%q origin=0,0\nwindows:\n  - %sApp\nelements (3):\n  e0 [button] %q conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\nocr_excerpt: %s UNIQUE_PAYLOAD extra text for folding\nhint: Use computer_click with ref=eN\n", label, label, label, label)
}

func sampleComputerObserveConversation(dumps ...string) []interface{} {
	msgs := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	for i, dump := range dumps {
		id := fmt.Sprintf("obs-%d", i)
		msgs = append(msgs,
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   id,
						"type": "function",
						"function": map[string]interface{}{
							"name":      "computer_observe",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": id,
				"content":      dump,
			},
		)
	}
	return msgs
}

func TestFoldComputerUseObservesKeepsLatestFullDump(t *testing.T) {
	first := sampleComputerObserveDump("FIRST")
	second := sampleComputerObserveDump("SECOND")
	got := FoldComputerUseObserves(sampleComputerObserveConversation(first, second))
	_, c0 := ExtractRoleContent(got[2])
	_, c1 := ExtractRoleContent(got[4])
	if !strings.HasPrefix(strings.TrimSpace(c0), ComputerObserveFingerprintPrefix) {
		t.Fatalf("older observe must be fingerprinted: %q", c0)
	}
	if strings.Contains(c0, "e0 [button]") || strings.Contains(c0, "hint: Use computer_click") {
		t.Fatal("older observe must not keep the full dump")
	}
	if !strings.Contains(c0, "FIRSTApp") {
		t.Fatalf("fingerprint should keep window: %q", c0)
	}
	if c1 != second {
		t.Fatalf("latest observe must stay full")
	}
}

func TestFoldComputerUseObservesIdempotent(t *testing.T) {
	msgs := sampleComputerObserveConversation(sampleComputerObserveDump("A"), sampleComputerObserveDump("B"))
	once := FoldComputerUseObserves(msgs)
	twice := FoldComputerUseObserves(once)
	_, c1 := ExtractRoleContent(once[2])
	_, c2 := ExtractRoleContent(twice[2])
	if c1 != c2 {
		t.Fatalf("second fold changed fingerprint:\n%s\n%s", c1, c2)
	}
}

func TestTrimConversationFoldsObservesUnderBudget(t *testing.T) {
	msgs := sampleComputerObserveConversation(sampleComputerObserveDump("OLD"), sampleComputerObserveDump("NEW"))
	got := TrimConversation(msgs, 100000, 0, nil)
	_, old := ExtractRoleContent(got[2])
	_, latest := ExtractRoleContent(got[4])
	if !isComputerObserveFingerprint(old) {
		t.Fatalf("TrimConversation must fold even under budget: %q", old)
	}
	if !strings.Contains(latest, "NEW UNIQUE_PAYLOAD") {
		t.Fatal("latest observe must remain full after trim")
	}
}

func TestFoldComputerUseObserveEntriesReloadStillFolded(t *testing.T) {
	first := sampleComputerObserveDump("DISK1")
	second := sampleComputerObserveDump("DISK2")
	saved := []ConversationEntry{
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id": "c1", "type": "function", "function": map[string]interface{}{"name": "computer_observe", "arguments": "{}"},
		}}},
		{Role: "tool", ToolCallID: "c1", ToolName: "computer_observe", Content: first},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id": "c2", "type": "function", "function": map[string]interface{}{"name": "computer_observe", "arguments": "{}"},
		}}},
		{Role: "tool", ToolCallID: "c2", ToolName: "computer_observe", Content: second},
	}
	// Persist unfolded, then reload through TrimHistory (ConversationMemory path).
	reloaded := TrimHistory(append([]ConversationEntry(nil), saved...))
	if !isComputerObserveFingerprint(entryContentString(reloaded[1])) {
		t.Fatalf("TrimHistory must fold older observe: %q", reloaded[1].Content)
	}
	if entryContentString(reloaded[3]) != second {
		t.Fatal("latest observe must stay full on reload trim")
	}
	// Rebuild loop messages the way RunLoop does, then fold again.
	msgs := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	for _, e := range reloaded {
		msgs = append(msgs, e.ToMessage())
	}
	folded := FoldComputerUseObserves(msgs)
	_, older := ExtractRoleContent(folded[2])
	_, latest := ExtractRoleContent(folded[4])
	if !isComputerObserveFingerprint(older) {
		t.Fatalf("reload conversation must stay folded: %q", older)
	}
	if !strings.Contains(latest, "DISK2 UNIQUE_PAYLOAD") {
		t.Fatal("latest observe must remain full after reload fold")
	}
}

func TestTrimHistoryFoldsObserves(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "tool", ToolName: "computer_observe", Content: sampleComputerObserveDump("H1")},
		{Role: "tool", ToolName: "computer_observe", Content: sampleComputerObserveDump("H2")},
	}
	got := TrimHistory(entries)
	if !isComputerObserveFingerprint(entryContentString(got[0])) {
		t.Fatalf("first=%q", got[0].Content)
	}
	if !strings.Contains(entryContentString(got[1]), "H2 UNIQUE_PAYLOAD") {
		t.Fatalf("second=%q", got[1].Content)
	}
}

func TestFoldComputerUseObservesPrefersWindowsOverCrop(t *testing.T) {
	dump := sampleComputerObserveDump("FIRST")
	got := computerObserveFingerprintWindow(dump)
	if got != "FIRSTApp" {
		t.Fatalf("window=%q, want FIRSTApp not crop title", got)
	}
}

func TestFoldComputerUseObservesKeepsLastVisionImage(t *testing.T) {
	msgs := sampleComputerObserveConversation(sampleComputerObserveDump("A"), sampleComputerObserveDump("B"))
	msgs = append(msgs,
		buildComputerUseVisionMessage("openai", []ToolModelImage{{Base64: "oldimg", MIME: "image/png"}}),
		buildComputerUseVisionMessage("openai", []ToolModelImage{{Base64: "newimg", MIME: "image/png"}}),
	)
	got := FoldComputerUseObserves(msgs)
	vision, omitted := 0, 0
	for _, raw := range got {
		mm, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if blocks, ok := mm["content"].([]interface{}); ok && computerUseVisionMessage(blocks) {
			vision++
			blob, _ := json.Marshal(blocks)
			if strings.Contains(string(blob), "oldimg") {
				t.Fatal("stale screenshot must not remain")
			}
			if !strings.Contains(string(blob), "newimg") {
				t.Fatal("latest screenshot must remain")
			}
		}
		if s, ok := mm["content"].(string); ok && strings.Contains(s, "previous screenshot omitted") {
			omitted++
		}
	}
	if vision != 1 {
		t.Fatalf("keep exactly one live screenshot, vision=%d", vision)
	}
	if omitted < 1 {
		t.Fatal("older screenshot must be omitted")
	}
}

func TestLooksLikeComputerObserveRequiresLabeledLine(t *testing.T) {
	embedded := "bash: grep found 'ocr_excerpt:' in docs/computer-use.md\nalso perception=llm_vision in a comment"
	if looksLikeComputerObserveResult(embedded) {
		t.Fatal("embedded ocr_excerpt / perception mention must not look like observe")
	}
	dump := sampleComputerObserveDump("OK")
	if !looksLikeComputerObserveResult(dump) {
		t.Fatal("real observe dump must still match")
	}
	vision := "mode=vision screen=100x100 scale=1.00 screen_index=0\nperception=llm_vision (OmniParser/OCR skipped; a11y marks drawn on screenshot)\n"
	if !looksLikeComputerObserveResult(vision) {
		t.Fatal("vision observe dump must still match")
	}
}
