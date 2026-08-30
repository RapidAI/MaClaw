package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// petitioningCallbacks grants a petitioned name exactly once, then renders it
// on every later request surface, mimicking a governed host whose trusted
// planner published a widened child revision.
type petitioningCallbacks struct {
	mockCallbacks
	petitions map[string]int
	granted   map[string]bool
	grantMsg  string
}

func (m *petitioningCallbacks) PetitionToolCall(name string) (bool, string) {
	name = strings.TrimSpace(name)
	if m.petitions == nil {
		m.petitions = map[string]int{}
	}
	m.petitions[name]++
	if m.granted == nil {
		m.granted = map[string]bool{}
	}
	if m.granted[name] {
		return false, ""
	}
	m.granted[name] = true
	return true, m.grantMsg
}

func (m *petitioningCallbacks) BuildToolsForModelRequest(string, int) []map[string]interface{} {
	tools := append([]map[string]interface{}(nil), m.tools...)
	for name := range m.granted {
		tools = append(tools, tooldef.BuildToolDef(name, "Petitioned tool", map[string]interface{}{"type": "object"}))
	}
	return tools
}

// A petitioned call is answered with the host grant message instead of the hard
// denial, and the next request surface must contain the petitioned tool so the
// model's retried call executes for real.
func TestRunLoop_ToolCallPetitionGrantsAndRendersNextIteration(t *testing.T) {
	callCount := 0
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		var resp map[string]interface{}
		switch callCount {
		case 1, 2:
			resp = toolCallResponse("web_search", `{"query":"布偶猫照片"}`)
		default:
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &petitioningCallbacks{
		mockCallbacks: mockCallbacks{
			config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
			maxIter:     6,
			sysPrompt:   "sys",
			tools:       []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write", map[string]interface{}{"type": "object"})},
			toolResult:  "search results",
			toolOutcome: ToolExecutionOutcomeOK,
		},
		grantMsg: "工具 web_search 已授权，请立即重新发起调用。",
	}
	result := RunLoop(cb, "find photos online", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if !strings.Contains(result.Text, "done") {
		t.Fatalf("text=%q", result.Text)
	}
	if cb.petitions["web_search"] != 1 {
		t.Fatalf("petitions=%v, want exactly one for web_search", cb.petitions)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "web_search" {
		t.Fatalf("toolCalls=%v, want one real web_search execution", cb.toolCalls)
	}
	if len(bodies) < 2 || !strings.Contains(string(bodies[1]), "web_search") {
		t.Fatalf("second request must render the petitioned tool: %q", bodies[1])
	}
	// The first (petition) tool response must carry the grant message, not the
	// generic unrendered denial.
	if !strings.Contains(string(bodies[1]), "已授权") {
		t.Fatalf("grant message not delivered to the model: %q", bodies[1])
	}
	if strings.Contains(string(bodies[1]), "was not available in this request's rendered tool surface") {
		t.Fatalf("hard denial must be replaced by the grant message: %q", bodies[1])
	}
}

// A host that does not grant the petition keeps the historical hard denial.
func TestRunLoop_ToolCallPetitionDeniedKeepsHardDenial(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = toolCallResponse("bash", `{"command":"rm -rf /"}`)
		} else {
			resp = textResponse("cannot do that")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &denyingPetitionerCallbacks{mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "sys",
		tools:       []map[string]interface{}{tooldef.BuildToolDef("write_file", "Write", map[string]interface{}{"type": "object"})},
		toolResult:  "ok",
		toolOutcome: ToolExecutionOutcomeOK,
	}}
	result := RunLoop(cb, "run a command", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if cb.petitions["bash"] != 1 {
		t.Fatalf("petitions=%v, want one consultation for bash", cb.petitions)
	}
	if len(cb.toolCalls) != 0 {
		t.Fatalf("denied petition must never execute: %v", cb.toolCalls)
	}
	if !strings.Contains(result.Text, "cannot do that") {
		t.Fatalf("text=%q", result.Text)
	}
}

type denyingPetitionerCallbacks struct {
	mockCallbacks
	petitions map[string]int
}

func (m *denyingPetitionerCallbacks) PetitionToolCall(name string) (bool, string) {
	if m.petitions == nil {
		m.petitions = map[string]int{}
	}
	m.petitions[strings.TrimSpace(name)]++
	return false, ""
}

// A consumed one-shot grant keeps its dedicated denial text and is never
// offered to the petitioner: the earlier success still stands.
func TestRunLoop_ToolCallPetitionNotConsultedForConsumedGrant(t *testing.T) {
	callCount := 0
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		var resp map[string]interface{}
		switch callCount {
		case 1, 2:
			resp = toolCallResponse("web_search", `{"query":"again"}`)
		default:
			resp = textResponse("done")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	cb := &consumedGrantPetitionerCallbacks{mockCallbacks: mockCallbacks{
		config:      corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:     6,
		sysPrompt:   "sys",
		toolResult:  "search results",
		toolOutcome: ToolExecutionOutcomeOK,
	}}
	result := RunLoop(cb, "search twice", nil, nil)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	if cb.petitionCalls != 0 {
		t.Fatalf("consumed grant must not reach the petitioner, got %d consultations", cb.petitionCalls)
	}
	if len(cb.toolCalls) != 1 {
		t.Fatalf("toolCalls=%v, want exactly one execution", cb.toolCalls)
	}
	if len(bodies) < 3 || !strings.Contains(string(bodies[2]), "usage limit") {
		t.Fatalf("consumed-grant denial missing from conversation: %q", bodies[2])
	}
}

// consumedGrantPetitionerCallbacks renders web_search only on the first request,
// so the model's second call hits the unrendered fence after a success.
type consumedGrantPetitionerCallbacks struct {
	mockCallbacks
	petitionCalls int
}

func (m *consumedGrantPetitionerCallbacks) PetitionToolCall(string) (bool, string) {
	m.petitionCalls++
	return true, "should never be used"
}

func (m *consumedGrantPetitionerCallbacks) BuildToolsForModelRequest(_ string, iteration int) []map[string]interface{} {
	if iteration == 0 {
		return []map[string]interface{}{tooldef.BuildToolDef("web_search", "Search", map[string]interface{}{"type": "object"})}
	}
	return nil
}
