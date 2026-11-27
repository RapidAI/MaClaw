package cloudagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
	MaxBytes        int64  `json:"max_bytes"`
	DataBase64      string `json:"data_base64"`
	Offset          int64  `json:"offset"`
	// Task persona + workflow setup shipped by the desktop at create time.
	ExpertName   string         `json:"expert_name"`
	ExpertPrompt string         `json:"expert_prompt"`
	WorkflowID   string         `json:"workflow_template_id"`
	WorkflowArgs map[string]any `json:"workflow_params"`
	// Values is the cloud_submit_workflow_form body: the gated phase's fields.
	Values map[string]any `json:"values"`
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
		return rt.send(ctx, inst, payload)
	case "cloud_submit_workflow_form":
		return rt.submitWorkflowForm(ctx, inst, payload)
	case "cloud_create_task":
		taskID := newID("task_")
		title := payload.Title
		if title == "" {
			title = payload.Name
		}
		if err := inst.book.EnsureTask(taskID, title); err != nil {
			return nil, err
		}
		if name, prompt := strings.TrimSpace(payload.ExpertName), strings.TrimSpace(payload.ExpertPrompt); name != "" || prompt != "" {
			if err := inst.book.SetPersona(taskID, name, prompt); err != nil {
				return nil, err
			}
		}
		var warning string
		if templateID := strings.TrimSpace(payload.WorkflowID); templateID != "" {
			// The task is already recorded, so a bad template id degrades to a
			// warning instead of leaving an orphan sidebar row — same contract
			// the desktop's unified create keeps. The workflow summary is the
			// full first instruction when the caller ships it — ActionCancel-
			// AndExecute replays Summary, and the 48-char title would truncate it.
			summary := payload.text()
			if summary == "" {
				summary = title
			}
			if err := rt.workflowCreate(inst, taskID, templateID, payload.WorkflowArgs, summary); err != nil {
				slog.Warn("cloudagent workflow create failed",
					slog.String("instance", inst.meta.ID), slog.String("task", taskID),
					slog.String("template", templateID), slog.Any("err", err))
				warning = err.Error()
			}
		}
		return json.Marshal(map[string]any{"accepted": true, "task_id": taskID, "workflow_warning": warning})
	case "cloud_list_tasks":
		tasks, err := inst.book.Tasks()
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"tasks": tasks})
	case "cloud_get_task":
		lines, flags, files, err := inst.book.history(payload.TaskID)
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
			"files":       files,
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
		rt.cancelIDsAndDrain(ids, inst)
		if err != nil {
			return nil, err
		}
		// Stopping the task also stops its workflow — same semantic the
		// desktop's stop button applies to a running flow.
		if machine, werr := rt.workflowMachine(inst); werr == nil {
			if state := machine.GetActive(payload.TaskID); state != nil && state.Status == v2.StatusActive {
				if cerr := machine.Cancel(payload.TaskID); cerr == nil {
					rt.publishWorkflowState(inst, payload.TaskID, workflowLatest(machine, payload.TaskID))
				}
			}
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
		return rt.serveFile(inst, payload.Path)
	case "cloud_list_dir":
		return rt.browseListDir(inst, payload.Path)
	case "cloud_read_file":
		return rt.browseReadFile(inst, payload.Path, payload.MaxBytes)
	case "cloud_stat":
		return rt.browseStat(inst, payload.Path)
	case "cloud_delete_file":
		return rt.browseDelete(inst, payload.Path)
	case "cloud_pack_dir":
		return rt.browsePackDir(inst, payload.Path)
	case "cloud_write_file":
		return rt.browseWriteFile(inst, payload.Path, payload.DataBase64, payload.Offset)
	case "cloud_mkdir":
		return rt.browseMkdir(inst, payload.Path)
	default:
		return nil, errors.New("不支持这个云端操作")
	}
}

func (rt *Runtime) sendSession(inst *instance, payload turnBody) (json.RawMessage, error) {
	text := payload.text()
	if text == "" {
		return nil, errors.New("请输入要发送的内容")
	}
	inst.drainMu.Lock()
	defer inst.drainMu.Unlock()
	requestID, taskID, created, err := rt.begin(inst, payload.ClientMessageID, payload.TaskID, text)
	if errors.Is(err, errBusy) {
		queueID, qerr := inst.book.Enqueue(payload.ClientMessageID, payload.TaskID, text, time.Now())
		if qerr != nil {
			return nil, qerr
		}
		rt.kickDrainIfIdle(inst)
		return json.Marshal(map[string]any{"accepted": true, "queued": true, "queue_id": queueID, "task_id": payload.TaskID})
	}
	if err != nil {
		return nil, err
	}
	if created {
		ctx, cancel := context.WithCancel(context.Background())
		rt.trackTurn(inst, requestID, cancel)
		go rt.runSession(ctx, inst, requestID, taskID, text)
	}
	return json.Marshal(map[string]any{"accepted": true, "request_id": requestID, "task_id": taskID})
}

func (rt *Runtime) kickDrainIfIdle(inst *instance) {
	if inst == nil {
		return
	}
	if _, requestID := inst.book.Running(); requestID == "" {
		go rt.drainQueue(inst)
	}
}

// drainQueue starts the next parked message once the running turn is
// done. Parked rows survive a GUI shutdown and an srv restart because they
// live in this instance's transcript db; Recover kicks the drain back up.
// The queue is per instance, so the runner is the instance's own kind.
func (rt *Runtime) drainQueue(inst *instance) {
	if got, err := rt.instance(inst.meta.ID); err != nil || got != inst {
		return
	}
	inst.drainMu.Lock()
	defer inst.drainMu.Unlock()
	run := rt.runSession
	if inst.meta.Kind == KindCoding {
		run = rt.runCoding
	}
	for {
		if got, err := rt.instance(inst.meta.ID); err != nil || got != inst {
			return
		}
		msg, ok, err := inst.book.PeekOldest()
		if err != nil || !ok {
			return
		}
		if _, running := inst.book.Running(); running != "" {
			return
		}
		// A row can already be terminal in the book while its goroutine is
		// still unwinding a tool call. Never overlap the next turn with that
		// teardown. The wait happens under drainMu because that mutex is also
		// the admission fence: a send cannot commit a durable row and then
		// leave it untracked while a drain is already past this check.
		if waits := rt.instanceTurnWaits(inst.meta.ID); len(waits) > 0 {
			for _, done := range waits {
				<-done
			}
			continue
		}
		if inst.meta.Kind == KindCoding {
			// Gate interpretation belongs to the moment the message executes,
			// not only to the moment it was accepted. This covers rows parked
			// before a crash or before the planner had written its record.
			rt.routePlanAnswer(inst, msg.TaskID, msg.Text)
		}
		requestID, taskID, created, err := rt.beginDrain(inst, msg.ClientID, msg.TaskID, msg.Text)
		if err != nil {
			return
		}
		if !created {
			_ = inst.book.DropQueued(msg.QueueID)
			continue
		}
		if err := inst.book.DropQueued(msg.QueueID); err != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		rt.trackTurn(inst, requestID, cancel)
		go run(ctx, inst, requestID, taskID, msg.Text)
		return
	}
}

func (rt *Runtime) begin(inst *instance, clientID, taskID, text string) (requestID, gotTask string, created bool, err error) {
	return rt.beginBook(inst, clientID, taskID, text, false)
}

func (rt *Runtime) beginDrain(inst *instance, clientID, taskID, text string) (requestID, gotTask string, created bool, err error) {
	return rt.beginBook(inst, clientID, taskID, text, true)
}

func (rt *Runtime) beginBook(inst *instance, clientID, taskID, text string, draining bool) (requestID, gotTask string, created bool, err error) {
	if draining {
		requestID, created, err = inst.book.BeginDrain(clientID, taskID, text, time.Now())
	} else {
		requestID, created, err = inst.book.Begin(clientID, taskID, text, time.Now())
	}
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
	rt.noteTurnStarted(inst, taskID, requestID)
	rt.publish(inst, taskID, requestID, "round_started", text, map[string]any{"display_text": text})
	route := rt.workflowRoute(inst, taskID, text)
	var content string
	var asError bool
	var files []string
	if route.notice != "" {
		// A workflow gate answers the turn itself — no agent loop, but the
		// transcript keeps the same user/assistant pair a model turn leaves.
		rt.publish(inst, taskID, requestID, "response", route.notice, map[string]any{
			"terminal": map[string]any{"text": route.notice},
		})
		content = route.notice
	} else {
		content, asError, files = rt.sessionResult(ctx, inst, taskID, requestID, route.modelText, route.phasePrompt)
	}
	if ctx.Err() != nil {
		return
	}
	if route.phaseRan && !asError {
		rt.workflowRecordOutput(inst, taskID, content)
	}
	// sessionResult owns the terminal response frame — publishing another
	// here would give viewers two "response" events for one turn.
	rt.finishTurn(inst, requestID, taskID, content, asError, files, time.Now())
	rt.untrack(requestID)
	rt.drainQueue(inst)
}

// sessionResult runs the session turn through the same agent kernel the
// coding kind uses — one shared host, one tool-routing implementation, real
// token streaming. The host asks the process semantic resolver for this
// turn's capability needs and publishes that surface when the reading is
// managed and runnable in the jail. A nil resolver, an error, or an
// unmanaged reading keeps the workspace tools.
func (rt *Runtime) sessionResult(ctx context.Context, inst *instance, taskID, requestID, text, phasePrompt string) (string, bool, []string) {
	fail := func(msg string) (string, bool, []string) {
		rt.publish(inst, taskID, requestID, "response", msg, map[string]any{
			"terminal": map[string]any{"text": msg},
		})
		return msg, true, nil
	}
	if inst.meta.LLMMode != llmCustom {
		if err := officialReady(inst.meta); err != nil {
			return fail(err.Error())
		}
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
		started: time.Now(),
	}
	host.phasePrompt = phasePrompt
	result := agentruntime.RunAgentTurn(host, text, codingHistory(history), rt.httpClient)
	if ctx.Err() != nil {
		return "", true, nil
	}
	files := host.producedFiles()
	rt.publishTurnReasoning(host, result)
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
			"terminal": attachFiles(map[string]any{"text": question}, files),
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
	return rt.readWorkspaceFile(inst, "", rel)
}

// readWorkspaceFile reads rel. An empty root uses the published jail; a
// turn passes the root it adopted under jailMu so the read matches the write.
func (rt *Runtime) readWorkspaceFile(inst *instance, root, rel string) (json.RawMessage, error) {
	var err error
	if strings.TrimSpace(root) == "" {
		root, err = jailRoot(inst.dir, inst.meta.Kind)
		if err != nil {
			return nil, err
		}
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if alt, _, ok := strandedRegular(inst.dir, root, canonicalArtifactRel(root, path)); ok {
			file, err = os.Open(alt)
		}
	}
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

// serveFileMaxBytes matches the desktop fetch_file cap so the same download
// path handles local-remote and cloud artifacts identically.
const serveFileMaxBytes = 4 << 20

// serveFile returns one produced file the desktop fetch_file shape — name,
// size, base64 bytes. Like the desktop op it only serves paths a turn
// recorded as produced, never an arbitrary workspace read.
func (rt *Runtime) serveFile(inst *instance, rel string) (json.RawMessage, error) {
	if inst != nil {
		inst.jailMu.Lock()
		defer inst.jailMu.Unlock()
	}
	deny := func(reason string, err error) (json.RawMessage, error) {
		slog.Warn("cloudagent fetch artifact denied",
			slog.String("instance", inst.meta.ID), slog.String("path", rel), slog.String("reason", reason))
		return nil, err
	}
	root, err := jailRoot(inst.dir, inst.meta.Kind)
	if err != nil {
		return deny("jail-root", err)
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return deny("jail", err)
	}
	key := canonicalArtifactRel(root, path)
	if !inst.book.producedPaths()[key] {
		return deny("not-produced", errors.New("没有找到这个文件"))
	}
	// Lstat like the desktop fetch: a recorded path swapped for a symlink or
	// special file after the turn must not leak an unrelated target. A miss
	// inside the checkout can still be a file the same turn wrote beside it,
	// before ConnectGit moved the jail root.
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		if alt, altInfo, ok := strandedRegular(inst.dir, root, key); ok {
			path, info = alt, altInfo
		} else {
			return deny("missing", errors.New("这个文件已经不在了"))
		}
	}
	if info.Size() > serveFileMaxBytes {
		return deny("too-large", fmt.Errorf("文件太大（%.1f MB），暂不支持回传", float64(info.Size())/(1<<20)))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return deny("read-failed", errors.New("这个文件暂时读取失败"))
	}
	slog.Info("cloudagent fetch artifact",
		slog.String("instance", inst.meta.ID), slog.String("path", rel), slog.Int64("size", info.Size()))
	return json.Marshal(map[string]any{
		"name":        filepath.Base(path),
		"size":        info.Size(),
		"data_base64": base64.StdEncoding.EncodeToString(raw),
	})
}
