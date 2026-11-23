package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	if _, ok := agent.ParseCommitApprovalAnswer(text); ok && taskID != "" {
		if err := inst.book.RecordAnswer(taskID, text); err != nil && !errors.Is(err, errNotApproval) {
			return nil, err
		}
		payload.TaskID = taskID
	}
	requestID, taskID, created, err := rt.begin(inst, payload.ClientMessageID, payload.TaskID, text)
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
	content, asError := rt.codingResult(ctx, inst, taskID, requestID, text)
	if ctx.Err() != nil {
		return
	}
	rt.finishTurn(inst, requestID, taskID, content, asError, time.Now())
}

func (rt *Runtime) codingResult(ctx context.Context, inst *instance, taskID, requestID, text string) (string, bool) {
	root, err := jailRoot(inst.dir, KindCoding)
	if err != nil {
		return err.Error(), true
	}
	cfg := rt.maclawCfg(inst)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "云端官方模型未接通", true
	}
	history, err := inst.book.History(taskID)
	if err != nil {
		return err.Error(), true
	}
	host := &codingHost{
		rt:      rt,
		inst:    inst,
		task:    taskID,
		request: requestID,
		root:    root,
		turn:    ctx,
		cfg:     cfg,
	}
	rt.publish(inst, taskID, requestID, "round_started", text, map[string]any{"display_text": text})
	result := agent.RunLoop(host, text, codingHistory(history), rt.httpClient)
	if ctx.Err() != nil {
		return "", true
	}
	if result.AskUser != nil {
		question := strings.TrimSpace(result.AskUser.Question)
		if strings.TrimSpace(result.Text) != "" {
			question = strings.TrimSpace(result.Text) + "\n" + question
		}
		rt.publish(inst, taskID, requestID, "response", question, map[string]any{
			"terminal": askTerminal(question, taskID),
		})
		return question, false
	}
	if strings.TrimSpace(result.Error) != "" {
		rt.publish(inst, taskID, requestID, "response", result.Error, map[string]any{
			"terminal": map[string]any{"text": result.Error},
		})
		return result.Error, true
	}
	rt.publish(inst, taskID, requestID, "response", result.Text, map[string]any{
		"terminal": map[string]any{"text": result.Text},
	})
	return result.Text, false
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

type codingHost struct {
	rt      *Runtime
	inst    *instance
	task    string
	request string
	root    string
	turn    context.Context
	cfg     corelib.MaclawLLMConfig
}

func (h *codingHost) GetLLMConfig() corelib.MaclawLLMConfig { return h.cfg }
func (h *codingHost) GetMaxIterations() int {
	return config.EffectiveMaxIterations(h.cfg.MaclawAgentMaxIterations)
}
func (h *codingHost) BuildSystemPrompt(string, bool) string {
	return "你是这个云端编程实例。工作区只有已检出的那一个代码库。" +
		"读和写必须留在工作区里，不能读取 git.json 或 llm.json。" +
		"提交用 git_commit。推送用 git_push，推送前必须等人确认。" +
		"不要执行 git clone、fetch、pull、push、commit。"
}
func (h *codingHost) BuildTools(string) []map[string]interface{} {
	return []map[string]interface{}{
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
	}
}
func (h *codingHost) ExecuteTool(name, argsJSON string) string {
	switch strings.TrimSpace(name) {
	case "bash":
		var body struct {
			Argv []string `json:"argv"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &body); err != nil || len(body.Argv) == 0 {
			return "请用 argv 调用"
		}
		out, err := RunBash(h.root, body.Argv, secretValues(h.inst.dir)...)
		if err != nil {
			return strings.TrimSpace(string(out) + "\n" + err.Error())
		}
		return string(out)
	case "read_file":
		var body struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
			return "路径无效"
		}
		raw, err := h.rt.fetchFile(h.inst, body.Path)
		if err != nil {
			return err.Error()
		}
		return string(raw)
	case "write_file":
		var body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
			return "路径无效"
		}
		path, err := resolveInJail(h.root, body.Path)
		if err != nil {
			return err.Error()
		}
		if err := os.WriteFile(path, []byte(body.Content), 0o644); err != nil {
			return err.Error()
		}
		return "已写入 " + body.Path
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
func (h *codingHost) OnToken(delta string) {
	h.rt.publish(h.inst, h.task, h.request, "token", delta, nil)
}
func (h *codingHost) OnProgress(text string) {
	h.rt.publish(h.inst, h.task, h.request, "progress", text, nil)
}
func (h *codingHost) OnToolCall(string)   {}
func (h *codingHost) OnToolResult(string) {}
func (h *codingHost) ShouldStop() bool {
	return h.turn != nil && h.turn.Err() != nil
}
func (h *codingHost) LLMRequestContext(int) (context.Context, func(error), error) {
	ctx := h.turn
	if ctx == nil {
		ctx = context.Background()
	}
	return withInstance(ctx, h.inst.meta.ID), func(error) {}, nil
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
