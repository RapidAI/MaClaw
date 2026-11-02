package desktopbot_test

import (
	"encoding/json"
	"fmt"
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

func TestCollapseHelperReportKeepsSpacesInOnePath(t *testing.T) {
	path := `C:\Users\ma139\Desktop\演讲稿 完整版.docx`
	shown, err := desktopbot.CollapseHelperReport("文件："+path, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(shown, "演讲稿") != 1 || !strings.Contains(shown, path) {
		t.Fatalf("spaced path split: %q", shown)
	}
}

func TestCollapseHelperReportNamesTheEmptyCard(t *testing.T) {
	body := "TRACE_SHOULD_NOT_LEAK Bearer sk-live-should-not-show\n文件：无\n执行策略阻止了 PowerShell，安装包没有落盘。"
	_, err := desktopbot.CollapseHelperReport(body, nil)
	if err == nil {
		t.Fatal("empty card succeeded")
	}
	got := err.Error()
	if strings.Contains(got, "TRACE_") || strings.Contains(got, "sk-") || strings.Contains(got, "Bearer") {
		t.Fatalf("reason leaked a trace or secret: %s", got)
	}
	for _, want := range []string{"helper_report_empty", "file: none", "执行策略阻止了 PowerShell"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reason %q missing %q", got, want)
		}
	}
	_, err = desktopbot.CollapseHelperReport("1.\n2.\n3.", nil)
	if err == nil || !strings.Contains(err.Error(), "file: missing") || strings.Contains(err.Error(), "detail:") {
		t.Fatalf("blank lines = %v", err)
	}
	_, err = desktopbot.CollapseHelperReport("TRACE_SHOULD_NOT_LEAK only", nil)
	if err == nil || strings.Contains(err.Error(), "TRACE_") || strings.Contains(err.Error(), "detail:") {
		t.Fatalf("trace body = %v", err)
	}
	mixed := "下载失败：策略阻止，密钥 sk-live-should-not-show 不能用。请改用 ask-user。\n文件：无。"
	_, err = desktopbot.CollapseHelperReport(mixed, nil)
	if err == nil {
		t.Fatal("punctuated none succeeded")
	}
	got = err.Error()
	if !strings.Contains(got, "file: none") || !strings.Contains(got, "下载失败：策略阻止") || !strings.Contains(got, "ask-user") || strings.Contains(got, "sk-live") {
		t.Fatalf("secret ate the reason: %s", got)
	}
	if _, err = desktopbot.CollapseHelperReport("文件：無", nil); err == nil || !strings.Contains(err.Error(), "file: none") {
		t.Fatalf("traditional none = %v", err)
	}
	var noise strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&noise, "PS C:\\Users> step %d running\n", i)
	}
	noise.WriteString("执行策略阻止了下载。\n文件：无\n")
	_, err = desktopbot.CollapseHelperReport(noise.String(), nil)
	if err == nil {
		t.Fatal("noisy card succeeded")
	}
	got = err.Error()
	if !strings.Contains(got, "执行策略阻止了下载") || strings.Contains(got, "step 0") {
		t.Fatalf("log head hid the reason: %s", got)
	}
	plain := "读取 error log\n安装包没有落到磁盘。\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(plain, nil)
	if err == nil || !strings.Contains(err.Error(), "安装包没有落到磁盘") {
		t.Fatalf("keyword hid the closing sentence: %v", err)
	}
	var later strings.Builder
	later.WriteString("执行策略阻止了下载。\n")
	for _, line := range []string{"然后检查了磁盘。", "页面仍停在下载按钮。", "等待窗口关掉。", "再次确认目录。", "安装包没有落到磁盘。"} {
		later.WriteString(line)
		later.WriteByte('\n')
	}
	later.WriteString("文件：无\n")
	_, err = desktopbot.CollapseHelperReport(later.String(), nil)
	if err == nil || !strings.Contains(err.Error(), "安装包没有落到磁盘") || !strings.Contains(err.Error(), "执行策略阻止了下载") {
		t.Fatalf("early keyword replaced the close: %v", err)
	}
	sameLine := "PS C:\\Users\\ma139> 执行策略阻止了下载。\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(sameLine, nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "PS C:") {
		t.Fatalf("prompt ate the reason: %v", err)
	}
	between := "安装包没有落到磁盘。\nstep 3 running\n请改用管理员权限。\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(between, nil)
	if err == nil || !strings.Contains(err.Error(), "安装包没有落到磁盘") || !strings.Contains(err.Error(), "请改用管理员权限") || strings.Contains(err.Error(), "step 3") {
		t.Fatalf("status line stayed between sentences: %v", err)
	}
	var after strings.Builder
	after.WriteString("执行策略阻止了下载。\n")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&after, "step %d running\n", i)
	}
	after.WriteString("C:\\Users\\ma139> Get-Item\n文件：无\n")
	_, err = desktopbot.CollapseHelperReport(after.String(), nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "step 0") || strings.Contains(err.Error(), "Get-Item") {
		t.Fatalf("status echo replaced the reason: %v", err)
	}
	nested := "PS C:\\Users\\ma139> PS C:\\Users\\ma139> 执行策略阻止了下载。\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(nested, nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "PS C:") {
		t.Fatalf("nested prompt ate the reason: %v", err)
	}
	var record strings.Builder
	record.WriteString("执行策略阻止了下载。\n")
	for i := 0; i < 5; i++ {
		record.WriteString("+ FullyQualifiedErrorId : UnauthorizedAccess\n")
	}
	record.WriteString("文件：无\n")
	_, err = desktopbot.CollapseHelperReport(record.String(), nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "FullyQualifiedErrorId") {
		t.Fatalf("diagnostic replaced the sentence: %v", err)
	}
	var stack strings.Builder
	stack.WriteString("执行策略阻止了下载。\n")
	for _, frame := range []string{
		"at System.Management.Automation.Runspaces.RunspaceBase.Open()",
		"at System.Management.Automation.Runspaces.LocalRunspace.OpenHelper()",
		"at System.Threading.ExecutionContext.Run()",
		"at System.Management.Automation.CommandProcessor.ProcessRecord()",
		"https://go.microsoft.com/fwlink/?LinkID=135170.",
	} {
		stack.WriteString(frame)
		stack.WriteByte('\n')
	}
	stack.WriteString("文件：无\n")
	_, err = desktopbot.CollapseHelperReport(stack.String(), nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "RunspaceBase") || strings.Contains(err.Error(), "LinkID") {
		t.Fatalf("stack frame replaced the sentence: %v", err)
	}
	var names strings.Builder
	names.WriteString("执行策略阻止了下载。\n")
	for _, name := range []string{"演讲稿.docx", "笔记.docx", "图片.png", "备份.zip", "日程.docx"} {
		names.WriteString(name)
		names.WriteByte('\n')
	}
	names.WriteString("文件：无\n")
	_, err = desktopbot.CollapseHelperReport(names.String(), nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), ".docx") {
		t.Fatalf("file name replaced the sentence: %v", err)
	}
	wrote := "无法写入 C:\\Users\\ma139\\Downloads\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(wrote, nil)
	if err == nil || !strings.Contains(err.Error(), "无法写入") || !strings.Contains(err.Error(), `C:\Users\ma139\Downloads`) {
		t.Fatalf("path mention ate the reason: %v", err)
	}
	comma := "读取 error log\n执行策略阻止了下载，安装包没有落盘\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(comma, nil)
	if err == nil || !strings.Contains(err.Error(), "安装包没有落盘") {
		t.Fatalf("comma sentence was dropped: %v", err)
	}
	spaced := "执行策略阻止了下载。\nC:\\Users\\马\\演讲稿 完整版.docx\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(spaced, nil)
	if err == nil || !strings.Contains(err.Error(), "执行策略阻止了下载") || strings.Contains(err.Error(), "完整版") {
		t.Fatalf("spaced path replaced the sentence: %v", err)
	}
	postscript := "PS the execution policy blocked the installer.\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(postscript, nil)
	if err == nil || !strings.Contains(err.Error(), "execution policy blocked the installer") {
		t.Fatalf("postscript was read as a prompt: %v", err)
	}
	ended := "at the end the download failed (policy).\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(ended, nil)
	if err == nil || !strings.Contains(err.Error(), "download failed (policy)") {
		t.Fatalf("sentence was read as a stack frame: %v", err)
	}
	_, err = desktopbot.CollapseHelperReport("exit code 1\n文件：无\n", nil)
	if err == nil || !strings.Contains(err.Error(), "exit code 1") {
		t.Fatalf("diagnostic-only reason dropped: %v", err)
	}
	mixedTrace := "下载失败 TRACE_SHOULD_NOT_LEAK\n文件：无\n"
	_, err = desktopbot.CollapseHelperReport(mixedTrace, nil)
	if err == nil || strings.Contains(err.Error(), "TRACE_") || !strings.Contains(err.Error(), "下载失败") {
		t.Fatalf("trace token ate the sentence: %v", err)
	}
}

func TestHelperReportIsTheCardNotOneNumberedLine(t *testing.T) {
	if desktopbot.HelperReport("已经改好。\n1. 只是其中一句") {
		t.Fatal("one numbered line was treated as a helper card")
	}
	if !desktopbot.HelperReport("1. 做好了\n2. 没有剩余\n文件：/tmp/a.pdf") {
		t.Fatal("card was not recognized")
	}
	if !desktopbot.HelperReport("文件：/tmp/a.pdf") {
		t.Fatal("file line was not recognized")
	}
}
