package guiapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFileCompanionTranslateSelectionSendsOnlyTheSelection(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	const bodyToken = "BODY_SHOULD_NOT_BE_SENT"
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"+bodyToken+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Error != "" {
		t.Fatal(opened.Error)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(canonical)
	documentUser := "file-companion:" + sessionID
	mainFile := filepath.Join(app.GetDataDir(), "ai_assistant_conversation.json")
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	store := filepath.Join(app.GetDataDir(), "file-companion", "conversations.json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		mainSentinel  = "main-transcript-sentinel\n"
		sideSentinel  = "document-chat-sentinel\n"
		storeSentinel = "conversations-sentinel\n"
	)
	if err := os.WriteFile(mainFile, []byte(mainSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(sideSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte(storeSentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	var seen []IMUserMessage
	prev := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		seen = append(seen, msg)
		return &IMAgentResponse{Text: "译文：" + msg.Text}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prev })

	got, err := app.TranslateFileCompanionSelection(path, "selected sentence")
	if err != nil {
		t.Fatal(err)
	}
	if got != "译文：selected sentence" {
		t.Fatalf("translation = %q", got)
	}
	if len(seen) != 1 {
		t.Fatalf("model calls = %d", len(seen))
	}
	msg := seen[0]
	if msg.Text != "selected sentence" {
		t.Fatalf("model text = %q", msg.Text)
	}
	prompt := ""
	if msg.AssistantBinding != nil {
		prompt = msg.AssistantBinding.InitialPrompt
	}
	if !strings.Contains(prompt, "只输出译文") || !strings.Contains(prompt, "原文已经是中文") {
		t.Fatalf("instruction = %q", prompt)
	}
	for _, piece := range []string{msg.Text, prompt, msg.UserID} {
		if strings.Contains(piece, bodyToken) || strings.Contains(piece, path) || strings.Contains(piece, "paper.pdf") {
			t.Fatalf("model payload included the file: %q", piece)
		}
	}
	if len(msg.Attachments) != 0 {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	if msg.UserID != "file-companion:translate:"+sessionID {
		t.Fatalf("user = %q", msg.UserID)
	}
	if msg.UserID == documentUser {
		t.Fatal("translate used the document chat user")
	}
	if !fileCompanionToolsDisabled(msg.UserID) {
		t.Fatal("translate user would carry tools")
	}

	second, err := app.TranslateFileCompanionSelection(path, "second line")
	if err != nil {
		t.Fatal(err)
	}
	if second != "译文：second line" {
		t.Fatalf("second translation = %q", second)
	}
	if len(seen) != 2 || seen[1].Text != "second line" || strings.Contains(seen[1].Text, "selected sentence") {
		t.Fatalf("second payload = %+v", seen)
	}

	long := strings.Repeat("选", fileCompanionTranslateRunes) + "尾"
	if _, err := app.TranslateFileCompanionSelection(path, long); err != nil {
		t.Fatal(err)
	}
	if got := seen[2].Text; utf8.RuneCountInString(got) != fileCompanionTranslateRunes || strings.Contains(got, "尾") || strings.Contains(got, bodyToken) {
		t.Fatalf("clipped selection = %q runes=%d", got, utf8.RuneCountInString(got))
	}

	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		return &IMAgentResponse{Text: strings.Repeat("译", fileCompanionTranslateReplyRunes+1)}
	}
	clipped, err := app.TranslateFileCompanionSelection(path, "short")
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(clipped) != fileCompanionTranslateReplyRunes {
		t.Fatalf("reply runes = %d", utf8.RuneCountInString(clipped))
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(info.ModTime()) || after.Size() != info.Size() {
		t.Fatal("translation rewrote the pdf")
	}
	assertBytes(t, mainFile, mainSentinel)
	assertBytes(t, side, sideSentinel)
	assertBytes(t, store, storeSentinel)
	entries, err := os.ReadDir(filepath.Dir(side))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != sessionID+".json" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("session files = %v", names)
	}
}

func TestFileCompanionTranslateSelectionRejectsEmptyAndUngranted(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	calls := 0
	prev := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		calls++
		return &IMAgentResponse{Text: "不应调用"}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prev })

	if _, err := app.TranslateFileCompanionSelection(path, "  "); err == nil || !strings.Contains(err.Error(), "请先选中要翻译的文字") {
		t.Fatalf("empty selection: %v", err)
	}
	other := filepath.Join(dir, "other.pdf")
	if err := os.WriteFile(other, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.TranslateFileCompanionSelection(other, "Hello"); err == nil || !strings.Contains(err.Error(), "path is outside the open file") {
		t.Fatalf("ungranted: %v", err)
	}
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse { return nil }
	if _, err := app.TranslateFileCompanionSelection(path, "Hello"); err == nil || !strings.Contains(err.Error(), "没有得到译文") {
		t.Fatalf("nil model: %v", err)
	}
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		return &IMAgentResponse{Text: "  "}
	}
	if _, err := app.TranslateFileCompanionSelection(path, "Hello"); err == nil || !strings.Contains(err.Error(), "没有得到译文") {
		t.Fatalf("blank model: %v", err)
	}
	if calls != 0 {
		t.Fatalf("rejected selection called the model %d times", calls)
	}

	idle := &App{testHomeDir: t.TempDir()}
	if _, err := idle.TranslateFileCompanionSelection(path, "Hello"); err == nil || !strings.Contains(err.Error(), "file companion is not running") {
		t.Fatalf("idle: %v", err)
	}
}

func TestFileCompanionTranslateFailureKeepsLoginAndCredits(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "翻译失败，请稍后重试"},
		{in: "HTTP 500: body_len=3 {}", want: "翻译失败，请稍后重试"},
		{in: "MaClaw Hub login is required to share documents", want: "需要先在主窗口登录"},
		{in: `HTTP 403: body_len=80 {"code":"LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST"}`, want: "本次请求额度不足"},
		{in: "insufficient credits for this request: need 1.000 credits, available 0.000", want: "本次请求额度不足"},
	}
	for _, tc := range cases {
		var err error
		if tc.in != "" {
			err = errors.New(tc.in)
		}
		got := fileCompanionTranslateFailure(err)
		if got == nil || got.Error() != tc.want {
			t.Fatalf("in %q got %v want %q", tc.in, got, tc.want)
		}
	}
}
