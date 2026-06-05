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
			Required:    []string{"session_id", "steps"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"steps": map[string]interface{}{
					"type":        "string",
					"description": `Action steps JSON array. Each step: {"action":"navigate|click|type|wait|scroll|select","params":{"url":"...","ref":"@e1","selector":"...","text":"...","content_format":"plain|markdown"}}. Type can omit ref/selector to write into the currently focused editable element after a click. For rich editors/article publishing, type steps may set content_format=markdown. eval/click_at are disabled.`,
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
				if strings.TrimSpace(stepsJSON) == "" {
					return "missing steps"
				}
				var steps []StepSpec
				if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
					return fmt.Sprintf("steps JSON parse failed: %v", err)
				}
				applyDefaultContentFormatToTypeSteps(steps, strVal(args, "content_format"))

				spec := TaskSpec{
					Steps:       steps,
					Description: strVal(args, "description"),
					MaxRetries:  intVal(args, "max_retries", 3),
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
				if err != nil {
					errResp := map[string]interface{}{
						"status": "failed",
						"error":  err.Error(),
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

				result, _ := json.Marshal(map[string]interface{}{
					"status":  string(state.Status),
					"task_id": state.ID,
					"step":    state.CurrentStep,
					"total":   state.TotalSteps,
				})
				return string(result)
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
						result, _ := json.Marshal(state)
						return string(result)
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
				out, _ := json.Marshal(result)
				return string(out)
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
	delete(browserSessionTaskSupervisors.items, sessionID)
	browserSessionTaskSupervisors.mu.Unlock()
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
