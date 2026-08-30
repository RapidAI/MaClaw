package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestSemanticClassificationNeedsTaskContext(t *testing.T) {
	cases := []struct {
		name   string
		result *intent.ClassificationResult
		want   bool
	}{
		{"nil", nil, false},
		{"degraded", &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .9, Degraded: true}, true},
		{"unknown", &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .5}, true},
		{"empty primary", &intent.ClassificationResult{Confidence: .5}, true},
		{"managed office", &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .9}, false},
		{"managed search", &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .9}, false},
	}
	for _, tc := range cases {
		if got := semanticClassificationNeedsTaskContext(tc.result); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestMergeTaskContextTextComposesPriorTaskAndCurrent(t *testing.T) {
	got := mergeTaskContextText("再加上照片", []string{"生成庆祝布偶宝宝5岁生日的PPT"})
	if got != "生成庆祝布偶宝宝5岁生日的PPT；再加上照片" {
		t.Fatalf("merged=%q", got)
	}
	// No prior context → no merge.
	if got := mergeTaskContextText("南京天气", nil); got != "南京天气" {
		t.Fatalf("no-context merge must stay the bare text: %q", got)
	}
	// The current text is never duplicated when it equals the prior one.
	if got := mergeTaskContextText("继续", []string{"继续"}); got != "继续" {
		t.Fatalf("duplicate current: %q", got)
	}
}

func TestRecentUserTaskTextsKeepsUserRolesOnly(t *testing.T) {
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "生成生日PPT"},
		{Role: "assistant", Content: "好的，已生成"},
		{Role: "user", Content: "发给我"},
	}
	got := recentUserTaskTexts(entries, 2)
	if len(got) != 2 || got[0] != "生成生日PPT" || got[1] != "发给我" {
		t.Fatalf("user texts=%#v", got)
	}
}

// The merge retry must only fire after a failed bare classification, and must
// accept the merged verdict only when it lands on a managed route.
func TestClassifyWithTaskContextMerge(t *testing.T) {
	uic := intent.New(intent.Config{LLMFunc: func(_, text string) (string, error) {
		if strings.Contains(text, "PPT") {
			return `{"top":[{"skill":"office","score":0.9}]}`, nil
		}
		return `{"top":[{"skill":"unknown","score":0.2}]}`, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	history := []agent.ConversationEntry{{Role: "user", Content: "生成庆祝布偶宝宝5岁生日的PPT"}}

	merged, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "再加上照片"}, history, nil)
	if !ok || merged.Primary != intent.LabelOffice {
		t.Fatalf("merged=%+v ok=%v", merged, ok)
	}
	if !strings.Contains(merged.Reason, "task-context merge") {
		t.Fatalf("merged reason must carry the merge marker: %q", merged.Reason)
	}
	// No history → no merge.
	if _, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "再加上照片"}, nil, nil); ok {
		t.Fatal("merge without prior user task must not fire")
	}
}
