package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// firstAllowThenDenyCallbacks allows the first tool call and policy-denies
// every later one, reproducing the production sequence where one parallel
// web_fetch succeeds and the next is rejected by the execution policy.
type firstAllowThenDenyCallbacks struct {
	mockCallbacks
	authCalls int
}

func (m *firstAllowThenDenyCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	_ = argsJSON
	m.authCalls++
	if m.authCalls == 1 {
		return true, ""
	}
	return false, "tool call " + name + " is not allowed by the current execution policy"
}

// A host policy denial is a fail-closed routing guardrail, not a diagnosable
// tool failure. It must not open a WorkingState item: a stale open triggers a
// spurious finish-nudge after the goal already completed, and that extra
// iteration once made the model emit unparseable tool markup that replaced
// the good final answer.
func TestRunLoop_PolicyDenialDoesNotOpenOrBlockFinish(t *testing.T) {
	callCount := 0
	var bodies [][]byte
	// The production incident had the success and the policy denial in one
	// parallel batch: apply() then prefers the failure, so RetryDiagnose opens
	// an item the same-batch trust never closes.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = toolCallsResponse(
				[2]string{"web_fetch", `{"url":"https://a.example"}`},
				[2]string{"web_fetch", `{"url":"https://b.example"}`},
			)
		default:
			resp = textResponse("final answer")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &firstAllowThenDenyCallbacks{mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("web_fetch", "Fetch", map[string]interface{}{"type": "object"})},
		toolResult:  "fetched ok",
		toolOutcome: ToolExecutionOutcomeOK,
	}}
	result := RunLoop(cb, "summarize two pages", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !strings.Contains(result.Text, "final answer") {
		t.Fatalf("text=%q", result.Text)
	}
	if result.WorkingState != nil && UnclosedOpenCount(result.WorkingState) != 0 {
		t.Fatalf("policy denial must not open items: %+v", result.WorkingState.Open)
	}
	for i, body := range bodies {
		if strings.Contains(string(body), "还有未关闭问题") {
			t.Fatalf("request %d carried a spurious finish-nudge", i+1)
		}
	}
	if callCount != 2 {
		t.Fatalf("callCount=%d, want 2 (no nudge iteration)", callCount)
	}
}

// A malformed content tool-markup interception is a transport artifact, not a
// user-meaningful answer. The loop must re-ask once for a plain-text reply
// instead of shipping the interception notice as the final text.
func TestRunLoop_MalformedContentToolMarkupRepromptsOnce(t *testing.T) {
	callCount := 0
	var repromptSeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		if callCount == 3 {
			repromptSeen = lastUserContentFromRequest(body)
		}
		var resp map[string]interface{}
		switch callCount {
		case 1:
			resp = toolCallResponse("write_file", `{"path":"a.txt","content":"x"}`)
		case 2:
			resp = textResponse("<function=send_file></function>")
		default:
			resp = textResponse("plain final answer")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write", map[string]interface{}{"type": "object"})},
		toolResult:  "ok",
		toolOutcome: ToolExecutionOutcomeOK,
	}
	result := RunLoop(cb, "make a file and send it", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !strings.Contains(result.Text, "plain final answer") {
		t.Fatalf("text=%q", result.Text)
	}
	if strings.Contains(result.Text, llm.MalformedContentToolCallErrorMsg) {
		t.Fatalf("interception notice leaked as final text: %q", result.Text)
	}
	if !strings.Contains(repromptSeen, "纯文本") {
		t.Fatalf("expected plain-text reprompt, last user=%q", repromptSeen)
	}
}

// The re-ask is one-shot: a model that keeps emitting unparseable markup must
// still terminate with the interception notice rather than loop forever.
func TestRunLoop_MalformedContentToolMarkupRepromptIsOneShot(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("write_file", `{"path":"a.txt","content":"x"}`)
		} else {
			resp = textResponse("<function=send_file></function>")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write", map[string]interface{}{"type": "object"})},
		toolResult:  "ok",
		toolOutcome: ToolExecutionOutcomeOK,
	}
	result := RunLoop(cb, "make a file and send it", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if callCount != 3 {
		t.Fatalf("callCount=%d, want exactly one reprompt", callCount)
	}
	if !strings.Contains(result.Text, llm.MalformedContentToolCallErrorMsg) {
		t.Fatalf("persistent malformed markup must still surface: %q", result.Text)
	}
}
