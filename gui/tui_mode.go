package main

// tui_mode.go provides a terminal-based interactive mode that uses the same
// IMMessageHandler as the desktop GUI and IM channels. Launched via `maclaw tui`.
//
// This is the canonical TUI implementation. It uses the exact same agent
// code path as desktop — same system prompt, same tools, same workflow engine,
// same drift detection, same coding gate. The only difference is the I/O layer
// (Bubble Tea terminal UI vs Wails desktop UI).
//
// The independent maclaw-tui binary (tui/) is a legacy path that will be
// deprecated in favor of this unified implementation.

import (
	"fmt"
	"os"
	"time"

	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// runTUIMode starts the terminal-based interactive mode.
func runTUIMode() {
	// Initialize the App — same as GUI startup, but we won't launch Wails.
	app := NewApp()

	// Ensure core infrastructure is ready (remote sessions, MCP, skills, tools).
	app.ensureInteractionInfra()

	// Initialize workflow engine and steering store (async, same as GUI).
	go app.initWorkflowEngine()
	go app.initSteeringStore()

	// Create IMMessageHandler directly — no Hub connection needed for local TUI.
	manager := &RemoteSessionManager{
		app:      app,
		sessions: make(map[string]*RemoteSession),
	}
	handler := NewIMMessageHandler(app, manager)

	// Create and run the Bubble Tea program.
	tuiApp := &tuiModeApp{
		handler: handler,
		root:    views.NewRootModel("zh"),
	}
	p := tea.NewProgram(tuiApp, tea.WithAltScreen())
	tuiApp.program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Cleanup.
	handler.memory.Stop()
}

// tuiModeApp is the Bubble Tea model for TUI mode.
type tuiModeApp struct {
	handler *IMMessageHandler
	program *tea.Program
	root    views.RootModel
	ready   bool
}

func (a *tuiModeApp) Init() tea.Cmd {
	return func() tea.Msg {
		// Brief wait for async init (workflow engine, steering store).
		time.Sleep(300 * time.Millisecond)
		return tuiReadyMsg{}
	}
}

type tuiReadyMsg struct{}

func (a *tuiModeApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tuiReadyMsg:
		a.ready = true
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

	case views.ChatSendMsg:
		return a, a.sendMessage(msg.Text)
	}

	// Delegate everything else to the root view.
	var cmd tea.Cmd
	a.root, cmd = a.root.Update(msg)
	return a, cmd
}

func (a *tuiModeApp) View() string {
	if !a.ready {
		return "Initializing..."
	}
	return a.root.View()
}

// sendMessage sends a user message through the unified IMMessageHandler.
func (a *tuiModeApp) sendMessage(text string) tea.Cmd {
	prog := a.program

	return func() tea.Msg {
		resp := a.handler.HandleIMMessageWithProgressAndStream(
			IMUserMessage{
				UserID:   "tui-user",
				Platform: "tui",
				Text:     text,
				Lang:     "zh",
			},
			func(progressText string) {
				if prog != nil {
					prog.Send(views.ChatStreamMsg{Type: "progress", Content: progressText})
				}
			},
			func(delta string) {
				if prog != nil {
					prog.Send(views.ChatStreamMsg{Type: "token", Content: delta})
				}
			},
			nil, nil,
		)

		if resp == nil {
			return views.ChatResponseMsg{Error: "no response"}
		}
		if resp.Error != "" {
			return views.ChatResponseMsg{Error: resp.Error}
		}
		return views.ChatResponseMsg{Text: resp.Text}
	}
}
