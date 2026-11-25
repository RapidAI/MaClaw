package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

// sendCoding starts a programming turn inside the workspace jail — the
// checked-out repo when one is connected, the bare workspace otherwise.
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
	// A plan gate answers on the task that parked it, but that task has no
	// running turn — the running-task fallback above misses it, and an
	// empty task id would file the gate answer as a brand-new request on a
	// new task while the parked plan stays open forever.
	if taskID == "" {
		taskID = inst.book.planPendingTask()
	}
	// A bare send continues the running task: park it under that task too,
	// or the drain would file it as a new one.
	payload.TaskID = taskID
	if _, ok := agent.ParseCommitApprovalAnswer(text); ok && taskID != "" {
		if err := inst.book.RecordAnswer(taskID, text); err != nil && !errors.Is(err, errNotApproval) {
			return nil, err
		}
	}
	// A plan-gate command (按计划开始 / 不用计划，直接做 / 取消这个计划) moves
	// the parked plan record before this send becomes a turn; a queued send
	// re-enters here on drain and sees the record already decided.
	rt.routePlanAnswer(inst, taskID, text)
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
	route := rt.workflowRoute(inst, taskID, text)
	if route.notice != "" {
		rt.publish(inst, taskID, requestID, "response", route.notice, map[string]any{
			"terminal": map[string]any{"text": route.notice},
		})
		return route.notice, false, nil
	}
	phaseRan := route.phaseRan
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
		// maclawCfg flattens llmConfig's error into an empty config; re-read
		// it so a broken custom llm.json reports its own reason instead of
		// blaming the official endpoint.
		if _, lerr := rt.llmConfig(inst); lerr != nil {
			return fail(lerr.Error())
		}
		return fail("云端官方模型未接通")
	}
	history, err := inst.book.History(taskID)
	if err != nil {
		return fail(err.Error())
	}
	// The user row and the display line carry what was typed; a gate answer
	// runs the parked request instead — the model input is `input`, not text.
	input := route.modelText
	var plan *planRecord
	if rec := inst.book.planLoadOrNil(taskID); rec != nil && !phaseRan {
		switch rec.State {
		case planStateRejected:
			_ = inst.book.planClear(taskID)
			rt.publishWorkbench(inst, taskID, requestID, nil)
			note := "已取消这个计划，本轮不会修改文件。"
			rt.publish(inst, taskID, requestID, "response", note, map[string]any{
				"terminal": map[string]any{"text": note},
			})
			return note, false, nil
		case planStateSkipped:
			input = rec.Request
			_ = inst.book.planClear(taskID)
			rt.publishWorkbench(inst, taskID, requestID, nil)
		case planStateApproved:
			input = rec.Request
			rec.State = planStateRunning
			if len(rec.Steps) > 0 && rec.Steps[0].Status == "pending" {
				rec.Steps[0].Status = "running"
				rec.Steps[0].UpdatedUnix = time.Now().Unix()
			}
			rec.Revision++
			_ = inst.book.planSave(taskID, rec)
			rt.publishWorkbench(inst, taskID, requestID, rec)
			plan = rec
		case planStateRunning:
			// A parked turn mid-plan (crash, or a queued commit answer) picks
			// the plan back up; a different ask closes it as failed.
			if planContinuationText(input) {
				plan = rec
			} else {
				rt.settlePlan(inst, taskID, requestID, true)
			}
		case planStatePending:
			// The gate answer raced a second send; the pending record is only
			// consumable through routePlanAnswer, so drop it here.
			_ = inst.book.planClear(taskID)
			rt.publishWorkbench(inst, taskID, requestID, nil)
		}
	}
	// A workflow phase turn carries its own instructions — the plan gate must
	// not park it for planning, or every phase would stall on a gate nobody
	// can answer.
	if plan == nil && input == text && !phaseRan {
		decision := rt.planDecisionFor(ctx, inst, cfg, input)
		if ctx.Err() != nil {
			return "", true, nil
		}
		if decision.NeedsPlan {
			rec := &planRecord{
				State:       planStatePending,
				Request:     input,
				Restatement: decision.Restatement,
				Plan:        planTextOf(decision.Steps),
				Steps:       decision.Steps,
				Revision:    1,
			}
			if err := inst.book.planSave(taskID, rec); err == nil {
				rt.publishWorkbench(inst, taskID, requestID, rec)
				note := "我先把这次任务拆成了执行计划，确认后开始实施。"
				rt.publish(inst, taskID, requestID, "response", note, map[string]any{
					"terminal": planTerminal(taskID),
				})
				return note, false, nil
			}
		}
	}

	rt.emitCodingTaskStatus(inst, taskID, requestID, "starting", input)
	host := &agentHost{
		rt:      rt,
		inst:    inst,
		task:    taskID,
		request: requestID,
		root:    root,
		turn:    ctx,
		cfg:     cfg,
		coding:  true,
		plan:    plan,
		started: time.Now(),
	}
	host.phasePrompt = route.phasePrompt
	result := agentruntime.RunAgentTurn(host, input, codingHistory(history), rt.httpClient)
	if ctx.Err() != nil {
		return "", true, nil
	}
	files := host.producedFiles()
	if len(files) > 0 {
		slog.Info("cloudagent turn produced files",
			slog.String("instance", inst.meta.ID), slog.String("task", taskID),
			slog.String("request", requestID), slog.Any("files", files))
	}
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
		rt.emitCodingTaskStatus(inst, taskID, requestID, "failed", input)
		rt.settlePlan(inst, taskID, requestID, true)
		rt.publish(inst, taskID, requestID, "response", result.Error, map[string]any{
			"terminal": attachFiles(map[string]any{"text": result.Error}, files),
		})
		return result.Error, true, files
	}
	rt.emitCodingTaskStatus(inst, taskID, requestID, "completed", input)
	rt.settlePlan(inst, taskID, requestID, false)
	rt.publish(inst, taskID, requestID, "response", result.Text, map[string]any{
		"terminal": attachFiles(map[string]any{"text": result.Text}, files),
	})
	if phaseRan {
		rt.workflowRecordOutput(inst, taskID, result.Text)
	}
	return result.Text, false, files
}

// planContinuationText recognizes the messages that resume a parked plan
// turn — a commit-gate answer or a bare "continue" — so the same workbench
// keeps reporting instead of being closed as a failed leftover.
func planContinuationText(text string) bool {
	if _, ok := agent.ParseCommitApprovalAnswer(text); ok {
		return true
	}
	switch strings.Join(strings.Fields(strings.TrimSpace(text)), "") {
	case "继续", "继续吧", "接着做", "继续执行", "continue", "resume":
		return true
	}
	return false
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
			AgentType:                item.AgentType,
			ContextLength:            item.ContextLength,
			TimeoutSec:               item.TimeoutSec,
			MaxOutputTokens:          item.MaxOutputTokens,
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
	// plan is the approved workbench plan this turn executes; nil for a
	// direct request. Its presence also surfaces update_plan_step.
	plan *planRecord
	// phasePrompt is the V2 workflow phase instruction; non-empty only on a
	// turn the workflow router marked as a phase run.
	phasePrompt string
	started     time.Time
	files       map[string]bool
	// iterationExtensions counts the extra waves this turn was granted
	// after spending its configured iteration budget.
	iterationExtensions int
}

func (h *agentHost) GetLLMConfig() corelib.MaclawLLMConfig { return h.cfg }
func (h *agentHost) GetMaxIterations() int {
	return config.EffectiveMaxIterations(h.cfg.MaclawAgentMaxIterations)
}

// ExtendIterationBudget keeps this same conversation going when the
// iteration counter runs out — a cloud turn has nobody to send 继续, and
// ending on "max iterations reached" abandons work that is still moving.
// Reaching the budget means the loop's own stall detectors (the
// no-progress hard stop, repeated-commit and drift checks) never fired,
// so the turn is still making progress; a wedged turn is ended by those
// verdicts, by Stop, or by a heartbeat interrupt, not by a fixed count.
func (h *agentHost) ExtendIterationBudget(used, toolCalls int) int {
	if h == nil || toolCalls <= 0 {
		return 0
	}
	if h.turn != nil && h.turn.Err() != nil {
		return 0
	}
	extra := h.GetMaxIterations()
	if extra <= 0 {
		return 0
	}
	h.iterationExtensions++
	instanceID := ""
	if h.inst != nil {
		instanceID = h.inst.meta.ID
	}
	slog.Info("cloudagent iteration budget extended",
		slog.String("instance", instanceID), slog.String("task", h.task),
		slog.String("request", h.request), slog.Int("used", used),
		slog.Int("tool_calls", toolCalls), slog.Int("extra", extra),
		slog.Int("wave", h.iterationExtensions))
	return extra
}
func (h *agentHost) BuildSystemPrompt(string, bool) string {
	var base string
	if !h.coding {
		base = "你是这个云端会话实例。读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
			"运行命令用 bash，读文件用 read_file，写文件用 write_file。"
	} else if _, err := readGit(h.inst.dir); err != nil {
		base = "你是这个云端编程实例。工作区还没有连接代码库，可以直接在里面创建和修改项目文件。" +
			"读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
			"连接代码库之后才能用 git_commit 和 git_push；不要在 shell 里执行 git。"
	} else {
		base = "你是这个云端编程实例。工作区只有已检出的那一个代码库。" +
			"读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
			"提交用 git_commit。推送用 git_push，推送前必须等人确认。" +
			"shell 里只允许只读 git 命令(status/diff/log 等)，其它 git 写操作不要执行。"
	}
	// The task's AI expert persona leads, the same way the desktop prompt
	// swaps the global role description for the expert's own.
	if name, prompt := h.inst.book.Persona(h.task); name != "" || prompt != "" {
		persona := "你是" + name
		if prompt != "" {
			persona += "。" + prompt
		}
		persona += "\n请始终以该专家身份回应，不越界处理无关事务。"
		base = persona + "\n\n" + base
	}
	if h.plan != nil && len(h.plan.Steps) > 0 {
		base += "\n本次任务按用户已确认的计划执行：\n" + planTextOf(h.plan.Steps) +
			"\n开始每一步之前先调用 update_plan_step {\"index\":<步骤号>,\"status\":\"running\"}；" +
			"完成时用 \"passed\"，失败用 \"failed\"，跳过用 \"skipped\"，可附 \"summary\" 写一句结果。"
	}
	if h.phasePrompt != "" {
		base += "\n\n" + h.phasePrompt
	}
	return base
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
	tools = append(tools,
		fnTool("git_commit", "用仓库里保存的作者提交仓库中的全部改动(含新文件)。这一步不会推送。", map[string]interface{}{
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
	if h.plan != nil {
		tools = append(tools, fnTool("update_plan_step", "汇报已确认计划的步骤进度：开始一步 status=running，完成 passed，失败 failed，跳过 skipped。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"index":   map[string]interface{}{"type": "integer"},
				"status":  map[string]interface{}{"type": "string"},
				"summary": map[string]interface{}{"type": "string"},
			},
			"required": []string{"index", "status"},
		}))
	}
	return tools
}
func (h *agentHost) ExecuteTool(name, argsJSON string) string {
	name = strings.TrimSpace(name)
	if !h.coding {
		// Session instances keep the styled activity line the desktop tray
		// emits; coding turns report Coding Agent Event lines instead so the
		// viewer paints the same feed the local coding agent does.
		if line := cloudToolStatusLine(name, argsJSON); line != "" {
			h.rt.publish(h.inst, h.task, h.request, "progress", line, nil)
		}
		result, _ := h.runTool(name, argsJSON)
		return result
	}
	h.rt.emitToolStarted(h.inst, h.task, h.request, name, argsJSON)
	started := time.Now()
	result, failed := h.runTool(name, argsJSON)
	h.rt.emitToolFinished(h.inst, h.task, h.request, name, argsJSON, result, failed, time.Since(started))
	return result
}

// runTool returns the tool result and whether the call failed — the second
// value feeds the tool_finished outcome, which a result string alone
// cannot decide (an error body and output that mentions errors look alike).
func (h *agentHost) runTool(name, argsJSON string) (string, bool) {
	switch name {
	case "bash":
		return h.rt.execBash(h.turn, h.inst, h.root, argsJSON)
	case "read_file":
		return h.rt.execReadFile(h.inst, argsJSON)
	case "write_file":
		out, path := h.rt.execWriteFile(h.inst, h.root, argsJSON)
		if path != "" {
			h.noteFile(path)
		}
		return out, path == ""
	case "git_commit":
		var body struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &body)
		text, err := h.rt.Commit(h.turn, h.inst.meta.ID, body.Message)
		if err != nil {
			return err.Error(), true
		}
		return text, false
	case "git_push":
		text, err := h.rt.Push(h.turn, h.inst.meta.ID, h.task)
		if err != nil {
			if strings.TrimSpace(text) != "" {
				// The push parked on human approval — the tool ran its part.
				return text, false
			}
			return err.Error(), true
		}
		return text, false
	case "update_plan_step":
		return h.rt.execUpdatePlanStep(h.inst, h.task, h.request, argsJSON)
	default:
		return "不支持这个工具", true
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
		slog.Debug("cloudagent artifact noted",
			slog.String("instance", h.inst.meta.ID), slog.String("task", h.task),
			slog.String("request", h.request), slog.String("path", rel))
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
func (rt *Runtime) execBash(ctx context.Context, inst *instance, root, argsJSON string) (string, bool) {
	var body struct {
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil || len(body.Argv) == 0 {
		return "请用 argv 调用", true
	}
	out, err := RunBash(ctx, root, body.Argv, secretValues(inst.dir)...)
	if err != nil {
		return strings.TrimSpace(string(out) + "\n" + err.Error()), true
	}
	return string(out), false
}

func (rt *Runtime) execReadFile(inst *instance, argsJSON string) (string, bool) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
		return "路径无效", true
	}
	raw, err := rt.fetchFile(inst, body.Path)
	if err != nil {
		return err.Error(), true
	}
	return string(raw), false
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
