package guiapp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestSkillStepOutputForModelKeepsSpilledHandle(t *testing.T) {
	body := strings.Repeat("uptime line\n", 400)
	footer := "\n" + toolresult.HandleFooterMarker + "\nid: handle-1\ntool: ssh\noriginal_bytes: 9000\n"
	got := skillStepOutputForModel(body+footer, 180)
	if !strings.Contains(got, "id: handle-1") || !strings.Contains(got, toolresult.HandleFooterMarker) {
		t.Fatalf("summary dropped the handle:\n%s", got)
	}
	if utf8.RuneCountInString(got) > 180 {
		t.Fatalf("summary runes=%d exceeds 180", utf8.RuneCountInString(got))
	}
}

func TestSkillStepOutputForModelKeepsHandleWhenBudgetIsTiny(t *testing.T) {
	footer := toolresult.HandleFooterMarker + "\nid: kept-id\ntool: ssh\nhint: page the rest\n"
	got := skillStepOutputForModel(strings.Repeat("metric\n", 50)+footer, 12)
	if !strings.Contains(got, "id: kept-id") || !strings.HasPrefix(got, toolresult.HandleFooterMarker) {
		t.Fatalf("tiny budget dropped the handle id:\n%s", got)
	}
}

func TestSkillSummaryKeepsHandleAfterBudgetIsSpent(t *testing.T) {
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		RunID:  "run-1",
		Skill:  "ssh-status",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{
			{Action: "bash", Status: skillStepStatusSuccess, Output: strings.Repeat("a", skillStepModelOutputRunes)},
			{Action: "bash", Status: skillStepStatusSuccess, Output: strings.Repeat("b", skillStepModelOutputRunes)},
			{Action: "ssh", Status: skillStepStatusSuccess, Output: "noise\n" + toolresult.HandleFooterMarker + "\nid: kept-id\n"},
		},
	}, "run-1")
	if !strings.Contains(b.String(), "id: kept-id") {
		t.Fatalf("summary dropped the handle after the budget was spent:\n%s", b.String())
	}
}

func TestProjectSkillSSHResultSpillsMiddle(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("banner\n", 200) + "SENTINEL_SKILL_SSH\n" + strings.Repeat("tail\n", 200)
	got := projectSkillSSHResult(raw, "owner-skill", dir)
	if !strings.Contains(got, toolresult.HandleFooterMarker) {
		t.Fatalf("expected a spilled handle, len=%d", len(got))
	}
	if len(got) > skillStepModelOutputRunes {
		t.Fatalf("preview %d exceeds skill summary budget %d", len(got), skillStepModelOutputRunes)
	}
	id := spilledHandleID(got)
	page, err := toolresult.Read(toolresult.ReadOptions{ID: id, SessionKey: "owner-skill", Root: dir, Limit: toolresult.DefaultReadLimit})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Content, "SENTINEL_SKILL_SSH") {
		t.Fatal("spilled skill output did not keep the middle")
	}
}

func TestProjectSkillSSHResultWithoutOwnerDoesNotAdvertiseHandle(t *testing.T) {
	raw := strings.Repeat("x", skillStepModelOutputRunes+100)
	got := projectSkillSSHResult(raw, "", t.TempDir())
	if strings.Contains(got, toolresult.HandleFooterMarker) {
		t.Fatal("anonymous skill output must not advertise a handle the reader cannot open")
	}
}
