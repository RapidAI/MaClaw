package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// sendCoding starts a programming turn inside the checked-out jail.
// git push goes through Push, which is the same confirmation gate a direct
// caller uses. The turn returns before the model finishes.
func (rt *Runtime) sendCoding(ctx context.Context, inst *instance, payload turnBody) (json.RawMessage, error) {
	if _, err := jailRoot(inst.dir, KindCoding); err != nil {
		return nil, err
	}
	text := payload.text()
	if text == "" {
		return nil, errors.New("请输入要发送的内容")
	}
	taskID := strings.TrimSpace(payload.TaskID)
	if taskID == "" {
		running, _ := inst.book.Running()
		taskID = running
	}
	// A bare send continues the running task: park it under that task too,
	// or the drain would file it as a new one.
	payload.TaskID = taskID
	if _, ok := agent.ParseCommitApprovalAnswer(text); ok && taskID != "" {
		if err := inst.book.RecordAnswer(taskID, text); err != nil && !errors.Is(err, errNotApproval) {
			return nil, err
		}
	}
	requestID, taskID, created, err := rt.begin(inst, payload.ClientMessageID, payload.TaskID, text)
	if errors.Is(err, errBusy) {
		// Same parked-input semantics as the session kind: the message waits
		// in this instance's queue and runCoding drains it after the turn.
		// An approval answer was already recorded above, so its queued turn
		// re-enters the conversation with the decision in place.
		queueID, qerr := inst.book.Enqueue(payload.ClientMessageID, payload.TaskID, text, time.Now())
		if qerr != nil {
			return nil, qerr
		}
		return json.Marshal(map[string]any{"accepted": true, "queued": true, "queue_id": queueID, "task_id": payload.TaskID})
	}
	if err != nil {
		return nil, err
	}
	if created {
		turnCtx, cancel := context.WithCancel(context.Background())
		rt.track(requestID, cancel)
		go rt.runCoding(turnCtx, inst, requestID, taskID, text)
	}
	_ = ctx
	return json.Marshal(map[string]any{"accepted": true, "request_id": requestID, "task_id": taskID})
}

func (rt *Runtime) runCoding(ctx context.Context, inst *instance, requestID, taskID, text string) {
	defer rt.untrack(requestID)
	rt.mu.Lock()
	rt.inFlight++
	rt.lastUsed = time.Now()
	rt.mu.Unlock()
	defer func() {
		rt.mu.Lock()
		rt.inFlight--
		rt.lastUsed = time.Now()
		rt.mu.Unlock()
	}()
	if ctx.Err() != nil {
		return
	}
	content, asError, files := rt.codingResult(ctx, inst, taskID, requestID, text)
	if ctx.Err() != nil {
		return
	}
	rt.finishTurn(inst, requestID, taskID, content, asError, files, time.Now())
	rt.drainQueue(inst)
}

func (rt *Runtime) codingResult(ctx context.Context, inst *instance, taskID, requestID, text string) (string, bool, []string) {
	// round_started precedes every terminal frame — the viewer drops events
	// for a round it has not seen open.
	rt.publish(inst, taskID, requestID, "round_started", text, map[string]any{"display_text": text})
	fail := func(msg string) (string, bool, []string) {
		rt.publish(inst, taskID, requestID, "response", msg, map[string]any{
			"terminal": map[string]any{"text": msg},
		})
		return msg, true, nil
	}
	root, err := jailRoot(inst.dir, KindCoding)
	if err != nil {
		return fail(err.Error())
	}
	cfg := rt.maclawCfg(inst)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fail("云端官方模型未接通")
	}
	history, err := inst.book.History(taskID)
	if err != nil {
		return fail(err.Error())
	}
	host := &agentHost{
		rt:      rt,
		inst:    inst,
		task:    taskID,
		request: requestID,
		root:    root,
		turn:    ctx,
		cfg:     cfg,
		coding:  true,
		started: time.Now(),
	}
	result := agentruntime.RunAgentTurn(host, text, codingHistory(history), rt.httpClient)
	if ctx.Err() != nil {
		return "", true, nil
	}
	files := host.producedFiles()
	if result.AskUser != nil {
		question := strings.TrimSpace(result.AskUser.Question)
		if strings.TrimSpace(result.Text) != "" {
			question = strings.TrimSpace(result.Text) + "\n" + question
		}
		rt.publish(inst, taskID, requestID, "response", question, map[string]any{
			"terminal": attachFiles(askTerminal(question, taskID), files),
		})
		return question, false, files
	}
	if strings.TrimSpace(result.Error) != "" {
		rt.publish(inst, taskID, requestID, "response", result.Error, map[string]any{
			"terminal": attachFiles(map[string]any{"text": result.Error}, files),
		})
		return result.Error, true, files
	}
	rt.publish(inst, taskID, requestID, "response", result.Text, map[string]any{
		"terminal": attachFiles(map[string]any{"text": result.Text}, files),
	})
	return result.Text, false, files
}

func (rt *Runtime) maclawCfg(inst *instance) corelib.MaclawLLMConfig {
	cfg, err := rt.llmConfig(inst)
	if err != nil {
		return corelib.MaclawLLMConfig{}
	}
	if len(cfg.MaclawLLMProviders) > 0 {
		item := cfg.MaclawLLMProviders[0]
		return corelib.MaclawLLMConfig{
			URL:                      strings.TrimRight(item.URL, "/"),
			Key:                      item.Key,
			Model:                    item.Model,
			Protocol:                 item.Protocol,
			WireAPI:                  item.WireAPI,
			ProviderName:             item.Name,
			MaclawAgentMaxIterations: 30,
		}
	}
	return corelib.MaclawLLMConfig{
		URL:                      cfg.MaclawLLMUrl,
		Key:                      cfg.MaclawLLMKey,
		Model:                    cfg.MaclawLLMModel,
		Protocol:                 cfg.MaclawLLMProtocol,
		WireAPI:                  "chat",
		MaclawAgentMaxIterations: 30,
	}
}

func askTerminal(question, taskID string) map[string]any {
	const options = `["仅这一次","本任务内允许","拒绝"]`
	return map[string]any{
		"text":                  question,
		"ask_user_question":     question,
		"ask_user_options_json": options,
		"ask_user_input_type":   "choice",
		"confirmation":          map[string]any{"id": "commit-approval;" + taskID},
		"actions": []map[string]string{
			{"label": "仅这一次", "command": "仅这一次"},
			{"label": "本任务内允许", "command": "本任务内允许"},
			{"label": "拒绝", "command": "拒绝"},
		},
	}
}

func codingHistory(rows []string) []agent.ConversationEntry {
	msgs := priorHistory(rows)
	out := make([]agent.ConversationEntry, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, agent.ConversationEntry{Role: string(msg.Role), Content: msg.Content})
	}
	return out
}

// agentHost is the shared workspace host for both instance kinds — the
// session loop and the coding loop run the same agent kernel, tools and
// streaming; only the prompt and the git tool pair differ.
type agentHost struct {
	rt      *Runtime
	inst    *instance
	task    string
	request string
	root    string
	turn    context.Context
	cfg     corelib.MaclawLLMConfig
	coding  bool
	started time.Time
	files   map[string]bool
}

func (h *agentHost) GetLLMConfig() corelib.MaclawLLMConfig { return h.cfg }
func (h *agentHost) GetMaxIterations() int {
	return config.EffectiveMaxIterations(h.cfg.MaclawAgentMaxIterations)
}
func (h *agentHost) BuildSystemPrompt(string, bool) string {
	if !h.coding {
		return "你是这个云端会话实例。读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
			"运行命令用 bash，读文件用 read_file，写文件用 write_file。"
	}
	return "你是这个云端编程实例。工作区只有已检出的那一个代码库。" +
		"读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
		"提交用 git_commit。推送用 git_push，推送前必须等人确认。" +
		"不要执行 git clone、fetch、pull、push、commit。"
}
func (h *agentHost) BuildTools(string) []map[string]interface{} {
	tools := []map[string]interface{}{
		fnTool("bash", "在工作区里运行一个 argv 命令。不要用 shell 字符串。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"argv": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"argv"},
		}),
		fnTool("read_file", "读取工作区里的文件。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path"},
		}),
		fnTool("write_file", "写入工作区里的文件。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "content"},
		}),
	}
	if !h.coding {
		return tools
	}
	return append(tools,
		fnTool("git_commit", "用仓库里保存的作者提交已跟踪的改动。这一步不会推送。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string"},
			},
			"required": []string{"message"},
		}),
		fnTool("git_push", "推送当前分支。没有人的确认时不会执行。", map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}),
	)
}
func (h *agentHost) ExecuteTool(name, argsJSON string) string {
	// The same styled activity line the desktop tray emits, so a remote
	// viewer folds cloud tool calls into the same transcript timeline.
	if line := cloudToolStatusLine(name, argsJSON); line != "" {
		h.rt.publish(h.inst, h.task, h.request, "progress", line, nil)
	}
	switch strings.TrimSpace(name) {
	case "bash":
		return h.rt.execBash(h.turn, h.inst, h.root, argsJSON)
	case "read_file":
		return h.rt.execReadFile(h.inst, argsJSON)
	case "write_file":
		out, path := h.rt.execWriteFile(h.inst, h.root, argsJSON)
		if path != "" {
			h.noteFile(path)
		}
		return out
	case "git_commit":
		var body struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &body)
		text, err := h.rt.Commit(h.turn, h.inst.meta.ID, body.Message)
		if err != nil {
			return err.Error()
		}
		return text
	case "git_push":
		text, err := h.rt.Push(h.turn, h.inst.meta.ID, h.task)
		if err != nil {
			if strings.TrimSpace(text) != "" {
				return text
			}
			return err.Error()
		}
		return text
	default:
		return "不支持这个工具"
	}
}
func (h *agentHost) OnToken(delta string) {
	if strings.HasPrefix(delta, "\x01") {
		// Reasoning deltas ride OnToken behind the sentinel. Forward them as
		// thinking events so viewers fold them into the thinking block like
		// the local transcript does, not into the answer body.
		h.rt.publish(h.inst, h.task, h.request, "thinking", delta[1:], map[string]any{"state": "delta"})
		return
	}
	h.rt.publish(h.inst, h.task, h.request, "token", delta, nil)
}
func (h *agentHost) OnProgress(text string) {
	h.rt.publish(h.inst, h.task, h.request, "progress", text, nil)
}
func (h *agentHost) OnToolCall(string)   {}
func (h *agentHost) OnToolResult(string) {}
func (h *agentHost) ShouldStop() bool {
	return h.turn != nil && h.turn.Err() != nil
}
func (h *agentHost) LLMRequestContext(int) (context.Context, func(error), error) {
	ctx := h.turn
	if ctx == nil {
		ctx = context.Background()
	}
	return withInstance(ctx, h.inst.meta.ID), func(error) {}, nil
}

// noteFile records one produced file under its canonical jail-relative path —
// the same form producedPaths and the fetch whitelist compare against.
func (h *agentHost) noteFile(abs string) {
	if h.files == nil {
		h.files = map[string]bool{}
	}
	if rel := canonicalArtifactRel(h.root, abs); rel != "" {
		h.files[rel] = true
	}
}

// canonicalArtifactRel is the stored artifact key: path relative to the jail
// root, slash-separated so it compares equal across platforms.
func canonicalArtifactRel(root, abs string) string {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(abs))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.ToSlash(rel)
}

// producedSkipDirs never count as artifacts — tool noise, not deliverables.
var producedSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"__pycache__": true, ".venv": true, "venv": true,
}

const producedFilesMax = 50
const producedScanEntries = 20000

// producedFiles is the turn's artifact set: explicit write_file targets plus
// any workspace file whose mtime landed inside the turn window, which is how
// files a bash command generated still reach the card.
func (h *agentHost) producedFiles() []string {
	seen := map[string]bool{}
	for rel := range h.files {
		seen[rel] = true
	}
	if !h.started.IsZero() {
		floor := h.started.Add(-2 * time.Second)
		walked := 0
		_ = filepath.WalkDir(h.root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			walked++
			if walked > producedScanEntries {
				return filepath.SkipAll
			}
			if entry.IsDir() {
				if path != h.root && producedSkipDirs[strings.ToLower(entry.Name())] {
					return filepath.SkipDir
				}
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil || !info.Mode().IsRegular() || info.ModTime().Before(floor) {
				return nil
			}
			if rel := canonicalArtifactRel(h.root, path); rel != "" {
				seen[rel] = true
			}
			return nil
		})
	}
	out := make([]string, 0, len(seen))
	for rel := range seen {
		out = append(out, rel)
	}
	sort.Strings(out)
	if len(out) > producedFilesMax {
		out = out[:producedFilesMax]
	}
	return out
}

// attachFiles puts the turn's produced-file list on a response terminal —
// the same local_file_paths shape the desktop terminal carries.
func attachFiles(terminal map[string]any, files []string) map[string]any {
	if len(files) > 0 {
		terminal["local_file_paths"] = files
	}
	return terminal
}

// execBash, execReadFile and execWriteFile are the shared workspace tool
// implementations — session and coding hosts route to the same bodies, so
// the tool surface is maintained in exactly one place.
func (rt *Runtime) execBash(ctx context.Context, inst *instance, root, argsJSON string) string {
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil || len(body.Argv) == 0 {
		return "请用 argv 调用"
	}
	out, err := RunBash(ctx, root, body.Argv, secretValues(inst.dir)...)
	if err != nil {
		return strings.TrimSpace(string(out) + "\n" + err.Error())
	}
	return string(out)
}

func (rt *Runtime) execReadFile(inst *instance, argsJSON string) string {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
		return "路径无效"
	}
	raw, err := rt.fetchFile(inst, body.Path)
	if err != nil {
		return err.Error()
	}
	return string(raw)
}

// execWriteFile returns the tool result text and the resolved file path on
// success — the host records that path as a produced artifact. A failed
// write returns an empty path so pre-existing files are never counted.
func (rt *Runtime) execWriteFile(inst *instance, root, argsJSON string) (string, string) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
		return "路径无效", ""
	}
	path, err := resolveInJail(root, body.Path)
	if err != nil {
		return err.Error(), ""
	}
	if err := os.WriteFile(path, []byte(body.Content), 0o644); err != nil {
		return err.Error(), ""
	}
	return "已写入 " + body.Path, path
}

// cloudToolStatusLine renders one tool call as the "工具 · action (name)"
// activity line the desktop tray and the remote tool timeline already parse.
// The viewer's language is not known here; the wire shape is zh, same as the
// rest of this service's user-facing strings.
func cloudToolStatusLine(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	action := "调用工具"
	detail := ""
	switch name {
	case "bash":
		action = "执行命令"
		detail = joinArgs(args, "argv")
	case "read_file":
		action = "读取文件"
		detail = stringArg(args, "path")
	case "write_file":
		action = "写入文件"
		detail = stringArg(args, "path")
	case "git_commit":
		action = "提交改动"
		detail = stringArg(args, "message")
	case "git_push":
		action = "推送代码"
	default:
		detail = name
	}
	line := "工具 · " + action + " (" + name + ")"
	if detail = collapseToolDetail(detail); detail != "" {
		line += "\n" + detail
	}
	return line
}

func stringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := args[key].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func joinArgs(args map[string]any, key string) string {
	list, ok := args[key].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func collapseToolDetail(detail string) string {
	detail = strings.Join(strings.Fields(strings.TrimSpace(detail)), " ")
	runes := []rune(detail)
	if len(runes) > 100 {
		return string(runes[:97]) + "..."
	}
	return detail
}

func fnTool(name, desc string, params map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}
