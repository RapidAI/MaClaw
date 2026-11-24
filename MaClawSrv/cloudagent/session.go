package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type executor interface {
	Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error)
	Close() error
}

type turnBody struct {
	ClientMessageID string `json:"client_message_id"`
	TaskID          string `json:"task_id"`
	Text            string `json:"text"`
	Content         string `json:"content"`
	Title           string `json:"title"`
	Name            string `json:"name"`
	Path            string `json:"path"`
}

func (b turnBody) text() string {
	if text := strings.TrimSpace(b.Text); text != "" {
		return text
	}
	return strings.TrimSpace(b.Content)
}

// Control handles one cloud op. cloud_send_message returns before the turn finishes.
func (rt *Runtime) Control(ctx context.Context, id, owner, op string, body json.RawMessage) (json.RawMessage, error) {
	inst, err := rt.instance(id)
	if err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	if owner != "" && owner != inst.meta.OwnerUserID {
		return nil, errOwner
	}
	var payload turnBody
	if len(strings.TrimSpace(string(body))) > 0 && string(body) != "null" {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, errors.New("请求无效")
		}
	}
	switch op {
	case "cloud_send_message":
		if inst.meta.Kind == KindCoding {
			return rt.sendCoding(ctx, inst, payload)
		}
		return rt.sendSession(inst, payload)
	case "cloud_create_task":
		taskID := newID("task_")
		title := payload.Title
		if title == "" {
			title = payload.Name
		}
		if err := inst.book.EnsureTask(taskID, title); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"accepted": true, "task_id": taskID})
	case "cloud_list_tasks":
		tasks, err := inst.book.Tasks()
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"tasks": tasks})
	case "cloud_get_task":
		lines, flags, err := inst.book.history(payload.TaskID)
		if err != nil {
			return nil, err
		}
		turnStatus, err := inst.book.latestTurn(payload.TaskID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"task_id":     payload.TaskID,
			"messages":    lines,
			"as_error":    flags,
			"turn_status": turnStatus,
		})
	case "cloud_rename_task":
		title := payload.Title
		if title == "" {
			title = payload.Name
		}
		if err := inst.book.Rename(payload.TaskID, title); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"ok": true})
	case "cloud_stop_task":
		ids, err := inst.book.CancelRunning(payload.TaskID, time.Now())
		rt.cancelIDs(ids)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"ok": true, "cancelled": ids})
	case "cloud_instance_state":
		// Hub-side ground truth reconcile: a lost finish beat leaves a stale
		// running row that pins the send slot for the whole miss window. The
		// book is authoritative — a send blocked by shadow state asks here.
		taskID, requestID := inst.book.Running()
		return json.Marshal(map[string]any{
			"running":    requestID != "",
			"task_id":    taskID,
			"request_id": requestID,
		})
	case "cloud_watch_task":
		return rt.watchSnapshot(inst, payload.TaskID)
	case "cloud_unwatch_task":
		return json.Marshal(map[string]any{"ok": true})
	case "cloud_fetch_file":
		return rt.fetchFile(inst, payload.Path)
	default:
		return nil, errors.New("不支持这个云端操作")
	}
}

func (rt *Runtime) sendSession(inst *instance, payload turnBody) (json.RawMessage, error) {
	text := payload.text()
	if text == "" {
		return nil, errors.New("请输入要发送的内容")
	}
	requestID, taskID, created, err := rt.begin(inst, payload.ClientMessageID, payload.TaskID, text)
	if err != nil {
		return nil, err
	}
	if created {
		ctx, cancel := context.WithCancel(context.Background())
		rt.track(requestID, cancel)
		go rt.runSession(ctx, inst, requestID, taskID, text)
	}
	return json.Marshal(map[string]any{"accepted": true, "request_id": requestID, "task_id": taskID})
}

func (rt *Runtime) begin(inst *instance, clientID, taskID, text string) (requestID, gotTask string, created bool, err error) {
	requestID, created, err = inst.book.Begin(clientID, taskID, text, time.Now())
	if err != nil || !created && requestID == "" {
		return requestID, taskID, created, err
	}
	gotTask, err = inst.book.TaskOf(requestID)
	if err != nil {
		return requestID, taskID, created, err
	}
	return requestID, gotTask, created, nil
}

func (rt *Runtime) runSession(ctx context.Context, inst *instance, requestID, taskID, text string) {
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
	rt.publish(inst, taskID, requestID, "round_started", text, map[string]any{"display_text": text})
	content, asError := rt.sessionResult(ctx, inst, taskID, requestID, text)
	if ctx.Err() != nil {
		return
	}
	// sessionResult owns the terminal response frame — publishing another
	// here would give viewers two "response" events for one turn.
	rt.finishTurn(inst, requestID, taskID, content, asError, time.Now())
}

// sessionResult runs the session turn through the same agent kernel the
// coding kind uses — one shared host, one tool-routing implementation, real
// token streaming — instead of the bare executor that had no workspace
// tools at all.
func (rt *Runtime) sessionResult(ctx context.Context, inst *instance, taskID, requestID, text string) (string, bool) {
	fail := func(msg string) (string, bool) {
		rt.publish(inst, taskID, requestID, "response", msg, map[string]any{
			"terminal": map[string]any{"text": msg},
		})
		return msg, true
	}
	if inst.meta.LLMMode != llmCustom {
		if err := officialReady(inst.meta); err != nil {
			return fail(err.Error())
		}
	}
	cfg := rt.maclawCfg(inst)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fail("云端官方模型未接通")
	}
	root, err := jailRoot(inst.dir, inst.meta.Kind)
	if err != nil {
		return fail(err.Error())
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
	}
	result := agentruntime.RunAgentTurn(host, text, codingHistory(history), rt.httpClient)
	if ctx.Err() != nil {
		return "", true
	}
	if result.AskUser != nil {
		question := strings.TrimSpace(result.AskUser.Question)
		if strings.TrimSpace(result.Text) != "" {
			question = strings.TrimSpace(result.Text) + "\n" + question
		}
		rt.publish(inst, taskID, requestID, "response", question, map[string]any{
			"terminal": map[string]any{"text": question},
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

func priorHistory(rows []string) []agentservice.Message {
	out := make([]agentservice.Message, 0, len(rows))
	for _, row := range rows {
		role, content, _ := strings.Cut(row, "\n")
		out = append(out, agentservice.Message{Role: agentservice.MessageRole(role), Content: content})
	}
	if n := len(out); n > 0 && out[n-1].Role == agentservice.MessageRoleUser {
		out = out[:n-1]
	}
	return out
}

func (rt *Runtime) executor() executor {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.exec == nil {
		rt.exec = &agentservice.CoreAgentExecutor{HTTPClient: rt.httpClient}
	}
	return rt.exec
}

func (rt *Runtime) fetchFile(inst *instance, rel string) (json.RawMessage, error) {
	root, err := jailRoot(inst.dir, inst.meta.Kind)
	if err != nil {
		return nil, err
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"path": rel, "content": string(raw)})
}
