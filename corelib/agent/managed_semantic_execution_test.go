package agent

import (
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

// managedSemanticCallbacks mimics a managed semantic turn. The rendered surface
// is a single opaque grant, and the authorizer answers from the grant map
// rather than from how a name is spelled.
type managedSemanticCallbacks struct {
	*mockCallbacks
	upgradeAttempts int
}

func (m *managedSemanticCallbacks) ManagedSemanticTurn() bool           { return true }
func (m *managedSemanticCallbacks) CurrentPromptProfile() PromptProfile { return PromptProfileLight }

// A managed surface emits opaque grant names, which no static light allowlist
// can recognise. The real host resolves such a name back to its planned
// selection and decides from that; here the grant map stands in for it.
func (m *managedSemanticCallbacks) IsToolAllowedForPromptProfile(name string, _ PromptProfile) bool {
	return m.IsToolAllowed(name)
}

func (m *managedSemanticCallbacks) UpgradeLightPromptToFull(string) bool {
	m.upgradeAttempts++
	return true
}

// TestRunLoopManagedSemanticTurnRefusesUngrantedToolCall drives the whole loop
// against a model that calls a tool the turn never granted.
//
// The surface tests elsewhere pin what gets rendered. This one pins what
// happens when the model ignores the surface: a legacy gateway must not execute
// just because the model can spell it, and the deny must not talk the model
// into asking for a bigger tool surface.
func TestRunLoopManagedSemanticTurnRefusesUngrantedToolCall(t *testing.T) {
	cases := []struct {
		name       string
		hallucated string
	}{
		{"legacy mcp gateway", "call_mcp_tool"},
		{"legacy skill gateway", "manage_skill"},
		{"unrelated builtin", "bash"},
		// An opaque name shaped like a real grant must fare no better: the
		// decision comes from the grant map, not from the spelling.
		{"grant shaped name", "invoke_lookup_2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			var denyText string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools    []map[string]interface{} `json:"tools"`
					Messages []map[string]interface{} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				for _, def := range req.Tools {
					if tooldef.Name(def) != "invoke_lookup" {
						t.Errorf("managed surface advertised %q", tooldef.Name(def))
					}
				}
				if requests.Add(1) == 1 {
					fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_hallucinated","type":"function","function":{"name":%q,"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, tc.hallucated)
					return
				}
				for _, msg := range req.Messages {
					if fmt.Sprint(msg["tool_call_id"]) == "tc_hallucinated" {
						denyText = fmt.Sprint(msg["content"])
					}
				}
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"answered without the gateway"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()

			cb := &managedSemanticCallbacks{mockCallbacks: &mockCallbacks{
				config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
				maxIter:    3,
				sysPrompt:  "managed semantic turn",
				tools:      []map[string]interface{}{tooldef.BuildToolDef("invoke_lookup", "Lookup", map[string]interface{}{"type": "object"})},
				allowed:    map[string]bool{"invoke_lookup": true},
				toolResult: "SIDE EFFECT RAN",
			}}

			result := RunLoop(cb, "查一下今天的情况", nil, server.Client())

			if result.Error != "" || result.Text != "answered without the gateway" {
				t.Fatalf("result = %+v", result)
			}
			if len(cb.toolCalls) != 0 {
				t.Fatalf("ungranted call reached execution: %v", cb.toolCalls)
			}
			if cb.upgradeAttempts != 0 {
				t.Fatalf("managed turn rebuilt its surface after the deny: %d attempts", cb.upgradeAttempts)
			}
			if denyText == "" {
				t.Fatal("model got no tool result for the denied call")
			}
			if !strings.Contains(denyText, tc.hallucated) {
				t.Fatalf("deny text does not say which tool was refused: %q", denyText)
			}
			// A grant deny is not a light-profile misroute. Telling the model to
			// switch profiles made it ask the user to re-authorize tools that
			// the turn structurally cannot run.
			if strings.Contains(denyText, "light prompt profile") || strings.Contains(denyText, PromptProfileEnvKey) {
				t.Fatalf("grant deny leaked light-upgrade guidance: %q", denyText)
			}
		})
	}
}

// TestRunLoopManagedSemanticTurnStillRunsItsGrant is the companion check: the
// refusal above must come from the grant map, not from a loop that has stopped
// executing tools altogether.
func TestRunLoopManagedSemanticTurnStillRunsItsGrant(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc_granted","type":"function","function":{"name":"invoke_lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &managedSemanticCallbacks{mockCallbacks: &mockCallbacks{
		config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "test", Key: "test-key"},
		maxIter:    3,
		sysPrompt:  "managed semantic turn",
		tools:      []map[string]interface{}{tooldef.BuildToolDef("invoke_lookup", "Lookup", map[string]interface{}{"type": "object"})},
		allowed:    map[string]bool{"invoke_lookup": true},
		toolResult: "lookup result",
	}}

	result := RunLoop(cb, "查一下今天的情况", nil, server.Client())

	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result = %+v", result)
	}
	if len(cb.toolCalls) != 1 || cb.toolCalls[0] != "invoke_lookup" {
		t.Fatalf("granted tool did not run: %v", cb.toolCalls)
	}
	if cb.upgradeAttempts != 0 {
		t.Fatalf("granted run should not touch the light upgrade path: %d", cb.upgradeAttempts)
	}
}
