package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestReasoningStutterHaltsDotRun(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	stopped := 0
	f.stop = func() { stopped++ }
	for i := 0; i < 40; i++ {
		f.Write(".\n")
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("dot run was forwarded")
	}
	if stopped != 1 {
		t.Fatalf("stop calls = %d, want 1", stopped)
	}
	lines := strings.Count(got.String(), ".")
	if lines >= reasoningStutterHaltCount {
		t.Fatalf("emitted %d dots, halt is %d", lines, reasoningStutterHaltCount)
	}
	if lines < reasoningStutterHaltCount-1 {
		t.Fatalf("emitted %d dots, want the lines before the halt", lines)
	}
	if f.SuppressedRunes() == 0 {
		t.Fatal("suppressed nothing")
	}
}

func TestReasoningStutterKeepsRealThought(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	text := "The PDF on disk has the photos.\nDeliver that file.\n"
	f.Write(text)
	f.Flush()
	if f.Halted() {
		t.Fatal("ordinary reasoning halted")
	}
	if got.String() != text {
		t.Fatalf("got %q", got.String())
	}
}

func TestRecoverReasoningStutterKeepsParsedToolCall(t *testing.T) {
	resp := &llm.Response{Choices: []llm.Choice{{
		Message: llm.Message{ToolCalls: []llm.ToolCall{{ID: "call-1"}}},
	}}}
	got, ok := recoverReasoningStutter(resp, nil, true)
	if !ok || len(got.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool call dropped: ok=%v resp=%v", ok, got)
	}
	if _, ok := recoverReasoningStutter(nil, context.Canceled, true); ok {
		t.Fatal("caller cancel was turned into an empty stop")
	}
	empty, ok := recoverReasoningStutter(nil, nil, true)
	if !ok || empty == nil || len(empty.Choices) != 1 {
		t.Fatal("halted stream without a tool call did not become an empty stop")
	}
}

func TestReasoningStutterKeepsRepeatedWords(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	var text strings.Builder
	for i := 0; i < 20; i++ {
		text.WriteString("OK\n")
	}
	f.Write(text.String())
	f.Flush()
	if f.Halted() {
		t.Fatal("repeated short words halted the reasoning stream")
	}
	if got.String() != text.String() {
		t.Fatalf("got %q", got.String())
	}
}

func TestReasoningStutterBlankLinesDoNotResetDotRun(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	for i := 0; i < 20; i++ {
		f.Write(".\n\n")
	}
	if !f.Halted() {
		t.Fatal("dots separated by blank lines were treated as fresh thoughts")
	}
}
