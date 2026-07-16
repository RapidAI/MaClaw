package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// guiReplayActivityAdapter wraps AgentActivityStore to satisfy guiautomation.GUIActivityUpdater.
type guiReplayActivityAdapter struct {
	store *AgentActivityStore
}

func (a *guiReplayActivityAdapter) UpdateReplay(flowName string, currentStep, totalSteps int, status string) {
	a.store.Update(&AgentActivity{
		Source:      "gui_replay",
		Task:        fmt.Sprintf("GUI 回放: %s", flowName),
		Iteration:   currentStep,
		MaxIter:     totalSteps,
		LastSummary: status,
	})
}

func (a *guiReplayActivityAdapter) ClearReplay() {
	a.store.Clear("gui_replay")
}

// registerGUIAutomationTools registers native GUI automation tools (recording,
// replay, click, type, screenshot) into the gui ToolRegistry.
// loopMgr and statusC enable async background replay; activityStore tracks progress.
func registerGUIAutomationTools(registry *ToolRegistry, loopMgr *BackgroundLoopManager, activityStore *AgentActivityStore, statusC chan StatusEvent) {
	// Initialize platform components
	bridge := accessibility.NewBridge()
	inputSim := guiautomation.NewInputSimulator()

	screenshotFn := func() (string, error) {
		return captureDesktopScreenshot(-1) // default: all monitors stitched
	}

	matcher := guiautomation.NewImageMatcher(nil, screenshotFn)

	locator := guiautomation.NewElementLocator(bridge, matcher, func(msg string) {
		log.Printf("[gui-locator] %s", msg)
	})

	recorder := guiautomation.NewGUIRecorder(bridge, screenshotFn)

	supervisor := guiautomation.NewGUITaskSupervisor(
		locator, inputSim, screenshotFn, nil, nil,
		func(msg string) { log.Printf("[gui-supervisor] %s", msg) },
	)

	replayer := guiautomation.NewGUIReplayer(supervisor, locator, inputSim)
	var guiActivity *guiReplayActivityAdapter
	if activityStore != nil {
		guiActivity = &guiReplayActivityAdapter{store: activityStore}
	}

	// --- gui_record_start ---
	registry.Register(RegisteredTool{
		Name:        "gui_record_start",
		Description: "开始录制 GUI 操作流程。Start recording GUI operations on native desktop applications.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "录制"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			if err := recorder.Start(); err != nil {
				return fmt.Sprintf("开始录制失败: %s", err)
			}
			return "GUI 录制已开始。请执行操作，完成后调用 gui_record_stop 停止录制。"
		},
	})

	// --- gui_record_stop ---
	registry.Register(RegisteredTool{
		Name:        "gui_record_stop",
		Description: "停止 GUI 录制并保存流程。Stop GUI recording and save the flow with a name and description.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "录制"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"name"},
		InputSchema: map[string]interface{}{
			"name":        map[string]interface{}{"type": "string", "description": "流程名称 / Flow name"},
			"description": map[string]interface{}{"type": "string", "description": "流程描述 / Flow description"},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			name := guiStrArg(args, "name", "")
			if name == "" {
				return "缺少 name 参数"
			}
			desc := guiStrArg(args, "description", "")
			flow, err := recorder.Stop(name, desc)
			if err != nil {
				return fmt.Sprintf("停止录制失败: %s", err)
			}
			return fmt.Sprintf("录制已保存: %s (%d 步)", flow.Name, len(flow.Steps))
		},
	})

	// --- gui_replay (async background) ---
	registry.Register(RegisteredTool{
		Name:        "gui_replay",
		Description: "重放已录制的 GUI 操作流程（后台异步执行）。Replay a previously recorded GUI flow by name, with optional parameter overrides. Runs asynchronously in background.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "重放"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"flow_name"},
		InputSchema: map[string]interface{}{
			"flow_name": map[string]interface{}{"type": "string", "description": "要重放的流程名称 / Flow name to replay"},
			"overrides": map[string]interface{}{"type": "string", "description": "参数替换 JSON，如 {\"username\":\"admin\"} / Override values as JSON string"},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			flowName := guiStrArg(args, "flow_name", "")
			if flowName == "" {
				return "缺少 flow_name 参数"
			}
			flow, err := recorder.LoadFlow(flowName)
			if err != nil {
				return fmt.Sprintf("加载流程失败: %s", err)
			}
			var overrides map[string]string
			if raw := guiStrArg(args, "overrides", ""); raw != "" {
				if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
					return fmt.Sprintf("解析 overrides 失败: %s", err)
				}
			}

			// Async background replay via gui BackgroundLoopManager
			if loopMgr != nil {
				desc := fmt.Sprintf("GUI 回放: %s", flow.Name)
				loopCtx, waitCh := loopMgr.SpawnOrQueue(SlotKindGUI, "", desc, 1)
				if loopCtx != nil {
					go runGUIReplayBackground(loopCtx, flow, overrides, replayer, guiActivity, statusC, loopMgr)
					result, _ := json.Marshal(map[string]interface{}{
						"status":  "submitted",
						"task_id": loopCtx.ID,
						"message": fmt.Sprintf("GUI 回放 [%s] 已提交后台执行", flow.Name),
					})
					return string(result)
				}
				// Slot full — queued
				queuePos := loopMgr.QueueLength(SlotKindGUI)
				go func() {
					ctx := <-waitCh
					runGUIReplayBackground(ctx, flow, overrides, replayer, guiActivity, statusC, loopMgr)
				}()
				result, _ := json.Marshal(map[string]interface{}{
					"status":         "queued",
					"queue_position": queuePos,
					"message":        fmt.Sprintf("GUI slot 已满，回放 [%s] 已排队（位置 %d）", flow.Name, queuePos),
				})
				return string(result)
			}

			// Fallback: synchronous execution
			state, err := replayer.Replay(flow, overrides)
			if err != nil {
				errResp := map[string]interface{}{"status": "failed", "error": err.Error()}
				if state != nil {
					errResp["step"] = state.CurrentStep
					errResp["total"] = state.TotalSteps
				}
				result, _ := json.Marshal(errResp)
				return string(result)
			}
			result, _ := json.Marshal(map[string]interface{}{
				"status": state.Status,
				"step":   state.CurrentStep,
				"total":  state.TotalSteps,
			})
			return string(result)
		},
	})

	// --- gui_list_flows ---
	registry.Register(RegisteredTool{
		Name:        "gui_list_flows",
		Description: "列出所有已保存的 GUI 操作流程。List all saved GUI recorded flows.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			flows, err := recorder.ListFlows()
			if err != nil {
				return fmt.Sprintf("列出流程失败: %s", err)
			}
			if len(flows) == 0 {
				return "无已保存的 GUI 流程"
			}
			var lines []string
			for _, f := range flows {
				lines = append(lines, fmt.Sprintf("  %s: %s (%d 步, 录制于 %s)",
					f.Name, f.Description, len(f.Steps), f.RecordedAt.Format("2006-01-02 15:04")))
			}
			result := fmt.Sprintf("已保存的 GUI 流程 (%d 个):\n", len(flows))
			for i, l := range lines {
				if i > 0 {
					result += "\n"
				}
				result += l
			}
			return result
		},
	})

	// --- gui_click ---
	registry.Register(RegisteredTool{
		Name:        "gui_click",
		Description: "在指定屏幕坐标执行鼠标点击。Click at the specified screen coordinates.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "点击"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"x", "y"},
		InputSchema: map[string]interface{}{
			"x": map[string]interface{}{"type": "integer", "description": "X 坐标"},
			"y": map[string]interface{}{"type": "integer", "description": "Y 坐标"},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			x := guiIntArg(args, "x", 0)
			y := guiIntArg(args, "y", 0)
			if err := inputSim.Click(x, y); err != nil {
				return fmt.Sprintf("点击失败: %s", err)
			}
			return fmt.Sprintf("已点击坐标 (%d, %d)", x, y)
		},
	})

	// --- gui_type ---
	registry.Register(RegisteredTool{
		Name:        "gui_type",
		Description: "在指定坐标点击后输入文本。Click at coordinates then type text.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "输入"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"x", "y", "text"},
		InputSchema: map[string]interface{}{
			"x":    map[string]interface{}{"type": "integer", "description": "X 坐标"},
			"y":    map[string]interface{}{"type": "integer", "description": "Y 坐标"},
			"text": map[string]interface{}{"type": "string", "description": "要输入的文本 / Text to type"},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			x := guiIntArg(args, "x", 0)
			y := guiIntArg(args, "y", 0)
			text := guiStrArg(args, "text", "")
			if text == "" {
				return "缺少 text 参数"
			}
			if err := inputSim.Click(x, y); err != nil {
				return fmt.Sprintf("点击失败: %s", err)
			}
			if err := inputSim.Type(text); err != nil {
				return fmt.Sprintf("输入失败: %s", err)
			}
			return fmt.Sprintf("已在 (%d, %d) 输入 %d 个字符", x, y, len([]rune(text)))
		},
	})

	// --- gui_screenshot (multi-monitor aware) ---
	registry.Register(RegisteredTool{
		Name:        "gui_screenshot",
		Description: "截取桌面屏幕截图。默认截取所有显示器拼接图；传 screen_index 可截取指定显示器。Take a desktop screenshot. Defaults to all monitors stitched; pass screen_index for a specific monitor.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "截图"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"screen_index": map[string]interface{}{"type": "integer", "description": "显示器索引（0=主显示器，1=第二个...）。不传则截取所有显示器拼接图 / Monitor index (0=primary). Omit for all monitors stitched."},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			screenIdx := guiIntArg(args, "screen_index", -1)
			data, err := captureDesktopScreenshot(screenIdx)
			if err != nil {
				return fmt.Sprintf("截图失败: %s", err)
			}
			resp := map[string]interface{}{
				"type":   "image",
				"format": "png",
				"base64": data,
			}
			if screenIdx >= 0 {
				resp["screen_index"] = screenIdx
			} else {
				resp["screen_index"] = "all"
			}
			result, _ := json.Marshal(resp)
			return string(result)
		},
	})

	// --- gui_list_displays ---
	registry.Register(RegisteredTool{
		Name:        "gui_list_displays",
		Description: "列出所有连接的显示器信息（索引、名称、分辨率、位置、是否主显示器）。List all connected displays with index, name, resolution, position, and primary flag.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "显示器"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			displays, err := remote.EnumDisplays()
			if err != nil {
				return fmt.Sprintf("枚举显示器失败: %s", err)
			}
			if len(displays) == 0 {
				return "未检测到显示器"
			}
			result, _ := json.Marshal(map[string]interface{}{
				"count":    len(displays),
				"displays": displays,
			})
			return string(result)
		},
	})

	// --- gui_observe ---
	// Provides structured state observation of desktop GUI applications,
	// analogous to browser_observe for web pages. Returns accessibility
	// tree elements, focused element info, and OCR text — all as structured
	// text, no screenshot, no LLM vision cost.
	// OCR via RapidOCR (same sidecar as browser) so text_contains / labels work
	// without multimodal LLM vision.
	ocrForGUI := &taskOCRFromBrowser{inner: browser.NewRapidOCRSidecar(func(msg string) {
		log.Printf("[gui-ocr] %s", msg)
	})}
	guiObserver := guiautomation.NewGUIStateObserver(bridge, ocrForGUI, screenshotFn, func(msg string) {
		log.Printf("[gui-observe] %s", msg)
	})

	// Inject YOLO-based ScreenParser for vision-based UI element detection.
	// The model is lazily loaded on first use — no startup cost if not needed.
	// Prefer YOLO as primary visual path (not only a11y fallback) when weights exist.
	yoloWeightsPath := findYOLOWeights()
	if yoloWeightsPath != "" {
		yoloParser := guiautomation.NewYOLOScreenParser(yoloWeightsPath, 0.3, 0.5)
		guiObserver.SetScreenParser(yoloParser)
		log.Printf("[gui-automation] YOLO ScreenParser configured: %s", yoloWeightsPath)
	}

	// Wire observer into supervisor for SuccessCriteria verification after replay.
	supervisor.SetObserver(guiObserver)

	registry.Register(RegisteredTool{
		Name:        "gui_observe",
		Description: "观测桌面 GUI 程序的结构化状态（窗口元素树、焦点元素、OCR 文本）。返回纯文本，不截屏，不消耗 vision token。Observe desktop GUI state: accessibility tree, focused element, OCR text. Returns structured text, no screenshot.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "观测", "accessibility"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"window": map[string]interface{}{"type": "string", "description": "窗口标题（子串匹配）。不传则返回所有顶层窗口列表 / Window title (substring match). Omit to list all top-level windows."},
			"depth":  map[string]interface{}{"type": "integer", "description": "元素树深度（默认 3，最大 5）/ Element tree depth (default 3, max 5)"},
			"offset": map[string]interface{}{"type": "integer", "description": "从第 N 个字符开始返回（用于分页查看被截断的内容）/ Start from character N (for paginating truncated output)"},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			window := guiStrArg(args, "window", "")
			depth := guiIntArg(args, "depth", 3)
			if depth > 5 {
				depth = 5
			}
			if depth < 1 {
				depth = 1
			}

			if window == "" {
				// List all top-level windows
				elements, err := bridge.EnumElements("")
				if err != nil {
					return fmt.Sprintf("枚举窗口失败: %v", err)
				}
				if len(elements) == 0 {
					return "未检测到窗口（accessibility bridge 可能不可用）"
				}
				var lines []string
				for _, el := range elements {
					lines = append(lines, fmt.Sprintf("  [%s] %q (%dx%d at %d,%d)",
						el.Role, el.Name, el.Bounds.Width, el.Bounds.Height, el.Bounds.X, el.Bounds.Y))
				}
				return fmt.Sprintf("顶层窗口 (%d 个):\n%s", len(elements), strings.Join(lines, "\n"))
			}

			// Get element tree for specific window
			elements, err := bridge.EnumElements(window)
			if err != nil {
				return fmt.Sprintf("枚举窗口 %q 元素失败: %v", window, err)
			}

			if len(elements) == 0 {
				// Accessibility returned nothing — try ScreenParser (YOLO) fallback.
				// Note: YOLO detects elements across the entire screen, not scoped
				// to a specific window. The results are labeled accordingly.
				if guiObserver != nil && guiObserver.ScreenParser() != nil && guiObserver.ScreenParser().IsAvailable() && screenshotFn != nil {
					img, serr := screenshotFn()
					if serr == nil {
						uiElements, perr := guiObserver.ScreenParser().Parse(img)
						if perr == nil && len(uiElements) > 0 {
							var lines []string
							for _, el := range uiElements {
								lines = append(lines, fmt.Sprintf("  [%s] (%dx%d at %d,%d) conf=%.2f",
									el.Type, el.BBox[2], el.BBox[3], el.BBox[0], el.BBox[1], el.Confidence))
							}
							return fmt.Sprintf("Accessibility 未找到窗口 %q。以下是全屏视觉检测到的 %d 个可交互元素:\n%s",
								window, len(uiElements), strings.Join(lines, "\n"))
						}
					}
				}
				return fmt.Sprintf("未找到标题包含 %q 的窗口", window)
			}

			// Format element tree
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("窗口 %q 元素树:\n", window))
			for _, el := range elements {
				formatElementTree(&sb, &el, 0, depth)
			}

			fullResult := sb.String()
			totalLen := len(fullResult)
			offset := guiIntArg(args, "offset", 0)
			if offset < 0 {
				offset = 0
			}
			if offset >= totalLen {
				return fmt.Sprintf("offset %d 超出内容长度 %d", offset, totalLen)
			}
			result := fullResult[offset:]
			const maxChunk = 8000
			if len(result) > maxChunk {
				result = result[:maxChunk] + fmt.Sprintf("\n... (truncated, total %d chars, use offset=%d to continue)", totalLen, offset+maxChunk)
			}
			return result
		},
	})

	// --- gui_verify ---
	// Structured verification of desktop GUI state, analogous to
	// browser_task_verify. Checks criteria against accessibility tree
	// and OCR without requiring screenshots or LLM vision.
	registry.Register(RegisteredTool{
		Name:        "gui_verify",
		Description: "验证桌面 GUI 状态是否满足指定条件。支持: text_contains（OCR 文本包含）、element_exists（元素存在）、element_value（元素值匹配）、window_exists（窗口存在）。Verify desktop GUI state against criteria.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"gui", "test", "automation", "桌面", "验证", "assert"},
		Priority:    5,
		Status:      RegToolAvailable,
		Required:    []string{"criteria"},
		InputSchema: map[string]interface{}{
			"criteria": map[string]interface{}{
				"type":        "string",
				"description": "JSON 数组，每个元素: {\"type\":\"...\", \"pattern\":\"...\", \"selector\":\"role::name\", \"window\":\"窗口标题\"}。type 可选: text_contains, element_exists, element_value, window_exists",
			},
		},
		Source: "builtin:gui_automation",
		Handler: func(args map[string]interface{}) string {
			criteriaJSON := guiStrArg(args, "criteria", "")
			if criteriaJSON == "" {
				return "缺少 criteria 参数"
			}

			var criteria []guiautomation.VerifyCriterion
			if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
				return fmt.Sprintf("criteria JSON 解析失败: %v", err)
			}
			if len(criteria) == 0 {
				return "criteria 为空"
			}

			// Convert to taskengine.CriterionSpec
			specs := make([]taskengine.CriterionSpec, len(criteria))
			for i, c := range criteria {
				specs[i] = c.ToSpec()
			}

			result, err := guiObserver.Verify(specs)
			if err != nil {
				return fmt.Sprintf("验证失败: %v", err)
			}

			resp, _ := json.Marshal(result)
			return string(resp)
		},
	})
}

// runGUIReplayBackground executes a GUI replay as a background task using
// gui-local BackgroundLoopManager types. Designed to run in a goroutine.
func runGUIReplayBackground(
	loopCtx *LoopContext,
	flow *guiautomation.GUIRecordedFlow,
	overrides map[string]string,
	replayer *guiautomation.GUIReplayer,
	activity *guiReplayActivityAdapter,
	statusC chan StatusEvent,
	loopMgr *BackgroundLoopManager,
) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[gui-replay-bg] panic recovered: %v", r)
		}
		if activity != nil {
			activity.ClearReplay()
		}
		loopMgr.Complete(loopCtx.ID)
	}()

	startTime := time.Now()
	flowName := flow.Name
	totalSteps := len(flow.Steps)

	log.Printf("[gui-replay-bg] starting replay: %s (%d steps)", flowName, totalSteps)
	if activity != nil {
		activity.UpdateReplay(flowName, 0, totalSteps, "running")
	}

	// Monitor cancel signal
	replayDone := make(chan struct{})
	go func() {
		select {
		case <-loopCtx.CancelC:
			replayer.CancelAll()
			log.Printf("[gui-replay-bg] cancel signal received for %s", flowName)
		case <-replayDone:
		}
	}()

	state, err := replayer.Replay(flow, overrides)
	close(replayDone)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("[gui-replay-bg] replay failed: %s — %v", flowName, err)
		failStep := 0
		if state != nil {
			failStep = state.CurrentStep
		}
		emitGUIReplayStatus(statusC, loopCtx.ID, flowName, elapsed, false, failStep, totalSteps, err.Error())
		return
	}

	log.Printf("[gui-replay-bg] replay completed: %s in %v", flowName, elapsed)
	emitGUIReplayStatus(statusC, loopCtx.ID, flowName, elapsed, true, totalSteps, totalSteps, "")
}

func emitGUIReplayStatus(statusC chan StatusEvent, loopID, flowName string, elapsed time.Duration, success bool, currentStep, totalSteps int, errMsg string) {
	if statusC == nil {
		return
	}
	evType := StatusEventSessionCompleted
	msg := fmt.Sprintf("GUI 回放 [%s] 完成，耗时 %v", flowName, elapsed.Round(time.Second))
	if !success {
		evType = StatusEventSessionFailed
		msg = fmt.Sprintf("GUI 回放 [%s] 失败（步骤 %d/%d）: %s", flowName, currentStep, totalSteps, errMsg)
	}
	ev := StatusEvent{
		Type:    evType,
		LoopID:  loopID,
		Message: msg,
	}
	select {
	case statusC <- ev:
	default:
	}
}

// captureDesktopScreenshot captures the desktop as a base64-encoded PNG.
// screenIndex < 0 means capture all monitors stitched; >= 0 means a specific monitor.
func captureDesktopScreenshot(screenIndex int) (string, error) {
	available, reason := remote.DetectDisplayServer()
	if !available {
		return "", fmt.Errorf("no graphical display: %s", reason)
	}

	// On macOS, use native capture directly — success proves permission.
	// Avoids CGRequestScreenCaptureAccess / screencapture TCC dialogs.
	if runtime.GOOS == "darwin" {
		b64, err := nativeCaptureScreenshot()
		if err == nil && b64 != "" && !remote.IsBlankImage(b64) {
			return b64, nil
		}
		if !HasScreenRecordingPermission() {
			return "", fmt.Errorf("screen recording permission not granted")
		}
		if err != nil {
			return "", fmt.Errorf("native screenshot failed: %w", err)
		}
		return "", fmt.Errorf("screenshot is blank — display may be off or locked")
	}

	// Non-macOS: permission check + shell command.
	if !EnsureScreenRecordingPermission() {
		return "", fmt.Errorf("screen recording permission not granted")
	}

	var cmdStr string
	if screenIndex >= 0 {
		res, err := remote.BuildSingleMonitorScreenshotCommandSafe(screenIndex)
		if err != nil {
			return "", err
		}
		cmdStr = res.Command
	} else {
		res := remote.BuildScreenshotCommandWithFallback()
		cmdStr = res.Command
	}
	if cmdStr == "" {
		return "", fmt.Errorf("screenshot not supported on this platform")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		psPath, psErr := coretool.ResolveWindowsPowerShell()
		if psErr != nil {
			return "", fmt.Errorf("screenshot: PowerShell not found: %w", psErr)
		}
		shellName = psPath
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", cmdStr}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", cmdStr}
	}

	cmd := exec.Command(shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)
	coretool.PrepareCommandForTreeKill(cmd)

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("screenshot failed to start: %w", err)
	} else if err := coretool.WaitCommandWithContext(ctx, cmd); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("screenshot timed out after 15s")
		}
		return "", fmt.Errorf("screenshot failed: %w (stderr_len=%d)", err, len([]rune(stderr.String())))
	}

	base64Data, blank, err := remote.ParseScreenshotOutputOpt(stdout.String())
	if err != nil {
		return "", fmt.Errorf("screenshot parse error: %w", err)
	}
	if blank {
		return "", fmt.Errorf("screenshot is blank — display may be off or locked")
	}
	return base64Data, nil
}

// ── arg helpers (gui-local, avoid collision with corelib/guiautomation) ──

func guiStrArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func guiIntArg(args map[string]interface{}, key string, fallback int) int {
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

// formatElementTree recursively formats an accessibility element tree
// as indented text for the gui_observe tool output.
func formatElementTree(sb *strings.Builder, el *accessibility.Element, indent, maxDepth int) {
	if indent >= maxDepth {
		return
	}
	prefix := strings.Repeat("  ", indent)
	valStr := ""
	if el.Value != "" {
		valStr = fmt.Sprintf(" value=%q", el.Value)
	}
	boundsStr := ""
	if el.Bounds.Width > 0 || el.Bounds.Height > 0 {
		boundsStr = fmt.Sprintf(" (%dx%d at %d,%d)", el.Bounds.Width, el.Bounds.Height, el.Bounds.X, el.Bounds.Y)
	}
	sb.WriteString(fmt.Sprintf("%s[%s] %q%s%s\n", prefix, el.Role, el.Name, valStr, boundsStr))
	for i := range el.Children {
		formatElementTree(sb, &el.Children[i], indent+1, maxDepth)
	}
}

// findYOLOWeights returns the path to the YOLO model if it exists.
func findYOLOWeights() string {
	p := yoloModelPath()
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}
