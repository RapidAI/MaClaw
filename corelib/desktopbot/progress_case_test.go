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

func TestCollapseHelperReportFoldsASentMessage(t *testing.T) {
	body := "1. 已发出\n2. 发给：王占一（奇安信-合伙人）\n3. 原文：码卡龙的computer use能力大大提升了\n文件：无\n"
	shown, err := desktopbot.CollapseHelperReport(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "1. 已发出；发给：王占一（奇安信-合伙人）；原文：码卡龙的computer use能力大大提升了") ||
		!strings.Contains(shown, "2. 无") || !strings.Contains(shown, "3. 无") || !strings.Contains(shown, "文件：无") {
		t.Fatalf("shown = %q", shown)
	}
	left, err := desktopbot.CollapseHelperReport("1. 安装包已下载\n2. 还要通知用户\n3. 无\n文件：C:\\a.exe\n", nil)
	if err != nil || !strings.Contains(left, "2. 还要通知用户") {
		t.Fatalf("left = %q err=%v", left, err)
	}
	unsent, err := desktopbot.CollapseHelperReport("1. 没发出\n2. 发给：王占一\n3. 原文：你好\n文件：无\n", nil)
	if err != nil || !strings.Contains(unsent, "2. 发给：王占一") || !strings.Contains(unsent, "3. 原文：你好") {
		t.Fatalf("unsent = %q err=%v", unsent, err)
	}
	labeledBody, err := desktopbot.CollapseHelperReport("1. 已发出\n2. 发给：王占一\n3. 发出原文：你好\n文件：无\n", nil)
	if err != nil || !strings.Contains(labeledBody, "1. 已发出；发给：王占一；原文：你好") || !strings.Contains(labeledBody, "2. 无") {
		t.Fatalf("labeled body = %q err=%v", labeledBody, err)
	}
}

func TestCollapseHelperReportAcceptsUnnumberedSendFacts(t *testing.T) {
	// The live 刘歌 report. The facts are labeled, not numbered, and a
	// separate line talks about a goal-system miss. That line is not the send.
	body := "目标失败原因已修复：原 goal 从未在目标系统中注册（首次 complete 无目标可关），已补建目标、核验并发送结果后正式关闭，验收通过。\n\n" +
		"已发出：消息发送成功，10:29 显示在与刘歌的聊天记录中，目标已正式完成关闭。\n" +
		"发给谁：蓝信用户「刘歌」（奇安信-产研中心-总师办公室）。\n" +
		"发出原文：“码卡龙现在很擅长给蓝信中的用户发消息了。”\n" +
		"文件：无"
	shown, err := desktopbot.CollapseHelperReport(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "1. 已发出：消息发送成功，10:29 显示在与刘歌的聊天记录中，目标已正式完成关闭。；发给：蓝信用户「刘歌」（奇安信-产研中心-总师办公室）。；原文：“码卡龙现在很擅长给蓝信中的用户发消息了。”") ||
		!strings.Contains(shown, "2. 无") || !strings.Contains(shown, "3. 无") || !strings.Contains(shown, "文件：无") ||
		strings.Contains(shown, "目标失败原因") {
		t.Fatalf("shown = %q", shown)
	}
	shortBody := "已发出：消息发送成功，10:29 显示在聊天记录中。\n发给谁：蓝信用户「刘歌」（奇安信-产研中心-总师办公室）。\n发出原文：“码卡龙现在很擅长给蓝信中的用户发消息了。”\n文件：无"
	short, err := desktopbot.CollapseHelperReport(shortBody, nil)
	if err != nil || !strings.Contains(short, "发给：蓝信用户「刘歌」") || !strings.Contains(short, "2. 无") {
		t.Fatalf("short = %q err=%v", short, err)
	}
	failed := "已发出：发送失败。\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(failed, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("failed send = %v", err)
	}
	partial := "已发出：消息发送成功。\n发给谁：刘歌\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(partial, nil); err == nil {
		t.Fatal("partial facts succeeded")
	}
	other := "1. 安装包已下载\n2. 无\n3. 无\n文件：无\n已发出：消息发送成功。\n发给谁：刘歌\n发出原文：你好\n"
	kept, err := desktopbot.CollapseHelperReport(other, nil)
	if err != nil || !strings.Contains(kept, "1. 安装包已下载") || strings.Contains(kept, "刘歌") {
		t.Fatalf("other conclusion = %q err=%v", kept, err)
	}
	prose := "已提交审核。\n发给用户之前要核对。\n内容：安装包没有落到磁盘。\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(prose, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("prose = %v", err)
	}
	specific := "已发出：消息发送成功。\n发给：群里所有人\n发给谁：刘歌\n内容：不要另发\n发出原文：你好\n文件：无\n"
	picked, err := desktopbot.CollapseHelperReport(specific, nil)
	if err != nil || !strings.Contains(picked, "发给：刘歌") || !strings.Contains(picked, "原文：你好") ||
		strings.Contains(picked, "群里所有人") || strings.Contains(picked, "不要另发") {
		t.Fatalf("specific = %q err=%v", picked, err)
	}
	twice := "1. 已发出；发给：刘歌；原文：你好\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	once, err := desktopbot.CollapseHelperReport(twice, nil)
	if err != nil || strings.Count(once, "发给：") != 1 || strings.Count(once, "原文：") != 1 {
		t.Fatalf("twice = %q err=%v", once, err)
	}
	roles := "1. 已完成：给蓝信用户刘歌发出消息，原文“码卡龙现在很擅长给蓝信中的用户发消息了。”，10:29 发送成功\n2. 剩余：无\n3. 阻塞：无\n文件：无\n"
	shownRoles, err := desktopbot.CollapseHelperReport(roles, nil)
	if err != nil || !strings.Contains(shownRoles, "1. 给蓝信用户刘歌发出消息") || !strings.Contains(shownRoles, "2. 无") ||
		!strings.Contains(shownRoles, "3. 无") || strings.Contains(shownRoles, "已完成") || strings.Contains(shownRoles, "剩余") || strings.Contains(shownRoles, "阻塞") {
		t.Fatalf("roles = %q err=%v", shownRoles, err)
	}
	echo := "1. 已完成：已完成：给蓝信\n2. 剩余：无\n3. 阻塞：无\n文件：无\n"
	stripped, err := desktopbot.CollapseHelperReport(echo, nil)
	if err != nil || !strings.Contains(stripped, "1. 给蓝信") || strings.Contains(stripped, "已完成") {
		t.Fatalf("echo = %q err=%v", stripped, err)
	}
	bare := "1. 完成安装包下载\n2. 还要通知用户\n3. 无\n文件：无\n"
	keptBare, err := desktopbot.CollapseHelperReport(bare, nil)
	if err != nil || !strings.Contains(keptBare, "1. 完成安装包下载") || !strings.Contains(keptBare, "2. 还要通知用户") {
		t.Fatalf("bare = %q err=%v", keptBare, err)
	}
	mentioned := "1. 已发出：向刘歌说你好\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	keptMention, err := desktopbot.CollapseHelperReport(mentioned, nil)
	if err != nil || strings.Count(keptMention, "发给：刘歌") != 1 || strings.Count(keptMention, "原文：你好") != 1 || !strings.Contains(keptMention, "2. 无") {
		t.Fatalf("mentioned = %q err=%v", keptMention, err)
	}
	fileName := "1. 已发出\n2. 发给：刘歌\n3. 原文件：readme.md\n文件：无\n"
	keptFile, err := desktopbot.CollapseHelperReport(fileName, nil)
	if err != nil || !strings.Contains(keptFile, "3. 原文件：readme.md") || strings.Contains(keptFile, "原文：") {
		t.Fatalf("filename = %q err=%v", keptFile, err)
	}
	outline := "1. 已发出\n2. 发给：刘歌\n3. 内容提要：本周\n文件：无\n"
	keptOutline, err := desktopbot.CollapseHelperReport(outline, nil)
	if err != nil || !strings.Contains(keptOutline, "3. 内容提要：本周") || strings.Contains(keptOutline, "原文：") {
		t.Fatalf("outline = %q err=%v", keptOutline, err)
	}
	buried := "1. 报告已提交，等待反馈\n2. 发给：老师\n3. 内容：周报\n文件：无\n"
	keptBuried, err := desktopbot.CollapseHelperReport(buried, nil)
	if err != nil || !strings.Contains(keptBuried, "1. 报告已提交，等待反馈") || !strings.Contains(keptBuried, "2. 发给：老师") || !strings.Contains(keptBuried, "3. 内容：周报") {
		t.Fatalf("buried = %q err=%v", keptBuried, err)
	}
	period := "1. 已发出。\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	foldedPeriod, err := desktopbot.CollapseHelperReport(period, nil)
	if err != nil || !strings.Contains(foldedPeriod, "1. 已发出。；发给：刘歌；原文：你好") || !strings.Contains(foldedPeriod, "2. 无") {
		t.Fatalf("period = %q err=%v", foldedPeriod, err)
	}
	underRole := "1. 已完成：已发出\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	foldedRole, err := desktopbot.CollapseHelperReport(underRole, nil)
	if err != nil || !strings.Contains(foldedRole, "1. 已发出；发给：刘歌；原文：你好") || strings.Contains(foldedRole, "2. 发给") {
		t.Fatalf("under role = %q err=%v", foldedRole, err)
	}
	aspect := "1. 已发出了\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	foldedAspect, err := desktopbot.CollapseHelperReport(aspect, nil)
	if err != nil || !strings.Contains(foldedAspect, "1. 已发出了；发给：刘歌；原文：你好") || !strings.Contains(foldedAspect, "2. 无") {
		t.Fatalf("aspect = %q err=%v", foldedAspect, err)
	}
	resultWord := "1. 已发送成功\n2. 发给：刘歌\n3. 原文：你好\n文件：无\n"
	foldedResult, err := desktopbot.CollapseHelperReport(resultWord, nil)
	if err != nil || !strings.Contains(foldedResult, "1. 已发送成功；发给：刘歌；原文：你好") || !strings.Contains(foldedResult, "3. 无") {
		t.Fatalf("result = %q err=%v", foldedResult, err)
	}
	submittedTo := "1. 已提交给\n2. 发给：老师\n3. 内容：周报\n文件：无\n"
	keptTo, err := desktopbot.CollapseHelperReport(submittedTo, nil)
	if err != nil || !strings.Contains(keptTo, "1. 已提交给") || !strings.Contains(keptTo, "2. 发给：老师") || strings.Contains(keptTo, "；发给：") {
		t.Fatalf("submitted to = %q err=%v", keptTo, err)
	}
	bareDone := "已发出。\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	foldedBare, err := desktopbot.CollapseHelperReport(bareDone, nil)
	if err != nil || !strings.Contains(foldedBare, "1. 已发出。；发给：刘歌；原文：你好") || !strings.Contains(foldedBare, "2. 无") {
		t.Fatalf("bare done = %q err=%v", foldedBare, err)
	}
	unnumberedRole := "已完成：已发出\n剩余：发给：刘歌\n阻塞：原文：你好\n文件：无\n"
	foldedUnnumbered, err := desktopbot.CollapseHelperReport(unnumberedRole, nil)
	if err != nil || !strings.Contains(foldedUnnumbered, "1. 已发出；发给：刘歌；原文：你好") || strings.Contains(foldedUnnumbered, "已完成") || strings.Contains(foldedUnnumbered, "剩余") {
		t.Fatalf("unnumbered role = %q err=%v", foldedUnnumbered, err)
	}
	oneLine := "已发出；发给：刘歌；原文：你好\n文件：无\n"
	foldedOne, err := desktopbot.CollapseHelperReport(oneLine, nil)
	if err != nil || strings.Count(foldedOne, "发给：") != 1 || strings.Count(foldedOne, "原文：") != 1 || !strings.Contains(foldedOne, "2. 无") {
		t.Fatalf("one line = %q err=%v", foldedOne, err)
	}
	keptBody := "1. 已发出\n2. 发给：刘歌\n3. 原文：你好；世界\n文件：无\n"
	foldedBody, err := desktopbot.CollapseHelperReport(keptBody, nil)
	if err != nil || !strings.Contains(foldedBody, "原文：你好；世界") || strings.Count(foldedBody, "原文：") != 1 {
		t.Fatalf("body semicolon = %q err=%v", foldedBody, err)
	}
	gone := "已发出去。\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(gone, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("gone = %v", err)
	}
	later := "已发出：消息发送成功，目标失败原因已修复。\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	foldedLater, err := desktopbot.CollapseHelperReport(later, nil)
	if err != nil || !strings.Contains(foldedLater, "1. 已发出：消息发送成功，目标失败原因已修复。；发给：刘歌；原文：你好") || !strings.Contains(foldedLater, "2. 无") {
		t.Fatalf("later failure = %q err=%v", foldedLater, err)
	}
	stillFailed := "已发出：发送失败，网络超时。\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(stillFailed, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("still failed = %v", err)
	}
	semiFailed := "已发出；发送失败\n发给谁：刘歌\n发出原文：你好\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(semiFailed, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("semi failed = %v", err)
	}
	joinedFail := "已发出；发送失败；发给：刘歌；原文：你好\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(joinedFail, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("joined fail = %v", err)
	}
	joinedBody := "已发出；发给：刘歌；原文：你好；世界\n文件：无\n"
	foldedJoined, err := desktopbot.CollapseHelperReport(joinedBody, nil)
	if err != nil || !strings.Contains(foldedJoined, "原文：你好；世界") || strings.Count(foldedJoined, "原文：") != 1 || !strings.Contains(foldedJoined, "2. 无") {
		t.Fatalf("joined body = %q err=%v", foldedJoined, err)
	}
	// The live 马勇 report. The 文件 line closes a three-line card whose
	// last two lines are 无. The conclusion is not numbered, and a
	// goal-failure paragraph is not one of those three lines.
	mayong := "goal 记录核验结果：goal 存储中无活跃目标记录（原始 goal 未持久化到本会话的 goal 系统，这是失败原因）。但实际任务已完成且已核验——消息已成功发送并显示在聊天窗口（时间 14:37，消息列表预览与原文一致），无需用户进一步确认。范围修正：goal 系统无记录可关闭，以本报告作为完成记录。\n\n" +
		"已发出，发给终端安全的马勇（奇安信-产研中心-产研BG-终端与办公网安全BU），原文：“马勇您好！我是MaClaw（码卡龙），一款运行在您电脑上的AI数智助手。我可以帮您：处理日常事务，如文件管理、文档撰写；操控电脑桌面，代替您完成重复性操作；查询和分析数据，生成报告；收发邮件和消息提醒；编写脚本实现自动化任务。我支持中文交互，随叫随到。有任何需要帮忙的地方，随时告诉我！”\n" +
		"无\n无\n文件：无"
	shownMayong, err := desktopbot.CollapseHelperReport(mayong, nil)
	if err != nil || !strings.Contains(shownMayong, "发给终端安全的马勇（奇安信-产研中心-产研BG-终端与办公网安全BU）") ||
		strings.Count(shownMayong, "马勇您好！") != 1 || !strings.Contains(shownMayong, "撰写；操控电脑桌面") ||
		!strings.Contains(shownMayong, "2. 无") || !strings.Contains(shownMayong, "3. 无") || !strings.Contains(shownMayong, "文件：无") ||
		strings.Contains(shownMayong, "失败原因") {
		t.Fatalf("mayong = %q err=%v", shownMayong, err)
	}
	glued := "已发出，发给用户之前要核对。\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(glued, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("glued recipient = %v", err)
	}
	samePara := "goal 这是失败原因。\n已发出，发给马勇，原文：你好\n无\n无\n文件：无\n"
	shownSame, err := desktopbot.CollapseHelperReport(samePara, nil)
	if err != nil || !strings.Contains(shownSame, "1. 已发出，发给马勇，原文：你好") ||
		!strings.Contains(shownSame, "2. 无") || strings.Contains(shownSame, "失败原因") {
		t.Fatalf("same paragraph = %q err=%v", shownSame, err)
	}
	failedNone := "已发出：发送失败。\n无\n无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(failedNone, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("failed none = %v", err)
	}
	roleFailed := "已完成：已发出：发送失败。\n剩余：无\n阻塞：无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(roleFailed, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("role failed = %v", err)
	}
	blankSlots := "无\n无\n无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(blankSlots, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("blank slots = %v", err)
	}
	roleNone := "goal 这是失败原因。\n已完成：已发出，发给马勇，原文：你好\n剩余：无\n阻塞：无\n文件：无\n"
	shownRole, err := desktopbot.CollapseHelperReport(roleNone, nil)
	if err != nil || !strings.Contains(shownRole, "1. 已发出，发给马勇，原文：你好") ||
		strings.Contains(shownRole, "已完成") || strings.Contains(shownRole, "剩余") ||
		strings.Contains(shownRole, "阻塞") || strings.Contains(shownRole, "失败原因") {
		t.Fatalf("role none = %q err=%v", shownRole, err)
	}
	laterNone := "已发出：消息发送成功，目标失败原因已修复。\n无\n无\n文件：无\n"
	shownLater, err := desktopbot.CollapseHelperReport(laterNone, nil)
	if err != nil || !strings.Contains(shownLater, "1. 已发出：消息发送成功，目标失败原因已修复。") ||
		!strings.Contains(shownLater, "2. 无") || strings.Contains(shownLater, "；发给：") {
		t.Fatalf("later none = %q err=%v", shownLater, err)
	}
	otherWord := "已发出通知：磁盘失败。\n无\n无\n文件：无\n"
	shownWord, err := desktopbot.CollapseHelperReport(otherWord, nil)
	if err != nil || !strings.Contains(shownWord, "1. 已发出通知：磁盘失败。") || !strings.Contains(shownWord, "2. 无") {
		t.Fatalf("other word = %q err=%v", shownWord, err)
	}
	numberedNone := "已发出，发给马勇，原文：你好\n2. 无\n3. 无\n文件：无\n"
	shownNumbered, err := desktopbot.CollapseHelperReport(numberedNone, nil)
	if err != nil || !strings.Contains(shownNumbered, "1. 已发出，发给马勇，原文：你好") ||
		!strings.Contains(shownNumbered, "2. 无") || !strings.Contains(shownNumbered, "3. 无") {
		t.Fatalf("numbered none = %q err=%v", shownNumbered, err)
	}
	earlierNumber := "1. 旧结论\n已发出，发给马勇，原文：你好\n无\n无\n文件：无\n"
	shownEarlier, err := desktopbot.CollapseHelperReport(earlierNumber, nil)
	if err != nil || !strings.Contains(shownEarlier, "1. 已发出，发给马勇，原文：你好") ||
		strings.Contains(shownEarlier, "旧结论") {
		t.Fatalf("earlier number = %q err=%v", shownEarlier, err)
	}
	numberedFail := "已发出：发送失败。\n2. 无\n3. 无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(numberedFail, nil); err == nil ||
		!strings.Contains(err.Error(), "helper_report_empty") || !strings.Contains(err.Error(), "发送失败") {
		t.Fatalf("numbered fail = %v", err)
	}
	keptNumberedFail := "1. 已发出：发送失败。\n2. 无\n3. 无\n文件：无\n"
	shownKept, err := desktopbot.CollapseHelperReport(keptNumberedFail, nil)
	if err != nil || !strings.Contains(shownKept, "1. 已发出：发送失败。") || !strings.Contains(shownKept, "2. 无") {
		t.Fatalf("kept numbered fail = %q err=%v", shownKept, err)
	}
	onlyNone := "1. 无\n2. 无\n3. 无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(onlyNone, nil); err == nil || !strings.Contains(err.Error(), "helper_report_empty") {
		t.Fatalf("only none = %v", err)
	}
	blankCard := "goal 这是失败原因。\n\n已发出，发给马勇，原文：你好\n\n无\n\n无\n\n文件：无\n"
	shownBlank, err := desktopbot.CollapseHelperReport(blankCard, nil)
	if err != nil || !strings.Contains(shownBlank, "1. 已发出，发给马勇，原文：你好") ||
		!strings.Contains(shownBlank, "2. 无") || strings.Contains(shownBlank, "失败原因") {
		t.Fatalf("blank card = %q err=%v", shownBlank, err)
	}
	labeledTail := "已发出：消息发送成功。\n发给谁：刘歌\n发出原文：你好\n\n无\n无\n文件：无\n"
	shownTail, err := desktopbot.CollapseHelperReport(labeledTail, nil)
	if err != nil || !strings.Contains(shownTail, "1. 已发出：消息发送成功。；发给：刘歌；原文：你好") ||
		!strings.Contains(shownTail, "2. 无") {
		t.Fatalf("labeled tail = %q err=%v", shownTail, err)
	}
	labeledSame := "已发出：消息发送成功。\n发给谁：刘歌\n发出原文：你好\n无\n无\n文件：无\n"
	shownLabeled, err := desktopbot.CollapseHelperReport(labeledSame, nil)
	if err != nil || !strings.Contains(shownLabeled, "发给：刘歌") || !strings.Contains(shownLabeled, "原文：你好") ||
		strings.Contains(shownLabeled, "1. 发出原文") {
		t.Fatalf("labeled same = %q err=%v", shownLabeled, err)
	}
	contentLine := "内容：周报已写好\n无\n无\n文件：无\n"
	shownContent, err := desktopbot.CollapseHelperReport(contentLine, nil)
	if err != nil || !strings.Contains(shownContent, "1. 内容：周报已写好") || !strings.Contains(shownContent, "2. 无") {
		t.Fatalf("content line = %q err=%v", shownContent, err)
	}
	staleFail := "1. 已发出：发送失败（第一次）。\n\n已发出：发送失败。\n无\n无\n文件：无\n"
	if _, err := desktopbot.CollapseHelperReport(staleFail, nil); err == nil ||
		!strings.Contains(err.Error(), "helper_report_empty") || !strings.Contains(err.Error(), "发送失败") ||
		strings.Contains(err.Error(), "第一次") {
		t.Fatalf("stale numbered fail = %v", err)
	}
	earlierDone := "已发出。\n已发出，发给马勇，原文：你好\n无\n无\n文件：无\n"
	shownEarlierDone, err := desktopbot.CollapseHelperReport(earlierDone, nil)
	if err != nil || !strings.Contains(shownEarlierDone, "1. 已发出，发给马勇，原文：你好") ||
		strings.Contains(shownEarlierDone, "1. 已发出。") {
		t.Fatalf("earlier done = %q err=%v", shownEarlierDone, err)
	}
	laterNumber := "1. 安装包已下载\n2. 还要通知用户\n3. 无\n文件：C:\\a.exe\n1. 后来的句子\n"
	shownLaterNum, err := desktopbot.CollapseHelperReport(laterNumber, nil)
	if err != nil || !strings.Contains(shownLaterNum, "1. 安装包已下载") ||
		!strings.Contains(shownLaterNum, "2. 还要通知用户") || strings.Contains(shownLaterNum, "后来的句子") {
		t.Fatalf("later number = %q err=%v", shownLaterNum, err)
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
