package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// RegisterTaskTools registers browser task supervisor tools into the registry.
// Call this after the supervisor is initialized (e.g. in GUI/TUI bootstrap).
func RegisterTaskTools(registry *tool.Registry, supervisor *BrowserTaskSupervisor) {
	tools := []tool.RegisteredTool{
		{
			Name:        "browser_task_run",
			Description: "执行浏览器自动化任务。接受操作步骤序列和成功标准，逐步执行并验证。支持自动重试。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "automation", "task", "浏览器", "自动化", "任务", "网页"},
			Priority:    5,
			Required:    []string{"steps"},
			InputSchema: map[string]interface{}{
				"steps": map[string]interface{}{
					"type":        "string",
					"description": `操作步骤 JSON 数组，每个步骤: {"action":"navigate|click|type|wait|eval|scroll|select","params":{"url":"...","selector":"...","text":"..."}}`,
				},
				"success_criteria": map[string]interface{}{
					"type":        "string",
					"description": `成功标准 JSON 数组（可选），每个标准: {"type":"dom_exists|dom_text|url_contains|url_matches|ocr_contains","selector":"...","pattern":"..."}`,
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "任务描述（可选）",
				},
				"max_retries": map[string]interface{}{
					"type":        "integer",
					"description": "最大重试次数（默认 3）",
				},
			},
			Handler: func(args map[string]interface{}) string {
				stepsJSON, _ := args["steps"].(string)
				if stepsJSON == "" {
					return "缺少 steps 参数"
				}
				var steps []StepSpec
				if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
					return fmt.Sprintf("steps JSON 解析失败: %v", err)
				}

				spec := TaskSpec{
					Steps:       steps,
					Description: strVal(args, "description"),
					MaxRetries:  intVal(args, "max_retries", 3),
				}

				if criteriaJSON, ok := args["success_criteria"].(string); ok && criteriaJSON != "" {
					var criteria []CriterionSpec
					if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
						return fmt.Sprintf("success_criteria JSON 解析失败: %v", err)
					}
					spec.SuccessCriteria = criteria
				}

				state, err := supervisor.Execute(spec)
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
			Description: "查询浏览器任务的当前状态和进度。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "task", "status", "浏览器", "任务", "状态"},
			Priority:    4,
			Required:    []string{"task_id"},
			InputSchema: map[string]interface{}{
				"task_id": map[string]interface{}{"type": "string", "description": "任务 ID"},
			},
			Handler: func(args map[string]interface{}) string {
				taskID, _ := args["task_id"].(string)
				if taskID == "" {
					return "缺少 task_id 参数"
				}
				state, ok := supervisor.GetState(taskID)
				if !ok {
					return fmt.Sprintf("任务 %s 不存在", taskID)
				}
				result, _ := json.Marshal(state)
				return string(result)
			},
		},
		{
			Name:        "browser_task_verify",
			Description: "对当前浏览器页面执行成功标准验证。可用于检查页面是否处于预期状态。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "verify", "test", "浏览器", "验证", "检查", "网页"},
			Priority:    5,
			Required:    []string{"criteria"},
			InputSchema: map[string]interface{}{
				"criteria": map[string]interface{}{
					"type":        "string",
					"description": `验证标准 JSON 数组: [{"type":"dom_exists|dom_text|url_contains|url_matches|ocr_contains","selector":"...","pattern":"..."}]`,
				},
			},
			Handler: func(args map[string]interface{}) string {
				criteriaJSON, _ := args["criteria"].(string)
				if criteriaJSON == "" {
					return "缺少 criteria 参数"
				}
				var criteria []CriterionSpec
				if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
					return fmt.Sprintf("criteria JSON 解析失败: %v", err)
				}
				result, err := supervisor.Verify(criteria)
				if err != nil {
					return fmt.Sprintf("验证失败: %v", err)
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

// RegisterOCRTool registers the browser_ocr tool. Call after OCR provider is ready.
func RegisterOCRTool(registry *tool.Registry, ocr OCRProvider, sessionFn func() (*Session, error)) {
	registry.Register(tool.RegisteredTool{
		Name:        "browser_ocr",
		Description: "对当前浏览器页面截图执行 OCR 文字识别。返回识别到的文本区域列表（含坐标和置信度）。首次调用会自动安装 RapidOCR。",
		Category:    tool.CategoryBuiltin,
		Tags:        []string{"browser", "ocr", "text", "recognition", "浏览器", "文字识别", "网页"},
		Priority:    5,
		Status:      tool.StatusAvailable,
		Source:      "builtin:browser-ocr",
		InputSchema: map[string]interface{}{
			"full_page": map[string]interface{}{"type": "boolean", "description": "是否截取整个页面（默认 false）"},
		},
		Handler: func(args map[string]interface{}) string {
			sess, err := sessionFn()
			if err != nil {
				return fmt.Sprintf("浏览器未连接: %v", err)
			}
			if ocr == nil || !ocr.IsAvailable() {
				return "OCR 不可用（未安装 python3 或 rapidocr-onnxruntime）"
			}
			fullPage, _ := args["full_page"].(bool)
			imgB64, err := sess.Screenshot(fullPage)
			if err != nil {
				return fmt.Sprintf("截图失败: %v", err)
			}
			results, err := ocr.Recognize(imgB64)
			if err != nil {
				return fmt.Sprintf("OCR 识别失败: %v", err)
			}
			if len(results) == 0 {
				return "未识别到任何文本"
			}
			return FormatOCRForLLM(results)
		},
	})
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
func RegisterRecorderTools(registry *tool.Registry, recorder *BrowserRecorder, replayer *FlowReplayer) {
	tools := []tool.RegisteredTool{
		{
			Name:        "browser_record_start",
			Description: "开始录制浏览器操作。录制期间，所有 browser_* 工具的操作会被自动记录。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "record", "浏览器", "录制"},
			Priority:    4,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				if err := recorder.Start(); err != nil {
					return fmt.Sprintf("录制启动失败: %v", err)
				}
				return "录制已开始。执行浏览器操作后，调用 browser_record_stop 保存。"
			},
		},
		{
			Name:        "browser_record_stop",
			Description: "停止录制并保存操作流程。保存到 ~/.maclaw/browser_flows/<name>.json。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "record", "浏览器", "录制", "保存"},
			Priority:    4,
			Required:    []string{"name"},
			InputSchema: map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "流程名称（用于保存文件名）"},
				"description": map[string]interface{}{"type": "string", "description": "流程描述（可选）"},
			},
			Handler: func(args map[string]interface{}) string {
				name, _ := args["name"].(string)
				if name == "" {
					return "缺少 name 参数"
				}
				desc, _ := args["description"].(string)
				flow, err := recorder.Stop(name, desc)
				if err != nil {
					return fmt.Sprintf("保存失败: %v", err)
				}
				return fmt.Sprintf("录制已保存: %s（%d 个步骤）", flow.Name, len(flow.Steps))
			},
		},
		{
			Name:        "browser_task_replay",
			Description: "回放录制的浏览器操作流程。支持参数覆盖（如替换用户名密码）。页面变化时会自动调整。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "replay", "automation", "浏览器", "回放", "自动化", "网页"},
			Priority:    5,
			Required:    []string{"name"},
			InputSchema: map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "流程名称"},
				"overrides": map[string]interface{}{
					"type":        "string",
					"description": `参数覆盖 JSON（可选），如 {"username":"admin","password":"123"}`,
				},
			},
			Handler: func(args map[string]interface{}) string {
				name, _ := args["name"].(string)
				if name == "" {
					return "缺少 name 参数"
				}
				flow, err := recorder.LoadFlow(name)
				if err != nil {
					return fmt.Sprintf("加载流程失败: %v", err)
				}

				var overrides map[string]string
				if ovJSON, ok := args["overrides"].(string); ok && ovJSON != "" {
					if err := json.Unmarshal([]byte(ovJSON), &overrides); err != nil {
						return fmt.Sprintf("overrides JSON 解析失败: %v", err)
					}
				}

				state, err := replayer.Replay(flow, overrides)
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
					}
					result, _ := json.Marshal(errResp)
					return string(result)
				}
				result, _ := json.Marshal(map[string]interface{}{
					"status": string(state.Status),
					"step":   state.CurrentStep,
					"total":  state.TotalSteps,
				})
				return string(result)
			},
		},
		{
			Name:        "browser_list_flows",
			Description: "列出所有已录制的浏览器操作流程。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "flows", "list", "浏览器", "流程", "列表"},
			Priority:    3,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				flows, err := recorder.ListFlows()
				if err != nil {
					return fmt.Sprintf("列出流程失败: %v", err)
				}
				if len(flows) == 0 {
					return "没有已录制的流程"
				}
				var lines []string
				for _, f := range flows {
					lines = append(lines, fmt.Sprintf("- %s: %s (%d 步, 录制于 %s)",
						f.Name, f.Description, len(f.Steps), f.RecordedAt.Format("2006-01-02 15:04")))
				}
				return fmt.Sprintf("已录制的流程（%d 个）:\n%s", len(flows), strings.Join(lines, "\n"))
			},
		},
	}

	for _, t := range tools {
		t.Status = tool.StatusAvailable
		t.Source = "builtin:browser-record"
		registry.Register(t)
	}
}
