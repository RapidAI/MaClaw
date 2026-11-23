package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestCompressionHookUsesLosslessCheckpointWhenConfigured(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{ContextLength: 4000}
	hook := NewCompressionHook(func() corelib.MaclawLLMConfig { return cfg }, nil)
	hook.Checkpoint = &ContextCheckpointOptions{
		SessionKey: "owner",
		Tools:      []map[string]interface{}{checkpointReaderTool()},
		Root:       t.TempDir(),
	}
	conversation := []interface{}{map[string]string{"role": "system", "content": "you are a helper"}}
	for i := 0; i < 30; i++ {
		conversation = append(conversation,
			map[string]string{"role": "user", "content": "preserve decision " + hugeContent("x", 3000)},
			map[string]string{"role": "assistant", "content": hugeContent("y", 3000)},
		)
	}
	out := hook.TransformConversation(conversation)
	if out == nil {
		t.Fatal("over-budget conversation was not compacted")
	}
	var checkpoint string
	for _, msg := range out {
		_, content := ExtractRoleContent(msg)
		if strings.Contains(content, toolresult.HandleFooterMarker) {
			checkpoint = content
			break
		}
	}
	if checkpoint == "" {
		t.Fatalf("checkpoint handle missing: %#v", out)
	}
	id := ""
	for _, line := range strings.Split(checkpoint, "\n") {
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			break
		}
	}
	res, err := toolresult.Read(toolresult.ReadOptions{ID: id, SessionKey: "owner", Root: hook.Checkpoint.Root, Query: "preserve decision"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "preserve decision") {
		t.Fatalf("dropped decision is not recoverable: %s", res.Content)
	}
}
