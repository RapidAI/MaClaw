package codingagent

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestParseRoleAndReadOnlyBoundary(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		want     Role
		readOnly bool
	}{
		{"", RoleWorker, false},
		{"implement", RoleWorker, false},
		{"explore", RoleExplorer, true},
		{"qa", RoleReviewer, true},
	} {
		got, err := ParseRole(tc.raw)
		if err != nil || got != tc.want || got.ReadOnly() != tc.readOnly {
			t.Fatalf("ParseRole(%q) = %q, %v, readonly=%v", tc.raw, got, err, got.ReadOnly())
		}
	}
	if _, err := ParseRole("admin"); err == nil {
		t.Fatal("unknown role was accepted")
	}
}

func TestToolPolicyFailsClosedForReadOnlyRoles(t *testing.T) {
	if (ToolPolicy{Role: RoleExplorer}).Allows("write_file") {
		t.Fatal("read-only role without allow-list must be denied")
	}
	if !(ToolPolicy{Role: RoleWorker}).Allows("write_file") {
		t.Fatal("worker without allow-list should retain host-defined full surface")
	}
	policy := ToolPolicy{Role: RoleReviewer, Allowed: map[string]bool{"read_file": true}}
	if !policy.Allows("read_file") || policy.Allows("write_file") {
		t.Fatal("policy did not apply allow-list")
	}
	if !policy.IsToolAllowed("read_file") || policy.IsToolAllowed("write_file") {
		t.Fatal("agent authorizer boundary did not apply allow-list")
	}
}

func TestReadOnlyToolPolicyRejectsWebFetchWriteAliases(t *testing.T) {
	policy := ToolPolicy{Role: RoleExplorer, Allowed: map[string]bool{"web_fetch": true}}
	for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
		if ok, reason := policy.IsToolCallAllowed("web_fetch", map[string]interface{}{key: "download.pdf"}); ok || !strings.Contains(reason, key) {
			t.Fatalf("web_fetch %s must be rejected: ok=%t reason=%q", key, ok, reason)
		}
	}
	if ok, reason := policy.IsToolCallAllowed("web_fetch", map[string]interface{}{"url": "https://example.com"}); !ok || reason != "" {
		t.Fatalf("observational web_fetch must remain allowed: ok=%t reason=%q", ok, reason)
	}
}

func TestLoopExecutorBoundsEvidenceAndNeverPersistsRawError(t *testing.T) {
	executor := LoopExecutor{Run: func(context.Context, codingruntime.ExecutionRequest) agent.LoopResult {
		return agent.LoopResult{Iterations: 2, ToolCalls: 1, Error: "provider included secret=do-not-store"}
	}}
	result := executor.Execute(context.Background(), codingruntime.ExecutionRequest{})
	if result.Status != codingruntime.TaskFailed || result.SideEffectState != codingruntime.SideEffectUncertain || result.ErrorCode != "agent_loop_failed" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.ErrorSummary == "" || strings.Contains(result.ErrorSummary, "do-not-store") || len(result.Evidence) != 2 {
		t.Fatalf("raw loop details leaked or evidence missing: %#v", result)
	}
	for _, evidence := range result.Evidence {
		if !strings.HasPrefix(evidence.Digest, "sha256:") {
			t.Fatalf("evidence must be a digest: %#v", evidence)
		}
	}
}

func TestLoopExecutorCancellationFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	result := (LoopExecutor{Run: func(context.Context, codingruntime.ExecutionRequest) agent.LoopResult {
		calls++
		return agent.LoopResult{}
	}}).Execute(ctx, codingruntime.ExecutionRequest{})
	if calls != 0 || result.Status != codingruntime.TaskInterrupted || result.SideEffectState != codingruntime.SideEffectUncertain {
		t.Fatalf("calls=%d result=%#v", calls, result)
	}
}

func TestLoopExecutorDoesNotTreatHardExitOrUserPauseAsCompleted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		loop   agent.LoopResult
		status codingruntime.TaskStatus
		code   string
	}{
		{name: "hard exit", loop: agent.LoopResult{HardExit: true, ToolCalls: 1}, status: codingruntime.TaskFailed, code: "agent_loop_hard_exit"},
		{name: "ask user", loop: agent.LoopResult{AskUser: &agent.AskUserRequest{}}, status: codingruntime.TaskBlocked, code: "agent_loop_waiting_for_user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := (LoopExecutor{Run: func(context.Context, codingruntime.ExecutionRequest) agent.LoopResult { return tc.loop }}).Execute(context.Background(), codingruntime.ExecutionRequest{})
			if result.Status != tc.status || result.ErrorCode != tc.code {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestLoopExecutorMapsLoopCancellationToInterrupted(t *testing.T) {
	result := (LoopExecutor{Run: func(context.Context, codingruntime.ExecutionRequest) agent.LoopResult {
		return agent.LoopResult{ToolCalls: 1, Error: "cancelled during tool execution"}
	}}).Execute(context.Background(), codingruntime.ExecutionRequest{})
	if result.Status != codingruntime.TaskInterrupted || result.SideEffectState != codingruntime.SideEffectUncertain || result.ErrorCode != "agent_loop_cancelled" {
		t.Fatalf("result=%#v", result)
	}
}
