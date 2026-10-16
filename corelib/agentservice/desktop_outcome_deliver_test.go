package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestDesktopOutcomeEvidenceKeepsEarlierDelivery(t *testing.T) {
	h := []agent.ConversationEntry{{Role: "tool", ToolName: "desktop", Content: "Delivered a.docx (100 bytes) on this chat reply."}}
	for i := 0; i < 6; i++ {
		h = append(h, agent.ConversationEntry{Role: "tool", ToolName: "desktop", Content: "window list"})
	}
	_, _, _, _, text := desktopOutcomeEvidence(h)
	if !strings.Contains(text, "Delivered a.docx") {
		t.Fatalf("delivery dropped: %s", text)
	}
}
