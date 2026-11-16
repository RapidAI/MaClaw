package guiapp

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/guiapp/petpack"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultPetSize         = 224
	minPetSize             = 56
	maxPetSize             = 240
	legacyDefaultPetSize   = 88
	previousDefaultPetSize = 176
)

// FloatingAssistantManager manages the MaClaw desktop pet window lifecycle.
// The pet is independent from the main window and is configured from Settings > Pet.
type FloatingAssistantManager struct {
	app           *App
	visible       bool
	posX          int
	posY          int
	mu            sync.Mutex
	window        floatingWindow
	companion     *PetCompanionSession
	companionPose bool
	lastDuty      petDutyDecision
}

// NewFloatingAssistantManager creates a new FloatingAssistantManager.
func NewFloatingAssistantManager(app *App) *FloatingAssistantManager {
	m := &FloatingAssistantManager{
		app:       app,
		companion: newPetCompanionSession(app),
	}
	m.window = newFloatingWindow(app)
	return m
}

// ShowFloatingButton creates and shows the MaClaw desktop pet window.
// No-op if the pet is disabled or already visible.
// On window creation failure, logs error silently and does not affect main window functionality.
func (m *FloatingAssistantManager) ShowFloatingButton() {
	m.mu.Lock()
	defer func() {
		m.mu.Unlock()
		m.syncCompanion()
		m.syncPetRearing()
	}()

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

	// Create platform-specific floating window.
	if m.window == nil {
		log.Printf("[floating-assistant] ShowFloatingButton: window is nil, cannot show")
		return
	}

	// Commit the session before Create. The Win32 window is WS_VISIBLE inside
	// that call, so a click can arrive before Show returns and before the
	// deferred sync. The stage cue stays unplanned until that sync: planning
	// here would announce the crossing and then drop it. fresh means the
	// clock started here; time until the window is up is not time on screen.
	_, fresh := m.app.openPetRearingSessionAt(petSkinID(config), true, petUserSize(config), time.Now())

	// Restore persisted position, or compute default (top-right corner).
	// Always re-clamp: a 1920-wide fallback or a position saved on a larger
	// display will otherwise place the pet off a 1280×800 remote desktop.
	if m.posX == 0 && m.posY == 0 {
		m.posX, m.posY = m.loadOrDefaultPosition(config)
	} else {
		m.posX, m.posY = clampPetScreenPosition(m.posX, m.posY, rearedWindowSize(m.app, config.PetSkin, config.PetSize))
	}

	log.Printf("[floating-assistant] ShowFloatingButton: pos=(%d,%d) window=%v", m.posX, m.posY, m.window != nil)

	winSize := rearedWindowSize(m.app, config.PetSkin, config.PetSize)
	log.Printf("[floating-assistant] ShowFloatingButton: calling window.Create(%d,%d,%d,%d)", m.posX, m.posY, winSize, winSize)
	if err := m.window.Create(m.posX, m.posY, winSize, winSize); err != nil {
		log.Printf("[floating-assistant] ShowFloatingButton: window.Create FAILED: %v", err)
		if fresh {
			m.app.rebasePetRearingClock(time.Now())
		}
		return
	}
	log.Printf("[floating-assistant] ShowFloatingButton: window.Create succeeded, calling Show()")
	m.window.Show()

	m.visible = true
	if fresh {
		m.app.rebasePetRearingClock(time.Now())
	}
	log.Printf("[floating-assistant] ShowFloatingButton: done, visible=true")
}

// HideFloatingButton hides and destroys the desktop pet window.
func (m *FloatingAssistantManager) HideFloatingButton() {
	var saveX, saveY int
	var save bool
	m.mu.Lock()
	defer func() {
		m.mu.Unlock()
		if save {
			go m.persistPosition(saveX, saveY)
		}
		m.syncCompanion()
		m.syncPetRearing()
	}()

	// End the click session before Destroy and before this lock is released.
	// The deferred rearing sync used to retire only after companion sync, so a
	// single click waiting out the double-click interval still opened the
	// main window and added growth after the pet was gone.
	if m.app != nil {
		m.app.retirePetClickBurst()
	}
	if x, y, _, changed := m.adoptLivePetOriginLocked(floatingWindowSizeForCurrentConfig(m.app)); changed {
		saveX, saveY, save = x, y, true
	}
	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
}

// petAppearanceReloader updates a live pet window in place. Destroying and
// recreating the macOS panel on every skin change over-releases the window
// and kills the process.
type petAppearanceReloader interface {
	ReloadAppearance(x, y, size int) error
}

// petOriginWindow is the live screen origin. Walking and border patrol move
// the native window without updating the manager's saved point. Hide, refresh,
// the pet-menu quit, and any other process exit read it back so the next
// create does not jump to the last drag.
type petOriginWindow interface {
	PetOrigin() (x, y int, ok bool)
}

func livePetOrigin(w floatingWindow) (int, int, bool) {
	if w == nil {
		return 0, 0, false
	}
	src, ok := w.(petOriginWindow)
	if !ok {
		return 0, 0, false
	}
	return src.PetOrigin()
}

// adoptLivePetOriginLocked stores the window's current origin. The caller
// holds m.mu. live is false when this window cannot report one. changed is
// true when the stored point moved and still needs to be written out.
func (m *FloatingAssistantManager) adoptLivePetOriginLocked(sz int) (x, y int, live, changed bool) {
	if m == nil {
		return 0, 0, false, false
	}
	x, y, live = livePetOrigin(m.window)
	if !live {
		return 0, 0, false, false
	}
	x, y = clampPetScreenPosition(x, y, sz)
	changed = x != m.posX || y != m.posY
	m.posX, m.posY = x, y
	return x, y, true, changed
}

// RefreshAppearance applies pet skin, size, and interaction changes. A live
// window that can reload does so without being destroyed.
func (m *FloatingAssistantManager) RefreshAppearance(config corelib.AppConfig) {
	var saveX, saveY int
	var save bool
	m.mu.Lock()
	defer func() {
		m.mu.Unlock()
		if save {
			go m.persistPosition(saveX, saveY)
		}
		m.syncCompanion()
		m.syncPetRearing()
	}()

	if !config.PetEnabled {
		if m.app != nil {
			m.app.retirePetClickBurst()
		}
		if x, y, _, changed := m.adoptLivePetOriginLocked(floatingWindowSizeForCurrentConfig(m.app)); changed {
			saveX, saveY, save = x, y, true
		}
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
	// Same order as show: the click session is live before the window is
	// touched, and the stage cue is planned only by the deferred sync.
	// A skin change retires the old gesture and opens the new one before
	// this reload, while the previous window can still take a click.
	var fresh bool
	if m.app != nil {
		_, fresh = m.app.openPetRearingSessionAt(petSkinID(config), true, petUserSize(config), time.Now())
	}

	winSize := rearedWindowSize(m.app, config.PetSkin, config.PetSize)
	if x, y, live, changed := m.adoptLivePetOriginLocked(winSize); live {
		if changed {
			saveX, saveY, save = x, y, true
		}
	} else if m.posX == 0 && m.posY == 0 {
		m.posX, m.posY = m.loadOrDefaultPosition(config)
	} else {
		m.posX, m.posY = clampPetScreenPosition(m.posX, m.posY, winSize)
	}
	if m.visible && m.window != nil && m.window.IsCreated() {
		if reloader, ok := m.window.(petAppearanceReloader); ok {
			if err := reloader.ReloadAppearance(m.posX, m.posY, winSize); err != nil {
				log.Printf("[floating-assistant] RefreshAppearance: reload failed: %v", err)
			} else {
				m.window.Show()
				m.visible = true
				if fresh {
					m.app.rebasePetRearingClock(time.Now())
				}
				return
			}
		}
		m.window.Destroy()
	}
	if err := m.window.Create(m.posX, m.posY, winSize, winSize); err != nil {
		log.Printf("[floating-assistant] RefreshAppearance: window.Create FAILED: %v", err)
		m.visible = false
		if fresh {
			m.app.rebasePetRearingClock(time.Now())
		}
		return
	}
	m.window.Show()
	m.visible = true
	if fresh {
		m.app.rebasePetRearingClock(time.Now())
	}
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

	buttonSize := floatingWindowSizeForCurrentConfig(m.app)
	x, y = clampPetScreenPosition(x, y, buttonSize)

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
	_ = m.app.PatchConfig(func(config *corelib.AppConfig) {
		config.FloatingBtnX = x
		config.FloatingBtnY = y
		config.FloatingBtnPositionSet = true
	})
}

// persistLivePetOrigin writes the origin the window is actually using.
// Patrol updates that origin without updating the saved point. Tray quit,
// command-line quit, restart, and Wails shutdown never pass through the
// pet menu, so this is what keeps the next launch on the same screen.
// The pet-menu quit already stored it and destroyed the window; a later
// call then sees no live origin and does not write again. A point already
// stored does not write again either.
func (m *FloatingAssistantManager) persistLivePetOrigin() {
	if m == nil {
		return
	}
	// Size is only the clamp inset. Read it before m.mu so config load does
	// not sit under the same lock Hide and the window origin use.
	sz := floatingWindowSizeForCurrentConfig(m.app)
	m.mu.Lock()
	x, y, _, changed := m.adoptLivePetOriginLocked(sz)
	m.mu.Unlock()
	if !changed {
		return
	}
	m.persistPosition(x, y)
}

// loadOrDefaultPosition returns the persisted position from config,
// or the default top-right corner position if not set.
func (m *FloatingAssistantManager) loadOrDefaultPosition(config corelib.AppConfig) (int, int) {
	screenW := getScreenWidth()
	screenH := getScreenHeight()
	winSize := rearedWindowSize(m.app, config.PetSkin, config.PetSize)
	if config.FloatingBtnPositionSet || config.FloatingBtnX > 0 || config.FloatingBtnY > 0 {
		x, y := clampPetScreenPosition(config.FloatingBtnX, config.FloatingBtnY, winSize)
		log.Printf("[floating-assistant] loadOrDefaultPosition: restored from config (%d, %d) clamped=(%d,%d) screen=%dx%d", config.FloatingBtnX, config.FloatingBtnY, x, y, screenW, screenH)
		return x, y
	}
	// Default: top-right corner, 150px from right edge, 100px from top.
	// Use generous margins to avoid taskbar on any edge.
	x := screenW - 150
	y := 100
	if x < 0 {
		x = 100
	}
	x, y = clampPetScreenPosition(x, y, winSize)
	log.Printf("[floating-assistant] loadOrDefaultPosition: default (%d, %d) screen=%dx%d", x, y, screenW, screenH)
	return x, y
}

func (m *FloatingAssistantManager) syncCompanion() {
	if m == nil || m.companion == nil {
		return
	}
	m.mu.Lock()
	visible := m.visible
	app := m.app
	m.mu.Unlock()
	cfg := corelib.AppConfig{}
	if app != nil {
		loaded, err := app.LoadConfig()
		if err != nil {
			m.companion.Stop()
			return
		}
		cfg = loaded
	}
	if visible && petVoiceCompanionArmed(cfg) {
		m.companion.Start()
		return
	}
	m.companion.Stop()
}

// OnFloatingButtonClicked handles a left-click on the desktop pet.
// Voice companion mode treats the click as a wake. Otherwise it opens the main window.
func (m *FloatingAssistantManager) OnFloatingButtonClicked() {
	if m == nil || m.app == nil {
		return
	}
	if config, err := m.app.LoadConfig(); err == nil && petVoiceCompanionArmed(config) {
		if m.companion != nil {
			m.companion.Poke()
		}
		return
	}
	if m.app.ctx == nil {
		return
	}

	// Show main window and bring to front. The desktop pet remains visible.
	runtime.WindowShow(m.app.ctx)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, true)
	runtime.WindowSetAlwaysOnTop(m.app.ctx, false)

	// Tell frontend to switch to AI assistant panel. When the pet is configured
	// for voice conversation, also request the panel to open voice input.
	m.app.emitEvent("switch-to-ai-panel", map[string]any{
		"source": "pet",
		"voice":  false,
	})
}

// OnFloatingButtonDragged moves the floating window to the given screen
// coordinates and saves the position for subsequent displays.
// The position is clamped to screen bounds before moving.
// Requirements: 3.3, 3.4
func (m *FloatingAssistantManager) OnFloatingButtonDragged(x, y int) {
	// Clamp position to screen bounds (Requirement 3.4)
	buttonSize := floatingWindowSizeForCurrentConfig(m.app)
	x, y = clampPetScreenPosition(x, y, buttonSize)

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
	if size < minPetSize {
		size = minPetSize
	}
	if size > maxPetSize {
		size = maxPetSize
	}
	return size + 16
}

func isPetMotionEnabled(config corelib.AppConfig) bool {
	return config.PetMotionEnabled == nil || *config.PetMotionEnabled
}

func petMotionSoundEnabled(config corelib.AppConfig) bool {
	return config.PetMotionSound == nil || *config.PetMotionSound
}

// petMotionSoundHeard is false while the microphone is hot or the pet is speaking,
// so a motion tone cannot be heard as the wake word or sit on top of the voice.
// Quiet mode and reduced motion stay silent too.
func petMotionSoundHeard(enabled, quiet, reduced bool, state string) bool {
	if !enabled || quiet || reduced {
		return false
	}
	switch petpack.NormalizeState(state) {
	case petpack.StateSpeaking, petpack.StateAlert, petpack.StateListening, petpack.StateThinking:
		return false
	default:
		return true
	}
}

// actionSoundAllowed is the per-action cue gate. Listening stays silent so a
// cue cannot be heard as the wake word. Speaking stays silent except for the
// sing cue, which is the speaking performance. Other cues, including thinking
// and alert, keep their own sounds. Quiet mode and reduced motion stay silent.
func actionSoundAllowed(enabled, quiet, reduced bool, state, cue string) bool {
	if !enabled || quiet || reduced {
		return false
	}
	switch petpack.NormalizeState(state) {
	case petpack.StateListening:
		return false
	case petpack.StateSpeaking:
		return cue == "sing"
	default:
		return true
	}
}

func petMotionSoundPreset(config corelib.AppConfig) string {
	switch config.PetMotionSoundPreset {
	case "classic", "bubble", "chime", "synth", "soft":
		return config.PetMotionSoundPreset
	default:
		return "classic"
	}
}

func sanitizePetConfig(config *corelib.AppConfig) {
	if config == nil {
		return
	}
	// Prefer registry allowlist when ready; never wipe well-formed user pack ids when not ready (B.4 / K20).
	reg := petpack.EnsureGlobal()
	ready := reg != nil && reg.Ready()
	var allow map[string]bool
	if ready {
		allow = reg.Allowlist()
	}
	config.PetSkin = petpack.SanitizeSkinID(config.PetSkin, ready, allow)
	// The mechanical crab is retired. A saved clawmate choice follows the new
	// default the next time settings are sanitized, including app load.
	if config.PetSkin == petpack.LegacyClawMatePackID {
		config.PetSkin = petpack.DefaultPackID
	}
	_ = petpack.MigratePetVariant(config)

	// 88 was the chibi default; 176 was the first standing-figure default.
	// Both follow the current size. A size the user picked otherwise stays.
	if config.PetSize == 0 || config.PetSize == legacyDefaultPetSize || config.PetSize == previousDefaultPetSize {
		config.PetSize = defaultPetSize
	} else if config.PetSize < minPetSize {
		config.PetSize = minPetSize
	} else if config.PetSize > maxPetSize {
		config.PetSize = maxPetSize
	}

	switch config.PetInteractionMode {
	case "quiet", "balanced", "active":
	default:
		config.PetInteractionMode = "balanced"
	}
	config.PetMotionSoundPreset = petMotionSoundPreset(*config)
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
		oldConfig.PetSize != newConfig.PetSize
}

// floatingVoiceCompanionChanged is true when the mic should open or close
// without rebuilding the pet window. Quiet mode only changes readback.
func floatingVoiceCompanionChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.PetVoiceInput != newConfig.PetVoiceInput ||
		strings.TrimSpace(oldConfig.PetConversationMode) != strings.TrimSpace(newConfig.PetConversationMode)
}

// floatingMotionChanged identifies settings that can be applied to an existing
// native pet window. They affect how the pet moves, not its size or assets.
func floatingMotionChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return isPetMotionEnabled(oldConfig) != isPetMotionEnabled(newConfig) ||
		oldConfig.PetQuietMode != newConfig.PetQuietMode ||
		oldConfig.PetInteractionMode != newConfig.PetInteractionMode ||
		oldConfig.PetReducedMotion != newConfig.PetReducedMotion
}

// floatingSoundChanged returns true when only sound-related settings changed
// (no visual rebuild needed, just update the in-memory flag).
func floatingSoundChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return petMotionSoundEnabled(oldConfig) != petMotionSoundEnabled(newConfig) ||
		petMotionSoundPreset(oldConfig) != petMotionSoundPreset(newConfig)
}

// floatingPositionLockChanged reports a pin toggle. It stops screen travel
// only, so the window stays up and the pose keeps playing.
func floatingPositionLockChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.PetPositionLocked != newConfig.PetPositionLocked
}

// UpdateSoundConfig updates the floating window's in-memory sound settings
// without destroying and recreating the window.
func (m *FloatingAssistantManager) UpdateSoundConfig(config corelib.AppConfig) {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win != nil {
		win.UpdateSoundConfig(petMotionSoundEnabled(config), petMotionSoundPreset(config))
	}
}

// UpdatePositionLock pushes the pin onto the live window. The manager lock
// is not held across the window call.
func (m *FloatingAssistantManager) UpdatePositionLock(config corelib.AppConfig) {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win != nil {
		win.UpdatePositionLock(config.PetPositionLocked)
	}
}

// UpdateMotionConfig pushes quiet/reduced-motion/interaction flags without full recreate when possible.
func (m *FloatingAssistantManager) UpdateMotionConfig(config corelib.AppConfig) {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win != nil {
		win.UpdateMotionConfig(
			isPetMotionEnabled(config),
			config.PetQuietMode,
			config.PetReducedMotion,
			config.PetInteractionMode,
			config.PetSkin,
			petpack.ResolveVariantForRuntime(config.PetVariant),
		)
	}
}

// InvalidatePetPackAssets refreshes a live desktop pet after pack management
// changes. This is separate from motion config because a market upgrade may
// keep the same skin and variant selected.
func (m *FloatingAssistantManager) InvalidatePetPackAssets() {
	forgetPetClickClipSets()
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win != nil {
		win.InvalidatePetPackAssets()
	}
}

// SetPetRuntimeState bridges companion and microphone poses to the native window.
// Listening and speaking outrank task duty. Idle restores the duty pose
// without playing the reminder again.
func (m *FloatingAssistantManager) SetPetRuntimeState(state string, ttlMs int) {
	if m == nil {
		return
	}
	st := petpack.NormalizeState(state)
	m.mu.Lock()
	switch st {
	case petpack.StateListening, petpack.StateSpeaking:
		m.companionPose = true
	case petpack.StateIdle:
		m.companionPose = false
	}
	win := m.window
	hold := m.companionPose
	last := m.lastDuty
	m.mu.Unlock()
	if win == nil {
		return
	}
	if st == petpack.StateIdle && last.Pose != "" {
		m.pushDutyWindow(win, dutyDecisionStillLive(last, time.Now()))
		return
	}
	if hold && st != petpack.StateListening && st != petpack.StateSpeaking {
		return
	}
	win.SetPetRuntimeState(string(st), ttlMs)
}

func (m *FloatingAssistantManager) applyDuty(decision petDutyDecision) {
	if m == nil || (decision.Unchanged && !decision.LineChanged) {
		return
	}
	m.mu.Lock()
	if decision.Unchanged {
		m.lastDuty.Line = decision.Line
	} else {
		decision.since = time.Now()
		m.lastDuty = decision
	}
	hold := m.companionPose
	win := m.window
	line := m.lastDuty.Line
	full := !decision.Unchanged
	m.mu.Unlock()
	if win == nil {
		return
	}
	// A sentence-only note, and any note while voice owns the pose, updates
	// the hover line without replaying the reminder.
	if !full || hold {
		setPetPresenceLine(win, line)
		return
	}
	m.pushDutyWindow(win, decision)
}

func (m *FloatingAssistantManager) pushDutyWindow(win floatingWindow, decision petDutyDecision) {
	if applier, ok := win.(interface {
		ApplyPetDuty(state string, ttlMs int, event, cue string)
	}); ok {
		applier.ApplyPetDuty(decision.Pose, decision.TTLMS, decision.Event, decision.Cue)
	} else {
		win.SetPetRuntimeState(decision.Pose, decision.TTLMS)
	}
	setPetPresenceLine(win, decision.Line)
}

func setPetPresenceLine(win floatingWindow, line string) {
	if win == nil {
		return
	}
	setter, ok := win.(interface{ SetPetPresenceLine(string) })
	if !ok {
		return
	}
	setter.SetPetPresenceLine(line)
}

func (m *FloatingAssistantManager) applyRearing(cmd rearingWindow) {
	if m == nil {
		return
	}
	m.mu.Lock()
	win := m.window
	visible := m.visible
	m.mu.Unlock()
	if !visible || win == nil {
		return
	}
	applier, ok := win.(interface {
		ApplyPetRearing(variant string, windowSize int, cue string, adornments []string)
	})
	if !ok {
		return
	}
	applier.ApplyPetRearing(cmd.variant, cmd.windowSize, cmd.cue, cmd.adornments)
}

func petSkinID(config corelib.AppConfig) string {
	pack := strings.TrimSpace(config.PetSkin)
	if pack == "" {
		return petpack.DefaultPackID
	}
	return pack
}

func petUserSize(config corelib.AppConfig) int {
	if config.PetSize > 0 {
		return config.PetSize
	}
	return defaultPetSize
}

func (m *FloatingAssistantManager) syncPetRearing() {
	if m == nil || m.app == nil {
		return
	}
	cfg, err := m.app.LoadConfig()
	userSize := defaultPetSize
	pack := ""
	enabled := false
	if err == nil {
		if cfg.PetSize > 0 {
			userSize = cfg.PetSize
		}
		enabled = cfg.PetEnabled
		pack = strings.TrimSpace(cfg.PetSkin)
		if pack == "" {
			pack = petpack.DefaultPackID
		}
	}
	// Read visibility under the same lock Hide and Quit take, and commit the
	// gate before releasing it. A snapshot taken before LoadConfig was already
	// stale: allow() then reopened a click session whose window was gone.
	m.mu.Lock()
	cmd, live, _ := m.app.stagePetRearingAt(pack, m.visible && enabled, userSize, time.Now())
	m.mu.Unlock()
	if !live {
		return
	}
	m.app.deliverPetRearing(cmd)
	m.app.hydratePetDutyFromRuntimeIfNeeded()
}

// CurrentPetRuntimeState returns the native window's selected runtime state (for tests).
func (m *FloatingAssistantManager) CurrentPetRuntimeState() string {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win == nil {
		return string(petpack.StateIdle)
	}
	return win.CurrentPetRuntimeState()
}

// PetPackRuntimeLevel delegates the effective renderer-level report to the
// live platform window. Without a window the declared level is assumed.
func (m *FloatingAssistantManager) PetPackRuntimeLevel(declared string) (string, string) {
	m.mu.Lock()
	win := m.window
	m.mu.Unlock()
	if win == nil {
		return declared, ""
	}
	return win.PetPackRuntimeLevel(declared)
}

func floatingWindowSizeForCurrentConfig(app *App) int {
	if app == nil {
		return defaultPetSize + 16
	}
	config, err := app.LoadConfig()
	if err != nil {
		return defaultPetSize + 16
	}
	return rearedWindowSize(app, config.PetSkin, config.PetSize)
}

// DisablePetFromMenu hides the desktop pet and persists the same setting as
// Settings > Pet > Enable Desktop Pet.
func (m *FloatingAssistantManager) DisablePetFromMenu() {
	log.Println("[floating-assistant] DisablePetFromMenu: user requested hide from desktop pet")
	if m == nil {
		return
	}
	if m.app != nil {
		_, _ = m.app.PatchConfigFields(map[string]interface{}{"pet_enabled": false})
	}
	m.HideFloatingButton()
}

// QuitApp terminates the application. Called from the desktop pet's
// right-click context menu "Quit" item.
func (m *FloatingAssistantManager) QuitApp() {
	log.Println("[floating-assistant] QuitApp: user requested exit from desktop pet")

	// Destroy the floating window first so the WebView is cleaned up
	// before the Wails shutdown sequence begins.
	m.mu.Lock()
	// Seal before Destroy. Retire alone is reopened by allow(), and a rearing
	// sync can still be outside this lock. Quit is process exit: the gate
	// must stay shut until the process is gone.
	if m.app != nil {
		m.app.sealDesktopPetClick()
	}
	saveX, saveY, _, save := m.adoptLivePetOriginLocked(floatingWindowSizeForCurrentConfig(m.app))
	if m.window != nil {
		m.window.Destroy()
	}
	m.visible = false
	m.mu.Unlock()
	// The process is about to exit. A background persist can be killed before
	// the patrol position reaches disk.
	if save {
		m.persistPosition(saveX, saveY)
	}
	m.syncCompanion()

	// Quit on a separate goroutine - runtime.Quit triggers the Wails
	// shutdown sequence which must not run on the WebView's JS callback
	// goroutine (same pattern as the system tray quit handler).
	// requestAppQuit also arms the hard-exit watchdog, so a wedged shutdown
	// cannot leave the process alive in the background.
	if m.app != nil {
		requestAppQuit(m.app.ctx)
		return
	}
	hardExitProcess(0)
}

// Screen dimension helpers
// Platform-specific implementations can override these via build-tag files
// (e.g. floating_assistant_windows.go, floating_assistant_darwin.go).
// Defaults to 1920x1080 when no platform hook is set.

var platformGetScreenWidth func() int
var platformGetScreenHeight func() int

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
