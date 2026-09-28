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

func TestReasoningStutterHaltsRotatingShortChant(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	stopped := 0
	f.stop = func() { stopped++ }
	cycle := []string{"Go.", "Final.", "OK.", "Writing.", "Done.", "Output.", "Alright.", "Let me write."}
	for i := 0; i < 80; i++ {
		f.Write(cycle[i%len(cycle)] + "\n\n")
	}
	f.Flush()
	if !f.Halted() || stopped != 1 {
		t.Fatalf("halted=%v stop=%d", f.Halted(), stopped)
	}
	if strings.Count(got.String(), "Let me write.") >= 8 {
		t.Fatalf("chant kept streaming: %d copies", strings.Count(got.String(), "Let me write."))
	}
}

func TestReasoningStutterChantSurvivesInterleavedDots(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	cycle := []string{"Go.", "Final.", "OK.", "Writing.", "Done.", "Output.", "Alright.", "Let me write."}
	for i := 0; i < 80; i++ {
		f.Write(cycle[i%len(cycle)] + "\n.\n")
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("dots between chant lines cleared the window")
	}
	if strings.Count(got.String(), "Let me write.") >= 8 {
		t.Fatalf("chant kept streaming: %d copies", strings.Count(got.String(), "Let me write."))
	}
}

func TestReasoningStutterHaltsChantBrokenByLongerLines(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	cycle := []string{"Go.", "Final.", "OK.", "Writing.", "Done.", "Output.", "Alright.", "Let me write."}
	var text strings.Builder
	for i := 0; i < 80; i++ {
		if i > 0 && i%5 == 0 {
			text.WriteString("Hmm, I'm stuck in a loop. Let me just write the reply.\n")
		}
		text.WriteString(cycle[i%len(cycle)] + "\n")
	}
	f.Write(text.String())
	f.Flush()
	if !f.Halted() {
		t.Fatal("longer lines between the same short words kept the chant going")
	}
	if strings.Count(got.String(), "Let me write.") >= 8 {
		t.Fatalf("chant kept streaming: %d copies", strings.Count(got.String(), "Let me write."))
	}
}

func TestReasoningStutterKeepsDistinctShortThoughts(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	var text strings.Builder
	for i := 0; i < reasoningChantWindow; i++ {
		text.WriteString(strings.Repeat(string(rune('a'+i%26)), 3) + " step " + strings.Repeat("x", i%9) + "\n")
	}
	f.Write(text.String())
	f.Flush()
	if f.Halted() {
		t.Fatal("different short thoughts halted")
	}
	if got.String() != text.String() {
		t.Fatalf("got %q", got.String())
	}
}

func TestReasoningStutterLongLineResetsChant(t *testing.T) {
	var got strings.Builder
	f := newReasoningStutterFilter(func(delta string) { got.WriteString(delta) })
	var text strings.Builder
	for i := 0; i < 20; i++ {
		text.WriteString("check backup " + strings.Repeat("n", i+1) + "\n")
	}
	text.WriteString("The user asked to delete redundant database backups and keep the newest binary backup.\n")
	for i := 0; i < 20; i++ {
		text.WriteString("read stat " + strings.Repeat("m", i+1) + "\n")
	}
	f.Write(text.String())
	f.Flush()
	if f.Halted() {
		t.Fatal("a real sentence between short lines still halted")
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
