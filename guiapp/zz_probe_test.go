package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestZZProbeCU2(t *testing.T) {
	resetComputerUseSessionForTest(t)
	defs := []map[string]interface{}{
		toolDef("bash", "run shell", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("computer_observe", "observe screen", nil, nil),
		toolDef("computer_click", "click element", nil, nil),
		toolDef("gui_click", "raw coordinate click", nil, nil),
		toolDef("gui_type", "raw coordinate type", nil, nil),
	}
	uic := intent.New(intent.Config{Embedder: &cuGateStubEmbedder{hit: "打开word程序"}})
	waitUICReady(t, uic)
	h := &IMMessageHandler{unifiedClassifier: uic, toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	set := h.prepareAgentLoopTools("u1", "@computer 打开word程序，写一份简历", nil, agentLoopPhase{})
	for _, tl := range set.Tools {
		t.Logf("tool=%s", extractToolName(tl))
	}
}
