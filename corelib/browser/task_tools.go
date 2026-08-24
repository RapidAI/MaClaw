package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var browserSessionTaskSupervisors = struct {
	mu    sync.Mutex
	items map[string]*BrowserTaskSupervisor
}{items: map[string]*BrowserTaskSupervisor{}}

// RegisterTaskTools registers browser task supervisor tools into the registry.
// loopMgr may be nil; when non-nil, browser_task_status can query background tasks.
func RegisterTaskTools(registry *tool.Registry, supervisor *BrowserTaskSupervisor, loopMgr *agent.BackgroundLoopManager) {
	tools := []tool.RegisteredTool{
		{
			Name:        "browser_task_run",
			Description: "Run stable browser automation steps. Requires session_id; eval/click_at are disabled. Type steps may target the focused editable element after a click.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "automation", "task"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"steps": map[string]interface{}{
					"type":        "string",
					"description": `Action steps JSON array. Required unless resume_task_id is set. Each step: {"action":"navigate|click|type|wait|scroll|select","params":{"url":"...","ref":"@e1","selector":"...","text":"...","content_format":"plain|markdown"}}. Type can omit ref/selector to write into the currently focused editable element after a click. For rich editors/article publishing, type steps may set content_format=markdown. eval/click_at are disabled.`,
				},
				"success_criteria": map[string]interface{}{
					"type":        "string",
					"description": `Optional success criteria JSON array. Each criterion: {"type":"dom_exists|dom_text|url_contains|url_matches|network_request|console_no_error","selector":"...","pattern":"..."}`,
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "optional task description",
				},
				"max_retries": map[string]interface{}{
					"type":        "integer",
					"description": "maximum retry count, default 3",
				},
				"content_format": map[string]interface{}{
					"type":        "string",
					"description": "Optional default for type steps: plain (default) or markdown. Per-step params.content_format overrides this.",
				},
				"resume_task_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional: continue a paused task from the captcha/ask step using a new Execute",
				},
			},
			Handler: func(args map[string]interface{}) string {
				execSupervisor, err := taskSupervisorForArgs(supervisor, args)
				if err != nil {
					return fmt.Sprintf("browser session unavailable: %v", err)
				}
				stepsJSON, err := browserJSONArg(args, "steps")
				if err != nil {
					return fmt.Sprintf("steps JSON parse failed: %v", err)
				}
				resumeID := strings.TrimSpace(strVal(args, "resume_task_id"))
				if strings.TrimSpace(stepsJSON) == "" && resumeID == "" {
					return "missing steps"
				}
				var steps []StepSpec
				if strings.TrimSpace(stepsJSON) != "" {
					if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
						return fmt.Sprintf("steps JSON parse failed: %v", err)
					}
					normalizeStepParams(stepsJSON, steps)
					applyDefaultContentFormatToTypeSteps(steps, strVal(args, "content_format"))
				}

				spec := TaskSpec{
					Steps:       steps,
					Description: strVal(args, "description"),
					MaxRetries:  intVal(args, "max_retries", 3),
				}

				if resumeID != "" {
					prev, from, ok := execSupervisor.ResumeSpec(resumeID)
					if !ok {
						return fmt.Sprintf("paused task %s not found", resumeID)
					}
					if len(spec.Steps) == 0 {
						if from >= len(prev.Steps) {
							return fmt.Sprintf("paused task %s has no remaining steps", resumeID)
						}
						spec = prev
						spec.Steps = append([]StepSpec(nil), prev.Steps[from:]...)
						spec.ID = ""
					}
				}

				if criteriaJSON, err := browserJSONArg(args, "success_criteria"); err != nil {
					return fmt.Sprintf("success_criteria JSON parse failed: %v", err)
				} else if strings.TrimSpace(criteriaJSON) != "" {
					var criteria []CriterionSpec
					if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
						return fmt.Sprintf("success_criteria JSON parse failed: %v", err)
					}
					spec.SuccessCriteria = criteria
				}

				state, err := execSupervisor.Execute(spec)
				return marshalTaskRunResult(state, err)
			},
		},
		{
			Name:        "browser_task_status",
			Description: "Query browser task status. Requires session_id.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "task", "status"},
			Priority:    4,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"task_id":    map[string]interface{}{"type": "string", "description": "optional task id"},
			},
			Handler: func(args map[string]interface{}) string {
				taskID, _ := args["task_id"].(string)
				statusSupervisor, err := taskSupervisorForArgs(supervisor, args)
				if err != nil {
					return fmt.Sprintf("browser session unavailable: %v", err)
				}

				if taskID != "" && statusSupervisor != nil {
					state, ok := statusSupervisor.GetState(taskID)
					if ok {
						return marshalTaskState(state)
					}
				}

				if loopMgr != nil {
					if taskID != "" {
						ctx := loopMgr.Get(taskID)
						if ctx == nil {
							return fmt.Sprintf("task %s not found", taskID)
						}
						result, _ := json.Marshal(map[string]interface{}{
							"task_id":     ctx.ID,
							"description": ctx.Description,
							"status":      ctx.State(),
							"iteration":   ctx.Iteration(),
							"max_iter":    ctx.MaxIterations(),
							"started_at":  ctx.StartedAt.Format("2006-01-02 15:04:05"),
						})
						return string(result)
					}
					views := loopMgr.ListViews()
					var browserViews []agent.BackgroundLoopView
					for _, v := range views {
						if v.SlotKind == "browser" {
							browserViews = append(browserViews, v)
						}
					}
					if len(browserViews) == 0 {
						return "no browser background tasks"
					}
					result, _ := json.Marshal(browserViews)
					return string(result)
				}

				if taskID != "" {
					return fmt.Sprintf("task %s not found", taskID)
				}
				return "no browser background tasks"
			},
		},
		{
			Name:        "browser_task_verify",
			Description: "Verify current page against success criteria. Requires session_id.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "verify", "test"},
			Priority:    5,
			Required:    []string{"session_id", "criteria"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"criteria": map[string]interface{}{
					"type":        "string",
					"description": `Success criteria JSON array: [{"type":"dom_exists|dom_text|url_contains|url_matches|network_request|console_no_error","selector":"...","pattern":"..."}]`,
				},
			},
			Handler: func(args map[string]interface{}) string {
				verifySupervisor, err := taskSupervisorForArgs(supervisor, args)
				if err != nil {
					return fmt.Sprintf("browser session unavailable: %v", err)
				}
				criteriaJSON, err := browserJSONArg(args, "criteria")
				if err != nil {
					return fmt.Sprintf("criteria JSON parse failed: %v", err)
				}
				if strings.TrimSpace(criteriaJSON) == "" {
					return "missing criteria"
				}
				var criteria []CriterionSpec
				if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
					return fmt.Sprintf("criteria JSON parse failed: %v", err)
				}
				result, err := verifySupervisor.Verify(criteria)
				if err != nil {
					return fmt.Sprintf("verification failed: %v", err)
				}
				return marshalVerifyResult(result)
			},
		},
	}

	for _, t := range tools {
		t.Status = tool.StatusAvailable
		t.Source = "builtin:browser-task"
		registry.Register(t)
	}
}

// RegisterOCRTool intentionally does not register browser_ocr in the stable
// browser mechanism. OCR captures pixels and reintroduces the screenshot path;
// browser automation should use DOM refs, extraction, URL, network, and console
// criteria instead.
func RegisterOCRTool(registry *tool.Registry, ocr OCRProvider, sessionFn func() (*Session, error)) {
}

func browserJSONArg(args map[string]interface{}, key string) (string, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return "", nil
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func taskSupervisorForArgs(base *BrowserTaskSupervisor, args map[string]interface{}) (*BrowserTaskSupervisor, error) {
	if base == nil {
		return nil, fmt.Errorf("browser task supervisor is nil")
	}
	sessionID := strings.TrimSpace(strArg(args, "session_id", ""))
	if sessionID == "" {
		return nil, fmt.Errorf("missing session_id")
	}
	agentSession, ok, err := agentSessionFromArgs(args)
	if err != nil {
		return nil, err
	}
	if !ok {
		return base, nil
	}
	browserSessionTaskSupervisors.mu.Lock()
	defer browserSessionTaskSupervisors.mu.Unlock()
	if existing := browserSessionTaskSupervisors.items[agentSession.ID]; existing != nil {
		return existing, nil
	}
	ocr := OCRProvider(nil)
	if base.verifier != nil {
		ocr = base.verifier.ocr
	}
	supervisor := NewBrowserTaskSupervisor(nil, nil, ocr, func() (*Session, error) {
		fresh, err := GetAgentSession(sessionID)
		if err != nil {
			return nil, err
		}
		return fresh.session, nil
	}, base.logger)
	supervisor.agentSessionFn = func() (*BrowserAgentSession, error) {
		return GetAgentSession(sessionID)
	}
	browserSessionTaskSupervisors.items[agentSession.ID] = supervisor
	return supervisor, nil
}

func forgetBrowserSessionTaskSupervisor(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	browserSessionTaskSupervisors.mu.Lock()
	if existing := browserSessionTaskSupervisors.items[sessionID]; existing != nil {
		existing.DiscardAll()
	}
	delete(browserSessionTaskSupervisors.items, sessionID)
	browserSessionTaskSupervisors.mu.Unlock()
}

func marshalTaskRunResult(state *TaskState, err error) string {
	if state == nil {
		if err != nil {
			result, _ := json.Marshal(map[string]interface{}{"status": "failed", "error": llmSafeBrowserError(err)})
			return string(result)
		}
		return marshalBrowserResult(false, "empty task state", nil)
	}
	if state != nil && state.Status == TaskStatusPaused && state.LastResultStatus == "ask" {
		req := attachResumeTaskID(state.AskUser, state.ID)
		return agent.AskUserResultMarker(req)
	}
	if err != nil {
		errResp := map[string]interface{}{
			"status": "failed",
			"error":  llmSafeBrowserError(err),
		}
		if state != nil {
			errResp["status"] = string(state.Status)
			errResp["step"] = state.CurrentStep
			errResp["total"] = state.TotalSteps
			errResp["retries"] = state.RetryCount
			errResp["task_id"] = state.ID
		}
		result, _ := json.Marshal(errResp)
		return string(result)
	}
	if state != nil && state.LastResultStatus == "blocked" {
		return marshalBrowserResult(false, firstNonEmpty(state.LastError, "blocked"), map[string]interface{}{
			"status":  string(state.Status),
			"task_id": state.ID,
			"step":    state.CurrentStep,
			"total":   state.TotalSteps,
			"reason":  "blocked",
		})
	}
	payload := map[string]interface{}{
		"status":  string(state.Status),
		"task_id": state.ID,
		"step":    state.CurrentStep,
		"total":   state.TotalSteps,
	}
	result, _ := json.Marshal(payload)
	return string(result)
}

func marshalTaskState(state *TaskState) string {
	if state == nil {
		return marshalBrowserResult(false, "empty task state", nil)
	}
	payload := map[string]interface{}{
		"status":  string(state.Status),
		"task_id": state.ID,
		"step":    state.CurrentStep,
		"total":   state.TotalSteps,
		"retries": state.RetryCount,
	}
	if state.LastError != "" {
		payload["last_error"] = llmSafeBrowserError(fmt.Errorf("%s", state.LastError))
	}
	if state.LastResultStatus != "" {
		payload["last_result_status"] = state.LastResultStatus
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func attachResumeTaskID(req *agent.AskUserRequest, taskID string) *agent.AskUserRequest {
	taskID = strings.TrimSpace(taskID)
	if req == nil {
		req = captchaAskUserRequest("")
	} else {
		cloned := *req
		req = &cloned
	}
	if taskID == "" || strings.Contains(req.Context, "resume_task_id="+taskID) {
		return req
	}
	marker := "resume_task_id=" + taskID
	if strings.TrimSpace(req.Context) == "" {
		req.Context = marker
	} else {
		req.Context = strings.TrimSpace(req.Context) + " " + marker
	}
	return req
}

func marshalVerifyResult(result *VerifyResult) string {
	if result == nil {
		return marshalBrowserResult(false, "empty verify result", nil)
	}
	display := "verification passed"
	if !result.Passed {
		display = "verification failed"
	}
	return marshalBrowserResult(result.Passed, display, map[string]interface{}{
		"passed":  result.Passed,
		"details": compactVerifyDetails(result),
	})
}

func compactVerifyDetails(result *VerifyResult) []map[string]interface{} {
	if result == nil {
		return nil
	}
	details := make([]map[string]interface{}, 0, len(result.Details))
	for _, d := range result.Details {
		details = append(details, map[string]interface{}{
			"type":   d.Criterion.Type,
			"passed": d.Passed,
			"actual": d.Actual,
			"error":  d.Error,
		})
	}
	return details
}

func compactVerifyFailure(result *VerifyResult) string {
	encoded, _ := json.Marshal(map[string]interface{}{
		"passed":  false,
		"details": compactVerifyDetails(result),
	})
	return "success criteria not met: " + string(encoded)
}

func formatStepVerifyFailure(result *VerifyResult) error {
	encoded, _ := json.Marshal(map[string]interface{}{
		"passed":  false,
		"details": compactVerifyDetails(result),
	})
	return fmt.Errorf("step verification failed: %s", encoded)
}

func strVal(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func intVal(args map[string]interface{}, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

// RegisterRecorderTools registers browser recording and replay tools.
// loopMgr and activityStore may be nil; when non-nil, replay runs in background.
func RegisterRecorderTools(registry *tool.Registry, recorder *BrowserRecorder, replayer *FlowReplayer,
	loopMgr *agent.BackgroundLoopManager, activityStore ActivityUpdater, statusC chan agent.StatusEvent, logger func(string)) {
	tools := []tool.RegisteredTool{
		{
			Name:        "browser_list_flows",
			Description: "List recorded browser flows.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "flows", "list"},
			Priority:    3,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				flows, err := recorder.ListFlows()
				if err != nil {
					return fmt.Sprintf("list flows failed: %v", err)
				}
				if len(flows) == 0 {
					return "no recorded browser flows"
				}
				var lines []string
				for _, f := range flows {
					lines = append(lines, fmt.Sprintf("- %s: %s (%d steps, recorded at %s)",
						f.Name, f.Description, len(f.Steps), f.RecordedAt.Format("2006-01-02 15:04")))
				}
				return fmt.Sprintf("recorded browser flows (%d):\n%s", len(flows), strings.Join(lines, "\n"))
			},
		},
	}

	for _, t := range tools {
		t.Status = tool.StatusAvailable
		t.Source = "builtin:browser-record"
		registry.Register(t)
	}
}

// normalizeStepParams handles LLM outputs that put step parameters at the
// top level (e.g. {"action":"click","ref":"@e1"}) instead of inside a "params"
// sub-object ({"action":"click","params":{"ref":"@e1"}}). This is a common
// pattern with models that interpret the schema as flat. We re-parse the raw
// JSON and move unrecognized top-level keys into Params.
func normalizeStepParams(stepsJSON string, steps []StepSpec) {
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(stepsJSON), &raw); err != nil {
		return
	}
	// Known top-level StepSpec fields (should not be moved to Params).
	knownFields := map[string]bool{
		"action": true, "params": true, "verify": true,
		"timeout": true, "target": true, "fallbacks": true,
	}
	for i := range steps {
		if i >= len(raw) {
			break
		}
		stepMap := raw[i]
		// Collect top-level keys that aren't known struct fields → treat as params.
		extras := map[string]string{}
		for k, v := range stepMap {
			if knownFields[k] {
				continue
			}
			// Convert value to string for Params map[string]string.
			switch val := v.(type) {
			case string:
				extras[k] = val
			case float64:
				// JSON numbers are float64; format without decimal for integers.
				if val == float64(int64(val)) {
					extras[k] = fmt.Sprintf("%d", int64(val))
				} else {
					extras[k] = fmt.Sprintf("%g", val)
				}
			case bool:
				extras[k] = fmt.Sprintf("%v", val)
			default:
				// For arrays/objects, marshal back to JSON string.
				if b, err := json.Marshal(val); err == nil {
					extras[k] = string(b)
				}
			}
		}
		if len(extras) > 0 {
			if steps[i].Params == nil {
				steps[i].Params = make(map[string]string, len(extras))
			}
			for k, v := range extras {
				// Don't overwrite params that were correctly nested in JSON.
				if _, exists := steps[i].Params[k]; !exists {
					steps[i].Params[k] = v
				}
			}
		}
	}
}

func applyDefaultContentFormatToTypeSteps(steps []StepSpec, defaultFormat string) {
	if normalizeBrowserContentFormat(defaultFormat) != BrowserContentFormatMarkdown {
		return
	}
	for i := range steps {
		if !strings.EqualFold(strings.TrimSpace(steps[i].Action), "type") {
			continue
		}
		if steps[i].Params == nil {
			steps[i].Params = map[string]string{}
		}
		if strings.TrimSpace(steps[i].Params["content_format"]) == "" {
			steps[i].Params["content_format"] = BrowserContentFormatMarkdown
		}
	}
}

// bgLoopManagerAdapter wraps *agent.BackgroundLoopManager to satisfy LoopManager interface.
type bgLoopManagerAdapter struct {
	mgr *agent.BackgroundLoopManager
}

func (a *bgLoopManagerAdapter) Complete(loopID string) { a.mgr.Complete(loopID) }
func (a *bgLoopManagerAdapter) Stop(loopID string)     { a.mgr.Stop(loopID) }
