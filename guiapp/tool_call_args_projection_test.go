package guiapp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestProjectOversizedToolCallArgumentsKeepsValidPreviewAndOriginal(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	original := `{"path":"D:/work/demo.go","content":"` + strings.Repeat("package demo\n", 200) + `UNIQUE_IMPLEMENTATION"}`
	tc := llm.ToolCall{ID: "call-1", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: original}}
	conversation := []interface{}{map[string]interface{}{
		"role": "assistant", "content": "",
		"tool_calls": []llm.ToolCall{tc},
	}}
	history := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	h := &IMMessageHandler{}
	conversation, history = h.projectOversizedToolCallArguments(conversation, history, tc, "desktop-user", tc.ID, nil)
	calls := conversation[0].(map[string]interface{})["tool_calls"].([]llm.ToolCall)
	summary := calls[0].Function.Arguments
	if len(summary) >= len(original) {
		t.Fatalf("summary did not shrink arguments: %d -> %d", len(original), len(summary))
	}
	var payload struct {
		Compressed bool   `json:"_context_compressed"`
		Preview    string `json:"preview"`
	}
	if err := json.Unmarshal([]byte(summary), &payload); err != nil || !payload.Compressed {
		t.Fatalf("projected arguments are not valid marked JSON: err=%v args=%s", err, summary)
	}
	if !strings.Contains(payload.Preview, toolresult.HandleFooterMarker) {
		t.Fatalf("preview has no recovery handle: %s", payload.Preview)
	}
	if got := history[0].ToolCalls.([]llm.ToolCall)[0].Function.Arguments; got != summary {
		t.Fatal("history and live conversation have divergent argument projections")
	}
	id := ""
	for _, line := range strings.Split(payload.Preview, "\n") {
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			break
		}
	}
	res, err := toolresult.Read(toolresult.ReadOptions{ID: id, SessionKey: "desktop-user", Query: "UNIQUE_IMPLEMENTATION"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "UNIQUE_IMPLEMENTATION") {
		t.Fatalf("original arguments not recoverable: %s", res.Content)
	}
}

func TestCommitAgentLoopToolResultProjectsOversizedArguments(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	original := `{"path":"D:/work/commit.go","content":"` + strings.Repeat("package commit\n", 200) + `"}`
	tc := llm.ToolCall{ID: "commit-call", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: original}}
	h := &IMMessageHandler{}
	result := h.commitAgentLoopToolResult(agentLoopToolCommitOptions{
		UserID:          "desktop-user",
		ToolCall:        tc,
		TruncatedResult: "ok",
		Execution:       toolExecutionResult{Outcome: toolOutcomeSucceeded},
		Conversation: []interface{}{map[string]interface{}{
			"role": "assistant", "content": "", "tool_calls": []llm.ToolCall{tc},
		}},
		History: []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}},
	})
	calls := result.History[0].ToolCalls.([]llm.ToolCall)
	if !strings.Contains(calls[0].Function.Arguments, `"_context_compressed":true`) {
		t.Fatalf("committed history kept the full arguments: %d bytes", len(calls[0].Function.Arguments))
	}
	if len(result.History) != 2 || result.History[1].Role != "tool" || result.History[1].ToolCallID != tc.ID {
		t.Fatalf("tool result pairing changed: %#v", result.History)
	}
}

func TestSharedLoopProjectsOversizedArgumentsBeforeExecution(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	original := `{"path":"D:/work/shared.go","content":"` + strings.Repeat("package shared\n", 200) + `"}`
	call := llm.ToolCall{ID: "shared-call", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: original}}
	conversation := []interface{}{map[string]interface{}{"role": "assistant", "tool_calls": []llm.ToolCall{call}}}
	history := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userID: "desktop-user"}
	conversation, history = cb.ProjectToolCallArguments(conversation, history, call)

	wireCalls := conversation[0].(map[string]interface{})["tool_calls"].([]llm.ToolCall)
	if wireCalls[0].Function.Arguments == original || len(wireCalls[0].Function.Arguments) >= len(original) {
		t.Fatal("shared live conversation retained oversized arguments")
	}
	if got := history[0].ToolCalls.([]llm.ToolCall)[0].Function.Arguments; got != wireCalls[0].Function.Arguments {
		t.Fatal("shared durable history diverged from the live argument projection")
	}
	if call.Function.Arguments != original {
		t.Fatal("projection must not mutate the original execution input")
	}
}

func TestCodingLoopsProjectOversizedArgumentsAndExposeReader(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })

	original := `{"path":"D:/work/coding.go","content":"` + strings.Repeat("package coding\n", 200) + `"}`
	call := llm.ToolCall{ID: "coding-call", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: original}}
	h := &IMMessageHandler{}
	local := &codingSubAgentCallbacks{subagent: &CodingSubAgent{handler: h}}
	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: h}}
	tests := []struct {
		name       string
		project    func([]interface{}, []agent.ConversationEntry, llm.ToolCall) ([]interface{}, []agent.ConversationEntry)
		readerOpen func() bool
	}{
		{name: "local", project: local.ProjectToolCallArguments, readerOpen: local.spilledToolResultReader.Load},
		{name: "remote", project: remote.ProjectToolCallArguments, readerOpen: remote.spilledToolResultReader.Load},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conversation := []interface{}{map[string]interface{}{"role": "assistant", "tool_calls": []llm.ToolCall{call}}}
			history := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{call}}}
			conversation, history = test.project(conversation, history, call)
			wireCalls := conversation[0].(map[string]interface{})["tool_calls"].([]llm.ToolCall)
			if wireCalls[0].Function.Arguments == original || len(wireCalls[0].Function.Arguments) >= len(original) {
				t.Fatal("coding conversation retained oversized arguments")
			}
			if got := history[0].ToolCalls.([]llm.ToolCall)[0].Function.Arguments; got != wireCalls[0].Function.Arguments {
				t.Fatal("coding history diverged from the live argument projection")
			}
			if !test.readerOpen() {
				t.Fatal("argument spill did not expose read_tool_result")
			}
			if call.Function.Arguments != original {
				t.Fatal("projection mutated the original coding execution input")
			}
		})
	}
}

func TestProjectOversizedToolCallArgumentsLeavesSmallArgumentsUnchanged(t *testing.T) {
	h := &IMMessageHandler{}
	tc := llm.ToolCall{ID: "small", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"demo.go"}`}}
	conversation := []interface{}{map[string]interface{}{"role": "assistant", "tool_calls": []llm.ToolCall{tc}}}
	history := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	gotConversation, gotHistory := h.projectOversizedToolCallArguments(conversation, history, tc, "desktop-user", tc.ID, nil)
	if gotConversation[0].(map[string]interface{})["tool_calls"].([]llm.ToolCall)[0].Function.Arguments != tc.Function.Arguments ||
		gotHistory[0].ToolCalls.([]llm.ToolCall)[0].Function.Arguments != tc.Function.Arguments {
		t.Fatal("small arguments were rewritten")
	}
}
