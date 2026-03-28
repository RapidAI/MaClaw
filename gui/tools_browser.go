package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// replayActivityAdapter wraps AgentActivityStore to satisfy browser.ActivityUpdater.
type replayActivityAdapter struct {
	store *AgentActivityStore
}

func (a *replayActivityAdapter) UpdateReplay(flowName string, currentStep, totalSteps int, status string) {
	a.store.Update(&AgentActivity{
		Source:      "browser_replay",
		Task:        fmt.Sprintf("回放: %s", flowName),
		Iteration:   currentStep,
		MaxIter:     totalSteps,
		LastSummary: status,
	})
}

func (a *replayActivityAdapter) ClearReplay() {
	a.store.Clear("browser_replay")
}

// registerBrowserTools registers browser automation tools (CDP-based) into the gui ToolRegistry.
// The tool definitions live in corelib/browser/tools.go (single source of truth).
// This function bridges them into the gui-local ToolRegistry.
func registerBrowserTools(registry *ToolRegistry) {
	// Register into a temporary corelib registry.
	coreReg := tool.NewRegistry()
	browser.RegisterTools(coreReg)

	// Create OCR provider (RapidOCR → LLM Vision fallback)
	ocrSidecar := browser.NewRapidOCRSidecar(func(msg string) {
		log.Printf("[browser-ocr] %s", msg)
	})
	compositeOCR := browser.NewCompositeOCRProvider(ocrSidecar)

	// Create BrowserTaskSupervisor
	sessionFn := func() (*browser.Session, error) {
		return browser.GetSession("")
	}
	supervisor := browser.NewBrowserTaskSupervisor(nil, nil, compositeOCR, sessionFn, func(msg string) {
		log.Printf("[browser-task] %s", msg)
	})

	// Register task supervisor tools
	browser.RegisterTaskTools(coreReg, supervisor, nil)

	// Register OCR tool
	browser.RegisterOCRTool(coreReg, compositeOCR, sessionFn)

	// Register recorder + replayer tools
	recorder := browser.NewBrowserRecorder(sessionFn, func(msg string) {
		log.Printf("[browser-record] %s", msg)
	})
	replayer := browser.NewFlowReplayer(supervisor, compositeOCR, nil)
	// Note: loopMgr/activityStore/statusC are nil here. Async replay is wired
	// at a higher level (app.go) when the gui BackgroundLoopManager is available.
	// The corelib browser package uses corelib/agent types, not gui-local types.
	browser.RegisterRecorderTools(coreReg, recorder, replayer, nil, nil, nil, func(msg string) {
		log.Printf("[browser-replay] %s", msg)
	})

	// Bridge all corelib browser tools into the gui registry.
	for _, ct := range coreReg.ListAvailable() {
		gt := RegisteredTool{
			Name:        ct.Name,
			Description: ct.Description,
			Category:    ToolCategory(ct.Category),
			Tags:        ct.Tags,
			Priority:    ct.Priority,
			Status:      RegToolStatus(ct.Status),
			InputSchema: ct.InputSchema,
			Required:    ct.Required,
			Source:      ct.Source,
		}
		if ct.Handler != nil {
			h := ct.Handler
			gt.Handler = func(args map[string]interface{}) string {
				return h(args)
			}
		}
		registry.Register(gt)
	}
}
