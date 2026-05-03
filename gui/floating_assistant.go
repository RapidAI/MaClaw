package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const defaultPetSize = 88

// FloatingAssistantManager manages the MaClaw desktop pet window lifecycle.
// The pet is independent from the main window and is configured from Settings > Pet.
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

// ShowFloatingButton creates and shows the MaClaw desktop pet window.
// No-op if the pet is disabled or already visible.
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
	if m.app == nil {
		log.Printf("[floating-assistant] ShowFloatingButton: app is nil")
		return
	}

	// Check config - do not show if disabled.
	config, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[floating-assistant] ShowFloatingButton: LoadConfig failed: %v", err)
		return
	}
	if !config.PetEnabled {
		log.Printf("[floating-assistant] ShowFloatingButton: pet disabled in config")
		return
	}

	// Idempotent - no-op if already visible.
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

	winSize := floatingWindowSize(config)
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

// HideFloatingButton hides and destroys the desktop pet window.
func (m *FloatingAssistantManager) HideFloatingButton() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
}

// RefreshAppearance recreates the desktop pet window so native renderers pick up
// pet skin, size, and interaction changes immediately.
func (m *FloatingAssistantManager) RefreshAppearance(config corelib.AppConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !config.PetEnabled {
		if m.window != nil {
			m.window.Destroy()
		}
		m.visible = false
		return
	}
	if m.window == nil {
		if m.app == nil {
			log.Printf("[floating-assistant] RefreshAppearance: app is nil")
			return
		}
		m.window = newFloatingWindow(m.app)
	}

	winSize := floatingWindowSize(config)
	if m.posX == 0 && m.posY == 0 {
		m.posX, m.posY = m.loadOrDefaultPosition(config)
	}
	if m.visible && m.window != nil {
		m.window.Destroy()
	}
	if err := m.window.Create(m.posX, m.posY, winSize, winSize); err != nil {
		log.Printf("[floating-assistant] RefreshAppearance: window.Create FAILED: %v", err)
		m.visible = false
		return
	}
	m.window.Show()
	m.visible = true
}

// IsVisible returns whether the desktop pet is currently shown.
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
	buttonSize := floatingWindowSizeForCurrentConfig(m.app)

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

// persistPosition saves the desktop pet position to AppConfig.
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
	if !config.FloatingBtnPositionSet {
		config.FloatingBtnPositionSet = true
		changed = true
	}
	if changed {
		_ = m.app.SaveConfig(config)
	}
}

// loadOrDefaultPosition returns the persisted position from config,
// or the default top-right corner position if not set.
func (m *FloatingAssistantManager) loadOrDefaultPosition(config corelib.AppConfig) (int, int) {
	if config.FloatingBtnPositionSet || config.FloatingBtnX > 0 || config.FloatingBtnY > 0 {
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

// OnFloatingButtonClicked handles a left-click on the desktop pet.
// It shows the main window and switches to the AI panel while keeping the pet visible.
func (m *FloatingAssistantManager) OnFloatingButtonClicked() {
	if m == nil || m.app == nil || m.app.ctx == nil {
		return
	}

	voiceRequested := false
	if m.app != nil {
		if config, err := m.app.LoadConfig(); err == nil && config.PetEnabled && config.PetVoiceInput {
			switch config.PetConversationMode {
			case "voice-turn", "continuous":
				voiceRequested = true
			}
		}
	}

	// Show main window and bring to front. The desktop pet remains visible.
	runtime.WindowShow(m.app.ctx)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, true)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, false)

	// Tell frontend to switch to AI assistant panel. When the pet is configured
	// for voice conversation, also request the panel to open voice input.
	runtime.EventsEmit(m.app.ctx, "switch-to-ai-panel", map[string]any{
		"source": "pet",
		"voice":  voiceRequested,
	})
}

// OnFloatingButtonDragged moves the floating window to the given screen
// coordinates and saves the position for subsequent displays.
// The position is clamped to screen bounds before moving.
// Requirements: 3.3, 3.4
func (m *FloatingAssistantManager) OnFloatingButtonDragged(x, y int) {
	// Clamp position to screen bounds (Requirement 3.4)
	screenW := getScreenWidth()
	screenH := getScreenHeight()
	buttonSize := floatingWindowSizeForCurrentConfig(m.app)

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

func floatingWindowSize(config corelib.AppConfig) int {
	size := config.PetSize
	if size <= 0 {
		size = defaultPetSize
	}
	if size < 56 {
		size = 56
	}
	if size > 120 {
		size = 120
	}
	return size + 16
}

func isPetMotionEnabled(config corelib.AppConfig) bool {
	return config.PetMotionEnabled == nil || *config.PetMotionEnabled
}

func petMotionSoundEnabled(config corelib.AppConfig) bool {
	return config.PetMotionSound == nil || *config.PetMotionSound
}

func sanitizePetConfig(config *corelib.AppConfig) {
	if config == nil {
		return
	}
	switch config.PetSkin {
	case "", "clawmate":
		config.PetSkin = "clawmate"
	case "mini-claw", "dev-claw", "focus-claw":
	default:
		config.PetSkin = "clawmate"
	}

	if config.PetSize == 0 {
		config.PetSize = defaultPetSize
	} else if config.PetSize < 56 {
		config.PetSize = 56
	} else if config.PetSize > 120 {
		config.PetSize = 120
	}

	switch config.PetInteractionMode {
	case "quiet", "balanced", "active":
	default:
		config.PetInteractionMode = "balanced"
	}
	switch config.PetConversationMode {
	case "text-first", "voice-turn", "continuous":
	default:
		config.PetConversationMode = "text-first"
	}
	switch config.PetReadbackMode {
	case "off", "summary", "full", "done-only":
	default:
		if config.PetVoiceReadback {
			config.PetReadbackMode = "summary"
		} else {
			config.PetReadbackMode = "off"
		}
	}
	if config.PetReadbackMode == "off" {
		config.PetVoiceReadback = false
	} else if config.PetReadbackMode != "" {
		config.PetVoiceReadback = true
	}

	if config.PetContinuousTimeout == 0 {
		config.PetContinuousTimeout = 30
	} else if config.PetContinuousTimeout < 5 {
		config.PetContinuousTimeout = 5
	} else if config.PetContinuousTimeout > 120 {
		config.PetContinuousTimeout = 120
	}
}

func floatingAppearanceChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.PetEnabled != newConfig.PetEnabled ||
		oldConfig.PetSkin != newConfig.PetSkin ||
		oldConfig.PetSize != newConfig.PetSize ||
		isPetMotionEnabled(oldConfig) != isPetMotionEnabled(newConfig) ||
		petMotionSoundEnabled(oldConfig) != petMotionSoundEnabled(newConfig) ||
		oldConfig.PetQuietMode != newConfig.PetQuietMode ||
		oldConfig.PetInteractionMode != newConfig.PetInteractionMode
}

func floatingWindowSizeForCurrentConfig(app *App) int {
	if app == nil {
		return defaultPetSize + 16
	}
	config, err := app.LoadConfig()
	if err != nil {
		return defaultPetSize + 16
	}
	return floatingWindowSize(config)
}

// QuitApp terminates the application. Called from the desktop pet's
// right-click context menu "Quit" item.
func (m *FloatingAssistantManager) QuitApp() {
	log.Println("[floating-assistant] QuitApp: user requested exit from desktop pet")

	// Destroy the floating window first so the WebView is cleaned up
	// before the Wails shutdown sequence begins.
	m.mu.Lock()
	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
	m.mu.Unlock()

	// Quit on a separate goroutine - runtime.Quit triggers the Wails
	// shutdown sequence which must not run on the WebView's JS callback
	// goroutine (same pattern as the system tray quit handler).
	// After runtime.Quit, also call quitSystray() to terminate the
	// systray event loop - otherwise the process stays alive in the
	// background even though the Wails window is gone.
	go func() {
		if m.app != nil && m.app.ctx != nil {
			runtime.Quit(m.app.ctx)
		}
		time.Sleep(500 * time.Millisecond)
		quitSystray()
	}()
}

// Screen dimension helpers
// Platform-specific implementations can override these via build-tag files
// (e.g. floating_assistant_windows.go, floating_assistant_darwin.go).
// Defaults to 1920x1080 when no platform hook is set.

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
