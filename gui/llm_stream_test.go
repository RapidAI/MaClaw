package main

import (
	"strings"
	"testing"
	"time"
)

func TestGuiSSEIdleTimeoutIsConservative(t *testing.T) {
	if guiSSEIdleTimeout < 4*time.Minute {
		t.Fatalf("guiSSEIdleTimeout = %s, want at least 4m", guiSSEIdleTimeout)
	}
}

func TestThinkFilter_BasicBlock(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>reasoning here</think>Hello world")
	tf.Flush()
	if got := out.String(); got != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", got)
	}
}

func TestThinkFilter_SplitOpenTag(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("Hi <thi")
	tf.Write("nk>secret</think> there")
	tf.Flush()
	if got := out.String(); got != "Hi there" {
		t.Errorf("expected %q, got %q", "Hi there", got)
	}
}

func TestThinkFilter_SplitCloseTag(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>secret</thi")
	tf.Write("nk>visible")
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_NoThinkTags(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("just normal text")
	tf.Flush()
	if got := out.String(); got != "just normal text" {
		t.Errorf("expected %q, got %q", "just normal text", got)
	}
}

func TestThinkFilter_MultipleBlocks(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>a</think>X<think>b</think>Y")
	tf.Flush()
	if got := out.String(); got != "XY" {
		t.Errorf("expected %q, got %q", "XY", got)
	}
}

func TestThinkFilter_CharByChar(t *testing.T) {
	// Simulate extreme fragmentation: one char per delta
	input := "<think>hidden</think>visible"
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		tf.Write(string(c))
	}
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_TrailingWhitespace(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>x</think>\n\nHello")
	tf.Flush()
	if got := out.String(); got != "Hello" {
		t.Errorf("expected %q, got %q", "Hello", got)
	}
}

func TestThinkFilter_PartialOpenNeverCompleted(t *testing.T) {
	// "<thi" at end of stream is not a real tag — should be emitted
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("hello <thi")
	tf.Flush()
	if got := out.String(); got != "hello <thi" {
		t.Errorf("expected %q, got %q", "hello <thi", got)
	}
}

func TestThinkFilter_LongThinkBlock(t *testing.T) {
	// Simulate a long reasoning block delivered in many small chunks.
	// The filter should not accumulate unbounded buffer.
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>")
	for i := 0; i < 1000; i++ {
		tf.Write("reasoning reasoning reasoning ")
	}
	tf.Write("</think>Answer")
	tf.Flush()
	if got := out.String(); got != "Answer" {
		t.Errorf("expected %q, got %q", "Answer", got)
	}
}

func TestThinkFilter_CloseTagSplitAcrossManyChunks(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>secret")
	tf.Write("</")
	tf.Write("th")
	tf.Write("ink>")
	tf.Write("visible")
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_FalseAlarmPartialTag(t *testing.T) {
	// Text ending with "<" that is NOT a think tag
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("a < b")
	tf.Flush()
	if got := out.String(); got != "a < b" {
		t.Errorf("expected %q, got %q", "a < b", got)
	}
}

// ---------------------------------------------------------------------------
// funcCallFilter tests
// ---------------------------------------------------------------------------

func TestFuncCallFilter_FullBlock(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`hello<|FunctionCallBegin|>[{"name":"set_nickname"}]<|FunctionCallEnd|>`)
	f.Flush()
	if got := out.String(); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestFuncCallFilter_TextAfterBlock(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`<|FunctionCallBegin|>stuff<|FunctionCallEnd|>继续`)
	f.Flush()
	if got := out.String(); got != "继续" {
		t.Errorf("expected %q, got %q", "继续", got)
	}
}

func TestFuncCallFilter_SplitAcrossChunks(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("好的<|FunctionCall")
	f.Write("Begin|>中间<|FunctionCallEnd|>后面")
	f.Flush()
	if got := out.String(); got != "好的后面" {
		t.Errorf("expected %q, got %q", "好的后面", got)
	}
}

func TestFuncCallFilter_NoMarkers(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("normal text")
	f.Flush()
	if got := out.String(); got != "normal text" {
		t.Errorf("expected %q, got %q", "normal text", got)
	}
}

func TestFuncCallFilter_MultipleBlocks(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("a<|FunctionCallBegin|>x<|FunctionCallEnd|>b<|FunctionCallBegin|>y<|FunctionCallEnd|>c")
	f.Flush()
	if got := out.String(); got != "abc" {
		t.Errorf("expected %q, got %q", "abc", got)
	}
}

func TestFuncCallFilter_CharByChar(t *testing.T) {
	input := "前<|FunctionCallBegin|>中<|FunctionCallEnd|>后"
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		f.Write(string(c))
	}
	f.Flush()
	if got := out.String(); got != "前后" {
		t.Errorf("expected %q, got %q", "前后", got)
	}
}

func TestStripFunctionCalls(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello world", "hello world"},
		{`好的<|FunctionCallBegin|>[{"name":"x"}]<|FunctionCallEnd|>`, "好的"},
		{`a<|FunctionCallBegin|>x<|FunctionCallEnd|>b<|FunctionCallBegin|>y<|FunctionCallEnd|>c`, "abc"},
	}
	for _, tt := range tests {
		got := stripFunctionCalls(tt.input)
		if got != tt.want {
			t.Errorf("stripFunctionCalls(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestThinkAndFuncCallFilterChained(t *testing.T) {
	// Simulate the real chain: thinkFilter -> funcCallFilter -> output
	var out strings.Builder
	fcf := newFuncCallFilter(func(s string) { out.WriteString(s) })
	tf := newThinkFilter(func(s string) { fcf.Write(s) })
	tf.Write("<think>reasoning</think>好的<|FunctionCallBegin|>call<|FunctionCallEnd|>结果")
	tf.Flush()
	fcf.Flush()
	if got := out.String(); got != "好的结果" {
		t.Errorf("expected %q, got %q", "好的结果", got)
	}
}
