package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
		lines, err := inst.book.History(payload.TaskID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"task_id": payload.TaskID, "messages": lines})
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
	content, asError := rt.sessionResult(ctx, inst, taskID, text)
	if ctx.Err() != nil {
		return
	}
	rt.publish(inst, taskID, requestID, "response", content, map[string]any{
		"terminal": map[string]any{"text": content},
	})
	rt.finishTurn(inst, requestID, taskID, content, asError, time.Now())
}

func (rt *Runtime) sessionResult(ctx context.Context, inst *instance, taskID, text string) (string, bool) {
	cfg, err := rt.llmConfig(inst)
	if err != nil {
		return err.Error(), true
	}
	if inst.meta.LLMMode != llmCustom {
		if err := officialReady(inst.meta); err != nil {
			return err.Error(), true
		}
	}
	ctx = withInstance(ctx, inst.meta.ID)
	history, err := inst.book.History(taskID)
	if err != nil {
		return err.Error(), true
	}
	result, err := rt.executor().Execute(ctx, sessionRequest(inst, taskID, text, cfg, history))
	if ctx.Err() != nil {
		return "", true
	}
	if err != nil {
		if result != nil && strings.TrimSpace(result.Content) != "" {
			return result.Content, true
		}
		return err.Error(), true
	}
	if result == nil {
		return "", false
	}
	return result.Content, false
}

func sessionRequest(inst *instance, taskID, text string, cfg corelib.AppConfig, history []string) agentservice.ExecuteRequest {
	// The memory store is keyed by tenant and user. Using the instance id
	// keeps two instances of one owner from sharing a store.
	userID := inst.meta.ID
	return agentservice.ExecuteRequest{
		Principal: agentservice.Principal{TenantID: inst.meta.TenantID, UserID: userID},
		Tenant:    agentservice.Tenant{ID: inst.meta.TenantID},
		User:      agentservice.User{ID: userID, TenantID: inst.meta.TenantID},
		Instance: agentservice.Instance{
			ID:        inst.meta.ID,
			TenantID:  inst.meta.TenantID,
			UserID:    userID,
			Name:      inst.meta.Name,
			DataDir:   inst.dir,
			Workspace: filepath.Join(inst.dir, "workspace"),
		},
		Session: agentservice.Session{
			ID:         taskID,
			TenantID:   inst.meta.TenantID,
			UserID:     userID,
			InstanceID: inst.meta.ID,
		},
		Message: agentservice.Message{Role: agentservice.MessageRoleUser, Content: text},
		History: priorHistory(history),
		DataDir: inst.dir,
		Config:  cfg,
		Host:    agentruntime.HeadlessHostCapabilities{},
	}
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
