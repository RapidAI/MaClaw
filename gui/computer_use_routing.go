package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

// computerUseEnabledFromConfig returns whether Computer Use product surface is on.
// Defaults to true when unset. Screen parsing (YOLO) can still be off independently.
func computerUseEnabledFromConfig(cfg *corelib.AppConfig) bool {
	if cfg == nil || cfg.ComputerUseEnabled == nil {
		return true
	}
	return *cfg.ComputerUseEnabled
}

func (h *IMMessageHandler) computerUseEnabled() bool {
	if h == nil || h.app == nil {
		return true
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return true
	}
	return computerUseEnabledFromConfig(&cfg)
}

// computerUseSessionActive is true after a successful computer_* action this process.
func computerUseSessionActive() bool {
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	return globalComputerUse.activated
}

func markComputerUseSessionActive() {
	globalComputerUse.mu.Lock()
	globalComputerUse.activated = true
	globalComputerUse.mu.Unlock()
	if UpdateComputerUseTray != nil {
		UpdateComputerUseTray()
	}
}

// shouldActivateComputerUse decides playbook + tool injection for this turn.
func (h *IMMessageHandler) shouldActivateComputerUse(userText string) bool {
	if h == nil || !h.computerUseEnabled() {
		return false
	}
	if computerUseSessionActive() {
		return true
	}
	return computeruse.ShouldActivate(userText)
}

// ensureComputerUseTools forces CU tools into the routed list when active.
// Also drops competing legacy gui_click/type/screenshot to steer text models
// toward ref-based computer_* tools.
func ensureComputerUseTools(tools, allTools []map[string]interface{}, active bool) []map[string]interface{} {
	if !active {
		return tools
	}
	have := make(map[string]bool, len(tools))
	for _, t := range tools {
		have[extractToolName(t)] = true
	}
	byName := make(map[string]map[string]interface{}, len(allTools))
	for _, t := range allTools {
		n := extractToolName(t)
		if n != "" {
			byName[n] = t
		}
	}
	out := make([]map[string]interface{}, 0, len(tools)+len(computeruse.ToolNames))
	// Prefer computer tools first in the list for model attention.
	for _, name := range computeruse.ToolNames {
		if def, ok := byName[name]; ok {
			out = append(out, def)
			have[name] = true
		}
	}
	for _, t := range tools {
		name := extractToolName(t)
		if computeruse.IsComputerUseTool(name) {
			continue // already prepended
		}
		if computeruse.LegacyGUICompeteTools[name] {
			continue // demote raw coordinate tools while CU is active
		}
		out = append(out, t)
	}
	return out
}

// computerUsePlaybookSection returns system-prompt text when CU is active.
func computerUsePlaybookSection(active bool) string {
	if !active {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Computer Use（桌面操控 · 文本模型优先）\n")
	b.WriteString(computeruse.Playbook())
	b.WriteString("\n")
	return b.String()
}

// computerUseYOLOAllowed is set from App.GetScreenParsingEnabled when tools register.
// Defaults to true so headless tests still exercise YOLO when weights exist.
var computerUseYOLOAllowedFn = func() bool { return true }

// computerUseEventEmitter pushes operator-preview events to the UI when bound.
var computerUseEventEmitter func(name string, data interface{})

func computerUseYOLOAllowed() bool {
	if computerUseYOLOAllowedFn == nil {
		return true
	}
	return computerUseYOLOAllowedFn()
}

func emitComputerUseEvent(name string, data interface{}) {
	if computerUseEventEmitter == nil {
		return
	}
	computerUseEventEmitter(name, data)
}

// bindComputerUseApp wires YOLO gate + UI event emission for Computer Use.
func bindComputerUseApp(app *App) {
	if app == nil {
		return
	}
	computerUseYOLOAllowedFn = func() bool {
		return app.GetScreenParsingEnabled()
	}
	computerUseEventEmitter = func(name string, data interface{}) {
		app.emitEvent(name, data)
	}
}

// Deprecated name kept as alias for call sites.
func bindComputerUseYOLOGate(app *App) { bindComputerUseApp(app) }

func computerUsePlaybookOneLiner() string {
	return "text-primary: computer_observe → computer_click(ref=eN) → re-observe; no pixel guessing; screenshots not sent to LLM"
}
