package guiapp

import (
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const paperSample = `
## 方法原理
使用通道注意力。

## 方法本质
使用 CNN 提取空间特征，再加权各通道。

## 主要原理图及说明
图 1 是整体框架。输入经过卷积，再按通道加权。

## 实验方法
在分类任务上和基线对比。

## 数据集
- 名称：ImageNet
- 论文中的下载地址：https://image-net.org/download
- 检索词：不需要
- 名称：CIFAR-10
- 论文中的下载地址：论文未给出
- 检索词：CIFAR-10 dataset download

## 是否开源
- 论文中的代码地址：论文未给出
- 检索词：cipl github

## 论文质量评价
消融不完整。
`

func TestFileCompanionPaperMarksOnlySearchedAddresses(t *testing.T) {
	var queries []string
	out, missed := fileCompanionSupplementPaperText(paperSample, "cipl", func(query string) (string, string) {
		queries = append(queries, query)
		switch {
		case strings.Contains(query, "github"):
			return "repo", "https://github.com/example/cipl"
		case strings.Contains(query, "CIFAR"):
			return "cifar", "https://example.com/cifar"
		default:
			t.Fatalf("searched an address the paper already gave: %s", query)
			return "", ""
		}
	})
	if missed {
		t.Fatal("lookup missed")
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %#v", queries)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "image-net.org") && strings.Contains(line, "可能") {
			t.Fatalf("stated dataset url was marked uncertain: %s", line)
		}
	}
	if !strings.Contains(out, "https://image-net.org/download") {
		t.Fatalf("stated url dropped: %s", out)
	}
	if !strings.Contains(out, "https://example.com/cifar（可能）") || !strings.Contains(out, "https://github.com/example/cipl（可能）") {
		t.Fatalf("searched urls = %s", out)
	}
	if strings.Count(out, "（可能）") != 2 {
		t.Fatalf("possible marks = %d\n%s", strings.Count(out, "（可能）"), out)
	}
}

func TestFileCompanionPaperDropsInventedURLAndReadsNumberedFields(t *testing.T) {
	markdown := `
## 5. 数据集
名称：CIFAR-10
论文中的下载地址：论文未给出 https://guess.example/cifar
检索词：CIFAR-10 dataset download

## 3）是否开源
1. 论文中的代码地址：论文未给出
检索词：cipl github
`
	var queries []string
	out, missed := fileCompanionSupplementPaperText(markdown, "cipl", func(query string) (string, string) {
		queries = append(queries, query)
		switch {
		case strings.Contains(query, "github"):
			return "repo", "https://github.com/example/cipl"
		case strings.Contains(query, "CIFAR"):
			return "cifar", "https://example.com/cifar"
		default:
			t.Fatalf("unexpected query %s", query)
			return "", ""
		}
	})
	if missed {
		t.Fatal("lookup missed")
	}
	if strings.Contains(out, "guess.example") {
		t.Fatalf("invented url kept: %s", out)
	}
	if !strings.Contains(out, "https://example.com/cifar（可能）") || !strings.Contains(out, "https://github.com/example/cipl（可能）") {
		t.Fatalf("searched urls = %s queries=%#v", out, queries)
	}
	if strings.Count(out, "（可能）") != 2 {
		t.Fatalf("possible marks = %d\n%s", strings.Count(out, "（可能）"), out)
	}
}

func TestFileCompanionPaperSearchSkipsResultPages(t *testing.T) {
	title, page := firstPaperSearchPage([]websearch.SearchResult{
		{Title: "bing", URL: "https://www.bing.com/search?q=cifar"},
		{Title: "google", URL: "https://www.google.com/search?q=cifar"},
		{Title: "baidu", URL: "https://www.baidu.com/s?wd=cifar"},
		{Title: "ddg", URL: "https://duckduckgo.com/?q=cifar"},
		{Title: "sogou", URL: "https://www.sogou.com/web?query=cifar"},
		{Title: "gh-search", URL: "https://github.com/search?q=cipl"},
		{Title: "repo", URL: "https://github.com/example/cipl"},
	})
	if title != "repo" || page != "https://github.com/example/cipl" {
		t.Fatalf("picked %s %s", title, page)
	}
	title, page = firstPaperSearchPage([]websearch.SearchResult{
		{Title: "bing", URL: "https://www.bing.com/search?q=cifar"},
		{Title: "gh-search", URL: "https://github.com/search?q=cipl"},
	})
	if title != "" || page != "" {
		t.Fatalf("serp was accepted: %s %s", title, page)
	}
	if paperSearchPageUsable("https://www.google.com/url?q=https://example.com/data") {
		t.Fatal("google redirect counted as a download page")
	}
	if got := paperTargetURL("https://www.google.com/url?q=https://example.com/data"); got != "https://example.com/data" {
		t.Fatalf("google redirect target = %s", got)
	}
	if got := paperTargetURL("https://www.google.com/url?q=" + url.QueryEscape("https://example.com/data?x=1")); got != "https://example.com/data?x=1" {
		t.Fatalf("encoded google redirect = %s", got)
	}
	if paperTargetURL("https://www.google.com/search?q=cifar") != "" || paperTargetURL("https://scholar.google.com/scholar?q=cipl") != "" || paperTargetURL("https://www.google.com/webhp") != "" {
		t.Fatal("a google search page was kept")
	}
	if paperTargetURL("https://www.google.com/url?q=https://www.google.com/search?q=cifar") != "" {
		t.Fatal("a redirect back to a search page was kept")
	}
	bingTarget := base64.StdEncoding.EncodeToString([]byte("https://example.com/data"))
	title, page = firstPaperSearchPage([]websearch.SearchResult{
		{Title: "bing-click", URL: "https://www.bing.com/ck/a?u=a1" + bingTarget},
		{Title: "repo", URL: "https://github.com/example/cipl"},
	})
	if title != "bing-click" || page != "https://example.com/data" {
		t.Fatalf("bing click = %s %s", title, page)
	}
	title, page = firstPaperSearchPage([]websearch.SearchResult{
		{Title: "ddg", URL: "https://duckduckgo.com/l/?uddg=" + url.QueryEscape("https://example.com/ddg-data")},
	})
	if page != "https://example.com/ddg-data" {
		t.Fatalf("ddg redirect = %s %s", title, page)
	}
	if !paperSearchPageUsable("https://storage.googleapis.com/bucket/data.zip") {
		t.Fatal("object storage was treated as a search page")
	}
	if !paperSearchPageUsable("https://pan.baidu.com/s/abc") {
		t.Fatal("pan.baidu.com was treated as a search page")
	}
	if paperSearchPageUsable("https://www.bing.com/ck/a?u=not-a-url") || paperSearchPageUsable("https://www.baidu.com/") {
		t.Fatal("a search host was treated as a target")
	}
}

func TestFileCompanionPaperReplacesSearchPageAddressWithTheTarget(t *testing.T) {
	markdown := `
## 数据集
- 名称：ImageNet
- 论文中的下载地址：https://image-net.org/download
- 检索词：不需要
- 名称：CIFAR-10
- 论文中的下载地址：https://www.google.com/search?q=cifar-10+download
- 检索词：CIFAR-10 dataset download

## 是否开源
- 论文中的代码地址：https://www.google.com/url?q=https://example.com/from-model
- 检索词：cipl github
`
	var queries []string
	out, missed := fileCompanionSupplementPaperText(markdown, "cipl", func(query string) (string, string) {
		queries = append(queries, query)
		switch {
		case strings.Contains(query, "github"):
			return "repo", "https://www.google.com/url?q=https://github.com/example/cipl"
		case strings.Contains(query, "CIFAR"):
			return "cifar", "https://www.bing.com/ck/a?u=a1" + base64.StdEncoding.EncodeToString([]byte("https://example.com/cifar"))
		default:
			t.Fatalf("searched an address the paper already gave: %s", query)
			return "", ""
		}
	})
	if missed {
		t.Fatal("lookup missed")
	}
	if len(queries) != 2 {
		t.Fatalf("queries = %#v", queries)
	}
	if strings.Contains(out, "google.com") || strings.Contains(out, "bing.com") || strings.Contains(out, "from-model") {
		t.Fatalf("search page kept: %s", out)
	}
	if !strings.Contains(out, "https://image-net.org/download") || strings.Contains(out, "image-net.org/download（可能）") {
		t.Fatalf("stated url changed: %s", out)
	}
	if !strings.Contains(out, "https://example.com/cifar（可能）") || !strings.Contains(out, "https://github.com/example/cipl（可能）") {
		t.Fatalf("targets = %s", out)
	}
	if strings.Count(out, "（可能）") != 2 {
		t.Fatalf("possible marks = %d\n%s", strings.Count(out, "（可能）"), out)
	}
}

func TestFileCompanionPaperKeepsAddressWrittenBeforeTheName(t *testing.T) {
	markdown := "## 数据集\n论文中的下载地址：论文未给出\n名称：CIFAR-10\n检索词：CIFAR-10 dataset download\n"
	var queries []string
	out, missed := fileCompanionSupplementPaperText(markdown, "cipl", func(query string) (string, string) {
		queries = append(queries, query)
		return "cifar", "https://example.com/cifar"
	})
	if missed || len(queries) != 1 || queries[0] != "CIFAR-10 dataset download" {
		t.Fatalf("queries = %#v missed=%v", queries, missed)
	}
	if !strings.Contains(out, "数据集 CIFAR-10：https://example.com/cifar（可能）") {
		t.Fatalf("named dataset lost: %s", out)
	}
}

func TestFileCompanionPaperSearchMissStaysUnmarked(t *testing.T) {
	markdown := "## 数据集\n- 名称：PrivateSet\n- 论文中的下载地址：论文未给出\n- 检索词：PrivateSet dataset download\n"
	out, missed := fileCompanionSupplementPaperText(markdown, "paper", func(string) (string, string) {
		return "", ""
	})
	if !missed {
		t.Fatal("expected a miss")
	}
	if strings.Contains(out, "（可能）") {
		t.Fatalf("missing page was marked as an address: %s", out)
	}
	if !strings.Contains(out, "网上未确认到下载地址") {
		t.Fatalf("miss copy = %s", out)
	}
}

func TestFileCompanionPaperTurnDropsSelection(t *testing.T) {
	start := 4
	path := filepath.Join(t.TempDir(), "notes.md")
	prompt, turn, selection, prefer := fileCompanionOutgoing(path, FileCompanionTurnRequest{
		Purpose:        "paper",
		Text:           "草稿不要发出去",
		Selection:      "秘密选区",
		SelectionStart: &start,
	})
	if selection != "" || prefer != -1 {
		t.Fatalf("selection = %q prefer = %d", selection, prefer)
	}
	if strings.Contains(turn, "秘密选区") || strings.Contains(turn, "草稿不要发出去") {
		t.Fatalf("paper turn kept the draft: %s", turn)
	}
	visible := fileCompanionVisibleUserText(turn)
	if visible != "论文解读" {
		t.Fatalf("visible = %q", visible)
	}
	if strings.Contains(visible, "dataset") || strings.Contains(visible, "数据集") {
		t.Fatalf("score query leaked into the bubble: %q", visible)
	}
	before, after, ok := strings.Cut(turn, "\n\n"+agent.FilePathPromptPrefix)
	if !ok || !strings.Contains(before, fileCompanionPaperMarker+" "+fileCompanionPaperScoreQuery+"-->") {
		t.Fatal("score query must sit inside the comment before the path")
	}
	if regexp.MustCompile(`(?i)(^|[^a-z0-9])(paper|file|companion)([^a-z0-9]|$)`).MatchString(before) {
		t.Fatalf("marker words are in the page-score prefix: %s", before)
	}
	if strings.Contains(after, fileCompanionPaperMarker) {
		t.Fatal("marker leaked into the file section")
	}
	if !strings.Contains(prompt, "不要使用工具") || !strings.Contains(turn, "方法本质") || !strings.Contains(turn, "是否开源") {
		t.Fatal("paper instruction missing")
	}
	if !strings.Contains(turn, agent.FilePathPromptPrefix) || !strings.Contains(turn, path) {
		t.Fatal("paper turn lost the open file")
	}
	_, ordinary, kept, _ := fileCompanionOutgoing(path, FileCompanionTurnRequest{Text: "hello", Selection: "秘密选区"})
	if kept != "秘密选区" || !strings.Contains(ordinary, "秘密选区") {
		t.Fatal("ordinary turn dropped the selection")
	}
}

func TestFileCompanionPaperPDFLeavesTheSourceAlone(t *testing.T) {
	prev := fileCompanionPaperLookupHook
	fileCompanionPaperLookupHook = func(query string) (string, string) {
		if strings.Contains(query, "github") {
			return "repo", "https://github.com/example/cipl"
		}
		return "cifar", "https://example.com/cifar"
	}
	t.Cleanup(func() { fileCompanionPaperLookupHook = prev })

	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	const original = "alpha beta\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	grant, canonical, err := app.fileCompanionGrant(path)
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + doc.SessionID
	if !fileCompanionTurnWithoutTools(userID, desktopLaunchFileCompanion) {
		t.Fatal("paper owner gained tools")
	}
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	if app.imHandler == nil {
		app.imHandler = &IMMessageHandler{app: app}
	}
	app.imHandler.memory = mem
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: fileCompanionPaperUserTurn(canonical)})
	resp := &IMAgentResponse{Text: paperSample}
	if err := app.fileCompanionDeliverPaper(canonical, doc.SessionID, userID, "paper-1", resp); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("source changed: %q", got)
	}
	if resp.LocalFilePath == "" || strings.EqualFold(filepath.Clean(resp.LocalFilePath), filepath.Clean(canonical)) {
		t.Fatalf("pdf path = %q source = %q", resp.LocalFilePath, canonical)
	}
	if !strings.Contains(resp.LocalFilePath, "论文解读.pdf") {
		t.Fatalf("pdf path = %q", resp.LocalFilePath)
	}
	if !strings.HasPrefix(filepath.Clean(resp.LocalFilePath), filepath.Clean(filepath.Join(app.GetDataDir(), "file-companion", "results"))) {
		t.Fatalf("pdf left the companion results dir: %s", resp.LocalFilePath)
	}
	pdf, err := os.ReadFile(resp.LocalFilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatal("result is not a pdf")
	}
	if !strings.Contains(resp.Text, "https://example.com/cifar（可能）") {
		t.Fatal("pdf body omitted the searched dataset")
	}
	record, err := os.ReadFile(filepath.Join(filepath.Dir(resp.LocalFilePath), "latest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(record)) != resp.LocalFilePath {
		t.Fatalf("latest = %q", record)
	}
	turnIndex, err := os.ReadFile(filepath.Join(filepath.Dir(resp.LocalFilePath), "latest-turn.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(turnIndex)) != "1" {
		t.Fatalf("turn index = %q", turnIndex)
	}

	chat := &IMAgentResponse{Text: "只是聊天"}
	if err := app.fileCompanionDeliverTurn(path, grant, canonical, doc.SessionID, userID, "chat-1", "", -1, chat); err != nil {
		t.Fatal(err)
	}
	if chat.LocalFilePath != "" {
		t.Fatalf("ordinary chat set local_file_path %q", chat.LocalFilePath)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatal("ordinary chat rewrote the source")
	}
}

func TestFileCompanionHistoryShowsPaperCardOnThePaperTurn(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	sessionID := "fc_paper"
	userID := "file-companion:" + sessionID
	dir := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(outside, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := app.fileCompanionLatestPaper(sessionID); got != "" {
		t.Fatalf("accepted a path outside results: %s", got)
	}
	pdf := filepath.Join(dir, "notes-论文解读.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "你好"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "普通回答"})
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "论文解读"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "手打的回答"})
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: fileCompanionPaperUserTurn(`C:/notes.md`)})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "很长的解读"})
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "再问一句"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "后续回答"})
	app.imHandler = &IMMessageHandler{app: app, memory: mem}
	got := app.fileCompanionHistory(sessionID)
	if len(got) != 8 {
		t.Fatalf("messages = %#v", got)
	}
	if got[1].Text != "普通回答" || got[1].ResultPath != "" {
		t.Fatalf("ordinary answer = %#v", got[1])
	}
	if got[2].Paper || got[2].Text != "论文解读" || got[3].Text != "手打的回答" || got[3].ResultPath != "" {
		t.Fatalf("typed label stole the card: %#v %#v", got[2], got[3])
	}
	if !got[4].Paper || got[4].Text != "论文解读" {
		t.Fatalf("button turn = %#v", got[4])
	}
	if got[5].Text != "已生成论文解读" || got[5].ResultPath != pdf {
		t.Fatalf("paper answer = %#v", got[5])
	}
	if got[7].Text != "后续回答" || got[7].ResultPath != "" {
		t.Fatalf("later answer = %#v", got[7])
	}
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: fileCompanionPaperUserTurn(`C:/notes.md`)})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "第二次没有生成"})
	if err := os.WriteFile(filepath.Join(dir, "latest-turn.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = app.fileCompanionHistory(sessionID)
	if len(got) != 10 {
		t.Fatalf("messages = %#v", got)
	}
	if got[5].Text != "已生成论文解读" || got[5].ResultPath != pdf {
		t.Fatalf("first paper answer = %#v", got[5])
	}
	if got[9].Text != "第二次没有生成" || got[9].ResultPath != "" {
		t.Fatalf("failed press took the card: %#v", got[9])
	}
}
