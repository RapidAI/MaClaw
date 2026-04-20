package main

import (
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FloatingAssistantManager manages the floating assistant button window lifecycle.
// It handles creation, display, hiding, and destruction of the floating button
// that appears when the main window is hidden.
type FloatingAssistantManager struct {
	app     *App
	visible bool
	posX    int
	posY    int
	mu      sync.Mutex
	window  floatingWindow
}

// NewFloatingAssistantManager creates a new FloatingAssistantManager.
func NewFloatingAssistantManager(app *App) *FloatingAssistantManager {
	m := &FloatingAssistantManager{
		app: app,
	}
	m.window = newFloatingWindow(app)
	return m
}

// ShowFloatingButton creates and shows the floating assistant window.
// No-op if config.ShowAssistantEntry is false or already visible.
// On window creation failure, logs error silently and does not affect main window functionality.
func (m *FloatingAssistantManager) ShowFloatingButton() {
	m.mu.Lock()
	defer m.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[floating-assistant] ShowFloatingButton PANIC: %v", r)
		}
	}()

	log.Printf("[floating-assistant] ShowFloatingButton: enter, app=%v", m.app != nil)

	// Check config — do not show if disabled.
	config, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[floating-assistant] ShowFloatingButton: LoadConfig failed: %v", err)
		return
	}
	if !config.ShowAssistantEntry {
		log.Printf("[floating-assistant] ShowFloatingButton: disabled in config")
		return
	}

	// Idempotent — no-op if already visible.
	if m.visible {
		log.Printf("[floating-assistant] ShowFloatingButton: already visible, skipping")
		return
	}

	// Restore persisted position, or compute default (top-right corner).
	if m.posX == 0 && m.posY == 0 {
		m.posX, m.posY = m.loadOrDefaultPosition(config)
	}

	log.Printf("[floating-assistant] ShowFloatingButton: pos=(%d,%d) window=%v", m.posX, m.posY, m.window != nil)

	// Create platform-specific floating window.
	if m.window == nil {
		log.Printf("[floating-assistant] ShowFloatingButton: window is nil, cannot show")
		return
	}

	winSize := 72 // must match floatWinSize on Windows
	log.Printf("[floating-assistant] ShowFloatingButton: calling window.Create(%d,%d,%d,%d)", m.posX, m.posY, winSize, winSize)
	if err := m.window.Create(m.posX, m.posY, winSize, winSize); err != nil {
		log.Printf("[floating-assistant] ShowFloatingButton: window.Create FAILED: %v", err)
		return
	}
	log.Printf("[floating-assistant] ShowFloatingButton: window.Create succeeded, calling Show()")
	m.window.Show()

	m.visible = true
	log.Printf("[floating-assistant] ShowFloatingButton: done, visible=true")
}

// HideFloatingButton hides and destroys the floating assistant window.
func (m *FloatingAssistantManager) HideFloatingButton() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
}

// IsVisible returns whether the floating button is currently shown.
func (m *FloatingAssistantManager) IsVisible() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visible
}

// UpdatePosition saves the dragged position for subsequent displays.
// The position is clamped to screen bounds and persisted to config.
func (m *FloatingAssistantManager) UpdatePosition(x, y int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	screenW := getScreenWidth()
	screenH := getScreenHeight()
	buttonSize := 64

	if x < 0 {
		x = 0
	} else if x > screenW-buttonSize {
		x = screenW - buttonSize
	}
	if y < 0 {
		y = 0
	} else if y > screenH-buttonSize {
		y = screenH - buttonSize
	}

	m.posX = x
	m.posY = y

	// Persist position to config (best-effort, don't block on failure).
	go m.persistPosition(x, y)
}

// persistPosition saves the floating button position to AppConfig.
func (m *FloatingAssistantManager) persistPosition(x, y int) {
	if m.app == nil {
		return
	}
	config, err := m.app.LoadConfig()
	if err != nil {
		return
	}
	changed := false
	if config.FloatingBtnX != x {
		config.FloatingBtnX = x
		changed = true
	}
	if config.FloatingBtnY != y {
		config.FloatingBtnY = y
		changed = true
	}
	if changed {
		_ = m.app.SaveConfig(config)
	}
}

// loadOrDefaultPosition returns the persisted position from config,
// or the default top-right corner position if not set.
func (m *FloatingAssistantManager) loadOrDefaultPosition(config AppConfig) (int, int) {
	if config.FloatingBtnX > 0 || config.FloatingBtnY > 0 {
		log.Printf("[floating-assistant] loadOrDefaultPosition: restored from config (%d, %d)", config.FloatingBtnX, config.FloatingBtnY)
		return config.FloatingBtnX, config.FloatingBtnY
	}
	// Default: top-right corner, 150px from right edge, 100px from top.
	// Use generous margins to avoid taskbar on any edge.
	screenW := getScreenWidth()
	x := screenW - 150
	y := 100
	if x < 0 {
		x = 100
	}
	log.Printf("[floating-assistant] loadOrDefaultPosition: default (%d, %d) screenW=%d", x, y, screenW)
	return x, y
}

// OnFloatingButtonClicked handles a left-click on the floating button.
// It shows the main window, switches to the AI panel, and hides the
// floating button (mutual exclusivity — Requirement 7).
func (m *FloatingAssistantManager) OnFloatingButtonClicked() {
	// Hide floating button first (Requirement 7: never simultaneously visible).
	m.HideFloatingButton()

	// Show main window and bring to front.
	runtime.WindowShow(m.app.ctx)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, true)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, false)

	// Tell frontend to switch to AI assistant panel.
	runtime.EventsEmit(m.app.ctx, "switch-to-ai-panel")
}

// OnFloatingButtonDragged moves the floating window to the given screen
// coordinates and saves the position for subsequent displays.
// The position is clamped to screen bounds before moving.
// Requirements: 3.3, 3.4
func (m *FloatingAssistantManager) OnFloatingButtonDragged(x, y int) {
	// Clamp position to screen bounds (Requirement 3.4)
	screenW := getScreenWidth()
	screenH := getScreenHeight()
	buttonSize := 64

	if x < 0 {
		x = 0
	} else if x > screenW-buttonSize {
		x = screenW - buttonSize
	}

	if y < 0 {
		y = 0
	} else if y > screenH-buttonSize {
		y = screenH - buttonSize
	}

	// Move the platform window under lock (window operations are not
	// covered by UpdatePosition's lock).
	m.mu.Lock()
	if m.window != nil {
		m.window.MoveTo(x, y)
	}
	m.mu.Unlock()

	// Save position (UpdatePosition acquires its own lock).
	m.UpdatePosition(x, y)
}

// QuitApp terminates the application. Called from the floating button's
// right-click context menu "退出" item.
func (m *FloatingAssistantManager) QuitApp() {
	log.Println("[floating-assistant] QuitApp: user requested exit from floating button")

	// Destroy the floating window first so the WebView is cleaned up
	// before the Wails shutdown sequence begins.
	m.mu.Lock()
	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
	m.mu.Unlock()

	// Quit on a separate goroutine — runtime.Quit triggers the Wails
	// shutdown sequence which must not run on the WebView's JS callback
	// goroutine (same pattern as the system tray quit handler).
	// After runtime.Quit, also call quitSystray() to terminate the
	// systray event loop — otherwise the process stays alive in the
	// background even though the Wails window is gone.
	go func() {
		if m.app != nil && m.app.ctx != nil {
			runtime.Quit(m.app.ctx)
		}
		time.Sleep(500 * time.Millisecond)
		quitSystray()
	}()
}

// ── Screen dimension helpers ────────────────────────────────────────────────
// Platform-specific implementations can override these via build-tag files
// (e.g. floating_assistant_windows.go, floating_assistant_darwin.go).
// Defaults to 1920×1080 when no platform hook is set.

var platformGetScreenWidth func() int
var platformGetScreenHeight func() int

// quitSystray terminates the system tray event loop. Set by platform-specific
// tray setup code (e.g. tray_windows.go). Without this, the process stays
// alive in the background after runtime.Quit because the systray loop blocks.
var quitSystray func() = func() {}

func getScreenWidth() int {
	if platformGetScreenWidth != nil {
		return platformGetScreenWidth()
	}
	return 1920 // sensible default
}

func getScreenHeight() int {
	if platformGetScreenHeight != nil {
		return platformGetScreenHeight()
	}
	return 1080 // sensible default
}
