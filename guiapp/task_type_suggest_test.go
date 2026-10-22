package guiapp

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestNewTaskCommandForTypeKeepsTheInstruction(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  修复登录空指针  ", "修复登录空指针"},
		{"\uFEFF/goal 修复登录", "修复登录"},
		{"/GOAL   Fix login", "Fix login"},
		{"／goal 写周报", "写周报"},
		{"/goal\t修复登录", "修复登录"},
		{"/goal", ""},
		{"/goal pause", ""},
		{"/goal 暂停", ""},
		{"/goaling", ""},
		{"/goal修复", ""},
		{"/GOAL修复登录", ""},
		{"/skill install foo", ""},
		{"／clear", ""},
		{"请 /goal 一下", "请 /goal 一下"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := newTaskCommandForType(tc.in); got != tc.want {
			t.Errorf("newTaskCommandForType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("修", newTaskCommandMaxRunes+20)
	if got := newTaskCommandForType(long); len([]rune(got)) != newTaskCommandMaxRunes {
		t.Fatalf("long command runes = %d, want %d", len([]rune(got)), newTaskCommandMaxRunes)
	}
}

func TestSuggestNewTaskTypeEmptyDoesNotClassify(t *testing.T) {
	if got := (*App)(nil).SuggestNewTaskType("修复登录"); got.Confident || got.Suggested != "" {
		t.Fatalf("nil app = %#v", got)
	}
	if got := (&App{}).SuggestNewTaskType("   /goal pause  "); got.Confident || got.Suggested != "" {
		t.Fatalf("session verb = %#v", got)
	}
	if got := (&App{}).SuggestNewTaskType("/goaling"); got.Confident || got.Suggested != "" {
		t.Fatalf("glued goal = %#v", got)
	}
}

func TestTaskTypeFromClassification(t *testing.T) {
	coding := intent.ClassificationResult{
		Primary:            intent.LabelCoding,
		Confidence:         0.86,
		EmbeddingEarlyExit: true,
	}
	if got, ok := taskTypeFromClassification(coding); !ok || got != "coding" {
		t.Fatalf("coding = %q %v", got, ok)
	}
	bug := coding
	bug.Primary = intent.LabelBugFix
	bug.Confidence = 0.80
	if got, ok := taskTypeFromClassification(bug); !ok || got != "coding" {
		t.Fatalf("bug_fix = %q %v", got, ok)
	}
	weak := coding
	weak.Confidence = 0.77
	if _, ok := taskTypeFromClassification(weak); ok {
		t.Fatal("coding below 0.78 must not suggest")
	}
	noRival := coding
	noRival.EmbeddingEarlyExit = false
	noRival.Confidence = 0.95
	if got, ok := taskTypeFromClassification(noRival); !ok || got != "coding" {
		t.Fatalf("coding with no rival = %q %v", got, ok)
	}
	sameFamily := coding
	sameFamily.EmbeddingEarlyExit = false
	sameFamily.Primary = intent.LabelBugFix
	sameFamily.Confidence = 0.84
	sameFamily.RunnerUp = intent.LabelCoding
	sameFamily.RunnerUpScore = 0.76
	if got, ok := taskTypeFromClassification(sameFamily); !ok || got != "coding" {
		t.Fatalf("same-family coding = %q %v", got, ok)
	}
	cross := sameFamily
	cross.RunnerUp = intent.LabelFileWrite
	cross.RunnerUpScore = 0.76
	if _, ok := taskTypeFromClassification(cross); ok {
		t.Fatal("a chat runner-up must not settle a coding lead")
	}
	tight := sameFamily
	tight.RunnerUpScore = 0.80
	if _, ok := taskTypeFromClassification(tight); ok {
		t.Fatal("a gap under 0.06 must not settle without an early exit")
	}
	unmappedRunner := sameFamily
	unmappedRunner.RunnerUp = intent.LabelShellCommand
	unmappedRunner.RunnerUpScore = 0.70
	if got, ok := taskTypeFromClassification(unmappedRunner); !ok || got != "coding" {
		t.Fatalf("unmapped runner = %q %v", got, ok)
	}
	degraded := coding
	degraded.Degraded = true
	if _, ok := taskTypeFromClassification(degraded); ok {
		t.Fatal("a degraded embedding must not suggest")
	}
	weather := intent.ClassificationResult{
		Primary:            intent.LabelLiveData,
		Confidence:         0.72,
		EmbeddingEarlyExit: true,
	}
	if got, ok := taskTypeFromClassification(weather); !ok || got != "chat" {
		t.Fatalf("live_data = %q %v", got, ok)
	}
	weakChat := intent.ClassificationResult{
		Primary:       intent.LabelSearch,
		Confidence:    0.74,
		RunnerUp:      intent.LabelLiveData,
		RunnerUpScore: 0.66,
	}
	if _, ok := taskTypeFromClassification(weakChat); ok {
		t.Fatal("a weak chat agreement must not suggest")
	}
	chatAgree := weakChat
	chatAgree.Confidence = 0.76
	chatAgree.RunnerUpScore = 0.68
	if got, ok := taskTypeFromClassification(chatAgree); !ok || got != "chat" {
		t.Fatalf("same-family chat = %q %v", got, ok)
	}
	file := intent.ClassificationResult{
		Primary:            intent.LabelFileWrite,
		Confidence:         0.93,
		EmbeddingEarlyExit: true,
	}
	if got, ok := taskTypeFromClassification(file); !ok || got != "chat" {
		t.Fatalf("file_write = %q %v, want chat", got, ok)
	}
	doc := intent.ClassificationResult{
		Primary:       intent.LabelDocumentGenerate,
		Confidence:    0.91,
		RunnerUp:      intent.LabelOffice,
		RunnerUpScore: 0.80,
	}
	if got, ok := taskTypeFromClassification(doc); !ok || got != "chat" {
		t.Fatalf("document without early exit = %q %v", got, ok)
	}
	doc.Secondary = []intent.IntentLabel{intent.LabelCoding}
	if _, ok := taskTypeFromClassification(doc); ok {
		t.Fatal("a coding companion must not settle as chat")
	}
	shell := intent.ClassificationResult{
		Primary:            intent.LabelShellCommand,
		Confidence:         0.95,
		EmbeddingEarlyExit: true,
	}
	if _, ok := taskTypeFromClassification(shell); ok {
		t.Fatal("shell_command must wait for the command reading")
	}
	ssh := shell
	ssh.Primary = intent.LabelSSH
	if _, ok := taskTypeFromClassification(ssh); ok {
		t.Fatal("ssh must wait for the command reading")
	}
	for _, label := range []intent.IntentLabel{
		intent.LabelMaintenance, intent.LabelGitMutate, intent.LabelGitInspect, intent.LabelDelegateTask,
	} {
		result := coding
		result.Primary = label
		if got, ok := taskTypeFromClassification(result); !ok || got != "coding" {
			t.Fatalf("%s = %q %v", label, got, ok)
		}
	}
	for _, label := range []intent.IntentLabel{
		intent.LabelSearch, intent.LabelDocumentGenerate, intent.LabelOffice, intent.LabelGoalManage, intent.LabelNonCoding,
	} {
		result := weather
		result.Primary = label
		result.Confidence = 0.80
		if got, ok := taskTypeFromClassification(result); !ok || got != "chat" {
			t.Fatalf("%s = %q %v", label, got, ok)
		}
	}
}

func TestTaskTypeFromHeadRejectsNonFinite(t *testing.T) {
	if _, ok := taskTypeFromHead("coding", math.NaN(), 0.5); ok {
		t.Fatal("NaN head probability must not suggest")
	}
	if _, ok := taskTypeFromHead("coding", 1.2, 0.5); ok {
		t.Fatal("a head probability above 1 must not suggest")
	}
	if got, ok := taskTypeFromHead("bug_fix", 0.9, 0.6); !ok || got != "coding" {
		t.Fatalf("head = %q %v", got, ok)
	}
}

func TestSuggestNewTaskTypePrefersLocalThenHeadThenLLM(t *testing.T) {
	local := intent.ClassificationResult{
		Primary:            intent.LabelCoding,
		Confidence:         0.9,
		EmbeddingEarlyExit: true,
		Reason:             "embedding",
	}
	got := suggestNewTaskType("修复登录", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return local, embeddingReady
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			t.Fatal("head must not run after a confident embedding")
			return "", 0, 0, false
		},
		askLLM: func(string) (string, bool) {
			t.Fatal("LLM must not run after a confident embedding")
			return "", false
		},
	})
	if !got.Confident || got.Suggested != "coding" || got.Source != "local" {
		t.Fatalf("local = %#v", got)
	}

	sameFamily := intent.ClassificationResult{
		Primary:       intent.LabelBugFix,
		Confidence:    0.86,
		RunnerUp:      intent.LabelCoding,
		RunnerUpScore: 0.78,
	}
	got = suggestNewTaskType("修复登录空指针", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return sameFamily, embeddingReady
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			t.Fatal("head must not run after a same-family embedding")
			return "", 0, 0, false
		},
		askLLM: func(string) (string, bool) {
			t.Fatal("LLM must not run after a same-family embedding")
			return "", false
		},
	})
	if !got.Confident || got.Suggested != "coding" || got.Source != "local" {
		t.Fatalf("same-family = %#v", got)
	}

	var asked string
	got = suggestNewTaskType("跑一下这个命令", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return intent.ClassificationResult{
				Primary:            intent.LabelShellCommand,
				Confidence:         0.91,
				EmbeddingEarlyExit: true,
				QueryVec:           []float32{0.25},
				AnchorMaxCos:       map[string]float64{"shell_command": 0.91},
			}, embeddingReady
		},
		probe: func(_ context.Context, _ string, prior intent.ClassificationResult) (string, float64, float64, bool) {
			if len(prior.QueryVec) != 1 || prior.AnchorMaxCos["shell_command"] != 0.91 {
				t.Fatalf("head did not receive the embedding pass: vec=%v scores=%v", prior.QueryVec, prior.AnchorMaxCos)
			}
			return "shell_command", 0.99, 0.5, true
		},
		askLLM: func(text string) (string, bool) {
			asked = text
			return `{"type":"coding","confidence":0.91}`, true
		},
	})
	if asked != "跑一下这个命令" || !got.Confident || got.Suggested != "coding" || got.Source != "llm" {
		t.Fatalf("llm fallback = %#v asked=%q", got, asked)
	}

	got = suggestNewTaskType("写周报", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return intent.ClassificationResult{Primary: intent.LabelUnknown, Degraded: true}, embeddingReady
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			return "document_generate", 0.82, 0.60, true
		},
		askLLM: func(string) (string, bool) {
			t.Fatal("LLM must not run after a confident head")
			return "", false
		},
	})
	if !got.Confident || got.Suggested != "chat" || got.Source != "local" {
		t.Fatalf("head = %#v", got)
	}

	probed := false
	got = suggestNewTaskType("帮我看看这个报错", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return intent.ClassificationResult{}, embeddingUnavailable
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			probed = true
			return "", 0, 0, false
		},
		askLLM: func(string) (string, bool) { return `{"type":"unsure","confidence":0.4}`, true },
	})
	if !probed || got.Confident || got.Suggested != "" {
		t.Fatalf("unsure = %#v probed=%v", got, probed)
	}

	probed = false
	got = suggestNewTaskType("实现一个 REST 接口", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			return intent.ClassificationResult{}, embeddingTimeout
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			probed = true
			return "coding", 0.99, 0.5, true
		},
		askLLM: func(string) (string, bool) {
			return `{"type":"coding","confidence":0.9}`, true
		},
	})
	if probed || !got.Confident || got.Suggested != "coding" || got.Source != "llm" {
		t.Fatalf("timeout = %#v probed=%v", got, probed)
	}

	got = suggestNewTaskType("继续", taskTypeSuggestDeps{
		classify: func(string) (intent.ClassificationResult, embeddingRead) {
			t.Fatal("a thin command must not classify")
			return intent.ClassificationResult{}, embeddingUnavailable
		},
		probe: func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool) {
			t.Fatal("a thin command must not probe")
			return "", 0, 0, false
		},
		askLLM: func(string) (string, bool) {
			t.Fatal("a thin command must not call the LLM")
			return `{"type":"coding","confidence":0.99}`, false
		},
	})
	if got.Confident || got.Suggested != "" {
		t.Fatalf("thin = %#v", got)
	}
}

func TestParseNewTaskTypeReply(t *testing.T) {
	suggested, confidence, ok := parseNewTaskTypeReply("```json\n{\"type\":\"编程\",\"confidence\":\"0.86\"}\n```")
	if !ok || suggested != "coding" || confidence < 0.86-0.001 {
		t.Fatalf("chinese type = %q %v %v", suggested, confidence, ok)
	}
	if _, _, ok := parseNewTaskTypeReply(`{"type":"chat","confidence":0.69}`); ok {
		t.Fatal("confidence under 0.70 must not suggest")
	}
	if _, _, ok := parseNewTaskTypeReply(`{"type":"coding","confidence":1.5}`); ok {
		t.Fatal("confidence above 1 must not suggest")
	}
	if _, _, ok := parseNewTaskTypeReply(`{"type":"coding","confidence":"NaN"}`); ok {
		t.Fatal("NaN confidence must not suggest")
	}
	if _, _, ok := parseNewTaskTypeReply(`{"type":"coding","confidence":"Inf"}`); ok {
		t.Fatal("infinite confidence must not suggest")
	}
	suggested, _, ok = parseNewTaskTypeReply(`{"type":"chat","confidence":0.70}`)
	if !ok || suggested != "chat" {
		t.Fatalf("floor = %q %v", suggested, ok)
	}
	suggested, _, ok = parseNewTaskTypeReply(`note {"type":"会话","confidence":0.8} trailing`)
	if !ok || suggested != "chat" {
		t.Fatalf("wrapped chat = %q %v", suggested, ok)
	}
	if _, _, ok := parseNewTaskTypeReply("not json"); ok {
		t.Fatal("prose must not suggest")
	}
}
