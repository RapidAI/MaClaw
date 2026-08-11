//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/gui/petpack"
	xdraw "golang.org/x/image/draw"
)

// floatingLogoPNG is the PNG logo used for the fallback desktop entry.
// Embedded separately because the main `icon` variable on Windows is .ico format.
//
//go:embed build/appicon.png
var floatingLogoPNG []byte

// Win32 constants

const (
	wsExTopmost    = 0x00000008
	wsExLayered    = 0x00080000
	wsExToolwindow = 0x00000080
	wsPopup        = 0x80000000
	wsVisible      = 0x10000000

	swShow = 5
	swHide = 0

	csHredraw = 0x0002
	csVredraw = 0x0001

	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmLbuttondown = 0x0201
	wmLbuttonup   = 0x0202
	wmMousemove   = 0x0200
	wmRbuttonup   = 0x0205
	wmNchittest   = 0x0084
	wmTimer       = 0x0113

	smCxscreenFloat = 0
	smCyscreenFloat = 1

	idcArrow = 32512

	ulwAlpha   = 0x00000002
	acSrcOver  = 0x00
	acSrcAlpha = 0x01

	biRgb = 0

	mfString     = 0x00000000
	mfSeparator  = 0x00000800
	mfChecked    = 0x00000008
	tpmReturncmd = 0x0100

	menuIdSoundOff = 1000
	menuIdSettings = 1001
	menuIdHide     = 1002
	menuIdQuit     = 1003

	// Timer ID for halo animation
	timerIdHalo = 1
)

// Win32 API bindings

var (
	floatUser32   = syscall.NewLazyDLL("user32.dll")
	floatKernel32 = syscall.NewLazyDLL("kernel32.dll")
	floatGdi32    = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW      = floatUser32.NewProc("RegisterClassExW")
	procCreateWindowExW       = floatUser32.NewProc("CreateWindowExW")
	procShowWindowFloat       = floatUser32.NewProc("ShowWindow")
	procDestroyWindowProc     = floatUser32.NewProc("DestroyWindow")
	procMoveWindowProc        = floatUser32.NewProc("MoveWindow")
	procDefWindowProcW        = floatUser32.NewProc("DefWindowProcW")
	procLoadCursorW           = floatUser32.NewProc("LoadCursorW")
	procGetSystemMetricsF     = floatUser32.NewProc("GetSystemMetrics")
	procGetModuleHandleW      = floatKernel32.NewProc("GetModuleHandleW")
	procUpdateLayeredWindow   = floatUser32.NewProc("UpdateLayeredWindow")
	procSetCapture            = floatUser32.NewProc("SetCapture")
	procReleaseCapture        = floatUser32.NewProc("ReleaseCapture")
	procGetCursorPos          = floatUser32.NewProc("GetCursorPos")
	procCreatePopupMenu       = floatUser32.NewProc("CreatePopupMenu")
	procAppendMenuW           = floatUser32.NewProc("AppendMenuW")
	procTrackPopupMenu        = floatUser32.NewProc("TrackPopupMenu")
	procDestroyMenu           = floatUser32.NewProc("DestroyMenu")
	procSetForegroundWindow   = floatUser32.NewProc("SetForegroundWindow")
	procGetMessageW           = floatUser32.NewProc("GetMessageW")
	procTranslateMessage      = floatUser32.NewProc("TranslateMessage")
	procDispatchMessageW      = floatUser32.NewProc("DispatchMessageW")
	procPostQuitMessage       = floatUser32.NewProc("PostQuitMessage")
	procSetTimer              = floatUser32.NewProc("SetTimer")
	procKillTimer             = floatUser32.NewProc("KillTimer")
	procPostMessageW          = floatUser32.NewProc("PostMessageW")
	procSystemParametersInfoW = floatUser32.NewProc("SystemParametersInfoW")

	procBeep             = floatKernel32.NewProc("Beep")
	procCreateDIBSection = floatGdi32.NewProc("CreateDIBSection")
)

// Win32 structures

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type point struct{ X, Y int32 }
type sizeW struct{ CX, CY int32 }

type blendFunction struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

type bitmapInfo struct{ BmiHeader bitmapInfoHeader }

// RECT for SystemParametersInfo work area query.
type rectW struct{ Left, Top, Right, Bottom int32 }

type msgW struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type petFacePose struct {
	HeadShiftX float64
	HeadShiftY float64
	HeadTilt   float64
	EyeShiftX  float64
	EyeShiftY  float64
	EyeOpen    float64
	MouthOpen  float64
	ArmWave    float64
	CheekAlpha float64
}

type petFrameCacheKey struct {
	Size    int
	Skin    string
	Variant string
	State   string
	Mode    string
	Bucket  int
}

// 24 distinct poses preserve a smooth organic loop at the 20fps timer cadence
// without retaining hundreds of full-size fallback bitmaps per pet state.
const petAnimationFrameBuckets = 24

// The cache holds at most one short procedural cycle for each runtime state
// plus native frames. Clearing as a whole creates recurring cold-start stutter,
// so we evict one matching procedural cycle instead.
const petFrameCacheLimit = petAnimationFrameBuckets * 12

// windowsFloatingWindow

type windowsFloatingWindow struct {
	app        *App
	hwnd       uintptr
	created    bool
	destroying bool
	mu         sync.Mutex
	size       int

	// Drag state (accessed only from the message loop goroutine)
	dragging     bool
	dragStartX   int32
	dragStartY   int32
	windowStartX int
	windowStartY int
	dragMoved    bool
	lastHoverAt  time.Time

	// Halo animation
	haloPhase        float64 // 0..2*pi, advances each timer tick
	petEnabled       bool
	petMotionEnabled bool
	// petMotionSoundRequested preserves the user's setting while quiet mode or
	// reduced motion temporarily suppresses audible feedback.
	petMotionSoundRequested bool
	petMotionSound          bool
	lastPetSoundBucket      int
	petQuietMode            bool
	petInteractionMode      string
	petSkin                 string
	petVariant              string
	petSoundPreset          string
	petReducedMotion        bool
	petRuntimeState         string
	petPreviousState        string
	petStateChangedAt       time.Time
	petAnimationStartedAt   time.Time
	petStateDeadline        time.Time
	packFrameCache          *petpack.FrameCache
	// rigRenderers hold immutable decoded skeleton assets. A failed renderer is
	// remembered per selection so a malformed third-party pack cannot trigger
	// disk/JSON work on every 20fps tick; native frames remain the fallback.
	rigRenderers            map[string]*petpack.RigRenderer
	rigRendererFailed       map[string]bool
	characterRenderers      map[string]*petpack.CharacterRenderer
	characterRendererFailed map[string]bool
	packPitch               float64
	petMotionAmplitude      float64
	petLastRenderedState    string
	// petLastInteractionAt records the last user interaction (click/drag/hover)
	// or runtime state change. petLongIdleFired latches the long_idle character
	// event so it fires once per idle period and rearms on new activity.
	petLastInteractionAt time.Time
	petLongIdleFired     bool
	// Increments for every motion update so an older, slower registry lookup
	// cannot overwrite a newer setting snapshot.
	motionConfigRevision uint64

	// Pre-rendered base image (logo + circle clip, without halo)
	baseImg *image.NRGBA

	// Quantized pet animation frames avoid rerasterizing the supersampled pet every timer tick.
	petFrameCache map[petFrameCacheKey]*image.NRGBA
	// Remembers whether a state can be served by a native pack image. This lets
	// procedural fallback use its animated frame buckets without repeatedly
	// probing the pack filesystem on every timer tick.
	petNativeFrameAvailable map[petFrameCacheKey]bool
	// Keys currently being resolved from a pack. The window has one render
	// thread today, but keeping this explicit makes state updates safe if a
	// future platform renderer asks for the same frame concurrently.
	petNativeFrameLoading map[petFrameCacheKey]bool

	// Set by state/config changes so a static or reduced-motion pet still redraws
	// once, while its idle timer no longer repaints the same pixels at 20fps.
	renderDirty bool

	// Pre-computed distance from center for each pixel (avoids sqrt per frame)
	distMap []float64

	// Stop signal for the message loop
	stopCh chan struct{}
}

func newFloatingWindow(app *App) floatingWindow {
	return &windowsFloatingWindow{app: app}
}

// globalFloatingWin is the callback target for the Win32 class procedure.
// Window creation/destruction happens on worker threads while the procedure
// runs on its owning OS thread, so this pointer needs its own synchronization;
// an individual window's mutex cannot protect a global pointer read before the
// procedure knows which window to lock.
var globalFloatingWin struct {
	sync.RWMutex
	window *windowsFloatingWindow
}

func setGlobalFloatingWindow(w *windowsFloatingWindow) {
	globalFloatingWin.Lock()
	globalFloatingWin.window = w
	globalFloatingWin.Unlock()
}

func clearGlobalFloatingWindow(w *windowsFloatingWindow) {
	globalFloatingWin.Lock()
	if globalFloatingWin.window == w {
		globalFloatingWin.window = nil
	}
	globalFloatingWin.Unlock()
}

func currentGlobalFloatingWindow() *windowsFloatingWindow {
	globalFloatingWin.RLock()
	w := globalFloatingWin.window
	globalFloatingWin.RUnlock()
	return w
}

func init() {
	// Use SPI_GETWORKAREA to get the usable desktop area (excludes taskbar,
	// and returns DPI-scaled logical pixels - not physical pixels).
	// This fixes the issue where GetSystemMetrics returns 3840 on a 4K display
	// with 150% scaling, but the actual usable area is only 2560 logical pixels.
	const spiGetworkarea = 0x0030
	platformGetScreenWidth = func() int {
		var rc rectW
		procSystemParametersInfoW.Call(spiGetworkarea, 0, uintptr(unsafe.Pointer(&rc)), 0)
		w := int(rc.Right - rc.Left)
		if w <= 0 {
			return 1920
		}
		return w
	}
	platformGetScreenHeight = func() int {
		var rc rectW
		procSystemParametersInfoW.Call(spiGetworkarea, 0, uintptr(unsafe.Pointer(&rc)), 0)
		h := int(rc.Bottom - rc.Top)
		if h <= 0 {
			return 1080
		}
		return h
	}
}

// floatingWindow interface

func (w *windowsFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	if w.created {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()

	sz := normalizeFloatingNativeSize(width)
	petEnabled := false
	petMotionEnabled := true
	petMotionSound := true
	petQuietMode := false
	petReducedMotion := false
	petInteractionMode := "balanced"
	petSkin := "clawmate"
	petVariant := petpack.VariantDefault
	petSoundPreset := "classic"
	packPitch := 1.0
	petMotionAmplitude := 0.85
	if w.app != nil {
		if cfg, err := w.app.LoadConfig(); err == nil {
			petEnabled = cfg.PetEnabled
			petQuietMode = cfg.PetQuietMode
			petReducedMotion = cfg.PetReducedMotion
			petMotionEnabled = isPetMotionEnabled(cfg)
			petMotionSound = petMotionSoundEnabled(cfg)
			petSoundPreset = petMotionSoundPreset(cfg)
			if cfg.PetInteractionMode != "" {
				petInteractionMode = cfg.PetInteractionMode
			}
			if cfg.PetSkin != "" {
				petSkin = cfg.PetSkin
			}
			petVariant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
			if reg := petpack.EnsureGlobal(); reg != nil {
				if resolved, err := reg.Resolve(petSkin, petVariant); err == nil && resolved != nil {
					motion := petpack.EffectiveMotionFrom(petpack.EffectiveMotionInput{
						Pack:            resolved.Motion,
						InteractionMode: petInteractionMode,
						QuietMode:       petQuietMode,
						ReducedMotion:   petReducedMotion,
						MotionEnabled:   petMotionEnabled,
						SoundEnabled:    petMotionSound,
					})
					petMotionAmplitude = motion.Amplitude
					packPitch = resolved.Motion.Pitch
					if packPitch <= 0 {
						packPitch = 1
					}
					eff := petpack.EffectiveSoundProfileFrom(petSoundPreset, resolved.Motion.SoundProfile, packPitch)
					petSoundPreset = eff.Preset
					packPitch = eff.Pitch
				}
			}
		}
	}

	base, err := renderFloatingBase(sz, petEnabled, petSkin, petVariant, string(petpack.StateIdle))
	if err != nil {
		return fmt.Errorf("renderFloatingBase: %w", err)
	}

	w.mu.Lock()
	w.baseImg = base
	w.size = sz
	w.haloPhase = 0
	w.petEnabled = petEnabled
	w.petMotionEnabled = petMotionEnabled && !petReducedMotion
	w.petMotionSoundRequested = petMotionSound
	w.petMotionSound = petMotionSound && !petReducedMotion && !petQuietMode
	w.lastPetSoundBucket = 0
	w.petQuietMode = petQuietMode
	w.petReducedMotion = petReducedMotion
	w.petInteractionMode = petInteractionMode
	w.petSkin = petSkin
	w.petVariant = petVariant
	w.petMotionAmplitude = petMotionAmplitude
	w.petSoundPreset = petSoundPreset
	w.packPitch = packPitch
	w.petRuntimeState = string(petpack.StateIdle)
	w.petPreviousState = string(petpack.StateIdle)
	w.petLastRenderedState = string(petpack.StateIdle)
	w.petStateChangedAt = time.Now()
	w.petAnimationStartedAt = w.petStateChangedAt
	w.petStateDeadline = time.Time{}
	w.petLastInteractionAt = w.petStateChangedAt
	w.petLongIdleFired = false
	w.packFrameCache = petpack.NewFrameCache()
	w.rigRenderers = make(map[string]*petpack.RigRenderer)
	w.rigRendererFailed = make(map[string]bool)
	w.characterRenderers = make(map[string]*petpack.CharacterRenderer)
	w.characterRendererFailed = make(map[string]bool)
	w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
	w.petNativeFrameAvailable = make(map[petFrameCacheKey]bool)
	w.petNativeFrameLoading = make(map[petFrameCacheKey]bool)
	w.renderDirty = true
	w.stopCh = make(chan struct{})

	w.distMap = make([]float64, sz*sz)
	cx, cy := float64(sz)/2, float64(sz)/2
	for py := 0; py < sz; py++ {
		for px := 0; px < sz; px++ {
			dx := float64(px) - cx + 0.5
			dy := float64(py) - cy + 0.5
			w.distMap[py*sz+px] = math.Sqrt(dx*dx + dy*dy)
		}
	}
	setGlobalFloatingWindow(w)
	w.mu.Unlock()

	// Create window and start message loop on a dedicated OS thread.
	errCh := make(chan error, 1)

	go func() {
		runtime.LockOSThread()

		hwnd, err2 := createFloatingWin32Window(x, y, sz, sz)
		if err2 != nil {
			w.mu.Lock()
			w.hwnd = 0
			w.created = false
			w.destroying = false
			clearGlobalFloatingWindow(w)
			if w.stopCh != nil {
				close(w.stopCh)
				w.stopCh = nil
			}
			w.mu.Unlock()
			errCh <- err2
			return
		}

		w.mu.Lock()
		w.hwnd = hwnd
		w.destroying = false
		w.windowStartX = x
		w.windowStartY = y
		w.created = true
		w.mu.Unlock()

		// Render initial frame.
		w.renderFrame()

		// The skeleton compositor draws five textured layers in software. Twenty
		// FPS is visually continuous at this compact desktop size while avoiding
		// needless CPU/GDI work from a 60 FPS timer.
		procSetTimer.Call(hwnd, timerIdHalo, 50, 0)

		errCh <- nil

		// Message loop runs until WM_DESTROY posts WM_QUIT.
		w.messageLoop()
	}()

	return <-errCh
}
func (w *windowsFloatingWindow) Show() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return
	}
	procShowWindowFloat.Call(hwnd, uintptr(swShow))
}

func (w *windowsFloatingWindow) Hide() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return
	}
	procShowWindowFloat.Call(hwnd, uintptr(swHide))
}

func (w *windowsFloatingWindow) Destroy() {
	w.mu.Lock()
	hwnd := w.hwnd
	created := w.created
	stopCh := w.stopCh
	alreadyDestroying := w.destroying
	if created && hwnd != 0 && !w.destroying {
		w.destroying = true
	}
	w.mu.Unlock()

	if !created || hwnd == 0 {
		return
	}

	if !alreadyDestroying {
		// Ask the owning window thread to close; it will call DestroyWindow and
		// then WM_DESTROY will terminate the message loop. Posting WM_DESTROY
		// directly can leave the HWND visible long enough for a new pet window
		// to overlap it.
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}

	// Block briefly until the message loop goroutine exits. Avoid hanging the
	// caller forever if the HWND was already gone or the close message was lost.
	if stopCh != nil {
		select {
		case <-stopCh:
			w.mu.Lock()
			w.hwnd = 0
			w.created = false
			w.destroying = false
			clearGlobalFloatingWindow(w)
			w.mu.Unlock()
		case <-time.After(2 * time.Second):
			log.Printf("[floating-assistant] Destroy timed out waiting for window thread")
			w.mu.Lock()
			if w.hwnd == hwnd {
				w.destroying = false
			}
			w.mu.Unlock()
		}
	}
}

func (w *windowsFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd == 0 {
		return
	}
	sz := w.currentSize()
	procMoveWindowProc.Call(hwnd, uintptr(x), uintptr(y), uintptr(sz), uintptr(sz), 1)
	w.mu.Lock()
	w.windowStartX = x
	w.windowStartY = y
	w.mu.Unlock()
}

func (w *windowsFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}

func (w *windowsFloatingWindow) UpdateSoundConfig(soundEnabled bool, preset string) {
	w.mu.Lock()
	previous := w.petMotionSound
	effectiveSoundEnabled := soundEnabled && !w.petReducedMotion && !w.petQuietMode
	w.petMotionSoundRequested = soundEnabled
	w.petMotionSound = effectiveSoundEnabled
	w.petSoundPreset = preset
	w.renderDirty = true
	w.motionConfigRevision++
	revision := w.motionConfigRevision
	motionEnabled := w.petMotionEnabled
	quiet := w.petQuietMode
	reducedMotion := w.petReducedMotion
	interactionMode := w.petInteractionMode
	skin := w.petSkin
	w.mu.Unlock()
	if previous != effectiveSoundEnabled {
		// Sound can affect a pack's effective motion policy, but it never changes
		// its art. Recompute only amplitude and keep decoded/pre-rendered frames.
		amplitude := resolvePetMotionAmplitude(skin, interactionMode, quiet, reducedMotion, motionEnabled, effectiveSoundEnabled)
		w.applyPetMotionAmplitude(revision, amplitude)
	}
}

func (w *windowsFloatingWindow) UpdateMotionConfig(motionEnabled, quiet, reducedMotion bool, interactionMode, skin, variant string) {
	w.mu.Lock()
	w.motionConfigRevision++
	revision := w.motionConfigRevision
	w.petMotionEnabled = motionEnabled && !reducedMotion
	w.petQuietMode = quiet
	w.petReducedMotion = reducedMotion
	if interactionMode != "" {
		w.petInteractionMode = interactionMode
	}
	if skin != "" {
		if skin != w.petSkin || (variant != "" && variant != w.petVariant) {
			w.rigRenderers = make(map[string]*petpack.RigRenderer)
			w.rigRendererFailed = make(map[string]bool)
			w.characterRenderers = make(map[string]*petpack.CharacterRenderer)
			w.characterRendererFailed = make(map[string]bool)
		}
		w.petSkin = skin
	}
	if variant != "" {
		w.petVariant = variant
	}
	w.petMotionSound = w.petMotionSoundRequested && !reducedMotion && !quiet
	w.renderDirty = true
	skinForMotion := w.petSkin
	interactionForMotion := w.petInteractionMode
	quietForMotion := w.petQuietMode
	reducedForMotion := w.petReducedMotion
	motionEnabledForMotion := w.petMotionEnabled
	soundEnabledForMotion := w.petMotionSound
	w.mu.Unlock()

	// Registry resolution can touch the filesystem. Keep it outside the window
	// lock so the 20fps render loop and state updates are never blocked on I/O.
	amplitude := resolvePetMotionAmplitude(skinForMotion, interactionForMotion, quietForMotion, reducedForMotion, motionEnabledForMotion, soundEnabledForMotion)

	if !w.applyPetMotionAmplitude(revision, amplitude) {
		return
	}
}

func (w *windowsFloatingWindow) InvalidatePetPackAssets() {
	w.mu.Lock()
	defer w.mu.Unlock()
	// A pack can be replaced at the same id while it remains selected. Clear
	// both successful and failed skeleton entries, plus scaled static frames,
	// so the next render resolves the newly scanned registry contents.
	w.rigRenderers = make(map[string]*petpack.RigRenderer)
	w.rigRendererFailed = make(map[string]bool)
	w.characterRenderers = make(map[string]*petpack.CharacterRenderer)
	w.characterRendererFailed = make(map[string]bool)
	w.packFrameCache = petpack.NewFrameCache()
	w.petNativeFrameAvailable = make(map[petFrameCacheKey]bool)
	w.petNativeFrameLoading = make(map[petFrameCacheKey]bool)
	// Native frames are also cached as scaled bitmaps in petFrameCache under
	// the bucket-less base key. Leaving them behind would keep serving the old
	// pack's artwork whenever the animation phase lands on bucket zero.
	w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
	w.renderDirty = true
}

func (w *windowsFloatingWindow) applyPetMotionAmplitude(revision uint64, amplitude float64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if revision != w.motionConfigRevision {
		return false
	}
	w.petMotionAmplitude = amplitude
	return true
}

func resolvePetMotionAmplitude(skin, interactionMode string, quiet, reducedMotion, motionEnabled, soundEnabled bool) float64 {
	if reg := petpack.EnsureGlobal(); reg != nil {
		if resolved, err := reg.Resolve(skin, petpack.VariantDefault); err == nil && resolved != nil {
			return petpack.EffectiveMotionFrom(petpack.EffectiveMotionInput{
				Pack:            resolved.Motion,
				InteractionMode: interactionMode,
				QuietMode:       quiet,
				ReducedMotion:   reducedMotion,
				MotionEnabled:   motionEnabled,
				SoundEnabled:    soundEnabled,
			}).Amplitude
		}
	}
	if reducedMotion || !motionEnabled {
		return 0
	}
	return 0.85
}

func (w *windowsFloatingWindow) SetPetRuntimeState(state string, ttlMs int) {
	st := string(petpack.NormalizeState(state))
	w.mu.Lock()
	previous := w.petRuntimeState
	w.setPetRuntimeStateLocked(st, ttlMs)
	// A state push counts as activity: it rearms the long-idle event latch.
	w.notePetInteractionLocked()
	// Keep cached state frames warm; the renderer crossfades only briefly when
	// the state changes, then reuses the current frame.
	w.mu.Unlock()
	if previous != st {
		switch st {
		case string(petpack.StateThinking):
			w.triggerCharacterEvent("task_started")
		case string(petpack.StateDone):
			w.triggerCharacterEvent("task_done")
		case string(petpack.StateAlert):
			w.triggerCharacterEvent("task_failed")
		}
	}
}

// petLongIdleThreshold is the idle dwell after which the long_idle character
// event fires once, until the next interaction or state change rearms it.
const petLongIdleThreshold = 5 * time.Minute

// notePetInteraction records activity that resets the long-idle clock and
// rearms the long_idle event latch.
func (w *windowsFloatingWindow) notePetInteraction() {
	w.mu.Lock()
	w.notePetInteractionLocked()
	w.mu.Unlock()
}

func (w *windowsFloatingWindow) notePetInteractionLocked() {
	w.petLastInteractionAt = time.Now()
	w.petLongIdleFired = false
}

// triggerCharacterEvent forwards only a fixed, local interaction vocabulary
// to a loaded v3 renderer. It neither creates new host inputs nor exposes
// screen, audio, network, or scripting capabilities to a pack.
func (w *windowsFloatingWindow) triggerCharacterEvent(event string) {
	if w == nil || !petpack.IsAllowedPerformerEvent(event) {
		return
	}
	w.mu.Lock()
	skin := w.petSkin
	variant := petpack.ResolveVariantForRuntime(w.petVariant)
	state := w.petRuntimeState
	if skin == "" {
		skin = petpack.DefaultPackID
	}
	if state == "" {
		state = string(petpack.StateIdle)
	}
	renderer := w.characterRenderers[skin+"\x00"+variant]
	w.mu.Unlock()
	if renderer == nil {
		// State changes can arrive before the first paint creates the v3
		// renderer. Initialize it against the current semantic state so the
		// accompanying event (for example task_started) is not silently lost.
		_ = w.renderCharacterPackFrame(w.currentSize(), skin, variant, state)
		w.mu.Lock()
		renderer = w.characterRenderers[skin+"\x00"+variant]
		w.mu.Unlock()
	}
	if renderer != nil && renderer.TriggerEvent(event, time.Now().UnixMilli()) {
		w.mu.Lock()
		w.renderDirty = true
		w.mu.Unlock()
	}
}

func (w *windowsFloatingWindow) setPetRuntimeStateLocked(state string, ttlMs int) {
	if state != w.petRuntimeState {
		w.petPreviousState = w.petRuntimeState
		if w.petPreviousState == "" {
			w.petPreviousState = string(petpack.StateIdle)
		}
		w.petStateChangedAt = time.Now()
	}
	w.petRuntimeState = state
	// Repeating the active state should renew its TTL without restarting its
	// entrance transition or forcing a redundant static redraw.
	if state != w.petLastRenderedState {
		w.renderDirty = true
	}
	if ttlMs > 0 {
		w.petStateDeadline = time.Now().Add(time.Duration(ttlMs) * time.Millisecond)
	} else {
		w.petStateDeadline = time.Time{}
	}
}

func (w *windowsFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.setPetRuntimeStateLocked(string(petpack.StateIdle), 0)
	}
	if w.petRuntimeState == "" {
		return string(petpack.StateIdle)
	}
	return w.petRuntimeState
}

// PetPackRuntimeLevel reports the renderer level this window is actually
// using for the selected pack. Quiet/reduced-motion/disabled motion force the
// static raster path; a renderer that failed to load degrades a character or
// skeleton pack to its static raster frames. A level not yet attempted by the
// render loop is assumed to work at its declared level — the first paint will
// degrade it if loading fails.
func (w *windowsFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	w.mu.Lock()
	quiet := w.petQuietMode
	reduced := w.petReducedMotion
	motionEnabled := w.petMotionEnabled
	key := w.petSkin + "\x00" + petpack.ResolveVariantForRuntime(w.petVariant)
	characterFailed := w.characterRendererFailed[key]
	rigFailed := w.rigRendererFailed[key]
	w.mu.Unlock()

	staticReason := ""
	switch {
	case quiet:
		staticReason = "安静模式已启用，动画已暂停"
	case reduced:
		staticReason = "系统“减少动态效果”已开启，动画已停用"
	case !motionEnabled:
		staticReason = "宠物动画已关闭"
	}
	if staticReason != "" {
		if declared == petpack.RendererNative {
			return declared, ""
		}
		return petpack.RendererNative, staticReason
	}
	switch declared {
	case petpack.RendererCharacter:
		if characterFailed {
			return petpack.RendererNative, "角色动画加载失败，已回退到静态图像"
		}
	case petpack.RendererSkeleton:
		if rigFailed {
			return petpack.RendererNative, "骨骼动画加载失败，已回退到静态图像"
		}
	}
	return declared, ""
}

// Message loop

func (w *windowsFloatingWindow) messageLoop() {
	defer func() {
		if w.stopCh != nil {
			close(w.stopCh)
		}
	}()

	var msg msgW
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 || int32(ret) == -1 {
			break // WM_QUIT or error
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Clean up: destroy the window on this thread.
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()
	if hwnd != 0 {
		procKillTimer.Call(hwnd, timerIdHalo)
		procDestroyWindowProc.Call(hwnd)
	}
}

// Circular logo rendering with glow

func normalizeFloatingNativeSize(sz int) int {
	if sz <= 0 {
		return 72
	}
	if sz < 72 {
		return 72
	}
	if sz > 136 {
		return 136
	}
	return sz
}

func (w *windowsFloatingWindow) currentSize() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size <= 0 {
		return 72
	}
	return w.size
}

func renderFloatingBase(sz int, petEnabled bool, petSkin, petVariant, state string) (*image.NRGBA, error) {
	if petEnabled {
		if frame := tryLoadPackFrame(petSkin, petVariant, state, sz, nil); frame != nil {
			return frame, nil
		}
		return renderClawMatePet(sz, petSkin), nil
	}
	return renderCircularLogo(sz)
}

// tryLoadPackFrame returns a scaled frame from the selected pack. The legacy
// variant argument remains for call compatibility, but no longer selects a
// different rendering path.
func tryLoadPackFrame(skin, variant, state string, size int, cache *petpack.FrameCache) *image.NRGBA {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil
	}
	st := petpack.NormalizeState(state)
	frame, _, err := reg.ResolveAndLoad(skin, petpack.VariantDefault, st, size, cache)
	if err != nil || frame == nil {
		return nil
	}
	return frame
}

// renderCircularLogo creates a circular-clipped logo with a soft glow border.
// The output image is `sz x sz` with transparent background.
func renderCircularLogo(sz int) (*image.NRGBA, error) {
	img, err := png.Decode(bytes.NewReader(floatingLogoPNG))
	if err != nil {
		return nil, fmt.Errorf("png.Decode: %w", err)
	}

	out := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	cx, cy := float64(sz)/2, float64(sz)/2
	logoRadius := float64(sz)/2 - 8 // leave margin for glow
	glowOuter := float64(sz) / 2

	// Scale logo to fit inside the circle (75% of circle diameter for padding).
	logoSize := int(logoRadius * 2 * 0.75)
	scaled := image.NewNRGBA(image.Rect(0, 0, logoSize, logoSize))
	xdraw.BiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	// Draw glow ring + circular-clipped logo.
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			dist := math.Sqrt(dx*dx + dy*dy)

			if dist <= logoRadius {
				// Inside logo circle - dark background + logo overlay.
				sx := x - (sz-logoSize)/2
				sy := y - (sz-logoSize)/2
				// Start with dark circle background.
				bg := color.NRGBA{R: 230, G: 235, B: 245, A: 245}
				if sx >= 0 && sx < logoSize && sy >= 0 && sy < logoSize {
					src := scaled.NRGBAAt(sx, sy)
					if src.A > 10 {
						// Composite logo over dark background.
						out.SetNRGBA(x, y, alphaOver(bg, src))
					} else {
						out.SetNRGBA(x, y, bg)
					}
				} else {
					out.SetNRGBA(x, y, bg)
				}
			} else if dist <= glowOuter {
				// Glow ring - soft blue-purple gradient fading outward.
				t := (dist - logoRadius) / (glowOuter - logoRadius) // 0 at inner, 1 at outer
				alpha := uint8(float64(120) * (1 - t*t))            // quadratic falloff
				out.SetNRGBA(x, y, color.NRGBA{R: 100, G: 140, B: 255, A: alpha})
			}
			// Outside glowOuter: transparent (default zero)
		}
	}

	return out, nil
}

func renderClawMatePet(sz int, skin string) *image.NRGBA {
	return renderClawMatePetWithPose(sz, skin, petFacePose{})
}

func renderClawMatePetWithPose(sz int, skin string, pose petFacePose) *image.NRGBA {
	renderScale := 3
	if sz < 96 {
		renderScale = 4
	}
	scaledPose := petFacePose{
		HeadShiftX: pose.HeadShiftX * float64(renderScale),
		HeadShiftY: pose.HeadShiftY * float64(renderScale),
		HeadTilt:   pose.HeadTilt,
		EyeShiftX:  pose.EyeShiftX * float64(renderScale),
		EyeShiftY:  pose.EyeShiftY * float64(renderScale),
		EyeOpen:    pose.EyeOpen,
		MouthOpen:  pose.MouthOpen,
		ArmWave:    pose.ArmWave,
		CheekAlpha: pose.CheekAlpha,
	}
	hiRes := renderClawMatePetRaster(sz*renderScale, skin, scaledPose)
	out := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	xdraw.CatmullRom.Scale(out, out.Bounds(), hiRes, hiRes.Bounds(), xdraw.Over, nil)
	return out
}

func renderClawMatePetRaster(sz int, skin string, pose petFacePose) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	bodyCx := float64(sz) / 2
	headCx := bodyCx + pose.HeadShiftX
	headCy := float64(sz)*0.47 + pose.HeadShiftY
	headR := float64(sz) * 0.31
	bodyY := float64(sz) * 0.67
	accent := color.NRGBA{R: 99, G: 102, B: 241, A: 245}
	body := color.NRGBA{R: 111, G: 125, B: 92, A: 230}
	bodyLine := color.NRGBA{R: 45, G: 55, B: 72, A: 235}
	eye := color.NRGBA{R: 45, G: 55, B: 72, A: 255}
	head := color.NRGBA{R: 248, G: 250, B: 252, A: 250}
	headLine := color.NRGBA{R: 216, G: 222, B: 232, A: 245}
	glow := color.NRGBA{R: 99, G: 102, B: 241, A: 0}

	switch skin {
	case "mini-claw":
		accent = color.NRGBA{R: 37, G: 99, B: 235, A: 245}
		body = color.NRGBA{R: 191, G: 219, B: 254, A: 235}
		headLine = color.NRGBA{R: 155, G: 189, B: 246, A: 245}
		bodyLine = color.NRGBA{R: 37, G: 99, B: 235, A: 235}
		glow = color.NRGBA{R: 37, G: 99, B: 235, A: 0}
		headR = float64(sz) * 0.34
		bodyY = float64(sz) * 0.68
	case "dev-claw":
		accent = color.NRGBA{R: 34, G: 211, B: 238, A: 245}
		body = color.NRGBA{R: 30, G: 41, B: 59, A: 240}
		bodyLine = color.NRGBA{R: 96, G: 165, B: 250, A: 245}
		eye = color.NRGBA{R: 15, G: 23, B: 42, A: 255}
		headLine = color.NRGBA{R: 96, G: 165, B: 250, A: 245}
		glow = color.NRGBA{R: 34, G: 211, B: 238, A: 0}
	case "focus-claw":
		accent = color.NRGBA{R: 95, G: 139, B: 104, A: 235}
		body = color.NRGBA{R: 155, G: 184, B: 161, A: 225}
		bodyLine = color.NRGBA{R: 51, G: 65, B: 85, A: 230}
		eye = color.NRGBA{R: 51, G: 65, B: 85, A: 255}
		head = color.NRGBA{R: 251, G: 253, B: 251, A: 248}
		headLine = color.NRGBA{R: 155, G: 184, B: 161, A: 240}
		glow = color.NRGBA{R: 95, G: 139, B: 104, A: 0}
	default:
		glow = color.NRGBA{R: 99, G: 102, B: 241, A: 0}
	}

	outline := math.Max(2, float64(sz)*0.012)
	tail := color.NRGBA{R: 226, G: 232, B: 240, A: 228}
	tailLine := color.NRGBA{R: 148, G: 163, B: 184, A: 220}
	if skin == "mini-claw" {
		tail = color.NRGBA{R: 219, G: 234, B: 254, A: 232}
		tailLine = color.NRGBA{R: 147, G: 197, B: 253, A: 225}
	} else if skin == "dev-claw" {
		tail = color.NRGBA{R: 30, G: 41, B: 59, A: 236}
		tailLine = color.NRGBA{R: 96, G: 165, B: 250, A: 230}
	} else if skin == "focus-claw" {
		tail = color.NRGBA{R: 226, G: 236, B: 228, A: 226}
		tailLine = color.NRGBA{R: 155, G: 184, B: 161, A: 220}
	}
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			tailDx := math.Abs(float64(x)-(bodyCx-float64(sz)*0.35)) / (float64(sz) * 0.18)
			tailDy := math.Abs(float64(y)-(bodyY+float64(sz)*0.07)) / (float64(sz) * 0.095)
			tailD := tailDx*tailDx + tailDy*tailDy
			if tailD <= 1.18 {
				out.SetNRGBA(x, y, tailLine)
			}
			if tailD <= 1 {
				out.SetNRGBA(x, y, tail)
			}

			bodyDx := math.Abs(float64(x)-bodyCx) / (float64(sz) * 0.32)
			bodyDy := math.Abs(float64(y)-bodyY) / (float64(sz) * 0.18)
			bodyD := bodyDx*bodyDx + bodyDy*bodyDy
			if bodyD <= 1.18 {
				out.SetNRGBA(x, y, bodyLine)
			}
			if bodyD <= 1 {
				out.SetNRGBA(x, y, body)
			}

			dx := float64(x) - headCx
			dy := float64(y) - headCy
			d := math.Sqrt(dx*dx + dy*dy)
			if d < float64(sz)*0.5 && d > headR+outline {
				alpha := uint8(math.Max(0, 92*(1-(d-headR)/(float64(sz)*0.2))))
				glow.A = alpha
				out.SetNRGBA(x, y, alphaOver(out.NRGBAAt(x, y), glow))
			}
			if d <= headR+outline {
				out.SetNRGBA(x, y, headLine)
			}
			if d <= headR {
				out.SetNRGBA(x, y, head)
			}
		}
	}

	eyeOpen := pose.EyeOpen
	if eyeOpen <= 0 {
		eyeOpen = 1
	}
	eyeOpen = math.Min(1, math.Max(0.08, eyeOpen))
	leftEyeX, leftEyeY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.12+pose.EyeShiftX, headCy-float64(sz)*0.04+pose.EyeShiftY, pose.HeadTilt)
	rightEyeX, rightEyeY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.12+pose.EyeShiftX, headCy-float64(sz)*0.04+pose.EyeShiftY, pose.HeadTilt)
	eyeRX := int(float64(sz) * 0.047)
	eyeRY := int(math.Max(1, float64(eyeRX)*eyeOpen))
	drawEllipse(out, int(leftEyeX), int(leftEyeY), eyeRX, eyeRY, eye)
	drawEllipse(out, int(rightEyeX), int(rightEyeY), eyeRX, eyeRY, eye)
	cheekAlpha := uint8(math.Min(120, math.Max(36, 52+pose.CheekAlpha*68)))
	cheek := color.NRGBA{R: accent.R, G: accent.G, B: accent.B, A: cheekAlpha}
	leftCheekX, leftCheekY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.205, headCy+float64(sz)*0.065, pose.HeadTilt)
	rightCheekX, rightCheekY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.205, headCy+float64(sz)*0.065, pose.HeadTilt)
	drawEllipse(out, int(leftCheekX), int(leftCheekY), int(float64(sz)*0.035), int(float64(sz)*0.018), cheek)
	drawEllipse(out, int(rightCheekX), int(rightCheekY), int(float64(sz)*0.035), int(float64(sz)*0.018), cheek)
	if eyeOpen > 0.35 {
		drawCircle(out, int(leftEyeX+float64(sz)*0.014), int(leftEyeY-float64(sz)*0.012), int(float64(sz)*0.013), color.NRGBA{R: 255, G: 255, B: 255, A: 245})
		drawCircle(out, int(rightEyeX+float64(sz)*0.014), int(rightEyeY-float64(sz)*0.012), int(float64(sz)*0.013), color.NRGBA{R: 255, G: 255, B: 255, A: 245})
	}
	mouthY := headCy + float64(sz)*(0.15+0.026*pose.MouthOpen)
	mouthHalf := float64(sz) * (0.075 + 0.018*pose.MouthOpen)
	mouthHeight := int(math.Max(1, float64(sz)*(0.012+0.042*pose.MouthOpen)))
	mouthLeftX, mouthLeftY := rotatePetPoint(headCx, headCy, headCx-mouthHalf, mouthY, pose.HeadTilt)
	mouthRightX, mouthRightY := rotatePetPoint(headCx, headCy, headCx+mouthHalf, mouthY, pose.HeadTilt)
	mouthCenterX, mouthCenterY := rotatePetPoint(headCx, headCy, headCx, mouthY, pose.HeadTilt)
	if pose.MouthOpen > 0.22 {
		drawEllipse(out, int(mouthCenterX), int(mouthCenterY), int(mouthHalf*0.82), mouthHeight, color.NRGBA{R: 45, G: 55, B: 72, A: 245})
	}
	drawLine(out, int(mouthLeftX), int(mouthLeftY), int(mouthRightX), int(mouthRightY), accent, int(math.Max(2, float64(sz)*0.028)))
	armWave := pose.ArmWave * float64(sz) * 0.045
	drawLine(out, int(bodyCx-float64(sz)*0.31), int(bodyY+float64(sz)*0.075-armWave), int(bodyCx-float64(sz)*0.13), int(bodyY+float64(sz)*0.13+armWave*0.35), bodyLine, int(math.Max(3, float64(sz)*0.047)))
	drawLine(out, int(bodyCx+float64(sz)*0.31), int(bodyY+float64(sz)*0.075+armWave), int(bodyCx+float64(sz)*0.13), int(bodyY+float64(sz)*0.13-armWave*0.35), bodyLine, int(math.Max(3, float64(sz)*0.047)))
	badgeW := int(float64(sz) * 0.26)
	badgeH := int(float64(sz) * 0.14)
	badgeX := int(bodyCx) - badgeW/2
	badgeY := int(bodyY - float64(sz)*0.015)
	drawRoundRect(out, badgeX-2, badgeY-2, badgeW+4, badgeH+4, int(float64(sz)*0.045), bodyLine)
	drawRoundRect(out, badgeX, badgeY, badgeW, badgeH, int(float64(sz)*0.04), color.NRGBA{R: 248, G: 250, B: 252, A: 232})
	drawLine(out, badgeX+int(float64(sz)*0.055), badgeY+badgeH/2, badgeX+badgeW-int(float64(sz)*0.08), badgeY+badgeH/2, accent, int(math.Max(2, float64(sz)*0.022)))
	drawCircle(out, badgeX+badgeW-int(float64(sz)*0.045), badgeY+badgeH/2, int(math.Max(2, float64(sz)*0.02)), accent)
	antBaseX, antBaseY := rotatePetPoint(headCx, headCy, headCx, headCy-headR+float64(sz)*0.02, pose.HeadTilt)
	antTipX, antTipY := rotatePetPoint(headCx, headCy, headCx, headCy-headR-float64(sz)*0.11, pose.HeadTilt)
	barLeftX, barLeftY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.1, headCy-headR-float64(sz)*0.06, pose.HeadTilt)
	barRightX, barRightY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.1, headCy-headR-float64(sz)*0.06, pose.HeadTilt)
	drawLine(out, int(antTipX), int(antTipY), int(antBaseX), int(antBaseY), accent, int(math.Max(3, float64(sz)*0.04)))
	drawLine(out, int(barLeftX), int(barLeftY), int(barRightX), int(barRightY), accent, int(math.Max(3, float64(sz)*0.04)))

	switch skin {
	case "mini-claw":
		drawLine(out, int(bodyCx-float64(sz)*0.22), int(bodyY+float64(sz)*0.18), int(bodyCx+float64(sz)*0.22), int(bodyY+float64(sz)*0.18), accent, int(math.Max(3, float64(sz)*0.05)))
		drawLine(out, int(bodyCx-float64(sz)*0.25), int(bodyY+float64(sz)*0.23), int(bodyCx-float64(sz)*0.06), int(bodyY+float64(sz)*0.23), headLine, int(math.Max(2, float64(sz)*0.035)))
		drawLine(out, int(bodyCx+float64(sz)*0.06), int(bodyY+float64(sz)*0.23), int(bodyCx+float64(sz)*0.25), int(bodyY+float64(sz)*0.23), headLine, int(math.Max(2, float64(sz)*0.035)))
	case "dev-claw":
		visorLeftX, visorLeftY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.28, headCy-float64(sz)*0.03, pose.HeadTilt)
		visorRightX, visorRightY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.28, headCy-float64(sz)*0.03, pose.HeadTilt)
		drawLine(out, int(visorLeftX), int(visorLeftY), int(visorRightX), int(visorRightY), color.NRGBA{R: 15, G: 23, B: 42, A: 245}, int(math.Max(4, float64(sz)*0.08)))
		drawLine(out, int(bodyCx-float64(sz)*0.12), int(bodyY), int(bodyCx-float64(sz)*0.03), int(bodyY+float64(sz)*0.07), accent, int(math.Max(2, float64(sz)*0.03)))
		drawLine(out, int(bodyCx+float64(sz)*0.12), int(bodyY), int(bodyCx+float64(sz)*0.03), int(bodyY+float64(sz)*0.07), accent, int(math.Max(2, float64(sz)*0.03)))
		drawLine(out, int(bodyCx-float64(sz)*0.24), int(bodyY+float64(sz)*0.23), int(bodyCx-float64(sz)*0.04), int(bodyY+float64(sz)*0.23), color.NRGBA{R: 148, G: 163, B: 184, A: 235}, int(math.Max(2, float64(sz)*0.035)))
		drawLine(out, int(bodyCx+float64(sz)*0.04), int(bodyY+float64(sz)*0.23), int(bodyCx+float64(sz)*0.24), int(bodyY+float64(sz)*0.23), color.NRGBA{R: 148, G: 163, B: 184, A: 235}, int(math.Max(2, float64(sz)*0.035)))
	case "focus-claw":
		browLeftAX, browLeftAY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.17, headCy-float64(sz)*0.02, pose.HeadTilt)
		browLeftBX, browLeftBY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.07, headCy-float64(sz)*0.02, pose.HeadTilt)
		browRightAX, browRightAY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.07, headCy-float64(sz)*0.02, pose.HeadTilt)
		browRightBX, browRightBY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.17, headCy-float64(sz)*0.02, pose.HeadTilt)
		drawLine(out, int(browLeftAX), int(browLeftAY), int(browLeftBX), int(browLeftBY), eye, int(math.Max(3, float64(sz)*0.04)))
		drawLine(out, int(browRightAX), int(browRightAY), int(browRightBX), int(browRightBY), eye, int(math.Max(3, float64(sz)*0.04)))
		drawLine(out, int(bodyCx-float64(sz)*0.23), int(bodyY+float64(sz)*0.22), int(bodyCx-float64(sz)*0.05), int(bodyY+float64(sz)*0.22), headLine, int(math.Max(2, float64(sz)*0.032)))
		drawLine(out, int(bodyCx+float64(sz)*0.05), int(bodyY+float64(sz)*0.22), int(bodyCx+float64(sz)*0.23), int(bodyY+float64(sz)*0.22), headLine, int(math.Max(2, float64(sz)*0.032)))
	}
	return out
}

func drawRoundRect(img *image.NRGBA, x, y, w, h, r int, c color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	b := img.Bounds()
	if r < 0 {
		r = 0
	}
	for py := y; py < y+h; py++ {
		if py < b.Min.Y || py >= b.Max.Y {
			continue
		}
		for px := x; px < x+w; px++ {
			if px < b.Min.X || px >= b.Max.X {
				continue
			}
			dx := 0
			if px < x+r {
				dx = x + r - px
			} else if px >= x+w-r {
				dx = px - (x + w - r - 1)
			}
			dy := 0
			if py < y+r {
				dy = y + r - py
			} else if py >= y+h-r {
				dy = py - (y + h - r - 1)
			}
			if dx == 0 || dy == 0 || dx*dx+dy*dy <= r*r {
				img.SetNRGBA(px, py, c)
			}
		}
	}
}

func drawCircle(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	if r <= 0 {
		return
	}
	b := img.Bounds()
	for y := cy - r; y <= cy+r; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := cx - r; x <= cx+r; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func drawEllipse(img *image.NRGBA, cx, cy, rx, ry int, c color.NRGBA) {
	if rx <= 0 || ry <= 0 {
		return
	}
	b := img.Bounds()
	rx2 := float64(rx * rx)
	ry2 := float64(ry * ry)
	for y := cy - ry; y <= cy+ry; y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := cx - rx; x <= cx+rx; x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx := float64((x - cx) * (x - cx))
			dy := float64((y - cy) * (y - cy))
			if dx/rx2+dy/ry2 <= 1 {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

func rotatePetPoint(cx, cy, x, y, radians float64) (float64, float64) {
	if radians == 0 {
		return x, y
	}
	sin, cos := math.Sin(radians), math.Cos(radians)
	dx, dy := x-cx, y-cy
	return cx + dx*cos - dy*sin, cy + dx*sin + dy*cos
}

func drawLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA, width int) {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	steps := int(math.Max(math.Abs(dx), math.Abs(dy)))
	if steps <= 0 {
		drawCircle(img, x0, y0, width/2, c)
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(float64(x0) + dx*t)
		y := int(float64(y0) + dy*t)
		drawCircle(img, x, y, width/2, c)
	}
}

func petFacePoseForPhase(phase float64, interactionMode string) petFacePose {
	look := math.Sin(phase)
	nod := math.Sin(phase*2 + math.Pi/6)
	blink := 0.5 + 0.5*math.Sin(phase*3)
	mouth := 0.5 + 0.5*math.Sin(phase*4+math.Pi/3)
	arm := math.Sin(phase*2 + math.Pi/8)
	headShift := 2.4 * look
	headLift := 0.9 * nod
	headTilt := 0.055 * look
	eyeShiftX := 2.7 * look
	eyeShiftY := 0.7 * math.Sin(phase*2+math.Pi/4)
	eyeOpen := 1.0
	mouthOpen := math.Max(0, mouth-0.38) / 0.62
	cheekAlpha := 0.35 + 0.65*mouthOpen
	if blink > 0.965 {
		eyeOpen = 0.1
		eyeShiftY += 0.9
	} else if blink > 0.92 {
		eyeOpen = 0.45
	}

	switch interactionMode {
	case "active":
		headShift *= 1.45
		headLift *= 1.3
		headTilt *= 1.55
		eyeShiftX *= 1.35
		eyeShiftY *= 1.25
		mouthOpen = math.Max(mouthOpen, 0.18+0.45*(0.5+0.5*math.Sin(phase*3.2)))
		arm *= 1.35
		cheekAlpha = math.Max(cheekAlpha, 0.8)
	case "quiet":
		headShift *= 0.45
		headLift *= 0.45
		headTilt *= 0.45
		eyeShiftX *= 0.45
		eyeShiftY *= 0.45
		mouthOpen *= 0.35
		arm *= 0.35
		cheekAlpha *= 0.55
	}

	return petFacePose{
		HeadShiftX: headShift,
		HeadShiftY: headLift,
		HeadTilt:   headTilt,
		EyeShiftX:  eyeShiftX,
		EyeShiftY:  eyeShiftY,
		EyeOpen:    eyeOpen,
		MouthOpen:  math.Min(1, math.Max(0, mouthOpen)),
		ArmWave:    math.Min(1, math.Max(-1, arm)),
		CheekAlpha: math.Min(1, math.Max(0, cheekAlpha)),
	}
}

func (w *windowsFloatingWindow) cachedPetFrame(sz int, skin, variant, state, interactionMode string, phase float64) *image.NRGBA {
	if skin == "" {
		skin = "clawmate"
	}
	variant = petpack.VariantDefault
	if state == "" {
		state = string(petpack.StateIdle)
	}
	if interactionMode == "" {
		interactionMode = "balanced"
	}
	if frame := w.renderCharacterPackFrame(sz, skin, variant, state); frame != nil {
		return frame
	}
	if frame := w.renderSkeletonPackFrame(sz, skin, variant, state); frame != nil {
		return frame
	}
	// ClawMate is the single official 3.0 sample. Its vector-like native
	// fallback is intentionally rendered from the same articulated crab pose,
	// rather than reverting to the former abstract mascot raster.
	if skin == petpack.DefaultPackID {
		bucketPhase := math.Mod(phase, 2*math.Pi)
		poseMode := interactionMode
		switch petpack.NormalizeState(state) {
		case petpack.StateListening:
			poseMode = "active"
		case petpack.StateQuiet:
			poseMode = "quiet"
		}
		return renderClawMatePetWithPose(sz, skin, petFacePoseForPhase(bucketPhase, poseMode))
	}
	phase = math.Mod(phase, 2*math.Pi)
	if phase < 0 {
		phase += 2 * math.Pi
	}
	bucket := int(math.Round(phase/(2*math.Pi)*petAnimationFrameBuckets)) % petAnimationFrameBuckets
	// Native pack frames are static per state. Procedural fallbacks keep their
	// phase bucket so their existing face animation remains alive.
	baseKey := petFrameCacheKey{Size: sz, Skin: skin, Variant: variant, State: state, Mode: interactionMode}

	w.mu.Lock()
	nativeAvailable, nativeKnown := w.petNativeFrameAvailable[baseKey]
	if nativeKnown && nativeAvailable {
		if frame := w.petFrameCache[baseKey]; frame != nil {
			w.mu.Unlock()
			return frame
		}
		// The native bitmap was evicted. Re-resolve it instead of treating the
		// stale availability bit as a permanent miss.
		delete(w.petNativeFrameAvailable, baseKey)
		nativeKnown = false
	}
	proceduralKey := baseKey
	proceduralKey.Bucket = bucket
	if frame := w.petFrameCache[proceduralKey]; frame != nil {
		w.mu.Unlock()
		return frame
	}
	packCache := w.packFrameCache
	if w.petNativeFrameLoading == nil {
		w.petNativeFrameLoading = make(map[petFrameCacheKey]bool)
	}
	nativeLoading := w.petNativeFrameLoading[baseKey]
	if !nativeKnown && !nativeLoading {
		w.petNativeFrameLoading[baseKey] = true
	}
	w.mu.Unlock()

	// Prefer frames from the selected pack. Once a state has no native frame,
	// remember that result and avoid filesystem work for each procedural bucket.
	if !nativeKnown && !nativeLoading {
		frame := tryLoadPackFrame(skin, variant, state, sz, packCache)
		w.mu.Lock()
		if w.petNativeFrameAvailable == nil {
			w.petNativeFrameAvailable = make(map[petFrameCacheKey]bool)
		}
		delete(w.petNativeFrameLoading, baseKey)
		// A concurrent render may have already resolved this state while I/O was
		// in flight. Reuse its decision/frame instead of overwriting it.
		if resolved, known := w.petNativeFrameAvailable[baseKey]; known {
			cached := w.petFrameCache[baseKey]
			w.mu.Unlock()
			if resolved && cached != nil {
				return cached
			}
		} else {
			w.petNativeFrameAvailable[baseKey] = frame != nil
			if frame != nil {
				if w.petFrameCache == nil {
					w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
				}
				w.evictPetFrameCacheCycleLocked(baseKey)
				w.petFrameCache[baseKey] = frame
				w.mu.Unlock()
				return frame
			}
			w.mu.Unlock()
		}
	}

	bucketPhase := float64(bucket) / float64(petAnimationFrameBuckets) * 2 * math.Pi
	// Blend runtime state into interaction pose for procedural fallback.
	poseMode := interactionMode
	switch petpack.NormalizeState(state) {
	case petpack.StateListening:
		poseMode = "active"
	case petpack.StateQuiet:
		poseMode = "quiet"
	}
	frame := renderClawMatePetWithPose(sz, skin, petFacePoseForPhase(bucketPhase, poseMode))

	w.mu.Lock()
	if w.petFrameCache == nil {
		w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
	}
	if cached := w.petFrameCache[proceduralKey]; cached != nil {
		w.mu.Unlock()
		return cached
	}
	w.evictPetFrameCacheCycleLocked(proceduralKey)
	w.petFrameCache[proceduralKey] = frame
	w.mu.Unlock()
	return frame
}

// hasActiveCharacterRenderer reports whether the selected pack is currently
// supplying a native-character frame. It intentionally checks the loaded
// renderer rather than resolving the registry again on the 20fps render path.
func (w *windowsFloatingWindow) hasActiveCharacterRenderer(skin, variant string) bool {
	if skin == "" {
		skin = petpack.DefaultPackID
	}
	variant = petpack.ResolveVariantForRuntime(variant)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.characterRenderers[skin+"\x00"+variant] != nil
}

// renderCharacterPackFrame is the v3 path. The performer has already been
// validated during registry scanning and is loaded once per selected pack.
// Any issue falls through to the v2 skeleton and static fallback paths.
func (w *windowsFloatingWindow) renderCharacterPackFrame(sz int, skin, variant, state string) *image.NRGBA {
	if skin == "" {
		skin = petpack.DefaultPackID
	}
	variant = petpack.ResolveVariantForRuntime(variant)
	key := skin + "\x00" + variant
	w.mu.Lock()
	renderer, failed := w.characterRenderers[key], w.characterRendererFailed[key]
	w.mu.Unlock()
	if failed {
		return nil
	}
	if renderer == nil {
		reg := petpack.EnsureGlobal()
		if reg == nil {
			return nil
		}
		resolved, err := reg.Resolve(skin, variant)
		if err != nil || resolved == nil || resolved.Renderer != petpack.RendererCharacter {
			return nil
		}
		renderer, err = petpack.NewCharacterRenderer(resolved)
		w.mu.Lock()
		// The maps are normally allocated in Create, but a window that only
		// receives state pushes (never shown) must not panic on a nil map.
		if w.characterRenderers == nil {
			w.characterRenderers = make(map[string]*petpack.CharacterRenderer)
		}
		if w.characterRendererFailed == nil {
			w.characterRendererFailed = make(map[string]bool)
		}
		if err != nil || renderer == nil {
			w.characterRendererFailed[key] = true
			w.mu.Unlock()
			return nil
		}
		if existing := w.characterRenderers[key]; existing != nil {
			renderer = existing
		} else {
			w.characterRenderers[key] = renderer
		}
		w.mu.Unlock()
	}
	return renderer.RenderState(petpack.NormalizeState(state), time.Now().UnixMilli(), sz)
}

// cachedStaticPetFrame is the accessibility path: reduced motion and quiet
// mode use the pack's declared static state image, never an arbitrary frame
// sampled from a continuously running skeleton clip.
func (w *windowsFloatingWindow) cachedStaticPetFrame(sz int, skin, variant, state string) *image.NRGBA {
	// InvalidatePetPackAssets swaps the cache pointer from another goroutine;
	// take it under the lock like cachedPetFrame does.
	w.mu.Lock()
	packCache := w.packFrameCache
	w.mu.Unlock()
	if frame := tryLoadPackFrame(skin, variant, state, sz, packCache); frame != nil {
		return frame
	}
	return renderClawMatePet(sz, skin)
}

// renderSkeletonPackFrame is the v2 primary path. It deliberately resolves
// and validates once per pack/variant, then renders only numeric transforms
// from the already-decoded local assets. Any error falls through to existing
// native raster and procedural paths.
func (w *windowsFloatingWindow) renderSkeletonPackFrame(sz int, skin, variant, state string) *image.NRGBA {
	if skin == "" {
		skin = petpack.DefaultPackID
	}
	variant = petpack.ResolveVariantForRuntime(variant)
	key := skin + "\x00" + variant
	w.mu.Lock()
	renderer := w.rigRenderers[key]
	failed := w.rigRendererFailed[key]
	w.mu.Unlock()
	if failed {
		return nil
	}
	if renderer == nil {
		reg := petpack.EnsureGlobal()
		if reg == nil {
			return nil
		}
		resolved, err := reg.Resolve(skin, variant)
		if err != nil || resolved == nil || resolved.Renderer != petpack.RendererSkeleton {
			return nil
		}
		renderer, err = petpack.NewRigRenderer(resolved)
		w.mu.Lock()
		// Same nil-map guard as the character path for never-created windows.
		if w.rigRenderers == nil {
			w.rigRenderers = make(map[string]*petpack.RigRenderer)
		}
		if w.rigRendererFailed == nil {
			w.rigRendererFailed = make(map[string]bool)
		}
		if err != nil || renderer == nil {
			w.rigRendererFailed[key] = true
			w.mu.Unlock()
			return nil
		}
		if existing := w.rigRenderers[key]; existing != nil {
			renderer = existing
		} else {
			w.rigRenderers[key] = renderer
		}
		w.mu.Unlock()
	}
	w.mu.Lock()
	started, changed := w.petAnimationStartedAt, w.petStateChangedAt
	w.mu.Unlock()
	clock := started
	if !renderer.IsLooping(petpack.NormalizeState(state)) {
		clock = changed
	}
	elapsed := int(time.Since(clock).Milliseconds())
	if clock.IsZero() {
		elapsed = int(time.Now().UnixMilli() % 60000)
	}
	return renderer.Render(petpack.NormalizeState(state), elapsed, sz)
}

func (w *windowsFloatingWindow) evictPetFrameCacheCycleLocked(incoming petFrameCacheKey) {
	if len(w.petFrameCache) < petFrameCacheLimit {
		return
	}
	// Remove the entire animated cycle for one other state/mode first. This
	// leaves cached image sequences coherent instead of causing a whole-cache
	// reset and a visible hitch on the next timer frame.
	for key := range w.petFrameCache {
		cycle := key
		cycle.Bucket = 0
		incomingCycle := incoming
		incomingCycle.Bucket = 0
		if cycle == incomingCycle {
			continue
		}
		if w.petNativeFrameAvailable[cycle] {
			continue // keep native/static frame assets whenever possible
		}
		for candidate := range w.petFrameCache {
			candidateCycle := candidate
			candidateCycle.Bucket = 0
			if candidateCycle == cycle {
				delete(w.petFrameCache, candidate)
			}
		}
		// A negative native lookup is only useful while its procedural cycle is
		// resident. Dropping it with the cycle bounds this metadata and lets a
		// newly installed/updated pack be discovered on the next request.
		delete(w.petNativeFrameAvailable, cycle)
		delete(w.petNativeFrameLoading, cycle)
		return
	}
	// A pathological cache full of only native/static entries is still bounded.
	// Evict the matching availability entry too: otherwise a later cache miss
	// would be remembered as native but have no way to reload that native frame.
	for key := range w.petFrameCache {
		delete(w.petFrameCache, key)
		staticKey := key
		staticKey.Bucket = 0
		if staticKey == key {
			delete(w.petNativeFrameAvailable, staticKey)
			delete(w.petNativeFrameLoading, staticKey)
		}
		return
	}
}

func playPetMotionSound(interactionMode, skin, preset string, packPitch float64) {
	go func() {
		if interactionMode == "quiet" {
			return
		}
		// K21: user preset selects tone table; pack only adjusts pitch.
		eff := petpack.EffectiveSoundProfileFrom(preset, "classic", packPitch)
		preset = eff.Preset
		type petTone struct {
			hz uintptr
			ms uintptr
		}
		// Preset-driven tones (user wins). Skin-specific tables removed for pack-driven pitch.
		toneSet := []petTone{{620, 28}, {930, 34}}
		switch preset {
		case "bubble":
			toneSet = []petTone{{560, 22}, {860, 28}, {1040, 18}}
		case "chime":
			toneSet = []petTone{{880, 40}, {1320, 52}}
		case "synth":
			toneSet = []petTone{{640, 22}, {480, 26}, {760, 18}}
		case "soft":
			toneSet = []petTone{{392, 42}, {588, 48}}
		}
		pitch := eff.Pitch
		if pitch <= 0 {
			pitch = 1
		}
		if interactionMode == "active" {
			pitch *= 1.12
		}
		// Mild skin pitch hint only when pack pitch is default
		if eff.Pitch == 1 {
			switch skin {
			case "mini-claw":
				pitch *= 1.1
			case "dev-claw":
				pitch *= 0.95
			case "focus-claw":
				pitch *= 0.85
			}
		}
		for _, tone := range toneSet {
			procBeep.Call(uintptr(float64(tone.hz)*pitch), tone.ms)
		}
	}()
}

// renderFrame composites the base image with the current halo animation phase
// and pushes it to the layered window via UpdateLayeredWindow.
func (w *windowsFloatingWindow) renderFrame() {
	w.mu.Lock()
	// Expire runtime state TTL back to idle through the same transition path as
	// an explicit state update, so the old image is available for crossfade.
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.setPetRuntimeStateLocked(string(petpack.StateIdle), 0)
	}
	hwnd := w.hwnd
	base := w.baseImg
	phase := w.haloPhase
	distMap := w.distMap
	petEnabled := w.petEnabled
	petMotionEnabled := w.petMotionEnabled
	petMotionSound := w.petMotionSound
	playPetSound := false
	if hwnd != 0 && base != nil && distMap != nil && w.petEnabled && w.petMotionEnabled && petMotionSound && !w.petQuietMode {
		bucket := int(math.Floor(w.haloPhase/(2*math.Pi)*4)) % 4
		if bucket != w.lastPetSoundBucket {
			w.lastPetSoundBucket = bucket
			playPetSound = bucket == 0 || (w.petInteractionMode == "active" && bucket == 2)
		}
	}
	petQuietMode := w.petQuietMode
	petReducedMotion := w.petReducedMotion
	petInteractionMode := w.petInteractionMode
	petSkin := w.petSkin
	petVariant := w.petVariant
	petPreviousState := w.petPreviousState
	petStateChangedAt := w.petStateChangedAt
	petMotionAmplitude := w.petMotionAmplitude
	petSoundPreset := w.petSoundPreset
	packPitch := w.packPitch
	if packPitch <= 0 {
		packPitch = 1
	}
	petState := w.petRuntimeState
	if petState == "" {
		petState = string(petpack.StateIdle)
	}
	if petQuietMode {
		petState = string(petpack.StateQuiet)
	}
	if petState != w.petLastRenderedState {
		w.renderDirty = true
	}
	staticOnly := petReducedMotion || !petMotionEnabled || petQuietMode
	// Fire the long_idle character event once per idle period. The static
	// accessibility paths (quiet / reduced-motion / motion off) never fire it;
	// any interaction or state change rearms the latch via notePetInteraction.
	longIdleDue := false
	if petEnabled && !staticOnly && !w.petLongIdleFired && !w.petLastInteractionAt.IsZero() && time.Since(w.petLastInteractionAt) >= petLongIdleThreshold {
		w.petLongIdleFired = true
		longIdleDue = true
	}
	// When no pet is visible, the frame is just the static fallback/logo; avoid
	// allocating and uploading an identical layered bitmap every timer tick.
	// (longIdleDue implies petEnabled && !staticOnly, so it cannot be set here.)
	if (staticOnly || !petEnabled) && !w.renderDirty {
		w.mu.Unlock()
		return
	}
	w.renderDirty = false
	w.petLastRenderedState = petState
	w.mu.Unlock()
	if longIdleDue {
		w.triggerCharacterEvent("long_idle")
	}
	if playPetSound && !petReducedMotion {
		playPetMotionSound(petInteractionMode, petSkin, petSoundPreset, packPitch)
	}

	if hwnd == 0 || base == nil || distMap == nil {
		return
	}

	sz := w.currentSize()
	frame := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	if petEnabled {
		petScale := 1.0
		petXOffset := 0.0
		petYOffset := 0.0
		if !staticOnly {
			intensity := 0.65 + 0.35*petMotionAmplitude
			petScale = 1.0 + 0.022*intensity*math.Sin(phase)
			petYOffset = math.Sin(phase+math.Pi/5) * float64(sz) * 0.016 * intensity
			if petInteractionMode == "active" {
				petScale = 1.0 + 0.03*intensity*math.Sin(phase*1.35)
				petYOffset = math.Sin(phase*1.35+math.Pi/5) * float64(sz) * 0.022 * intensity
			} else if petInteractionMode == "quiet" {
				petScale = 1.0 + 0.01*intensity*math.Sin(phase*0.75)
				petYOffset = math.Sin(phase*0.75+math.Pi/5) * float64(sz) * 0.006 * intensity
			}
			switch petpack.NormalizeState(petState) {
			case petpack.StateListening:
				petXOffset = math.Sin(phase*1.55) * float64(sz) * 0.008 * intensity
				petScale += 0.006 * intensity * math.Sin(phase*1.55+math.Pi/4)
			case petpack.StateThinking:
				petXOffset = math.Sin(phase*0.95) * float64(sz) * 0.012 * intensity
				petYOffset += math.Abs(math.Sin(phase*0.95)) * float64(sz) * 0.007 * intensity
			case petpack.StateSpeaking:
				petScale += 0.018 * intensity * math.Sin(phase*2.7)
				petYOffset += math.Abs(math.Sin(phase*2.7)) * float64(sz) * 0.012 * intensity
			case petpack.StateDone:
				bounce := math.Max(0, math.Sin(phase*1.8))
				petScale += 0.016 * intensity * bounce
				petYOffset -= bounce * float64(sz) * 0.024 * intensity
			case petpack.StateAlert:
				petXOffset = math.Sin(phase*4.2) * float64(sz) * 0.016 * intensity
			}
		}
		var petFrame *image.NRGBA
		nativeCharacter := false
		if staticOnly {
			petFrame = w.cachedStaticPetFrame(sz, petSkin, petVariant, petState)
		} else {
			petFrame = w.cachedPetFrame(sz, petSkin, petVariant, petState, petInteractionMode, phase)
			nativeCharacter = w.hasActiveCharacterRenderer(petSkin, petVariant)
		}
		previousPetFrame := (*image.NRGBA)(nil)
		stateTransition := 1.0
		if nativeCharacter {
			// The v3 renderer owns authored bone motion and its internal crossfade.
			// Applying legacy sine-wave and a second state blend would turn the
			// performance back into the mechanical wobble this version replaces.
			petScale, petXOffset, petYOffset = 1, 0, 0
		} else if !staticOnly && petPreviousState != "" && petPreviousState != petState && !petStateChangedAt.IsZero() {
			stateTransition = math.Min(1, time.Since(petStateChangedAt).Seconds()/0.28)
			if stateTransition < 1 {
				previousPetFrame = w.cachedPetFrame(sz, petSkin, petVariant, petPreviousState, petInteractionMode, phase)
			}
		}
		stateScale, stateXOffset, stateYOffset := 1.0, 0.0, 0.0
		if !nativeCharacter {
			stateScale, stateXOffset, stateYOffset = petStateTransitionPose(petState, stateTransition, sz, petMotionAmplitude)
		}
		renderAnimatedPetFrame(frame, petFrame, previousPetFrame, petScale*stateScale, petXOffset+stateXOffset, petYOffset+stateYOffset, stateTransition)
	} else {
		copy(frame.Pix, base.Pix)
	}

	// Animated pulsing glow overlay - modulate the glow ring alpha.
	logoRadius := float64(sz)/2 - 8
	glowOuter := float64(sz) / 2
	glowRange := glowOuter - logoRadius
	pulse := 0.5 + 0.5*math.Sin(phase) // 0..1 pulsing

	for i := 0; i < sz*sz; i++ {
		dist := distMap[i]
		if dist > logoRadius && dist <= glowOuter {
			t := (dist - logoRadius) / glowRange
			extraAlpha := uint8(float64(60) * pulse * (1 - t*t))
			idx := i * 4
			a := frame.Pix[idx+3]
			newA := uint16(a) + uint16(extraAlpha)
			if newA > 255 {
				newA = 255
			}
			frame.Pix[idx+3] = byte(newA)
			b := frame.Pix[idx+2]
			newB := uint16(b) + uint16(float64(30)*pulse*(1-t))
			if newB > 255 {
				newB = 255
			}
			frame.Pix[idx+2] = byte(newB)
		}
	}

	applyNRGBAToLayeredWindow(hwnd, frame, sz)
}

// petStateTransitionPose gives each semantic pack image a short, readable
// entrance gesture. This works with the existing single image per state, so
// pet-pack authors do not need to provide sprite sheets or animation metadata.
func petStateTransitionPose(state string, progress float64, size int, amplitude float64) (scale, offsetX, offsetY float64) {
	progress = math.Max(0, math.Min(1, progress))
	amplitude = math.Max(0, math.Min(1, amplitude))
	if size <= 0 || progress >= 1 || amplitude == 0 {
		return 1, 0, 0
	}
	remaining := 1 - progress
	travel := float64(size) * (0.035 + 0.025*amplitude)
	switch petpack.NormalizeState(state) {
	case petpack.StateListening:
		return 1 + 0.05*remaining, -travel * remaining, travel * 0.16 * remaining
	case petpack.StateThinking:
		return 1 - 0.035*remaining, travel * 0.5 * remaining, travel * 0.28 * remaining
	case petpack.StateSpeaking:
		return 1 + 0.07*remaining, 0, travel * 0.38 * remaining
	case petpack.StateDone:
		// A quick upward pop and settle makes completion feel intentional.
		return 1 + 0.09*remaining, 0, -travel * 0.82 * remaining
	case petpack.StateAlert:
		return 1 + 0.03*remaining, travel * remaining, 0
	case petpack.StateQuiet:
		return 1 - 0.025*remaining, 0, travel * 0.2 * remaining
	default:
		return 1, 0, travel * 0.15 * remaining
	}
}

func renderAnimatedPetFrame(dst, src, previous *image.NRGBA, scale, offsetX, offsetY, transition float64) {
	b := dst.Bounds()
	sz := b.Dx()
	if sz <= 0 || src == nil {
		return
	}
	if previous != nil && transition < 1 {
		src = blendPetFrames(previous, src, transition)
	}
	renderScaledPetFrame(dst, src, scale, offsetX, offsetY, 1)
}

func blendPetFrames(previous, current *image.NRGBA, progress float64) *image.NRGBA {
	if previous == nil || current == nil || !previous.Bounds().Eq(current.Bounds()) {
		return current
	}
	progress = math.Max(0, math.Min(1, progress))
	out := image.NewNRGBA(current.Bounds())
	for i := 0; i < len(out.Pix); i++ {
		out.Pix[i] = byte(math.Round(float64(previous.Pix[i])*(1-progress) + float64(current.Pix[i])*progress))
	}
	return out
}

func renderScaledPetFrame(dst, src *image.NRGBA, scale, offsetX, offsetY, alpha float64) {
	if src == nil || alpha <= 0 {
		return
	}
	b := dst.Bounds()
	sz := b.Dx()
	target := int(math.Round(float64(sz) * scale))
	if target < 1 {
		target = 1
	}
	x := int(math.Round((float64(sz)-float64(target))/2 + offsetX))
	y := int(math.Round((float64(sz)-float64(target))/2 + offsetY))
	rect := image.Rect(x, y, x+target, y+target)
	if rect.Min.X < b.Min.X {
		rect = rect.Add(image.Pt(b.Min.X-rect.Min.X, 0))
	}
	if rect.Min.Y < b.Min.Y {
		rect = rect.Add(image.Pt(0, b.Min.Y-rect.Min.Y))
	}
	if rect.Max.X > b.Max.X {
		rect = rect.Add(image.Pt(b.Max.X-rect.Max.X, 0))
	}
	if rect.Max.Y > b.Max.Y {
		rect = rect.Add(image.Pt(0, b.Max.Y-rect.Max.Y))
	}
	if alpha >= 0.999 {
		xdraw.CatmullRom.Scale(dst, rect, src, src.Bounds(), xdraw.Over, nil)
		return
	}
	tmp := image.NewNRGBA(dst.Bounds())
	xdraw.CatmullRom.Scale(tmp, rect, src, src.Bounds(), xdraw.Over, nil)
	alphaOverNRGBA(dst, tmp, alpha)
}

func alphaOverNRGBA(dst, src *image.NRGBA, alpha float64) {
	alpha = math.Max(0, math.Min(1, alpha))
	for i := 0; i < len(dst.Pix) && i < len(src.Pix); i += 4 {
		sa := float64(src.Pix[i+3]) / 255 * alpha
		if sa <= 0 {
			continue
		}
		da := float64(dst.Pix[i+3]) / 255
		outA := sa + da*(1-sa)
		if outA <= 0 {
			continue
		}
		for channel := 0; channel < 3; channel++ {
			sc := float64(src.Pix[i+channel]) / 255
			dc := float64(dst.Pix[i+channel]) / 255
			out := (sc*sa + dc*da*(1-sa)) / outA
			dst.Pix[i+channel] = byte(math.Round(out * 255))
		}
		dst.Pix[i+3] = byte(math.Round(outA * 255))
	}
}

// UpdateLayeredWindow helper

func applyNRGBAToLayeredWindow(hwnd uintptr, img *image.NRGBA, sz int) {
	screenDC := uintptr(0)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return
	}
	defer procScreenshotDeleteDC.Call(memDC)

	bmi := bitmapInfo{
		BmiHeader: bitmapInfoHeader{
			BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			BiWidth:       int32(sz),
			BiHeight:      -int32(sz), // top-down
			BiPlanes:      1,
			BiBitCount:    32,
			BiCompression: biRgb,
		},
	}

	var bits unsafe.Pointer
	hBitmap, _, _ := procCreateDIBSection.Call(
		memDC, uintptr(unsafe.Pointer(&bmi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0,
	)
	if hBitmap == 0 {
		return
	}
	defer procScreenshotDeleteObject.Call(hBitmap)

	oldBmp, _, _ := procScreenshotSelectObject.Call(memDC, hBitmap)
	defer procScreenshotSelectObject.Call(memDC, oldBmp)

	// Copy NRGBA to pre-multiplied BGRA.
	pixelSlice := unsafe.Slice((*byte)(bits), sz*sz*4)
	for i := 0; i < sz*sz; i++ {
		si := i * 4
		r, g, b, a := img.Pix[si], img.Pix[si+1], img.Pix[si+2], img.Pix[si+3]
		a16 := uint16(a)
		pixelSlice[si+0] = byte(uint16(b) * a16 / 255)
		pixelSlice[si+1] = byte(uint16(g) * a16 / 255)
		pixelSlice[si+2] = byte(uint16(r) * a16 / 255)
		pixelSlice[si+3] = a
	}

	ptSrc := point{0, 0}
	szW := sizeW{int32(sz), int32(sz)}
	blend := blendFunction{acSrcOver, 0, 255, acSrcAlpha}

	procUpdateLayeredWindow.Call(
		hwnd, 0, 0,
		uintptr(unsafe.Pointer(&szW)),
		memDC,
		uintptr(unsafe.Pointer(&ptSrc)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		uintptr(ulwAlpha),
	)
}

// Win32 window procedure

func floatingWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := currentGlobalFloatingWindow()

	switch msg {
	case wmNchittest:
		return 1 // HTCLIENT

	case wmTimer:
		if w != nil && wParam == timerIdHalo {
			w.mu.Lock()
			// 2π / 40: keep the intended two-second ambient cycle at 20 FPS.
			w.haloPhase += 2 * math.Pi / 40
			if w.haloPhase > 2*math.Pi {
				w.haloPhase -= 2 * math.Pi
			}
			w.mu.Unlock()
			w.renderFrame()
		}
		return 0

	case wmLbuttondown:
		if w == nil {
			break
		}
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		w.dragging = true
		w.dragMoved = false
		w.dragStartX = pt.X
		w.dragStartY = pt.Y
		procSetCapture.Call(hwnd)
		w.notePetInteraction()
		return 0

	case wmMousemove:
		if w == nil {
			break
		}
		if !w.dragging {
			// Acknowledge a visitor once, not once per moved pixel. This timestamp
			// is owned by the message loop and carries no cursor coordinates.
			now := time.Now()
			if w.lastHoverAt.IsZero() || now.Sub(w.lastHoverAt) >= 5*time.Second {
				w.lastHoverAt = now
				w.notePetInteraction()
				w.triggerCharacterEvent("hover")
			}
			return 0
		}
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		dx := pt.X - w.dragStartX
		dy := pt.Y - w.dragStartY
		if !w.dragMoved {
			if abs32(dx) > 5 || abs32(dy) > 5 {
				w.dragMoved = true
				w.triggerCharacterEvent("drag_start")
			} else {
				return 0
			}
		}
		newX := w.windowStartX + int(dx)
		newY := w.windowStartY + int(dy)
		sz := w.currentSize()
		procMoveWindowProc.Call(hwnd, uintptr(newX), uintptr(newY), uintptr(sz), uintptr(sz), 1)
		return 0

	case wmLbuttonup:
		if w == nil {
			break
		}
		procReleaseCapture.Call()
		if !w.dragging {
			break
		}
		w.dragging = false
		w.notePetInteraction()
		if !w.dragMoved {
			w.triggerCharacterEvent("click")
			go func() {
				if w.app != nil {
					w.app.onFloatingButtonClicked()
				}
			}()
		} else {
			w.triggerCharacterEvent("drag_end")
			var pt point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			finalX := w.windowStartX + int(pt.X-w.dragStartX)
			finalY := w.windowStartY + int(pt.Y-w.dragStartY)
			w.windowStartX = finalX
			w.windowStartY = finalY
			go func() {
				if fa := w.app.existingFloatingAssistant(); fa != nil {
					fa.UpdatePosition(finalX, finalY)
				}
			}()
		}
		return 0

	case wmRbuttonup:
		if w == nil {
			break
		}
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		hMenu, _, _ := procCreatePopupMenu.Call()
		if hMenu == 0 {
			break
		}

		// "音效关闭" — checkable menu item
		soundOffText, _ := syscall.UTF16PtrFromString("\u97f3\u6548\u5173\u95ed")
		soundFlags := uintptr(mfString)
		w.mu.Lock()
		if !w.petMotionSound {
			soundFlags |= mfChecked
		}
		w.mu.Unlock()
		procAppendMenuW.Call(hMenu, soundFlags, uintptr(menuIdSoundOff), uintptr(unsafe.Pointer(soundOffText)))

		// Separator
		procAppendMenuW.Call(hMenu, uintptr(mfSeparator), 0, 0)

		settingsText, _ := syscall.UTF16PtrFromString("\u8bbe\u7f6e")
		hideText, _ := syscall.UTF16PtrFromString("\u9690\u85cf")
		quitText, _ := syscall.UTF16PtrFromString("\u9000\u51fa")
		procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(menuIdSettings), uintptr(unsafe.Pointer(settingsText)))
		procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(menuIdHide), uintptr(unsafe.Pointer(hideText)))
		procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(menuIdQuit), uintptr(unsafe.Pointer(quitText)))
		procSetForegroundWindow.Call(hwnd)
		cmd, _, _ := procTrackPopupMenu.Call(hMenu, uintptr(tpmReturncmd), uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
		procDestroyMenu.Call(hMenu)
		switch cmd {
		case menuIdSoundOff:
			go func() {
				if w.app == nil {
					return
				}
				cfg, err := w.app.LoadConfig()
				if err != nil {
					return
				}
				// Toggle: if currently enabled → disable, if disabled → enable
				newEnabled := !petMotionSoundEnabled(cfg)
				// PatchConfigFields triggers floatingSoundChanged → UpdateSoundConfig,
				// which updates w.petMotionSound without rebuilding the window.
				_, _ = w.app.PatchConfigFields(map[string]interface{}{"pet_motion_sound_enabled": newEnabled})
			}()
		case menuIdSettings:
			go func() {
				if w.app != nil {
					w.app.openPetSettingsFromMenu()
				}
			}()
		case menuIdHide:
			go func() {
				if w.app != nil {
					w.app.DisablePetFromMenu()
				}
			}()
		case menuIdQuit:
			go func() {
				if w.app != nil {
					w.app.QuitApp()
				}
			}()
		}
		return 0

	case wmClose:
		procKillTimer.Call(hwnd, timerIdHalo)
		procDestroyWindowProc.Call(hwnd)
		return 0

	case wmDestroy:
		if w != nil {
			w.mu.Lock()
			if w.hwnd == hwnd {
				w.hwnd = 0
				w.created = false
				w.destroying = false
				clearGlobalFloatingWindow(w)
			}
			w.mu.Unlock()
		}
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// alphaOver composites src over dst using standard alpha blending.
func alphaOver(dst, src color.NRGBA) color.NRGBA {
	sa := uint32(src.A)
	da := uint32(dst.A)
	outA := sa + da*(255-sa)/255
	if outA == 0 {
		return color.NRGBA{}
	}
	outR := (uint32(src.R)*sa + uint32(dst.R)*da*(255-sa)/255) / outA
	outG := (uint32(src.G)*sa + uint32(dst.G)*da*(255-sa)/255) / outA
	outB := (uint32(src.B)*sa + uint32(dst.B)*da*(255-sa)/255) / outA
	return color.NRGBA{R: uint8(outR), G: uint8(outG), B: uint8(outB), A: uint8(outA)}
}

// Win32 window creation

var classRegistered bool

func createFloatingWin32Window(x, y, w, h int) (uintptr, error) {
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("MaclawFloatingAssistant")

	if !classRegistered {
		cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
		wcx := wndClassExW{
			Style:         csHredraw | csVredraw,
			LpfnWndProc:   syscall.NewCallback(floatingWndProc),
			HInstance:     syscall.Handle(hInstance),
			HCursor:       syscall.Handle(cursor),
			HbrBackground: 0,
			LpszClassName: className,
		}
		wcx.CbSize = uint32(unsafe.Sizeof(wcx))
		atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
		if atom == 0 {
			return 0, fmt.Errorf("RegisterClassExW: %v", err)
		}
		classRegistered = true
	}

	exStyle := wsExTopmost | wsExLayered | wsExToolwindow
	style := wsPopup | wsVisible

	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		0, uintptr(style),
		uintptr(x), uintptr(y),
		uintptr(w), uintptr(h),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %v", err)
	}

	log.Printf("[floating-window] Win32 window created: hwnd=0x%X pos=(%d,%d) size=(%d,%d)", hwnd, x, y, w, h)
	return hwnd, nil
}
