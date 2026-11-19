package agentservice

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestOnProgressKeepsRetryStatusOnItsOwnRun(t *testing.T) {
	t.Cleanup(func() {
		ClearRunRuntimeStatus("run_a")
		ClearRunRuntimeStatus("run_b")
	})
	a := &coreAgentCallbacks{runID: "run_a"}
	b := &coreAgentCallbacks{runID: "run_b"}
	a.OnProgress(agent.LLMRetryWaitStatus(1, 5, time.Second, false))
	b.OnProgress("当前模型服务限流，已切换到备用服务继续。")
	if !strings.Contains(RunRuntimeStatus("run_a"), "重试 1/5") {
		t.Fatalf("run A status=%q", RunRuntimeStatus("run_a"))
	}
	if RunRuntimeStatus("run_b") != "" {
		t.Fatalf("a chat line without the live-status marker was stored: %q", RunRuntimeStatus("run_b"))
	}
	named, ok := agent.LLMRetrySwitchedInFlightStatus(2,
		corelib.MaclawLLMConfig{ProviderName: "Primary", Model: "old", URL: "https://a.example/v1"},
		corelib.MaclawLLMConfig{ProviderName: "Backup", Model: "new", URL: "https://b.example/v1"},
	)
	if !ok {
		t.Fatal("expected a visible switch")
	}
	b.OnProgress(named)
	if strings.Contains(RunRuntimeStatus("run_a"), "改用") {
		t.Fatalf("run B's switch replaced run A: %q", RunRuntimeStatus("run_a"))
	}
	if !strings.Contains(RunRuntimeStatus("run_b"), "Backup/new") || strings.Contains(RunRuntimeStatus("run_b"), "LiveStatus") {
		t.Fatalf("run B status=%q", RunRuntimeStatus("run_b"))
	}
	a.OnToolCall("desktop")
	if RunRuntimeStatus("run_a") != "" {
		t.Fatalf("a tool start left the retry notice up: %q", RunRuntimeStatus("run_a"))
	}
	if !strings.Contains(RunRuntimeStatus("run_b"), "Backup/new") {
		t.Fatal("clearing run A dropped run B")
	}
	b.OnToken("x")
	if RunRuntimeStatus("run_b") != "" {
		t.Fatalf("a model token left the notice up: %q", RunRuntimeStatus("run_b"))
	}
}
