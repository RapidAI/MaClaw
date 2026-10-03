package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestPendingAnswerDoesNotMergePureDocumentDelivery(t *testing.T) {
	delivery := &intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}
	if pendingAnswerPrefersTaskMerge(delivery, true) {
		t.Fatal("pure document delivery was merged into the open task")
	}
	attachment := &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: 0.70, Layer: 3}
	if pendingAnswerPrefersTaskMerge(attachment, true) {
		t.Fatal("pure attachment delivery was merged into the open task")
	}
	if pendingAnswerPrefersTaskMerge(delivery, false) {
		t.Fatal("a delivery that is not a pending answer was merged")
	}
	weak := &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.40}
	if !pendingAnswerPrefersTaskMerge(weak, true) {
		t.Fatal("a weak unknown answer must still merge")
	}
	coding := &intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.80}
	if !pendingAnswerPrefersTaskMerge(coding, true) {
		t.Fatal("a weak coding answer must still merge")
	}
	mixed := &intent.ClassificationResult{
		Primary: intent.LabelDocumentDelivery, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: 0.70, Layer: 3,
	}
	if !pendingAnswerPrefersTaskMerge(mixed, true) {
		t.Fatal("delivery plus a lookup must still merge")
	}
}

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

	merged, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "再加上照片"}, history, nil, "")
	if !ok || merged.Primary != intent.LabelOffice {
		t.Fatalf("merged=%+v ok=%v", merged, ok)
	}
	if !strings.Contains(merged.Reason, "task-context merge") {
		t.Fatalf("merged reason must carry the merge marker: %q", merged.Reason)
	}
	// No history → no merge.
	if _, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "再加上照片"}, nil, nil, ""); ok {
		t.Fatal("merge without prior user task must not fire")
	}
	// Compacted history still classifies from the stored task summary.
	fromSummary, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "再加上照片"}, nil, nil, "生成庆祝布偶宝宝5岁生日的PPT")
	if !ok || fromSummary.Primary != intent.LabelOffice {
		t.Fatalf("summary merge=%+v ok=%v", fromSummary, ok)
	}
}

func TestTaskContextMergeRestateMarksOnlyTheOpenContext(t *testing.T) {
	uic := intent.New(intent.Config{LLMFunc: func(_, text string) (string, error) {
		switch {
		case strings.Contains(text, "跑一遍"):
			return `{"top":[{"skill":"ssh","score":0.90}]}`, nil
		case strings.Contains(text, "保存原始配置"):
			return `{"top":[{"skill":"file_write","score":0.92}]}`, nil
		case strings.Contains(text, "周报"):
			return `{"top":[{"skill":"office","score":0.90}]}`, nil
		default:
			return `{"top":[{"skill":"unknown","score":0.2}]}`, nil
		}
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}

	restated, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "已经解封"}, nil, nil, "更新api2服务器上的omniroute，保存原始配置")
	if !ok || restated.Primary != intent.LabelFileWrite {
		t.Fatalf("restate=%+v ok=%v", restated, ok)
	}
	if !strings.Contains(restated.Reason, "open-task restate") {
		t.Fatalf("open context was read as a new task: %q", restated.Reason)
	}

	switched, ok := h.classifyWithTaskContextMerge(context.Background(), IMUserMessage{UserID: "user-1", Text: "然后连上服务器跑一遍检查"}, nil, nil, "做一份项目周报")
	if !ok || switched.Primary != intent.LabelSSH {
		t.Fatalf("switch=%+v ok=%v", switched, ok)
	}
	if strings.Contains(switched.Reason, "open-task restate") {
		t.Fatalf("a sentence that named ssh was kept on the open document: %q", switched.Reason)
	}
}
