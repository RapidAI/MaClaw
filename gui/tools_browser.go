package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// replayActivityAdapter wraps AgentActivityStore to satisfy browser.ActivityUpdater.
type replayActivityAdapter struct {
	store *AgentActivityStore
}

func (a *replayActivityAdapter) UpdateReplay(flowName string, currentStep, totalSteps int, status string) {
	a.store.Update(&AgentActivity{
		Source:      agentActivitySourceBrowserReplay.String(),
		Task:        fmt.Sprintf("回放: %s", flowName),
		Iteration:   currentStep,
		MaxIter:     totalSteps,
		LastSummary: status,
	})
}

func (a *replayActivityAdapter) ClearReplay() {
	a.store.Clear(agentActivitySourceBrowserReplay.String())
}

// registerBrowserTools registers browser automation tools into the gui ToolRegistry.
//
// Architecture (root-cause fix for Browser role-prefix drift):
//
// The 22 core browser actions (session_start, navigate, click, type, observe, etc.)
// are registered as individual tools in the registry (for handler dispatch), but
// only ONE merged "browser" tool definition is visible to the LLM. This reduces
// LLM context token density from ~4500 tokens (30 definitions x ~150 tokens) to
// ~500 tokens (1 definition), eliminating the root cause of "Browser:" role-prefix
// hallucinations.
//
// Root cause: LLM training data contains multi-agent dialogue formats where
// "Browser" is a high-frequency agent role name. When 30 tool definitions all
// prefixed with "browser_" appear in context, the token density activates this
// training pattern, causing the model to switch from assistant to "Browser agent"
// role. Other tools (ssh, bash) don't trigger this because they have 1 definition
// each and aren't agent role names in training data.
//
// The merged tool uses dispatchMergedBrowser() (in tools_browser_merged.go) to
// route browser(action="navigate", ...) to the individual browser_navigate handler.
func registerBrowserTools(registry *ToolRegistry, app *App) {
	// --- Step 1: Register all individual browser tools into the registry ---
	// These are registered with Status=RegToolAvailable so their handlers are
	// available for dispatch, but they will NOT be included in the LLM tool
	// definitions because the merged "browser" tool replaces them.
	coreReg := tool.NewRegistry()
	browser.RegisterTools(coreReg)

	// Create OCR provider (shared process-wide native PP-OCRv6 engine).
	ocrSidecar := sharedNativeOCRProvider()
	compositeOCR := browser.NewCompositeOCRProvider(ocrSidecar)

	// Create BrowserTaskSupervisor
	sessionFn := func() (*browser.Session, error) {
		return nil, fmt.Errorf("browser session_id required")
	}
	supervisor := browser.NewBrowserTaskSupervisor(nil, nil, compositeOCR, sessionFn, func(msg string) {
		log.Printf("[browser-task] %s", msg)
	})

	browser.RegisterTaskTools(coreReg, supervisor, nil)

	recorder := browser.NewBrowserRecorder(sessionFn, func(msg string) {
		log.Printf("[browser-record] %s", msg)
	})
	replayer := browser.NewFlowReplayer(supervisor, compositeOCR, nil)
	browser.RegisterRecorderTools(coreReg, recorder, replayer, nil, nil, nil, func(msg string) {
		log.Printf("[browser-replay] %s", msg)
	})

	// Bridge individual tools into gui registry (handlers only, for dispatch).
	// Description is set to empty so BuildAll() skips them; only the merged
	// "browser" tool definition is visible to the LLM.
	for _, ct := range coreReg.ListAvailable() {
		toolName := ct.Name   // capture for closure
		handler := ct.Handler // capture for closure
		gt := RegisteredTool{
			Name:        toolName,
			Description: "", // Empty: excluded from LLM tool definitions by BuildAll()
			Category:    ToolCategory(ct.Category),
			Tags:        ct.Tags,
			Priority:    ct.Priority,
			Status:      RegToolStatus(ct.Status),
			InputSchema: ct.InputSchema,
			Required:    ct.Required,
			Source:      ct.Source,
		}
		if handler != nil {
			gt.Handler = func(args map[string]interface{}) string {
				if strings.HasPrefix(toolName, "browser_") && browserToolRequiresSessionID(toolName) && strings.TrimSpace(browserToolStringArg(args, "session_id")) == "" {
					return fmt.Sprintf("browser tool %s requires session_id. First call browser(action=\"session_start\") and use the returned browser-session-*.", toolName)
				}
				result := handler(args)
				if app != nil && app.browserSessions != nil && strings.HasPrefix(toolName, "browser_session_") {
					app.browserSessions.syncFromCore()
					app.emitRemoteStateChanged()
				}
				return result
			}
		}
		registry.Register(gt)
	}

	// --- Step 2: Register the merged "browser" tool ---
	// This is the ONLY browser tool definition visible to the LLM.
	// It dispatches to individual handlers via dispatchMergedBrowser().
	registry.Register(RegisteredTool{
		Name:        MergedBrowserToolName,
		Description: "Stable browser automation. session_start/connect default to persistent managed profile and preserve login/cookies; auto maps to persistent.\n\n" + mergedBrowserToolDescription,
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"browser", "web", "automation"},
		Priority:    6,
		Status:      RegToolAvailable,
		InputSchema: mergedBrowserInputSchema,
		Required:    []string{"action"},
		Source:      "builtin:browser:merged",
		Handler: func(args map[string]interface{}) string {
			result := dispatchMergedBrowser(registry, args)
			// Sync browser session state after session management actions.
			if app != nil && app.browserSessions != nil {
				action, _ := args["action"].(string)
				if normalizeBrowserToolAction(action).ShouldSyncSessions() {
					app.browserSessions.syncFromCore()
					app.emitRemoteStateChanged()
				}
			}
			return result
		},
	})
}

func browserToolRequiresSessionID(toolName string) bool {
	action := normalizeBrowserToolAction(strings.TrimPrefix(strings.TrimSpace(toolName), "browser_"))
	switch action {
	case browserToolActionSessionStart, browserToolActionConnect, browserToolActionListFlows:
		return false
	case browserToolActionUnknown:
		return false
	default:
		return true
	}
}

func browserToolStringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}
