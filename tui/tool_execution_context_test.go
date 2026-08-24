package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestExecuteTUIRegisteredToolUsesRoutedDocumentContext(t *testing.T) {
	path := writeTUIDocumentFixture(t, strings.Repeat("文", 200_000), ".txt")
	args := map[string]interface{}{
		"file_path": path,
		// This untrusted value deliberately resembles a host implementation
		// detail. It must not change the selected page budget.
		"_runtime_context_tokens": 1,
	}
	low := executeTUIRegisteredTool(context.Background(), agent.NewCoreToolRegistry(), "read_document", args, corelib.MaclawLLMConfig{ContextLength: 10_000})
	high := executeTUIRegisteredTool(context.Background(), agent.NewCoreToolRegistry(), "read_document", args, corelib.MaclawLLMConfig{ContextLength: 400_000})

	if lowChars, highChars := documentPageChars(t, low), documentPageChars(t, high); lowChars != agent.DocumentReadMaxRunesForContext(8_000) || highChars != agent.DocumentReadMaxRunesForContext(320_000) {
		t.Fatalf("routed document pages = %d and %d, want %d and %d", lowChars, highChars, agent.DocumentReadMaxRunesForContext(8_000), agent.DocumentReadMaxRunesForContext(320_000))
	}
	if strings.Contains(high, "_runtime_context_tokens") {
		t.Fatalf("untrusted runtime metadata leaked into document result: %q", high)
	}
}

func TestProjectTUIToolResultUsesRoutedContext(t *testing.T) {
	raw := strings.Repeat("文", 60_000) // 180 KiB: fits 400K context document budget, not the 10K budget.
	result := agent.ToolExecutionResult{Result: raw, Outcome: agent.ToolExecutionOutcomeOK}
	low := projectTUIToolResult("read_document", result, corelib.MaclawLLMConfig{ContextLength: 10_000})
	high := projectTUIToolResult("read_document", result, corelib.MaclawLLMConfig{ContextLength: 400_000})
	if !strings.Contains(low, "[tool_result_handle]") {
		t.Fatalf("small routed context should spill large document result, got %d bytes", len(low))
	}
	if strings.Contains(high, "[tool_result_handle]") || high != raw {
		t.Fatalf("400K routed context should keep result inline: bytes=%d handle=%v", len(high), strings.Contains(high, "[tool_result_handle]"))
	}
}

func TestTUIAgentCallbacksImplementToolResultProjector(t *testing.T) {
	var _ agent.ToolResultProjector = (*tuiCallbacks)(nil)
	var _ agent.ToolResultProjector = (*tuiBtwCallbacks)(nil)
	var _ agent.ToolResultProjector = (*pipeCallbacks)(nil)
	var _ agent.ToolResultProjector = (*rpcCallbacks)(nil)
	var _ agent.ToolResultProjector = (*tuiLoopCycleCallbacks)(nil)
	var _ agent.ToolResultProjector = (*tuiSchedulerCallbacks)(nil)
	var _ agent.ToolResultProjector = (*tuiWeixinCallbacks)(nil)
}

func TestTUIAgentCallbacksImplementToolExecutionEscalator(t *testing.T) {
	var _ agent.ToolExecutionEscalator = (*tuiCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*tuiBtwCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*pipeCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*rpcCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*tuiLoopCycleCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*tuiSchedulerCallbacks)(nil)
	var _ agent.ToolExecutionEscalator = (*tuiWeixinCallbacks)(nil)
}

func TestTUIAgentCallbacksImplementToolExecutionSurfaceRefresher(t *testing.T) {
	var _ agent.ToolExecutionSurfaceRefresher = (*tuiCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*tuiBtwCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*pipeCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*rpcCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*tuiLoopCycleCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*tuiSchedulerCallbacks)(nil)
	var _ agent.ToolExecutionSurfaceRefresher = (*tuiWeixinCallbacks)(nil)
}

func writeTUIDocumentFixture(t *testing.T, body, extension string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document"+extension)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func documentPageChars(t *testing.T, output string) int {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, "# chars: "); ok {
			var n int
			if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
				t.Fatalf("parse page chars %q: %v", line, err)
			}
			return n
		}
	}
	t.Fatalf("read_document output missing chars metadata: %q", output)
	return 0
}
