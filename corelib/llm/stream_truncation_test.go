package llm

import (
	"strings"
	"testing"
)

func TestFilterStreamTruncatedToolCallsWriteFileContract(t *testing.T) {
	longPassage := strings.Repeat("x", 5000)
	longBody := strings.Repeat("y", 5000)
	// JSON source, so the newline is the two-character escape. The decoded
	// old_string is a newline plus spaces, which is a real passage.
	whitespacePassage := `\n  ` + strings.Repeat(" ", 5000)

	tests := []struct {
		name     string
		finish   string
		tool     string
		args     string
		wantDrop bool
	}{
		{
			name:   "tool_calls edit over 4000 bytes is a finished call",
			finish: "tool_calls",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"` + longPassage + `","new_string":"@article{added,title={Added}}"}`,
		},
		{
			name:   "tool_calls text alias over 4000 bytes is a finished call",
			finish: "tool_calls",
			tool:   "write_file",
			args:   `{"file_path":"references.bib","text":"` + longBody + `"}`,
		},
		{
			name:   "tool_calls path plus a large unknown field stays with the tool",
			finish: "tool_calls",
			tool:   "write_file",
			args:   `{"path":"references.bib","note":"` + longBody + `"}`,
		},
		{
			name:   "tool_calls empty old_string stays with the tool",
			finish: "Tool_Calls",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"","new_string":"` + longBody + `"}`,
		},
		{
			name:   "function_call finish keeps a path-only object",
			finish: "function_call",
			tool:   "write_file",
			args:   pathOnlyArgs(4001),
		},
		{
			name:     "stop and path only over 4000 bytes is still a cut object",
			finish:   "stop",
			tool:     "write_file",
			args:     pathOnlyArgs(4001),
			wantDrop: true,
		},
		{
			name:   "stop and path only at 4000 bytes stays with the tool",
			finish: "stop",
			tool:   "write_file",
			args:   pathOnlyArgs(4000),
		},
		{
			name:     "length and path only is a cut object",
			finish:   "length",
			tool:     "write_file",
			args:     `{"path":"references.bib"}`,
			wantDrop: true,
		},
		{
			name:   "max_tokens and empty content is a finished clear",
			finish: "max_tokens",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":""}`,
		},
		{
			name:   "length and null content stays with the tool",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":null}`,
		},
		{
			name:   "stop and null content over 4000 bytes stays with the tool",
			finish: "stop",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":null,"pad":"` + longBody + `"}`,
		},
		{
			name:   "length and content beside a null old_string is a whole-file write",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":"@article{new}","old_string":null}`,
		},
		{
			name:   "length and a complete edit is a finished call",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"@article{old}","new_string":"@article{new}"}`,
		},
		{
			name:   "length and a text body is a finished call",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","text":"@article{new}"}`,
		},
		{
			name:   "length and whitespace content is a finished call",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":"  \n"}`,
		},
		{
			name:   "stop and a complete edit over 4000 bytes is a finished call",
			finish: "stop",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"` + whitespacePassage + `","new_string":"@article{new}"}`,
		},
		{
			name:   "length and an empty old_string stays with the tool",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"","new_string":"@article{new}"}`,
		},
		{
			name:   "stop and an edit_file delete over 4000 bytes is a finished call",
			finish: "stop",
			tool:   "edit_file",
			args:   `{"path":"references.bib","old_string":"` + longPassage + `","new_string":""}`,
		},
		{
			name:   "length and whitespace old_string is a finished edit",
			finish: "length",
			tool:   "edit_file",
			args:   `{"path":"references.bib","old_string":" \n","new_string":"@article{new}"}`,
		},
		{
			name:   "length and a null edit partner stays with the tool",
			finish: "length",
			tool:   "write_file",
			args:   `{"path":"references.bib","old_string":"@article{old}","new_string":null}`,
		},
		{
			name:     "length and a missing new_string key is a cut edit",
			finish:   "length",
			tool:     "write_file",
			args:     `{"path":"references.bib","old_string":"@article{old}"}`,
			wantDrop: true,
		},
		{
			name:   "length and a null edit_file new_string stays with the tool",
			finish: "length",
			tool:   "edit_file",
			args:   `{"path":"references.bib","old_string":"@article{old}","new_string":null}`,
		},
		{
			name:   "tool_calls mixed content and edit stays with the tool",
			finish: "tool_calls",
			tool:   "write_file",
			args:   `{"path":"references.bib","content":"` + longBody + `","old_string":"@article{old}","new_string":"@article{new}"}`,
		},
		{
			name:     "invalid JSON stays truncated on tool_calls",
			finish:   "tool_calls",
			tool:     "write_file",
			args:     `{"path":"references.bib","content":"unterminated}`,
			wantDrop: true,
		},
		{
			name:     "length and an empty bash command is a cut call",
			finish:   "length",
			tool:     "bash",
			args:     `{"command":""}`,
			wantDrop: true,
		},
		{
			name:   "tool_calls and an empty bash command stays with the tool",
			finish: "tool_calls",
			tool:   "bash",
			args:   `{"command":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: ToolCallFunction{
					Name:      tt.tool,
					Arguments: tt.args,
				},
			}}}
			finish, truncated, truncatedArgs := filterStreamTruncatedToolCalls(msg, tt.finish)
			if finish != tt.finish {
				t.Fatalf("finish = %q, want %q", finish, tt.finish)
			}
			if tt.wantDrop {
				if len(truncated) != 1 || truncated[0] != tt.tool {
					t.Fatalf("truncated = %#v, want [%s]", truncated, tt.tool)
				}
				if len(msg.ToolCalls) != 0 {
					t.Fatalf("tool calls = %#v, want none", msg.ToolCalls)
				}
				if truncatedArgs[tt.tool] != strings.TrimSpace(tt.args) {
					t.Fatalf("truncated args missing for %s", tt.tool)
				}
				return
			}
			if len(truncated) != 0 || len(truncatedArgs) != 0 {
				t.Fatalf("truncated = %#v args = %#v, want none", truncated, truncatedArgs)
			}
			if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != tt.tool {
				t.Fatalf("tool calls = %#v, want the %s call kept", msg.ToolCalls, tt.tool)
			}
		})
	}
}

func TestFilterStreamTruncatedToolCallsDropsOnlyTheCutCall(t *testing.T) {
	msg := &Message{ToolCalls: []ToolCall{
		{
			ID:   "call_ok",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"wc -l references.bib"}`,
			},
		},
		{
			ID:   "call_cut",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"path":"references.bib"}`,
			},
		},
	}}
	_, truncated, _ := filterStreamTruncatedToolCalls(msg, "length")
	if len(truncated) != 1 || truncated[0] != "write_file" {
		t.Fatalf("truncated = %#v, want write_file", truncated)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("kept calls = %#v, want bash", msg.ToolCalls)
	}
}

func TestFilterStreamTruncatedToolCallsNilMessage(t *testing.T) {
	finish, truncated, truncatedArgs := FilterStreamTruncatedToolCalls(nil, "length")
	if finish != "length" || truncated != nil || truncatedArgs != nil {
		t.Fatalf("nil message = %q %#v %#v", finish, truncated, truncatedArgs)
	}
}

// pathOnlyArgs builds a path-only write_file object of exactly n bytes.
// The stop-finish size check uses a strict greater-than 4000.
func pathOnlyArgs(n int) string {
	const prefix = `{"path":"a.bib","pad":"`
	const suffix = `"}`
	pad := n - len(prefix) - len(suffix)
	if pad < 0 {
		panic("pathOnlyArgs size below the fixed envelope")
	}
	return prefix + strings.Repeat("p", pad) + suffix
}
