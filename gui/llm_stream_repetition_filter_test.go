package main

import (
	"strings"
	"testing"
)

func TestRepetitionFilter_NoRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	f.Write("你好，这是第一句话。这是第二句话。这是第三句话。")
	f.Flush()
	if f.Halted() {
		t.Fatal("expected no halt for non-repetitive text")
	}
	expected := "你好，这是第一句话。这是第二句话。这是第三句话。"
	if out.String() != expected {
		t.Fatalf("got %q, want %q", out.String(), expected)
	}
}

func TestRepetitionFilter_DetectsSingleSentenceRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	sentence := "这是一个足够长的句子，用来测试重复检测过滤器的行为是否正确！"
	// Write the same sentence 5 times
	for i := 0; i < 5; i++ {
		f.Write(sentence)
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt for repetitive text")
	}
	// The sentence should appear exactly repMaxConsecutive times (detection
	// happens after the Nth occurrence is emitted).
	count := strings.Count(out.String(), "是否正确！")
	if count != repMaxConsecutive {
		t.Fatalf("expected %d occurrences, got %d in output: %q", repMaxConsecutive, count, out.String())
	}
}

func TestRepetitionFilter_DetectsBlockRepetition(t *testing.T) {
	// This is the real-world pattern from the screenshot: a block of
	// multiple sentences repeating as a unit (A+B, A+B, A+B...).
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	block := "看起来截屏已经成功发送给你了。如果你需要我换一个PPT打开，或者做其他操作，随时告诉我！"
	// Write the block 5 times
	for i := 0; i < 5; i++ {
		f.Write(block)
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt for block-repetitive text")
	}
	// After halt, further writes should be suppressed.
	f.Write("这些内容应该被丢弃，因为过滤器已经停止了输出。")
	if f.SuppressedRunes() == 0 {
		t.Fatal("expected some suppressed runes after halt")
	}
}

func TestRepetitionFilter_StreamingTokenByToken(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	sentence := "看起来截屏已经成功发送给你了，如果你需要我换一个PPT打开，或者做其他操作，随时告诉我！"
	// Simulate streaming: write one rune at a time, repeating 4 times
	full := strings.Repeat(sentence, 4)
	for _, r := range full {
		f.Write(string(r))
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt for repetitive streaming text")
	}
}

func TestRepetitionFilter_ShortSentencesNotFiltered(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	// Short sentences (< repMinSentenceRunes) should not trigger filtering
	f.Write("好的。好的。好的。好的。好的。")
	f.Flush()
	if f.Halted() {
		t.Fatal("expected no halt for short repeated sentences")
	}
	expected := "好的。好的。好的。好的。好的。"
	if out.String() != expected {
		t.Fatalf("got %q, want %q", out.String(), expected)
	}
}

func TestRepetitionFilter_DifferentSentencesNotFiltered(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	f.Write("这是一个很长的第一句话，包含了足够多的内容来超过最小长度限制。")
	f.Write("这是一个完全不同的第二句话，也包含了足够多的内容来超过限制。")
	f.Write("第三句话又是不一样的内容，确保不会触发重复检测机制。")
	f.Flush()
	if f.Halted() {
		t.Fatal("expected no halt for different sentences")
	}
}

func TestRepetitionFilter_HaltedDropsAllSubsequent(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	sentence := "这是一个足够长的句子，用来测试重复检测过滤器的行为是否正确！"
	// Write enough times to trigger halt
	for i := 0; i < 5; i++ {
		f.Write(sentence)
		if f.Halted() {
			break
		}
	}
	if !f.Halted() {
		t.Fatal("expected halt after repetitions")
	}
	beforeLen := len(out.String())
	// Write more — should be silently dropped
	f.Write("这些内容应该被丢弃，因为过滤器已经停止了。")
	f.Write("这些也应该被丢弃，不会出现在输出中。")
	f.Flush()
	if len(out.String()) != beforeLen {
		t.Fatalf("expected no additional output after halt, got %d extra bytes", len(out.String())-beforeLen)
	}
}

func TestRepetitionFilter_MixedContentWithRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	f.Write("我用的是内置的 screenshot 工具截屏的。这个工具会直接调用系统 API 截取当前桌面屏幕画面，然后以图片形式发送给你。")
	// Now repeat a block
	block := "看起来截屏已经成功发送给你了。如果你需要我换一个PPT打开，或者做其他操作，随时告诉我！"
	for i := 0; i < 5; i++ {
		f.Write(block)
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt after repeated block in mixed content")
	}
	// The first unique content should be present
	if !strings.Contains(out.String(), "screenshot 工具截屏的") {
		t.Fatal("expected first unique sentence to be present")
	}
}

func TestRepetitionFilter_WhitespaceNormalization(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	for i := 0; i < 3; i++ {
		if i%2 == 0 {
			f.Write("这是一个足够长的句子，用来测试  空格  归一化  的行为是否正确！")
		} else {
			f.Write("这是一个足够长的句子，用来测试 空格 归一化 的行为是否正确！")
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt — sentences differ only in whitespace")
	}
}

func TestRepetitionFilter_ThreeSentenceBlock(t *testing.T) {
	// Test a repeating block of 3 sentences (A, B, C, A, B, C).
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	a := "这是第一个足够长的句子，包含了很多内容来确保超过最小长度。"
	b := "这是第二个足够长的句子，也包含了很多内容来确保超过限制。"
	c := "这是第三个足够长的句子，同样包含了很多内容来确保检测。"
	for i := 0; i < 3; i++ {
		f.Write(a)
		f.Write(b)
		f.Write(c)
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt for 3-sentence block repetition")
	}
}

func TestRepetitionFilter_FourSentenceBlock(t *testing.T) {
	// Test a repeating block of 4 sentences.
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	sentences := []string{
		"第一个足够长的句子，包含了很多内容来确保超过最小长度限制。",
		"第二个足够长的句子，也包含了很多内容来确保超过最小限制。",
		"第三个足够长的句子，同样包含了很多内容来确保能被检测到。",
		"第四个足够长的句子，最后一个也包含了足够多的内容来测试。",
	}
	for rep := 0; rep < 3; rep++ {
		for _, s := range sentences {
			f.Write(s)
			if f.Halted() {
				break
			}
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()
	if !f.Halted() {
		t.Fatal("expected halt for 4-sentence block repetition")
	}
}

func TestNormalizeSentence(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  world  ", "hello world"},
		{"no\t\textra\n\nspaces", "no extra spaces"},
		{"already clean", "already clean"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeSentence(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeSentence(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSentenceBoundary(t *testing.T) {
	boundaries := []rune{'。', '！', '？', '!', '?'}
	for _, r := range boundaries {
		if !sentenceBoundary(r) {
			t.Errorf("expected %q to be a sentence boundary", string(r))
		}
	}
	nonBoundaries := []rune{',', '，', '.', '、', ' ', 'a', '中', '\n'}
	for _, r := range nonBoundaries {
		if sentenceBoundary(r) {
			t.Errorf("expected %q to NOT be a sentence boundary", string(r))
		}
	}
}

func TestDetectRepetition_PatternLength1(t *testing.T) {
	s := "这是一个足够长的句子，用来测试重复检测过滤器的行为是否正确"
	window := []string{s, s}
	if !detectRepetition(window, repMaxPatternLen) {
		t.Fatal("expected repetition detected for pattern length 1")
	}
}

func TestDetectRepetition_PatternLength2(t *testing.T) {
	a := "第一个足够长的句子，包含了很多内容来确保超过最小长度限制"
	b := "第二个足够长的句子，也包含了很多内容来确保超过最小限制"
	window := []string{a, b, a, b}
	if !detectRepetition(window, repMaxPatternLen) {
		t.Fatal("expected repetition detected for pattern length 2")
	}
}

func TestDetectRepetition_NoRepetition(t *testing.T) {
	window := []string{
		"第一个足够长的句子，包含了很多内容来确保超过最小长度限制",
		"第二个足够长的句子，也包含了很多内容来确保超过最小限制",
		"第三个足够长的句子，同样包含了很多内容来确保能被检测到",
	}
	if detectRepetition(window, repMaxPatternLen) {
		t.Fatal("expected no repetition for different sentences")
	}
}

func TestRepetitionFilter_CodeBlockNotFiltered(t *testing.T) {
	// Code blocks often have repeated structural patterns (imports, function
	// signatures). The filter should not trigger on these because code lines
	// end with \n (not a sentence boundary) and rarely with 。！？.
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	code := `package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println("hello")
	fmt.Println("hello")
	fmt.Println("hello")
	fmt.Println("hello")
	fmt.Println("hello")
}
`
	f.Write(code)
	f.Flush()
	if f.Halted() {
		t.Fatal("expected no halt for code block with repeated lines")
	}
	if out.String() != code {
		t.Fatalf("expected code to pass through unchanged")
	}
}

func TestRepetitionFilter_MarkdownListNotFiltered(t *testing.T) {
	// Markdown numbered lists can have similar-looking items.
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })
	list := "以下是任务列表：\n1. 实现用户登录功能\n2. 实现用户注册功能\n3. 实现密码重置功能\n4. 实现用户登录功能\n"
	f.Write(list)
	f.Flush()
	if f.Halted() {
		t.Fatal("expected no halt for markdown list")
	}
}
