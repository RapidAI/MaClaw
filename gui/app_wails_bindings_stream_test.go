package main

import (
	"strings"
	"testing"
)

func TestAIAssistantStreamDeltaNormalizerKeepsDeltas(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	inputs := []string{"hello", " ", "world", "!"}
	var got string
	for _, input := range inputs {
		got += normalizer.Normalize(input)
	}
	if got != "hello world!" {
		t.Fatalf("normalized stream = %q, want %q", got, "hello world!")
	}
}

func TestAIAssistantStreamDeltaNormalizerConvertsSnapshotsToDeltas(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	first := "Step 1: update step2b_detect_nontext_pii prompt to request bbox coordinates."
	second := first + "\n\nFix 1: broaden the non-text detection marker."

	gotFirst := normalizer.Normalize(first)
	gotSecond := normalizer.Normalize(second)

	if gotFirst != first {
		t.Fatalf("first delta = %q, want %q", gotFirst, first)
	}
	if gotSecond != "\n\nFix 1: broaden the non-text detection marker." {
		t.Fatalf("second delta = %q", gotSecond)
	}
}

func TestAIAssistantStreamDeltaNormalizerHandlesUnicodeOverlap(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	unicodeToken := "\u5750\u6807"
	first := "prefix " + strings.Repeat(unicodeToken, 14)
	second := strings.Repeat(unicodeToken, 14) + " suffix"

	if got := normalizer.Normalize(first); got != first {
		t.Fatalf("first delta = %q, want %q", got, first)
	}
	if got := normalizer.Normalize(second); got != " suffix" {
		t.Fatalf("unicode overlap delta = %q, want %q", got, " suffix")
	}
}

func TestAIAssistantStreamDeltaNormalizerDropsDuplicateSnapshots(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	first := "Now I have a clear plan. Let me make the changes step by step."
	second := first + "\nI will update the prompt, run tests, and summarize the result."

	if got := normalizer.Normalize(first); got != first {
		t.Fatalf("first delta = %q, want %q", got, first)
	}
	if got := normalizer.Normalize(second); got != "\nI will update the prompt, run tests, and summarize the result." {
		t.Fatalf("snapshot extension delta = %q", got)
	}
	if got := normalizer.Normalize(second); got != "" {
		t.Fatalf("duplicate snapshot delta = %q, want empty", got)
	}
}

func TestAIAssistantStreamDeltaNormalizerPreservesRepeatedLongDeltas(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	text := "Repeat this exact substantial sentence intentionally.\n"

	if got := normalizer.Normalize(text); got != text {
		t.Fatalf("first delta = %q, want %q", got, text)
	}
	if got := normalizer.Normalize(text); got != text {
		t.Fatalf("intentional repeated delta = %q, want %q", got, text)
	}
}

func TestAIAssistantStreamDeltaNormalizerSeparatesReasoningAndContent(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	reasoningFirst := "\x01Analyze the issue and draft the implementation plan."
	reasoningSecond := reasoningFirst + "\nContinue checking the code path."
	content := "Final answer"

	if got := normalizer.Normalize(reasoningFirst); got != reasoningFirst {
		t.Fatalf("first reasoning delta = %q, want %q", got, reasoningFirst)
	}
	if got := normalizer.Normalize(reasoningSecond); got != "\x01\nContinue checking the code path." {
		t.Fatalf("second reasoning delta = %q", got)
	}
	if got := normalizer.Normalize(content); got != content {
		t.Fatalf("content delta = %q, want %q", got, content)
	}
}

func TestAIAssistantStreamDeltaNormalizerResetStartsNewRound(t *testing.T) {
	var normalizer aiAssistantStreamDeltaNormalizer
	first := "Shared prefix from the first assistant round."
	firstSnapshot := first + "\nMore text in the first round."
	secondRound := first + "\nDifferent text in the second round."

	if got := normalizer.Normalize(first); got != first {
		t.Fatalf("first delta = %q, want %q", got, first)
	}
	if got := normalizer.Normalize(firstSnapshot); got != "\nMore text in the first round." {
		t.Fatalf("first snapshot delta = %q", got)
	}

	normalizer.Reset()

	if got := normalizer.Normalize(secondRound); got != secondRound {
		t.Fatalf("second round delta = %q, want %q", got, secondRound)
	}
}
