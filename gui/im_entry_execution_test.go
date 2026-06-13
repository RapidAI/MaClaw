package main

import (
	"strings"
	"testing"
)

func TestSanitizeWorkflowDocPhaseResponseTextExtractsContentToolCall(t *testing.T) {
	resp := &IMAgentResponse{Text: "writing\n<details><summary>思考</summary>hidden</details>\n<tool_call[]>\n" +
		`{"name":"write_file","arguments":{"file_path":"d:\\project\\docs\\task-breakdown.md","content":"# Tasks\n\n- T1"}}`}

	if !sanitizeWorkflowDocPhaseResponseText(resp, nil, "task_breakdown") {
		t.Fatal("expected content")
	}
	if resp.Text != "# Tasks\n\n- T1" {
		t.Fatalf("Text = %q", resp.Text)
	}
	if strings.Contains(resp.Text, "<tool_call") || strings.Contains(resp.Text, "write_file") || strings.Contains(resp.Text, "hidden") {
		t.Fatalf("protocol leaked: %q", resp.Text)
	}
}

func TestSanitizeWorkflowDocPhaseResponseTextPrefersCleanBuffer(t *testing.T) {
	ctx := &LoopContext{WorkflowDocPhase: true, WorkflowPhaseID: "tasks"}
	ctx.WorkflowDocBuffer.WriteString("buffer intro\n<tool_call[]>\n" +
		`{"name":"write_file","arguments":{"file_path":"d:\\project\\docs\\tasks.md","content":"# Buffered Tasks\n\n- T1"}}`)
	resp := &IMAgentResponse{Text: "<tool_call[]>garbage"}

	if !sanitizeWorkflowDocPhaseResponseText(resp, ctx, "tasks") {
		t.Fatal("expected buffered content")
	}
	if resp.Text != "# Buffered Tasks\n\n- T1" {
		t.Fatalf("Text = %q", resp.Text)
	}
}
