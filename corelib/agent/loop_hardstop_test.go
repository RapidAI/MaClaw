package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// outcomeVaryingCallbacks is a mock that can return different ToolExecutionOutcome
// values per call, driven by a callback function. This lets tests simulate
// business failures (error) vs. unrecoverable failures (timeout, guard).
type outcomeVaryingCallbacks struct {
	mockCallbacks
	callSeq int32
	// outcomeFn determines the outcome for call N (1-based).
	outcomeFn func(callN int32) ToolExecutionOutcome
	// resultFn determines the result string for call N.
	resultFn func(callN int32) string
}

func (m *outcomeVaryingCallbacks) ExecuteTool(name, args string) string {
	n := atomic.AddInt32(&m.callSeq, 1)
	if m.resultFn != nil {
		return m.resultFn(n)
	}
	return m.toolResult
}

// ExecuteToolCall is the path the loop prefers once this mock implements
// ToolCallExecutor. It delegates without bumping the sequence a second time,
// so the per-call outcome functions see exactly one call number per tool call.
func (m *outcomeVaryingCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	return m.executeOnce(name, args)
}

func (m *outcomeVaryingCallbacks) ExecuteToolStructured(name, args string) ToolExecutionResult {
	return m.executeOnce(name, args)
}

func (m *outcomeVaryingCallbacks) executeOnce(name, args string) ToolExecutionResult {
	n := atomic.AddInt32(&m.callSeq, 1)
	var outcome ToolExecutionOutcome
	if m.outcomeFn != nil {
		outcome = m.outcomeFn(n)
	} else {
		outcome = m.toolOutcome
	}
	var result string
	if m.resultFn != nil {
		result = m.resultFn(n)
	} else {
		result = m.toolResult
	}
	m.toolCalls = append(m.toolCalls, name)
	m.toolArgs = append(m.toolArgs, args)
	return ToolExecutionResult{Result: result, Outcome: outcome}
}

// makeLLMServer that always returns a tool call to "bash" with a unique call ID.
func makeBashToolCallServer(t *testing.T) *httptest.Server {
	t.Helper()
	callCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		respData := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"role": "assistant",
						"tool_calls": []map[string]interface{}{
							{
								"id":   fmt.Sprintf("call_%d", n),
								"type": "function",
								"function": map[string]interface{}{
									"name":      "bash",
									"arguments": fmt.Sprintf(`{"command":"go test ./... -run Test%d"}`, n),
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respData); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestRunLoop_BusinessFailureDoesNotHardStop verifies that when bash keeps
// returning ToolExecutionOutcomeError (simulating go test assertion failures,
// compile errors, lint errors), the consecutive counter is reset each time
// and the loop does NOT hard-stop.
func TestRunLoop_BusinessFailureDoesNotHardStop(t *testing.T) {
	server := makeBashToolCallServer(t)

	cb := &outcomeVaryingCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   20,
			sysPrompt: "test",
			tools:     []map[string]interface{}{tooldef.BuildToolDef("bash", "Run a command", map[string]interface{}{"type": "object"})},
			toolResult: "FAIL\ngithub.com/foo/bar  0.5s\n--- FAIL: TestAssert (0.01s)\n    assert_test.go:42: expected 1, got 2",
			// Simulate a plain business failure: outcome is "error" but no
			// guard rejection, no timeout, no FailureKind.
			toolOutcome: ToolExecutionOutcomeError,
		},
		// Every call returns an "error" outcome (business failure).
		outcomeFn: func(callN int32) ToolExecutionOutcome {
			return ToolExecutionOutcomeError
		},
	}

	result := RunLoop(cb, "fix the failing tests", nil, nil)

	// The loop should NOT hard-stop because every failure is a recoverable
	// business failure (go test assertion). It should exit via maxIter.
	if result.HardExit {
		t.Fatalf("business failures triggered hard stop; Error=%q", result.Error)
	}
	// We should have run all iterations (not stopped early).
	if result.Iterations < 10 {
		t.Fatalf("expected at least 10 iterations, got %d (stopped too early)", result.Iterations)
	}
}

// TestRunLoop_GuardRejectionTriggersHardStop verifies that when bash keeps
// returning a guard-rejected outcome (simulating permission denied), the
// loop DOES hard-stop and the Error contains the real reason with the last
// failure snippet.
func TestRunLoop_GuardRejectionTriggersHardStop(t *testing.T) {
	server := makeBashToolCallServer(t)

	cb := &outcomeVaryingCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   20,
			sysPrompt: "test",
			tools:     []map[string]interface{}{tooldef.BuildToolDef("bash", "Run a command", map[string]interface{}{"type": "object"})},
			// Guard rejection: the result string starts with "[system rejected]"
			// and the outcome is "error".
			toolResult:  "[system rejected] permission denied: /etc/shadow is not writable",
			toolOutcome: ToolExecutionOutcomeError,
		},
		outcomeFn: func(callN int32) ToolExecutionOutcome {
			return ToolExecutionOutcomeError
		},
	}

	result := RunLoop(cb, "edit system files", nil, nil)

	if !result.HardExit {
		t.Fatalf("guard rejection did not trigger hard stop; Iterations=%d", result.Iterations)
	}
	// Error must contain the tool name and the failure count.
	if !strings.Contains(result.Error, "bash") {
		t.Fatalf("hard stop Error missing tool name: %q", result.Error)
	}
	// Error must contain the last failure snippet (the real reason), not just
	// the generic "连续失败" text.
	if !strings.Contains(result.Error, "permission denied") {
		t.Fatalf("hard stop Error missing the real failure reason: %q", result.Error)
	}
	// Must NOT be the fallback "empty response" message.
	if strings.Contains(result.Error, "空响应") {
		t.Fatalf("hard stop Error fell back to empty-response text: %q", result.Error)
	}
}

// TestRunLoop_TimeoutOutcomeTriggersHardStop verifies that repeated timeouts
// (unrecoverable) trigger hard stop with the timeout reason in Error.
func TestRunLoop_TimeoutOutcomeTriggersHardStop(t *testing.T) {
	server := makeBashToolCallServer(t)

	cb := &outcomeVaryingCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   20,
			sysPrompt: "test",
			tools:     []map[string]interface{}{tooldef.BuildToolDef("bash", "Run a command", map[string]interface{}{"type": "object"})},
			toolResult:  "[错误] 命令超时（60 秒）",
			toolOutcome: ToolExecutionOutcomeTimeout,
		},
		outcomeFn: func(callN int32) ToolExecutionOutcome {
			return ToolExecutionOutcomeTimeout
		},
	}

	result := RunLoop(cb, "run long command", nil, nil)

	if !result.HardExit {
		t.Fatalf("timeout failures did not trigger hard stop; Iterations=%d", result.Iterations)
	}
	if !strings.Contains(result.Error, "bash") {
		t.Fatalf("hard stop Error missing tool name: %q", result.Error)
	}
	// Should mention the timeout in the snippet.
	if !strings.Contains(result.Error, "超时") {
		t.Fatalf("hard stop Error missing timeout snippet: %q", result.Error)
	}
}

// TestRunLoop_SuccessResetsConsecutiveCounter verifies that a successful call
// to the previously-failing tool resets the consecutive counter, so even
// after 11 unrecoverable failures, one success + 11 more = no hard stop.
func TestRunLoop_SuccessResetsConsecutiveCounter(t *testing.T) {
	server := makeBashToolCallServer(t)

	// maxIter is 22: 11 timeouts, one success, then 10 timeouts reach the
	// iteration cap before the streak can reach the threshold of 12.
	cb := &outcomeVaryingCallbacks{
		mockCallbacks: mockCallbacks{
			config: corelib.MaclawLLMConfig{
				URL:   server.URL,
				Model: "test",
				Key:   "test-key",
			},
			maxIter:   22,
			sysPrompt: "test",
			tools:     []map[string]interface{}{tooldef.BuildToolDef("bash", "Run a command", map[string]interface{}{"type": "object"})},
			toolResult:  "[错误] command timed out",
			toolOutcome: ToolExecutionOutcomeTimeout,
		},
		// Calls 1-11: unrecoverable (timeout). Call 12: success, which
		// clears the streak. Calls 13-23 are the next 11 timeouts, still
		// below the threshold of 12 consecutive unrecoverable failures.
		outcomeFn: func(callN int32) ToolExecutionOutcome {
			if callN == 12 {
				return ToolExecutionOutcomeOK
			}
			return ToolExecutionOutcomeTimeout
		},
		resultFn: func(callN int32) string {
			if callN == 12 {
				return "ok"
			}
			return "[错误] command timed out"
		},
	}

	result := RunLoop(cb, "retry pattern", nil, nil)

	// After 11 failures, success resets to 0. Then 11 more failures
	// (calls 13-23) = 11 consecutive, still below threshold of 12.
	// The loop should not have hard-stopped from the consecutive breaker.
	if result.HardExit {
		t.Fatalf("should not hard-stop: the success at call 12 clears the streak. Iterations=%d Error=%q", result.Iterations, result.Error)
	}
}

// Ensure the test file compiles with the context import used by httptest.
var _ = context.Background
