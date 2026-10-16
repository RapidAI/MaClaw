package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestFileCompanionTurnLeavesMainTranscriptAndUsesEmptyTools(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	mainFile := filepath.Join(app.GetDataDir(), "ai_assistant_conversation.json")
	if err := os.MkdirAll(filepath.Dir(mainFile), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("main-transcript-sentinel\n")
	if err := os.WriteFile(mainFile, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	st := app.companionRuntime()
	st.mu.Lock()
	st.recordEvents = true
	st.mu.Unlock()

	err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: path, Text: "总结这一份"})
	if err == nil {
		t.Fatal("cancelled companion turn returned nil error")
	}
	st.mu.Lock()
	recorded := append([]recordedCompanionEvent(nil), st.events...)
	st.mu.Unlock()
	sawReply := false
	for _, ev := range recorded {
		if ev.Name != "ai-assistant-response" || !strings.Contains(ev.Data, `"session_key":"file-companion:`) {
			continue
		}
		sawReply = true
		if strings.Contains(ev.Data, "alpha") {
			t.Fatal("companion turn event included the file body")
		}
	}
	if !sawReply {
		t.Fatal("companion turn did not emit ai-assistant-response")
	}
	got, readErr := os.ReadFile(mainFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(sentinel) {
		t.Fatal("companion turn modified ai_assistant_conversation.json")
	}
	companionStore := filepath.Join(app.GetDataDir(), "file-companion", "conversations.json")
	if _, statErr := os.Stat(companionStore); statErr != nil {
		t.Fatalf("private transcript shell: %v", statErr)
	}

	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + fileCompanionSessionID(canonical)
	if petCompanionToolsDisabled(userID) {
		t.Fatal("file companion turned on pet tool mode")
	}
	callbacks := &sharedAgentLoopCallbacks{
		userID: userID,
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "write_file"}},
		},
		semanticSurface: &semanticCallSurface{},
	}
	if tools := callbacks.BuildToolsForModelRequest("总结这一份", 1); tools != nil {
		t.Fatalf("model tool list = %#v, want empty", tools)
	}

	target := filepath.Join(dir, "forged.txt")
	result := app.imHandler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{ID: "forged", Type: "function", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"` + filepath.ToSlash(target) + `","content":"nope"}`,
		}},
	})
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("forged tool result = %+v", result)
	}
	if !strings.Contains(result.Text, "文件伴读") || strings.Contains(result.Text, "闲聊") {
		t.Fatalf("rejection text = %q", result.Text)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("forged tool wrote %s (%v)", target, statErr)
	}
}

func TestFileCompanionOfficeReplaceRefusesXlsPptAndSibling(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()

	xls := filepath.Join(dir, "book.xls")
	ppt := filepath.Join(dir, "deck.ppt")
	sibling := filepath.Join(dir, "other.xlsx")
	if err := os.WriteFile(xls, []byte("biff-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ppt, []byte("ppt-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(xls); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(ppt); err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"sheets": []interface{}{
			map[string]interface{}{
				"name": "Sheet1",
				"rows": []interface{}{[]interface{}{map[string]interface{}{"value": "x"}}},
			},
		},
	}
	if err := app.fileCompanionReplaceOffice(xls, payload); err == nil {
		t.Fatal(".xls write was accepted")
	}
	if err := app.fileCompanionReplaceOffice(ppt, map[string]interface{}{
		"title":  "Deck",
		"slides": []interface{}{map[string]interface{}{"title": "One", "bullets": []string{"a"}}},
	}); err == nil {
		t.Fatal(".ppt write was accepted")
	}
	if err := app.fileCompanionReplaceOffice(sibling, payload); err == nil {
		t.Fatal("sibling write was accepted")
	}
	assertBytes(t, xls, "biff-bytes")
	assertBytes(t, ppt, "ppt-bytes")
	assertBytes(t, sibling, "sibling-bytes")
	if _, err := os.Stat(ppt + ".pptx"); !os.IsNotExist(err) {
		t.Fatalf("refused .ppt created a sibling deck: %v", err)
	}

	xlsx := filepath.Join(dir, "book.xlsx")
	if err := excel.WriteFile(xlsx, excel.WriteData{Sheets: []excel.WriteSheet{{
		Name: "Sheet1",
		Rows: [][]excel.WriteCell{{{Value: "seed"}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(xlsx); err != nil {
		t.Fatal(err)
	}
	if err := app.fileCompanionReplaceOffice(xlsx, payload); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatal("granted .xlsx was not replaced")
	}

	huge := filepath.Join(dir, "huge.xlsx")
	// 360_000 ASCII letters are about 90_000 tokens. Half of the default
	// 88_000-token window is 44_000, so the shared injector truncates this
	// workbook and the host must not write it back.
	cell := strings.Repeat("A", 360_000)
	if err := excel.WriteFile(huge, excel.WriteData{Sheets: []excel.WriteSheet{{
		Name: "Sheet1",
		Rows: [][]excel.WriteCell{{{Value: cell}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(huge); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(huge)
	if err != nil {
		t.Fatal(err)
	}
	tokens := app.GetMaclawLLMConfig().EffectiveContextTokens()
	if tokens != 110_000*80/100 {
		t.Fatalf("live budget context = %d, want default 88000", tokens)
	}
	if !app.fileCompanionExtractTruncated(huge) {
		probe := agent.FilePathPromptPrefix + "\n" + huge + "\n"
		block := agent.ExpandUserSelectedFilePathsWithContext(probe, tokens)
		t.Fatalf("live extract was not truncated, prefix %q", trimForTest(block))
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.FileCompanionReplaceOffice(huge, string(rawPayload)); err == nil {
		t.Fatal("truncated extract was written")
	}
	still, err := os.ReadFile(huge)
	if err != nil {
		t.Fatal(err)
	}
	if string(still) != string(original) {
		t.Fatal("truncated extract changed the workbook")
	}
}

func TestFileCompanionConversationKeepsAFileThatFitsTheTokenBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.txt")
	// 100_000 ASCII letters are 25_000 tokens. Half of 88_000 is 44_000.
	body := strings.Repeat("A", 100_000)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	turn := fileCompanionUserTurn(path, "文件是什么？", "")
	cfg := corelib.MaclawLLMConfig{ContextLength: 110_000}
	started := h.buildAgentLoopConversationStart("chat", "file-companion:s", turn, "system", desktopLaunchFileCompanion, nil, cfg, nil, 0, nil, nil, nil, true, nil, false)
	text, _ := started.UserContent.(string)
	if !strings.Contains(text, "truncated=false ---") {
		t.Fatalf("companion truncated a fitting file: %s", trimForTest(text))
	}
	if !strings.Contains(text, body) {
		t.Fatal("full body was not injected")
	}
	shared := agent.ExpandUserSelectedFilePathsWithContext(turn, cfg.EffectiveContextTokens())
	if !strings.Contains(shared, "truncated=false ---") {
		t.Fatal("shared injector truncated a file that fits the token budget")
	}
}

func TestFileCompanionExportGrantRejectsOnlyInCompanionMode(t *testing.T) {
	dir := t.TempDir()
	granted := filepath.Join(dir, "notes.md")
	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(granted, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	companion := &App{testHomeDir: t.TempDir()}
	companion.seedFileCompanionPaths(nil)
	if _, err := companion.FileCompanionOpen(granted); err != nil {
		t.Fatal(err)
	}
	if _, err := companion.KnowledgeImportFiles(knowledge.DirectoryImportRequest{
		OwnerID:   desktopUserID,
		SaveScope: knowledge.SaveScopePersonal,
	}, []string{other}); !errors.Is(err, errFileCompanionNotGranted) {
		t.Fatalf("companion knowledge import err = %v", err)
	}
	if _, err := companion.ImportMobileDocumentFromPath(other); !errors.Is(err, errFileCompanionNotGranted) {
		t.Fatalf("companion cloud upload err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(companion.GetDataDir(), "knowledge.db")); !os.IsNotExist(err) {
		t.Fatalf("ungranted import opened the knowledge store: %v", err)
	}

	mainApp := &App{testHomeDir: t.TempDir()}
	_, err := mainApp.KnowledgeImportFiles(knowledge.DirectoryImportRequest{
		OwnerID:   desktopUserID,
		SaveScope: knowledge.SaveScopePersonal,
	}, []string{granted})
	if errors.Is(err, errFileCompanionNotGranted) {
		t.Fatal("main process knowledge import was rejected by the companion grant")
	}
	_, err = mainApp.ImportMobileDocumentFromPath(granted)
	if errors.Is(err, errFileCompanionNotGranted) {
		t.Fatal("main process cloud upload was rejected by the companion grant")
	}
	if err == nil || !strings.Contains(err.Error(), "MaClaw Hub login is required") {
		t.Fatalf("main process upload err = %v", err)
	}
}

func TestFileCompanionOpenRestoresSideFileAndMemoryTranscript(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		t.Fatal(err)
	}
	storedUser := "总结这一段\n\n选中的原文：\nhello\n\n" + agent.FilePathPromptPrefix + "\n" + path + "\n"
	payload := map[string]interface{}{
		"sessions": map[string]interface{}{
			userID: map[string]interface{}{
				"entries": []agent.ConversationEntry{
					{Role: "user", Content: storedUser},
					{Role: "assistant", Content: "这是一段说明", ReasoningContent: "先看目录"},
					{Role: "tool", Content: "hidden"},
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if app.imHandler != nil {
		t.Fatal("opening a file built the IM handler")
	}
	if len(doc.Messages) != 2 || doc.Messages[0].Role != "user" || doc.Messages[0].Text != "总结这一段" {
		t.Fatalf("restored messages = %#v", doc.Messages)
	}
	if doc.Messages[1].Role != "assistant" || doc.Messages[1].Text != "这是一段说明" || doc.Messages[1].Reasoning != "先看目录" {
		t.Fatalf("assistant message = %#v", doc.Messages[1])
	}

	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "内存原文\n\n选中的原文：\n选区\n\n" + agent.FilePathPromptPrefix + "\n" + path + "\n"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "内存回答", ReasoningContent: "内存里的思考"})
	app.imHandler = &IMMessageHandler{app: app, memory: mem}
	again, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 2 || again.Messages[0].Text != "内存原文" || again.Messages[1].Text != "内存回答" || again.Messages[1].Reasoning != "内存里的思考" {
		t.Fatalf("memory messages = %#v", again.Messages)
	}
}

func TestFileCompanionDeliverTurnReplacesOfficeOutsideTheLoop(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	xlsx := filepath.Join(dir, "book.xlsx")
	if err := excel.WriteFile(xlsx, excel.WriteData{Sheets: []excel.WriteSheet{{
		Name: "Sheet1",
		Rows: [][]excel.WriteCell{{{Value: "seed"}}},
	}}}); err != nil {
		t.Fatal(err)
	}
	doc, err := app.FileCompanionOpen(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	grant, canonical, err := app.fileCompanionGrant(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	st.recordEvents = true
	st.mu.Unlock()
	userID := "file-companion:" + doc.SessionID
	chat := &IMAgentResponse{Text: "这个表只有一行种子数据"}
	if err := app.fileCompanionDeliverTurn(xlsx, grant, canonical, doc.SessionID, userID, "req-chat", "", -1, chat); err != nil {
		t.Fatal(err)
	}
	same, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != string(before) {
		t.Fatal("ordinary chat replaced the workbook")
	}
	if chat.LocalFilePath != "" {
		t.Fatal("ordinary chat set local_file_path")
	}

	reply := "```json\n" + `{"sheets":[{"name":"Sheet1","rows":[[{"value":"replaced"}]]}]}` + "\n```"
	resp := &IMAgentResponse{Text: reply}
	if err := app.fileCompanionDeliverTurn(xlsx, grant, canonical, doc.SessionID, userID, "req-office", "", -1, resp); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) {
		t.Fatal("model office payload did not replace the workbook")
	}
	if resp.LocalFilePath != canonical {
		t.Fatalf("local_file_path = %q", resp.LocalFilePath)
	}
	if app.imHandler != nil {
		t.Fatal("office replace started an IM handler")
	}
	st.mu.Lock()
	recorded := append([]recordedCompanionEvent(nil), st.events...)
	st.mu.Unlock()
	saw := false
	for _, ev := range recorded {
		if ev.Name != "ai-assistant-response" {
			continue
		}
		if strings.Contains(ev.Data, `"session_key":"`+userID+`"`) && strings.Contains(ev.Data, `"local_file_path"`) {
			saw = true
		}
		if strings.Contains(ev.Data, "seed") {
			t.Fatal("office event included the sheet body")
		}
	}
	if !saw {
		t.Fatal("office reply was not emitted with local_file_path")
	}

	pptxPath := filepath.Join(dir, "deck.pptx")
	if _, err := agent.WritePPTXDetailed(map[string]interface{}{
		"file_path": pptxPath,
		"data": map[string]interface{}{
			"title":  "Seed",
			"slides": []interface{}{map[string]interface{}{"title": "Old", "bullets": []string{"a"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	deck, err := app.FileCompanionOpen(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	deckGrant, deckCanonical, err := app.fileCompanionGrant(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	deckBefore, err := os.ReadFile(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	deckResp := &IMAgentResponse{Text: `{"title":"Next","slides":[{"title":"New","bullets":["b"]}]}`}
	if err := app.fileCompanionDeliverTurn(pptxPath, deckGrant, deckCanonical, deck.SessionID, "file-companion:"+deck.SessionID, "req-pptx", "", -1, deckResp); err != nil {
		t.Fatal(err)
	}
	deckAfter, err := os.ReadFile(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(deckAfter) == string(deckBefore) {
		t.Fatal("model deck payload did not replace the pptx")
	}
	if deckResp.LocalFilePath != deckCanonical {
		t.Fatalf("pptx local_file_path = %q", deckResp.LocalFilePath)
	}
}

func TestFileCompanionDeliverTurnAppendsSelectionAndReportsRefusal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("alpha beta"), 0o644); err != nil {
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
	resp := &IMAgentResponse{Text: "gamma"}
	if err := app.fileCompanionDeliverTurn(path, grant, canonical, doc.SessionID, userID, "req-append", "beta", -1, resp); err != nil {
		t.Fatal(err)
	}
	assertBytes(t, path, "alpha beta\n\ngamma")
	if resp.LocalFilePath != canonical || resp.Error != "" {
		t.Fatalf("append resp path=%q err=%q", resp.LocalFilePath, resp.Error)
	}

	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	refused := &IMAgentResponse{Text: "delta"}
	if err := app.fileCompanionDeliverTurn(path, grant, canonical, doc.SessionID, userID, "req-refuse", "gamma", -1, refused); err == nil {
		t.Fatal("read-only append was accepted")
	}
	if refused.Error == "" || refused.LocalFilePath != "" {
		t.Fatalf("refusal resp path=%q err=%q", refused.LocalFilePath, refused.Error)
	}
	assertBytes(t, path, "alpha beta\n\ngamma")
}

func assertBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func trimForTest(text string) string {
	if len(text) > 240 {
		return text[:240]
	}
	return text
}

func TestFileCompanionIsClearCommand(t *testing.T) {
	for _, text := range []string{"/clear", "  /CLEAR  ", "／clear", "/new", "/reset", "\uFEFF/clear"} {
		if !fileCompanionIsClearCommand(text) {
			t.Fatalf("%q should clear the companion chat", text)
		}
	}
	for _, text := range []string{"/clear now", "clear", "/help", "总结一下"} {
		if fileCompanionIsClearCommand(text) {
			t.Fatalf("%q should stay a normal question", text)
		}
	}
}

func TestFileCompanionClearDropsChatAndLeavesTheFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	other := filepath.Join(dir, "other.md")
	if err := os.WriteFile(path, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCompanionSideTranscript(t, app, path, "总结一下", "这是摘要"); err != nil {
		t.Fatal(err)
	}
	if err := writeCompanionSideTranscript(t, app, other, "另一份", "还在"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(other); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(opened.Messages) != 2 || opened.Messages[1].Text != "这是摘要" {
		t.Fatalf("seeded chat = %#v", opened.Messages)
	}
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + fileCompanionSessionID(canonical)
	if err := app.ensureFileCompanionHandler(userID, fileCompanionSessionID(canonical)); err != nil {
		t.Fatal(err)
	}
	app.imHandler.beginInFlightTurn(userID, "总结一下", &LoopContext{})
	st := app.companionRuntime()
	st.mu.Lock()
	st.recordEvents = true
	st.mu.Unlock()

	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: path, Text: "/clear"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "alpha" {
		t.Fatalf("file body = %q", body)
	}
	st.mu.Lock()
	for _, ev := range st.events {
		if ev.Name == "ai-assistant-response" {
			t.Fatal("/clear asked the model")
		}
	}
	st.mu.Unlock()
	if loaded := app.imHandler.memory.Load(userID); len(loaded) != 0 {
		t.Fatalf("memory after /clear = %#v", loaded)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", fileCompanionSessionID(canonical)+".json")
	if _, statErr := os.Stat(side); !os.IsNotExist(statErr) {
		t.Fatalf("side file after /clear: %v", statErr)
	}
	app.imHandler.saveConversationHistoryTimed(userID, []agent.ConversationEntry{
		{Role: "user", Content: "总结一下"},
		{Role: "assistant", Content: "这是摘要"},
	}, nil)
	if loaded := app.imHandler.memory.Load(userID); len(loaded) != 0 {
		t.Fatalf("stale turn restored the chat: %#v", loaded)
	}
	again, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) != 0 {
		t.Fatalf("reopen restored %#v", again.Messages)
	}
	kept, err := app.FileCompanionOpen(other)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Messages) != 2 || kept.Messages[0].Text != "另一份" || kept.Messages[1].Text != "还在" {
		t.Fatalf("other file chat = %#v", kept.Messages)
	}

	app.imHandler.beginInFlightTurn(userID, "下一问", &LoopContext{})
	app.imHandler.saveConversationHistoryTimed(userID, []agent.ConversationEntry{
		{Role: "user", Content: "下一问"},
		{Role: "assistant", Content: "新的回答"},
	}, nil)
	if loaded := app.imHandler.memory.Load(userID); len(loaded) != 2 {
		t.Fatalf("chat after a new turn = %#v", loaded)
	}

	if err := app.ClearFileCompanionChat(filepath.Join(dir, "missing.md")); !errors.Is(err, errFileCompanionNotGranted) {
		t.Fatalf("ungranted clear err = %v", err)
	}
	mainApp := &App{testHomeDir: t.TempDir()}
	if err := mainApp.ClearFileCompanionChat(path); err == nil || !strings.Contains(err.Error(), "file companion is not running") {
		t.Fatalf("main process clear err = %v", err)
	}
}

func writeCompanionSideTranscript(t *testing.T, app *App, path, userText, assistantText string) error {
	t.Helper()
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		return err
	}
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if err := os.MkdirAll(filepath.Dir(side), 0o755); err != nil {
		return err
	}
	payload := map[string]interface{}{
		"sessions": map[string]interface{}{
			userID: map[string]interface{}{
				"entries": []agent.ConversationEntry{
					{Role: "user", Content: userText},
					{Role: "assistant", Content: assistantText},
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(side, raw, 0o644)
}
