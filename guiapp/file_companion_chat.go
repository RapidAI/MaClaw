package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// fileCompanionToolsDisabled reports a companion owner. The model request for
// that owner carries no tool definitions. It does not consult pet tool mode.
func fileCompanionToolsDisabled(userID string) bool {
	return strings.HasPrefix(strings.TrimSpace(userID), "file-companion:")
}

// fileCompanionTurnWithoutTools is the start-of-turn gate. A file-companion
// user id or platform is handled before semantic tool routing.
func fileCompanionTurnWithoutTools(userID, platform string) bool {
	if fileCompanionToolsDisabled(userID) {
		return true
	}
	return strings.TrimSpace(platform) == desktopLaunchFileCompanion
}

func fileCompanionLoopPlatform(ctx *LoopContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.Platform
}

// FileCompanionTurnRequest is one chat turn about the open file.
type FileCompanionTurnRequest struct {
	Path           string `json:"path"`
	Text           string `json:"text"`
	Selection      string `json:"selection,omitempty"`
	SelectionStart *int   `json:"selection_start,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Lang           string `json:"lang,omitempty"`
	// Purpose "paper" asks for a host-written interpretation PDF.
	// Ordinary chat leaves this empty and still has no tools.
	Purpose string `json:"purpose,omitempty"`
}

// SendFileCompanionMessage runs one turn on the private transcript. The model
// request has no tools. A selection rewrite is appended after the selection
// by the host; the model cannot write a sibling path.
func (a *App) SendFileCompanionMessage(req FileCompanionTurnRequest) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return errors.New("file companion is not running")
	}
	path := strings.TrimSpace(req.Path)
	text := strings.TrimSpace(req.Text)
	if path == "" || text == "" {
		return errors.New("path and text are required")
	}
	// /clear is a local command. The model turn would append the file path
	// and stop matching the slash command, so handle it before that wrap.
	if fileCompanionIsClearCommand(text) {
		return a.ClearFileCompanionChat(path)
	}
	grant, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return err
	}
	prompt, turn, selection, prefer := fileCompanionOutgoing(canonical, req)
	paper := fileCompanionIsPaperPurpose(req.Purpose)
	a.rememberFileCompanionSelection(path, selection, prefer)
	a.noteFileCompanionConfigChanged()
	if _, err := a.LoadConfig(); err != nil {
		logFileCompanion("config reload failed")
	}
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	logFileCompanion("turn start session=%s", sessionID)
	cancelCtx := a.ctx
	if cancelCtx == nil {
		cancelCtx = context.Background()
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("file-companion-%d", time.Now().UnixNano())
	}
	msg := IMUserMessage{
		RequestID:              requestID,
		UserID:                 userID,
		Platform:               desktopLaunchFileCompanion,
		Text:                   turn,
		Lang:                   strings.TrimSpace(req.Lang),
		NoWorkflowInterception: true,
		AssistantBinding: &agent.AssistantBinding{
			Mode:          "file-companion",
			InitialPrompt: prompt,
		},
		CancelCtx: cancelCtx,
	}
	if agent.IsImageFilePath(path) {
		msg = a.attachFileCompanionImage(msg, path)
	}
	streamEvents := newAIAssistantStreamEventEmitter(a, requestID, userID)
	onProgress := func(progressText string) {
		if progressText == "" || progressText == imHeartbeatMsg {
			return
		}
		if !isVisibleAIAssistantProgressText(progressText) {
			return
		}
		streamEvents.emit("ai-assistant-progress", progressText)
	}
	onToken := func(delta string) {
		if delta == "" {
			return
		}
		streamEvents.emit("ai-assistant-token", delta)
	}
	onNewRound := func() {
		streamEvents.emit("ai-assistant-new-round", "")
	}
	onStreamDone := func() {
		streamEvents.emit("ai-assistant-stream-done", "")
	}
	resp := a.imHandler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
	if paper {
		return a.fileCompanionDeliverPaper(canonical, sessionID, userID, requestID, resp)
	}
	return a.fileCompanionDeliverTurn(path, grant, canonical, sessionID, userID, requestID, selection, prefer, resp)
}

func (a *App) fileCompanionDeliverTurn(path string, grant *fileCompanionGrant, canonical, sessionID, userID, requestID, selection string, selectionStart int, resp *IMAgentResponse) error {
	if resp == nil {
		logFileCompanion("turn end session=%s empty", sessionID)
		return errors.New("empty response")
	}
	if strings.TrimSpace(resp.Error) != "" {
		logFileCompanion("turn end session=%s error", sessionID)
		a.emitFileCompanionTurn(requestID, userID, resp)
		return errors.New(resp.Error)
	}
	reply := strings.TrimSpace(resp.Text)
	ext := strings.ToLower(filepath.Ext(canonical))
	if ext == ".xlsx" || ext == ".pptx" {
		applied, officeErr := a.fileCompanionApplyModelOfficeReply(canonical, reply)
		if officeErr != nil {
			logFileCompanion("turn end session=%s office-refused", sessionID)
			if strings.TrimSpace(resp.Error) == "" {
				resp.Error = officeErr.Error()
			}
			a.emitFileCompanionTurn(requestID, userID, resp)
			return officeErr
		}
		if applied {
			resp.LocalFilePath = canonical
		}
	} else if strings.TrimSpace(selection) != "" && grant != nil && grant.Editable && reply != "" {
		result, appendErr := a.fileCompanionWriteText(path, grant.LoadedHash, func(current string) (string, error) {
			return fileCompanionAppendAfterSelection(current, selection, reply, selectionStart), nil
		})
		if appendErr != nil {
			logFileCompanion("turn end session=%s append-refused", sessionID)
			if strings.TrimSpace(resp.Error) == "" {
				resp.Error = appendErr.Error()
			}
			a.emitFileCompanionTurn(requestID, userID, resp)
			return appendErr
		}
		if result.Conflict || !result.Saved {
			if result.Conflict {
				logFileCompanion("turn end session=%s conflict", sessionID)
			} else {
				logFileCompanion("turn end session=%s append-refused", sessionID)
			}
			if strings.TrimSpace(resp.Error) == "" {
				if result.Error != "" {
					resp.Error = result.Error
				} else {
					resp.Error = "file changed on disk"
				}
			}
			a.emitFileCompanionTurn(requestID, userID, resp)
			return errors.New(resp.Error)
		}
		resp.LocalFilePath = canonical
	}
	a.emitFileCompanionTurn(requestID, userID, resp)
	logFileCompanion("turn end session=%s", sessionID)
	return nil
}

func (a *App) emitFileCompanionTurn(requestID, sessionKey string, resp *IMAgentResponse) {
	if a == nil {
		return
	}
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.RequestID = requestID
	resp.SessionKey = sessionKey
	payload, err := json.Marshal(resp)
	if err != nil {
		logFileCompanion("turn emit failed")
		return
	}
	a.recordCompanionEvent("ai-assistant-response", string(payload))
	a.emitEvent("ai-assistant-response", string(payload))
}

func (a *App) recordCompanionEvent(name, data string) {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.recordEvents {
		return
	}
	st.events = append(st.events, recordedCompanionEvent{Name: name, Data: data})
}

func fileCompanionPrompt(name, ext string) string {
	base := "你是文件伴读。只讨论当前打开的这一个文件：" + name + "。不要引用其他文件，不要使用工具。用户没有要求生成新文件时，不要声称已经生成或发送了 PDF。"
	switch strings.ToLower(ext) {
	case ".xlsx":
		return base + "如果用户要求修改这个工作簿，只返回一个完整的 JSON 对象 {\"sheets\":[...]}，不要附加说明。宿主会在工具循环之外整本替换当前文件。只讨论内容时用普通文字回答，不要返回 sheets JSON。"
	case ".pptx":
		return base + "如果用户要求修改这个幻灯片，只返回一个完整的 JSON 对象 {\"title\":\"\",\"slides\":[...]}，不要附加说明。宿主会在工具循环之外整本替换当前文件。只讨论内容时用普通文字回答，不要返回 slides JSON。"
	default:
		return base + "用户选中一段并要求翻译或改写时，只返回改写后的文字。宿主会把这段文字追加到选区之后，原文保留。"
	}
}

func fileCompanionUserTurn(path, text, selection string) string {
	var b strings.Builder
	b.WriteString(text)
	if strings.TrimSpace(selection) != "" {
		b.WriteString("\n\n选中的原文：\n")
		b.WriteString(selection)
	}
	b.WriteString("\n\n")
	b.WriteString(agent.FilePathPromptPrefix)
	b.WriteString("\n")
	b.WriteString(path)
	b.WriteString("\n")
	return b.String()
}

func (a *App) attachFileCompanionImage(msg IMUserMessage, path string) IMUserMessage {
	if a == nil || a.imHandler == nil {
		return msg
	}
	cfg := a.imHandler.getMaclawLLMConfig()
	if !cfg.SupportsVision {
		msg.Text += "\n\n当前模型不能看图，只保留文件名：" + filepath.Base(path)
		return msg
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > 20<<20 {
		msg.Text += "\n\n当前模型不能看图，只保留文件名：" + filepath.Base(path)
		return msg
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		msg.Text += "\n\n当前模型不能看图，只保留文件名：" + filepath.Base(path)
		return msg
	}
	msg.Attachments = []agent.MessageAttachment{{
		Type:     "image",
		FileName: filepath.Base(path),
		MimeType: fileCompanionImageMime(path),
		Data:     base64.StdEncoding.EncodeToString(raw),
		Size:     info.Size(),
	}}
	return msg
}

func fileCompanionImageMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// fileCompanionIsClearCommand reports /new, /reset, and /clear. A fullwidth
// slash and any letter case match. Extra words stay a normal question.
func fileCompanionIsClearCommand(text string) bool {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "\uFEFF"))
	if strings.HasPrefix(text, "／") {
		text = "/" + strings.TrimPrefix(text, "／")
	}
	switch strings.ToLower(text) {
	case "/new", "/reset", "/clear":
		return true
	default:
		return false
	}
}

// ClearFileCompanionChat drops the right-hand chat for one open file.
// The file itself is left as it is. A turn that is still running is cancelled
// and cannot write the old transcript back.
func (a *App) ClearFileCompanionChat(path string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return errors.New("file companion is not running")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path is required")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return err
	}
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	if a.imHandler != nil {
		_, _ = a.imHandler.RequestCancelSessionForUser(userID)
	}
	side := filepath.Join(a.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if err := a.fileCompanionDropChat(userID, side); err != nil {
		return err
	}
	logFileCompanion("chat cleared session=%s", sessionID)
	return nil
}

// fileCompanionCaptureChatGen records that the next companion turn may save.
// It returns the clear generation that turn must still match at save time.
func (a *App) fileCompanionCaptureChatGen(userID string) uint64 {
	if a == nil {
		return 0
	}
	st := a.companionRuntime()
	st.chatMu.Lock()
	defer st.chatMu.Unlock()
	if st.chatDiscard != nil {
		st.chatDiscard[userID] = false
	}
	if st.chatGen == nil {
		return 0
	}
	return st.chatGen[userID]
}

// fileCompanionCommitChat runs save only when this turn is still the chat
// the user has not cleared. Clear holds the same lock, so a save cannot
// land between the generation bump and the empty transcript.
func (a *App) fileCompanionCommitChat(userID string, turn *inFlightTurn, save func()) bool {
	if a == nil || save == nil {
		return false
	}
	st := a.companionRuntime()
	st.chatMu.Lock()
	defer st.chatMu.Unlock()
	current := uint64(0)
	if st.chatGen != nil {
		current = st.chatGen[userID]
	}
	if turn != nil && turn.clearGen != current {
		return false
	}
	if turn == nil && st.chatDiscard != nil && st.chatDiscard[userID] {
		return false
	}
	save()
	return true
}

// fileCompanionDropChat forgets one companion transcript. The side file is
// removed after the flush, so a reopen cannot restore the cleared bubbles.
func (a *App) fileCompanionDropChat(userID, side string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	st := a.companionRuntime()
	st.chatMu.Lock()
	defer st.chatMu.Unlock()
	if st.chatGen == nil {
		st.chatGen = map[string]uint64{}
	}
	st.chatGen[userID]++
	if st.chatDiscard == nil {
		st.chatDiscard = map[string]bool{}
	}
	st.chatDiscard[userID] = true
	if a.imHandler != nil && a.imHandler.memory != nil {
		a.imHandler.memory.Clear(userID)
		_ = a.imHandler.memory.FlushNow()
	}
	if strings.TrimSpace(side) == "" {
		return nil
	}
	if err := os.Remove(side); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ensureFileCompanionHandler builds the companion IM handler on a private
// store. It does not call ensureConversationMemory, so the main assistant
// transcript is neither opened nor rewritten.
func (a *App) ensureFileCompanionHandler(userID, sessionID string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion")
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		return err
	}
	if a.imHandler == nil {
		mem := agent.NewPersistentConversationMemory(filepath.Join(dir, "conversations.json"))
		if err := mem.EnsureStoreFile(); err != nil {
			return err
		}
		manager := a.remoteSessions
		a.imHandler = newIMMessageHandler(a, manager, mem, nil)
	}
	if a.imHandler == nil || a.imHandler.memory == nil {
		return errors.New("file companion transcript is unavailable")
	}
	side := filepath.Join(dir, "sessions", sessionID+".json")
	return a.imHandler.memory.UseSeparateSessionFile(userID, side)
}

// fileCompanionReplaceOffice wholly replaces the current .xlsx or .pptx.
// It runs outside the agent loop and calls the existing writers directly.
func (a *App) fileCompanionReplaceOffice(path string, payload map[string]interface{}) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	grant, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(canonical))
	switch ext {
	case ".xlsx", ".pptx":
	default:
		return fmt.Errorf("office replace refused for %s", ext)
	}
	if a.fileCompanionExtractTruncated(canonical) {
		return errors.New("truncated extract cannot be written")
	}
	args := map[string]interface{}{
		"file_path": canonical,
		"data":      payload,
	}
	var writeErr error
	if ext == ".pptx" {
		_, writeErr = agent.WritePPTXDetailed(args)
	} else {
		_, writeErr = agent.WriteExcelDetailed(args)
	}
	if writeErr != nil {
		logFileCompanion("office replace refused ext=%s", ext)
		return writeErr
	}
	logFileCompanion("office replace ext=%s", ext)
	a.emitEvent("file-companion:preview-reload", map[string]string{
		"path": canonical,
		"ext":  ext,
	})
	_, _ = a.FileCompanionOpen(grant.UserPath)
	return nil
}

// FileCompanionReplaceOffice wholly replaces the open .xlsx or .pptx from a
// JSON payload. The companion window and the model reply both call it outside
// the agent tool loop.
func (a *App) FileCompanionReplaceOffice(path, payloadJSON string) error {
	payloadJSON = strings.TrimSpace(payloadJSON)
	if payloadJSON == "" {
		return errors.New("office payload is empty")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return err
	}
	return a.fileCompanionReplaceOffice(path, payload)
}

func (a *App) fileCompanionApplyModelOfficeReply(path, reply string) (bool, error) {
	ext := strings.ToLower(filepath.Ext(path))
	raw, ok := fileCompanionOfficePayloadJSON(reply, ext)
	if !ok {
		return false, nil
	}
	if err := a.FileCompanionReplaceOffice(path, raw); err != nil {
		return false, err
	}
	return true, nil
}

func fileCompanionOfficePayloadJSON(reply, ext string) (string, bool) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != ".xlsx" && ext != ".pptx" {
		return "", false
	}
	raw := strings.TrimSpace(reply)
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw, "\n"); i >= 0 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSpace(raw)
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", false
	}
	body := raw[start : end+1]
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", false
	}
	switch ext {
	case ".xlsx":
		if _, ok := payload["sheets"]; !ok {
			return "", false
		}
	case ".pptx":
		if _, ok := payload["slides"]; !ok {
			return "", false
		}
	}
	return body, true
}

// fileCompanionExtractTruncated uses the shared injector at
// EffectiveContextTokens. A begin line with truncated=true means the extract's
// token estimate does not fit that budget.
func (a *App) fileCompanionExtractTruncated(path string) bool {
	tokens := 0
	if a != nil {
		tokens = a.GetMaclawLLMConfig().EffectiveContextTokens()
	}
	probe := agent.FilePathPromptPrefix + "\n" + path + "\n"
	block := agent.ExpandUserSelectedFilePathsWithContext(probe, tokens)
	return fileCompanionBeginTruncated(block)
}

// fileCompanionBeginTruncated reads the live begin line. The injection notice
// mentions truncated=true as an instruction even when the document is whole.
func fileCompanionBeginTruncated(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, agent.AutoExtractBeginMarker) && strings.Contains(line, "truncated=true") {
			return true
		}
	}
	return false
}

func (a *App) fileCompanionHistory(sessionID string) []FileCompanionMessage {
	sessionID = strings.TrimSpace(sessionID)
	if a == nil || sessionID == "" {
		return nil
	}
	userID := "file-companion:" + sessionID
	if a.imHandler != nil && a.imHandler.memory != nil {
		if entries := a.imHandler.memory.Load(userID); len(entries) > 0 {
			return a.fileCompanionAttachPaperResult(sessionID, fileCompanionDisplayMessages(entries))
		}
	}
	side := filepath.Join(a.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	entries, err := agent.LoadSeparateSessionEntries(side, userID)
	if err != nil {
		logFileCompanion("history unreadable session=%s", sessionID)
		return nil
	}
	return a.fileCompanionAttachPaperResult(sessionID, fileCompanionDisplayMessages(entries))
}

func fileCompanionDisplayMessages(entries []agent.ConversationEntry) []FileCompanionMessage {
	if len(entries) == 0 {
		return nil
	}
	out := make([]FileCompanionMessage, 0, len(entries))
	for _, entry := range entries {
		role := strings.ToLower(strings.TrimSpace(entry.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		raw := strings.TrimSpace(fileCompanionEntryText(entry.Content))
		paper := role == "user" && strings.Contains(raw, fileCompanionPaperMarker)
		text := raw
		if role == "user" {
			text = fileCompanionVisibleUserText(text)
		}
		reasoning := ""
		if role == "assistant" {
			reasoning = strings.TrimSpace(entry.ReasoningContent)
		}
		if text == "" && reasoning == "" {
			continue
		}
		out = append(out, FileCompanionMessage{Role: role, Text: text, Reasoning: reasoning, Paper: paper})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fileCompanionVisibleUserText(text string) string {
	if idx := strings.Index(text, "\n\n选中的原文："); idx >= 0 {
		text = text[:idx]
	}
	marker := "\n\n" + agent.FilePathPromptPrefix
	if idx := strings.Index(text, marker); idx >= 0 {
		text = text[:idx]
	}
	if idx := strings.Index(text, fileCompanionPaperMarker); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func fileCompanionEntryText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func (a *App) fileCompanionRejectUngranted(path string) error {
	if !a.isFileCompanionProcess() {
		return nil
	}
	path = strings.TrimSpace(path)
	_, _, err := a.fileCompanionGrant(path)
	if err != nil {
		logFileCompanion("export refused")
		return err
	}
	return nil
}

func (a *App) fileCompanionRejectUngrantedPaths(paths []string) error {
	if !a.isFileCompanionProcess() {
		return nil
	}
	if len(paths) == 0 {
		return errFileCompanionNotGranted
	}
	for _, path := range paths {
		if err := a.fileCompanionRejectUngranted(path); err != nil {
			return err
		}
	}
	return nil
}

// FileCompanionImportKnowledge imports the open file into the desktop
// knowledge base as the personal desktop user. The local original stays.
func (a *App) FileCompanionImportKnowledge(path string) (knowledge.DirectoryImportResult, error) {
	if _, _, err := a.fileCompanionGrant(path); err != nil {
		return knowledge.DirectoryImportResult{}, err
	}
	return a.KnowledgeImportFiles(knowledge.DirectoryImportRequest{
		OwnerID:   desktopUserID,
		SaveScope: knowledge.SaveScopePersonal,
	}, []string{path})
}

// FileCompanionUploadCloud uploads the open file through the existing cloud
// drive (mobile document library). The local original stays.
func (a *App) FileCompanionUploadCloud(path string) (*MobileDocumentDraftSummary, error) {
	if _, _, err := a.fileCompanionGrant(path); err != nil {
		return nil, err
	}
	return a.ImportMobileDocumentFromPath(path)
}

// ChooseFileCompanionFiles opens the system file dialog. The main process
// starts the companion with those paths. The companion process returns them
// so the window can open tabs. Neither path is inserted into the assistant.
func (a *App) ChooseFileCompanionFiles() ([]string, error) {
	if a == nil || a.ctx == nil {
		return nil, errors.New("app unavailable")
	}
	files, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "用伴读打开",
	})
	if err != nil {
		return nil, err
	}
	cleaned := make([]string, 0, len(files))
	for _, path := range files {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsRune(path, 0) {
			continue
		}
		cleaned = append(cleaned, path)
	}
	if len(cleaned) == 0 {
		return nil, nil
	}
	if a.isFileCompanionProcess() {
		return cleaned, nil
	}
	if err := a.LaunchFileCompanion(cleaned); err != nil {
		return nil, err
	}
	return cleaned, nil
}
