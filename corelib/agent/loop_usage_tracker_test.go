package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// usageTrackerCallbacks implements UsageTrackerProvider on top of mockCallbacks.
type usageTrackerCallbacks struct {
	mockCallbacks
	tracker *tool.UsageTracker
}

func (c *usageTrackerCallbacks) UsageTracker() *tool.UsageTracker { return c.tracker }

// usageTrackerTestServer returns a fake LLM server that answers the first
// request with one read_file tool call and the second with a plain stop.
func usageTrackerTestServer(t *testing.T, calls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		*calls++
		if *calls == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
}

func waitForUsageRecords(t *testing.T, tracker *tool.UsageTracker, toolName string, want int) []tool.SkillUsageRecordExport {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		records := tracker.RecentSkillUsageRecords(toolName, 7)
		if len(records) >= want {
			return records
		}
		time.Sleep(10 * time.Millisecond)
	}
	records := tracker.RecentSkillUsageRecords(toolName, 7)
	t.Fatalf("timed out waiting for %d usage records for %q, got %d", want, toolName, len(records))
	return nil
}

func TestRunLoopFeedsSuccessfulToolExecutionToUsageTracker(t *testing.T) {
	calls := 0
	server := usageTrackerTestServer(t, &calls)
	defer server.Close()

	tracker, err := tool.NewUsageTracker(filepath.Join(t.TempDir(), "tool_usage.json"))
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	cb := &usageTrackerCallbacks{tracker: tracker, mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:    4,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"})},
		toolResult: "file contents",
	}}

	result := RunLoop(cb, "inspect the config please", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}

	records := waitForUsageRecords(t, tracker, "read_file", 1)
	if !records[0].Success {
		t.Fatalf("record.Success = false, want true: %+v", records[0])
	}
	// Consumer-visible effect: the router-facing OutcomeScore must reflect the
	// success once the record lands.
	if score := tracker.OutcomeScore("read_file"); score < 0.9 {
		t.Fatalf("OutcomeScore(read_file) = %v, want >= 0.9", score)
	}
}

func TestRunLoopFeedsFailedToolExecutionToUsageTracker(t *testing.T) {
	calls := 0
	server := usageTrackerTestServer(t, &calls)
	defer server.Close()

	tracker, err := tool.NewUsageTracker(filepath.Join(t.TempDir(), "tool_usage.json"))
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	cb := &usageTrackerCallbacks{tracker: tracker, mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:     4,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"})},
		toolResult:  "Error: permission denied",
		toolOutcome: ToolExecutionOutcomeError,
	}}

	result := RunLoop(cb, "inspect the config please", nil, server.Client())
	if result.Error != "" {
		t.Fatalf("result=%+v", result)
	}

	records := waitForUsageRecords(t, tracker, "read_file", 1)
	if records[0].Success {
		t.Fatalf("record.Success = true, want false: %+v", records[0])
	}
	if records[0].FollowUp != "" {
		t.Fatalf("record.FollowUp = %q, want empty (no suppression claim)", records[0].FollowUp)
	}
	// A single decisive failure scores 0 (bounded mild negative, no suppression).
	if score := tracker.OutcomeScore("read_file"); score != 0 {
		t.Fatalf("OutcomeScore(read_file) = %v, want 0", score)
	}
}

func TestRunLoopNilUsageTrackerProviderIsNoOp(t *testing.T) {
	calls := 0
	server := usageTrackerTestServer(t, &calls)
	defer server.Close()

	cb := &usageTrackerCallbacks{tracker: nil, mockCallbacks: mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:    4,
		sysPrompt:  "sys",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"})},
		toolResult: "file contents",
	}}

	result := RunLoop(cb, "inspect the config please", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
}

func TestRunLoopPolicyDenialDoesNotFeedUsageTracker(t *testing.T) {
	// A host that never authorizes the tool: the call is a policy denial, not a
	// real execution, and must not record a usage outcome.
	denials := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		denials++
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	tracker, err := tool.NewUsageTracker(filepath.Join(t.TempDir(), "tool_usage.json"))
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	cb := &usageTrackerCallbacks{tracker: tracker, mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
		maxIter:     4,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("read_file", "read", map[string]interface{}{"type": "object"})},
		toolResult:  "denied",
		callAllowed: map[string]bool{"read_file": false},
		callReason:  "not authorized",
	}}

	result := RunLoop(cb, "inspect the config please", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if records := tracker.RecentSkillUsageRecords("read_file", 7); len(records) != 0 {
		t.Fatalf("policy denial recorded %d usage records, want 0", len(records))
	}
}
