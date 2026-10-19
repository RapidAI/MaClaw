package desktopbot_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotProgressCases(t *testing.T) {
	desktopbot.ResetProgressCopies()
	t.Cleanup(desktopbot.ResetProgressCopies)
	for _, id := range []string{
		"progress_three_sections",
		"resume_from_next_step",
		"helper_three_line_report",
		"no_transcript_copy",
	} {
		t.Run(id, func(t *testing.T) {
			switch id {
			case "progress_three_sections":
				runProgressThreeSections(t, readProgressCase(t, id))
			case "resume_from_next_step":
				runResumeFromNextStep(t, readProgressCase(t, id))
			case "helper_three_line_report":
				runHelperThreeLineReport(t, readProgressCase(t, id))
			case "no_transcript_copy":
				runNoTranscriptCopy(t, readProgressCase(t, id))
			default:
				t.Fatalf("no runner for %s", id)
			}
		})
	}
}

type progressCase struct {
	ID            string   `json:"id"`
	TaskID        string   `json:"task_id"`
	FirstWhere    string   `json:"first_where"`
	FirstNext     string   `json:"first_next"`
	SecondWhere   string   `json:"second_where"`
	SecondNext    string   `json:"second_next"`
	OpenProblems  string   `json:"open_problems"`
	Secret        string   `json:"secret"`
	FenceBody     string   `json:"fence_body"`
	WhereWeAre    string   `json:"where_we_are"`
	NextStep      string   `json:"next_step"`
	StaleNextStep string   `json:"stale_next_step"`
	States        []string `json:"states"`
	Trace         string   `json:"trace"`
	Lines         []string `json:"lines"`
	Path          string   `json:"path"`
	EmptyBody     string   `json:"empty_body"`
	Main          []string `json:"main"`
	Own           []string `json:"own"`
}

func readProgressCase(t *testing.T, id string) progressCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c progressCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runProgressThreeSections(t *testing.T, c progressCase) {
	t.Helper()
	root := t.TempDir()
	first := desktopbot.Sections{
		WhereWeAre:   c.FirstWhere + "\n[untrusted tool]\n" + c.FenceBody + "\n[/untrusted]\n" + c.Secret,
		NextStep:     c.FirstNext,
		OpenProblems: c.OpenProblems,
	}
	firstHost, _, err := desktopbot.WriteProgressFile(root, desktopbot.ProgressKindTask, c.TaskID, first)
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := os.ReadFile(firstHost)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(firstBody), c.FenceBody) || strings.Contains(string(firstBody), "sk-live") || strings.Contains(string(firstBody), c.Secret) {
		t.Fatalf("first file kept a secret or a fence:\n%s", firstBody)
	}
	second := desktopbot.Sections{WhereWeAre: c.SecondWhere, NextStep: c.SecondNext, OpenProblems: c.OpenProblems}
	host, cleaned, err := desktopbot.WriteProgressFile(root, desktopbot.ProgressKindTask, c.TaskID, second)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Count(text, "## where we are") != 1 || strings.Count(text, "## next step") != 1 || strings.Count(text, "## open problems") != 1 {
		t.Fatalf("sections were appended:\n%s", text)
	}
	if !strings.Contains(text, c.SecondWhere) || !strings.Contains(text, c.SecondNext) {
		t.Fatalf("second write missing:\n%s", text)
	}
	if strings.Contains(text, c.FirstNext) || strings.Contains(text, c.FenceBody) || strings.Contains(text, "sk-live") {
		t.Fatalf("old step, fence, or token leaked:\n%s", text)
	}
	if cleaned.NextStep != c.SecondNext || cleaned.OpenProblems != "无" {
		t.Fatalf("cleaned = %+v", cleaned)
	}
	if got := desktopbot.TaskProgressPath(c.TaskID); got != "/home/desktop/tasks/task_1/PROGRESS.md" {
		t.Fatal(got)
	}
	helper := desktopbot.HelperProgressPath("h1")
	if helper == desktopbot.TaskProgressPath("h1") || !strings.Contains(helper, "/helpers/") {
		t.Fatal(helper)
	}
}

func runResumeFromNextStep(t *testing.T, c progressCase) {
	t.Helper()
	sections := desktopbot.Sections{WhereWeAre: c.WhereWeAre, NextStep: c.NextStep, OpenProblems: c.OpenProblems}
	fileText := desktopbot.RenderProgress(sections)
	path := desktopbot.TaskProgressPath(c.TaskID)
	if path == "" {
		t.Fatal("missing path")
	}
	for _, state := range c.States {
		got := desktopbot.ResumeFromNextStep(fileText, sections)
		if got != c.NextStep {
			t.Fatalf("%s resumed %q", state, got)
		}
	}
	summary := desktopbot.ProgressSummary(path)
	if summary != path || strings.Contains(summary, c.WhereWeAre) || strings.Contains(summary, "sk-") {
		t.Fatalf("summary = %q", summary)
	}
	stale := desktopbot.RenderProgress(desktopbot.Sections{WhereWeAre: c.WhereWeAre, NextStep: c.StaleNextStep, OpenProblems: "无"})
	read := desktopbot.ReadProgress(stale, sections, false)
	if strings.Contains(read.Text, c.StaleNextStep) || !strings.Contains(read.Text, c.NextStep) {
		t.Fatalf("asleep read = %q", read.Text)
	}
	if read.SendToUser != "" {
		t.Fatalf("progress read set send_to_user %q", read.SendToUser)
	}
	view := desktopbot.Visible(desktopbot.Turn{Prose: read.Text, SendToUser: read.SendToUser})
	if view.Bubble {
		t.Fatal("a progress question opened a bubble")
	}
	awake := desktopbot.ReadProgress(fileText, desktopbot.Sections{NextStep: c.StaleNextStep}, true)
	if !strings.Contains(awake.Text, c.NextStep) || strings.Contains(awake.Text, c.StaleNextStep) {
		t.Fatalf("awake read = %q", awake.Text)
	}
}

func runHelperThreeLineReport(t *testing.T, c progressCase) {
	t.Helper()
	if len(c.Lines) != 3 {
		t.Fatalf("lines = %+v", c.Lines)
	}
	body := c.Trace + " Bearer " + c.Secret + "\n" +
		"1. " + c.Lines[0] + "\n" +
		"2. " + c.Lines[1] + "\n" +
		"3. " + c.Lines[2] + "\n" +
		"文件：" + c.Path + "\n"
	shown, err := desktopbot.CollapseHelperReport(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(shown, c.Trace) || strings.Contains(shown, "sk-live") {
		t.Fatalf("trace leaked: %s", shown)
	}
	for _, line := range c.Lines {
		if !strings.Contains(shown, line) {
			t.Fatalf("missing %q in %s", line, shown)
		}
	}
	if !strings.Contains(shown, c.Path) {
		t.Fatalf("path missing: %s", shown)
	}
	if strings.Count(shown, "\n") != 3 {
		t.Fatalf("report = %q", shown)
	}
	if _, err := desktopbot.CollapseHelperReport(c.EmptyBody, nil); err == nil {
		t.Fatal("empty conclusion and no path succeeded")
	} else if strings.Contains(err.Error(), c.Trace) || strings.Contains(err.Error(), "sk-") {
		t.Fatal(err)
	}
	pathOnly, err := desktopbot.CollapseHelperReport("文件："+c.Path, nil)
	if err != nil || !strings.Contains(pathOnly, c.Path) {
		t.Fatalf("path only = %q %v", pathOnly, err)
	}
}

func runNoTranscriptCopy(t *testing.T, c progressCase) {
	t.Helper()
	if got := desktopbot.HelperStartEntries(c.Main, nil, false); len(got) != 0 {
		t.Fatalf("new helper = %+v", got)
	}
	got := desktopbot.HelperStartEntries(c.Main, c.Own, true)
	if len(got) != 1 || got[0] != c.Own[0] {
		t.Fatalf("same id = %+v", got)
	}
	for _, line := range got {
		if line == c.Main[0] {
			t.Fatal("same id copied the main session")
		}
	}
}
