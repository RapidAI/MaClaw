package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// End-to-end through agent.RunLoop: on the correlated local desktop boundary
// the effectful static families are rendered, and a write_file call carried by
// a provider response with a real response ID passes the epoch+ResponseID
// dispatch gate and actually lands on disk.
func TestCodingCorrelatedLocalRunLoopExecutesWriteWithResponseCorrelation(t *testing.T) {
	project := t.TempDir()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"id":"resp-write-1","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_w","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"hello.cpp\",\"content\":\"#include <iostream>\\nint main(){return 0;}\\n\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"resp-final","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:                  &IMMessageHandler{},
			cfg:                      corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
			projectPath:              project,
			fullEnvironment:          true,
			correlatedLocalExecution: true,
		},
		task: &TaskItem{Title: "implement hello", Description: "create hello.cpp", RequestKind: codingRequestImplementation},
	}
	result := agent.RunLoop(cb, "create hello.cpp", nil, server.Client())
	if result.Error != "" || result.Text != "done" || result.ToolCalls != 1 {
		t.Fatalf("result=%+v", result)
	}
	data, err := os.ReadFile(filepath.Join(project, "hello.cpp"))
	if err != nil {
		t.Fatalf("correlated write did not land on disk: %v", err)
	}
	if !strings.Contains(string(data), "iostream") {
		t.Fatalf("unexpected written content: %q", data)
	}
}

// Same boundary, but a provider response without a response ID: the effectful
// dispatch must fail closed and the model must see the named rejection.
func TestCodingCorrelatedLocalRunLoopRejectsWriteWithoutResponseID(t *testing.T) {
	project := t.TempDir()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			// Deliberately no "id" field.
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_w","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"hello.cpp\",\"content\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			handler:                  &IMMessageHandler{},
			cfg:                      corelib.MaclawLLMConfig{URL: server.URL, Model: "test"},
			projectPath:              project,
			fullEnvironment:          true,
			correlatedLocalExecution: true,
		},
		task: &TaskItem{Title: "implement hello", Description: "create hello.cpp", RequestKind: codingRequestImplementation},
	}
	result := agent.RunLoop(cb, "create hello.cpp", nil, server.Client())
	if result.Error != "" || result.Text != "done" {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(project, "hello.cpp")); !os.IsNotExist(err) {
		t.Fatalf("write without provider response correlation must not land on disk")
	}
	var denial string
	for _, entry := range result.HistoryDelta {
		text, _ := entry.Content.(string)
		if entry.Role == "tool" {
			denial = text
		}
	}
	if !strings.Contains(denial, "static_response_correlation_missing") {
		t.Fatalf("denial must name the missing correlation, got %q", denial)
	}
}
