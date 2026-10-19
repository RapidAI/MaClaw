package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

func TestDesktopBotComputerCases(t *testing.T) {
	desktopbot.ResetComputerScreens()
	t.Cleanup(desktopbot.ResetComputerScreens)

	h := &IMMessageHandler{}
	ctx := &LoopContext{ComputerUseRoutingText: "@computer 打开记事本"}
	active, _ := recordComputerUseGate(h, ctx, ctx.ComputerUseRoutingText)
	if active || ctx.ComputerUseActive {
		t.Fatalf("main loop became the operator active=%v flag=%v", active, ctx.ComputerUseActive)
	}
	if !ctx.ComputerHelperDispatched || h.chatTasks != nil {
		t.Fatalf("nil app dispatched=%v registry=%v", ctx.ComputerHelperDispatched, h.chatTasks != nil)
	}
	card := desktopbot.HelperCard{
		Goal:            ctx.ComputerHelperGoal,
		Constraints:     ctx.ComputerHelperLimits,
		SuccessCriteria: ctx.ComputerHelperSuccess,
	}.Text()
	if !strings.Contains(card, "打开记事本") || !strings.Contains(card, "constraints:") || !strings.Contains(card, "success:") {
		t.Fatalf("card = %s", card)
	}
	for _, word := range []string{"click", "type", "computer"} {
		if strings.Contains(strings.ToLower(card), word) {
			t.Fatalf("card names %s: %s", word, card)
		}
	}

	if !strings.Contains(desktopBotUnderstandSystem, "goal, constraints, and success criteria") || !strings.Contains(desktopBotUnderstandSystem, "The foreground agent sends the chat reply") {
		t.Fatal("instruction is not a helper card")
	}
	if strings.Contains(desktopBotUnderstandSystem, "Keep a desktop action name") || strings.Contains(desktopBotUnderstandSystem, "State the outcome, the steps that matter, and what the chat reply must contain") || strings.Contains(desktopBotUnderstandSystem, "including what that run's reply must contain") {
		t.Fatal("instruction still tells the worker how to write the chat reply")
	}
	if !strings.Contains(desktopBotUnderstandSystem, desktopbot.WorthSayingPrompt) {
		t.Fatal("worth-saying left the foreground prompt")
	}

	tools := []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("computer_observe", "observe desktop", nil, nil),
		toolDef("gui_click", "legacy desktop click", nil, nil),
		toolDef("screenshot", "shot", map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string"},
			"x":          map[string]interface{}{"type": "number"},
			"y":          map[string]interface{}{"type": "number"},
		}, nil),
	}
	mainNames := toolNameSetForWorkflowFilterTest(filterComputerUseToolsForLocalFileWork(nil, "@computer 打开记事本", tools))
	if mainNames["computer_observe"] || mainNames["gui_click"] || !mainNames["read_file"] || !mainNames["screenshot"] {
		t.Fatalf("main agent tools = %#v", mainNames)
	}
	shot := toolDefinitionByNameForWorkflowFilterTest(filterComputerUseToolsForLocalFileWork(nil, "@computer 打开记事本", tools), "screenshot")
	props := screenshotPropertiesForTest(t, shot)
	if _, ok := props["x"]; ok {
		t.Fatal("read-only screenshot still has x")
	}
	if _, ok := props["session_id"]; !ok {
		t.Fatal("read-only screenshot lost session_id")
	}

	ring := NewLoopContext("gui-executor", 1, nil)
	ring.HorizonRole = longhorizon.RoleGUIExecutor
	ring.ComputerUseActive = true
	ring.ComputerUseGateSettled = true
	ringNames := toolNameSetForWorkflowFilterTest(filterComputerUseToolsForLocalFileWork(ring, "打开记事本", tools))
	if !ringNames["computer_observe"] {
		t.Fatalf("gui_executor dropped computer_observe: %#v", ringNames)
	}
}

func screenshotPropertiesForTest(t *testing.T, def map[string]interface{}) map[string]interface{} {
	t.Helper()
	fn, _ := def["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if props == nil {
		t.Fatalf("screenshot properties missing: %#v", def)
	}
	return props
}
