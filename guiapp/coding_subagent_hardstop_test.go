package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// TestHardExitSubAgentErrorPreservesRealReason verifies that when the agent
// loop returns a non-empty Error (the real hard-stop cause), the GUI layer
// preserves it in CodingSubAgentResult.Error instead of replacing it with
// the generic "模型连续返回空响应" fallback.
func TestHardExitSubAgentErrorPreservesRealReason(t *testing.T) {
	// Simulate the exact code path at coding_subagent.go:854-875:
	// result.Error is non-empty, result.HardExit is true.
	realError := "工具 bash 不可恢复调用级失败连续 12 次，已停止执行。最后失败摘要：[错误] 权限被拒绝: git push 需要额外权限"
	loopRes := agent.LoopResult{
		Text:       realError,
		Error:      realError,
		Iterations: 15,
		ToolCalls:  12,
		HardExit:   true,
	}

	errMsg := ""
	if loopRes.Error != "" {
		errMsg = compactSubAgentErrorSummary(loopRes.Error)
	}
	if loopRes.HardExit {
		errMsg = hardExitSubAgentError(errMsg)
	}

	// errMsg must carry the real reason, not the generic fallback.
	if !strings.Contains(errMsg, "不可恢复调用级失败连续 12 次") {
		t.Fatalf("errMsg lost the real cause:\n  got:  %q", errMsg)
	}
	if strings.Contains(errMsg, "模型连续返回空响应") {
		t.Fatalf("errMsg used the generic fallback instead of the real cause:\n  got: %q", errMsg)
	}
	// The failure snippet must be present.
	if !strings.Contains(errMsg, "权限被拒绝") {
		t.Fatalf("errMsg lost the failure snippet:\n  got: %q", errMsg)
	}
}

// TestHardExitSubAgentErrorEmptyFallsBack verifies that when the loop does
// NOT provide a specific Error (consecutive-empty stop), the GUI layer falls
// back to the generic "模型连续返回空响应" message.
func TestHardExitSubAgentErrorEmptyFallsBack(t *testing.T) {
	loopRes := agent.LoopResult{
		Text:       "",
		Error:      "",
		Iterations: 5,
		ToolCalls:  0,
		HardExit:   true,
	}

	errMsg := ""
	if loopRes.Error != "" {
		errMsg = compactSubAgentErrorSummary(loopRes.Error)
	}
	if loopRes.HardExit {
		errMsg = hardExitSubAgentError(errMsg)
	}

	if errMsg != "模型连续返回空响应，任务中断" {
		t.Fatalf("expected generic fallback, got: %q", errMsg)
	}
}
