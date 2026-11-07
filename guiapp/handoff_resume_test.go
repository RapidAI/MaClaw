package guiapp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestCodingAgentResumeMemoSurvivesLaterDelta(t *testing.T) {
	dir := t.TempDir()
	userID := projectSessionOwnerID(dir)
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "did the work"},
	}, nil)
	memo := "<handoff-memo>\n## task\nkeep the number 17.4\n</handoff-memo>"
	recordCodingAgentResumeMemo(userID, dir, memo)
	recordCodingAgentResumeMemo(userID, dir, "  ")
	if got := loadCodingAgentResume(userID, dir); !strings.Contains(got.Memo, "17.4") {
		t.Fatalf("empty memo write cleared the file: %q", got.Memo)
	}
	recordCodingAgentResume(userID, dir, []agent.ConversationEntry{
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "continued"},
	}, nil)
	got := loadCodingAgentResume(userID, dir)
	if !strings.Contains(got.Memo, "17.4") {
		t.Fatalf("later delta dropped the memo: %q", got.Memo)
	}
	if len(got.History) < 2 {
		t.Fatalf("history=%d", len(got.History))
	}
	recordCodingAgentResume(userID, dir, nil, nil)
	after := loadCodingAgentResume(userID, dir)
	if after.Memo != got.Memo || len(after.History) != len(got.History) {
		t.Fatal("empty delta rewrote the resume file")
	}
}

func TestCodingModelHistoryOmitsTranscript(t *testing.T) {
	resume := codingAgentResume{
		History: []agent.ConversationEntry{{Role: "user", Content: "secret tool body 17.4"}},
		Memo:    "<handoff-memo>\n## progress\np50=12.5 exit 0\n</handoff-memo>",
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	if codingModelHistory(resume) == nil {
		t.Fatal("flag off must keep the transcript")
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	if codingModelHistory(resume) != nil {
		t.Fatal("flag on must not pass the stored transcript to the model")
	}
	strip := codingResumeStatusStrip(resume)
	if !strings.Contains(strip, "12.5") || strings.Contains(strip, "secret tool body") {
		t.Fatalf("strip=%q", strip)
	}
	upgrade := codingAgentResume{History: []agent.ConversationEntry{{Role: "user", Content: "ship 17.4 into notes"}}}
	projected := codingResumeStatusStrip(upgrade)
	if !strings.Contains(projected, "17.4") {
		t.Fatalf("empty memo should project from history: %q", projected)
	}
	if codingModelHistory(upgrade) != nil {
		t.Fatal("projected history must still stay off the model call")
	}
}

func TestFormatRemotePlanStepCarrySummaryKeepsVerify(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	var b strings.Builder
	b.WriteString(strings.Repeat("测", 450))
	b.WriteString("\nD:\\workprj\\aicoder\\bench_out.txt\n")
	b.WriteString(strings.Repeat("compiler warning line\n", 20))
	b.WriteString("median 17.4 ms\n")
	b.WriteString(strings.Repeat("more bench log\n", 10))
	b.WriteString("go test ./corelib/agent exit 0\n")
	got := formatRemotePlanStepCarrySummary(2, "测量", "success", b.String())
	header, body, ok := strings.Cut(got, "\n")
	if !ok || !strings.Contains(header, "still execute the CURRENT step fully") {
		t.Fatalf("header=%q", header)
	}
	if utf8.RuneCountInString(body) > 400 {
		t.Fatalf("body runes=%d", utf8.RuneCountInString(body))
	}
	for _, want := range []string{"exit 0", "17.4", "bench_out.txt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %q", want, body)
		}
	}
	if strings.Contains(body, "compiler warning") || strings.Contains(body, "more bench log") || strings.Contains(body, agent.HandoffRecoveryMarker) {
		t.Fatalf("body kept the log or a cut marker: %q", body)
	}
	var mem stickyCodingWorkbenchMemory
	setRemotePlanLastSummary(&mem, false, 1, "记录", "success", strings.Repeat("测", 900))
	if utf8.RuneCountInString(mem.LastSummary) != 900 {
		t.Fatalf("unplanned remote summary was cut: %d", utf8.RuneCountInString(mem.LastSummary))
	}
}

func TestHandoffStripDoesNotChangeOperationalOrGoalPrompt(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	var b strings.Builder
	b.WriteString(strings.Repeat("测", 200))
	b.WriteString("\nD:/work/bench_out.txt\n")
	b.WriteString(strings.Repeat("compiler warning line\n", 12))
	b.WriteString("median 17.4 ms\n")
	b.WriteString("go test ./corelib/agent exit 0\n")
	item := formatCodingCompletedStepResult(1, "测量", b.String())
	if !strings.HasPrefix(item, codingCompletedStepResultPrefix) {
		t.Fatalf("prefix changed: %q", item)
	}
	if strings.Contains(item, "compiler warning") || strings.Contains(item, agent.HandoffRecoveryMarker) {
		t.Fatalf("step strip kept the log: %q", item)
	}
	if !strings.Contains(item, "exit 0") || !strings.Contains(item, "17.4") || !strings.Contains(item, "bench_out.txt") {
		t.Fatalf("step strip dropped the delivery: %q", item)
	}
	opTask := &TaskItem{Index: 2, Title: "运行", RequestKind: codingRequestOperational}
	opMsg := (&codingSubAgentCallbacks{task: opTask, prevOutputs: []string{item}}).buildTaskUserMessage()
	if strings.Contains(opMsg, codingPriorDeliverableInputRule) || strings.Contains(opMsg, "compiler warning") || !strings.Contains(opMsg, "不要改代码") || !strings.Contains(opMsg, "exit 0") {
		t.Fatalf("operational prompt:\n%s", opMsg)
	}
	opPrompt := buildCodingSubAgentSystemPrompt(opTask, t.TempDir(), "", "", []string{item})
	if strings.Contains(opPrompt, codingPriorDeliverableInputRule) {
		t.Fatal("operational system prompt gained the prior-deliverable rule")
	}

	goal := &TaskItem{Index: 2, Title: "执行", Description: "[系统续接][execute]\n完成当前步骤", RequestKind: codingRequestImplementation}
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	off := (&codingSubAgentCallbacks{task: goal, prevOutputs: []string{item}}).buildTaskUserMessage()
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	on := (&codingSubAgentCallbacks{task: goal, prevOutputs: []string{item}}).buildTaskUserMessage()
	offLine, _, _ := strings.Cut(off, "\n")
	onLine, _, _ := strings.Cut(on, "\n")
	if offLine != onLine || strings.HasPrefix(onLine, codingCompletedStepResultPrefix) {
		t.Fatalf("goal first line changed: off=%q on=%q", offLine, onLine)
	}
	noted := insertCodingChecklistResumeNote(goal.Description, "【续跑】上次执行在中途停下。")
	if !strings.HasPrefix(noted, "[系统续接]") || strings.Index(noted, "【续跑】") < strings.Index(noted, "[系统续接]") {
		t.Fatalf("resume note moved ahead of the marker: %q", noted)
	}
}

func TestVerificationGateIgnoresDoneMemo(t *testing.T) {
	_ = agent.HandoffStripFromText("全部完成\ngo test ./... exit 0")
	status, summary := summarizeSubAgentVerification([]string{"a.go"}, nil, 1)
	if status != codingSubAgentQualityMissing {
		t.Fatalf("status=%v summary=%s", status, summary)
	}
}
