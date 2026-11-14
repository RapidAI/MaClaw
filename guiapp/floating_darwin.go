//go:build darwin && cgo

package guiapp

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics

#import <Cocoa/Cocoa.h>
#include "floating_darwin.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

//go:embed build/appicon.png
var floatingLogoPNGDarwin []byte

// darwinFloatingWindow is the macOS desktop pet. Character and skeleton packs
// play on a timer and can roam. Quiet mode, reduced motion, and motion-off
// stay on one still. The panel is never released on close: ARC owns it, and
// an extra release crashes the process when the pet is switched.
type darwinFloatingWindow struct {
	app              *App
	created          bool
	mu               sync.Mutex
	size             int
	petSkin          string
	petVariant       string
	petRuntimeState  string
	petStateDeadline time.Time
	petMotionEnabled bool
	petQuietMode     bool
	petReducedMotion bool
	petInteraction   string
	packFrameCache   *petpack.FrameCache
	anim             petDesktopAnimator
	animStop         chan struct{}
	paintGen         uint64
	configGen        uint64
	painting         int32
	lastFrameSig     uint64
	haveFrameSig     bool
	lastFaceLeft     bool
	staticReady      bool
	staticSkin       string
	staticVariant    string
	staticState      string
	staticPixels     int
}

func newFloatingWindow(app *App) floatingWindow {
	if testing.Testing() {
		return &noopFloatingWindow{app: app}
	}
	return &darwinFloatingWindow{app: app}
}

var globalDarwinFloating struct {
	sync.RWMutex
	window *darwinFloatingWindow
}

func setGlobalDarwinFloating(w *darwinFloatingWindow) {
	globalDarwinFloating.Lock()
	globalDarwinFloating.window = w
	globalDarwinFloating.Unlock()
}

func clearGlobalDarwinFloating(w *darwinFloatingWindow) {
	globalDarwinFloating.Lock()
	if globalDarwinFloating.window == w {
		globalDarwinFloating.window = nil
	}
	globalDarwinFloating.Unlock()
}

func currentDarwinFloating() *darwinFloatingWindow {
	globalDarwinFloating.RLock()
	w := globalDarwinFloating.window
	globalDarwinFloating.RUnlock()
	return w
}

func init() {
	platformGetScreenWidth = func() int {
		w := int(C.darwinScreenWidth())
		if w <= 0 {
			return 1920
		}
		return w
	}
	platformGetScreenHeight = func() int {
		h := int(C.darwinScreenHeight())
		if h <= 0 {
			return 1080
		}
		return h
	}
}

func darwinPixelScale() int {
	return darwinRenderScale(int(C.darwinBackingScale()))
}

func (w *darwinFloatingWindow) pixelSizeLocked() int {
	sz := w.size
	if sz < minPetSize {
		sz = defaultPetSize
	}
	return sz * darwinPixelScale()
}

func (w *darwinFloatingWindow) loadPetConfigLocked() {
	w.petSkin = petpack.DefaultPackID
	w.petVariant = petpack.VariantDefault
	w.petMotionEnabled = true
	w.petQuietMode = false
	w.petReducedMotion = false
	w.petInteraction = "balanced"
	if w.petRuntimeState == "" {
		w.petRuntimeState = string(petpack.StateIdle)
	}
	if w.app == nil {
		return
	}
	cfg, err := w.app.LoadConfig()
	if err != nil {
		return
	}
	if cfg.PetSkin != "" {
		w.petSkin = cfg.PetSkin
	}
	w.petVariant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
	w.petMotionEnabled = isPetMotionEnabled(cfg) && !cfg.PetReducedMotion
	w.petQuietMode = cfg.PetQuietMode
	w.petReducedMotion = cfg.PetReducedMotion
	if cfg.PetInteractionMode != "" {
		w.petInteraction = cfg.PetInteractionMode
	}
}

func (w *darwinFloatingWindow) pushAnimatorLocked() {
	if w.packFrameCache == nil {
		w.packFrameCache = petpack.NewFrameCache()
	}
	state := w.petRuntimeState
	if state == "" {
		state = string(petpack.StateIdle)
	}
	w.anim.Configure(petAnimConfig{
		Skin:          w.petSkin,
		Variant:       w.petVariant,
		State:         state,
		PixelSize:     w.pixelSizeLocked(),
		WindowSize:    w.size,
		MotionEnabled: w.petMotionEnabled && !w.petReducedMotion,
		Quiet:         w.petQuietMode,
		Reduced:       w.petReducedMotion,
	})
}

func (w *darwinFloatingWindow) invalidateFrameLocked() {
	w.configGen++
	w.haveFrameSig = false
	w.staticReady = false
}

func (w *darwinFloatingWindow) ensureAnimationLocked() {
	if w.animStop != nil || !w.created {
		return
	}
	stop := make(chan struct{})
	w.animStop = stop
	go w.animationLoop(stop)
}

func (w *darwinFloatingWindow) stopAnimationLocked() {
	if w.animStop == nil {
		return
	}
	close(w.animStop)
	w.animStop = nil
}

func (w *darwinFloatingWindow) animationLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	w.paintAnimatedFrame()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.paintAnimatedFrame()
			select {
			case <-ticker.C:
			default:
			}
		}
	}
}

func (w *darwinFloatingWindow) paintAnimatedFrame() {
	if !atomic.CompareAndSwapInt32(&w.painting, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&w.painting, 0)
	for attempt := 0; attempt < 2; attempt++ {
		if w.paintOnce() {
			return
		}
	}
}

// paintOnce uploads one pose and steps locomotion. It returns false when a
// skin or size change landed during the render, so the caller can try again
// with the new config. The window mutex is not held across AppKit calls.
func (w *darwinFloatingWindow) paintOnce() bool {
	w.mu.Lock()
	if !w.created {
		w.mu.Unlock()
		return true
	}
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.petRuntimeState = string(petpack.StateIdle)
		w.petStateDeadline = time.Time{}
	}
	w.pushAnimatorLocked()
	gen := w.paintGen
	cfg := w.configGen
	size := w.size
	state := w.petRuntimeState
	if state == "" {
		state = string(petpack.StateIdle)
	}
	if w.petQuietMode {
		state = string(petpack.StateQuiet)
	}
	pixels := w.pixelSizeLocked()
	staticPose := w.petQuietMode || w.petReducedMotion || !w.petMotionEnabled
	skipRender := staticPose && w.staticReady &&
		w.staticSkin == w.petSkin && w.staticVariant == w.petVariant &&
		w.staticState == state && w.staticPixels == pixels
	skin := w.petSkin
	variant := w.petVariant
	w.mu.Unlock()

	now := time.Now()
	var framePix []byte
	var frameW, frameH, frameStride int
	var frameOK bool
	if !skipRender {
		framePix, frameW, frameH, frameStride, frameOK = nrgbaBytes(w.anim.Render(now))
	}
	w.mu.Lock()
	stale := !w.created || w.paintGen != gen || w.configGen != cfg
	w.mu.Unlock()
	if stale {
		return false
	}

	maxX, maxY := 0, 0
	if size >= 1 {
		maxX = getScreenWidth() - size
		maxY = getScreenHeight() - size
	}
	nx, ny, moved := w.anim.Advance(now, C.darwinPetDragActive() != 0, maxX, maxY)
	faceLeft := w.anim.FacingLeft()

	w.mu.Lock()
	if !w.created || w.paintGen != gen || w.configGen != cfg {
		w.mu.Unlock()
		return true
	}
	sendPixels := false
	sendFace := false
	if frameOK {
		sig := petFrameSig(framePix)
		same := w.haveFrameSig && w.lastFrameSig == sig
		if !same {
			w.lastFrameSig = sig
			w.haveFrameSig = true
			w.lastFaceLeft = faceLeft
			sendPixels = true
		} else if w.lastFaceLeft != faceLeft {
			w.lastFaceLeft = faceLeft
			sendFace = true
		}
	}
	if staticPose && (skipRender || sendPixels || frameOK) {
		w.staticReady = true
		w.staticSkin = skin
		w.staticVariant = variant
		w.staticState = state
		w.staticPixels = pixels
	} else if !staticPose {
		w.staticReady = false
	}
	w.mu.Unlock()

	genArg := C.uint(uint32(gen))
	if sendPixels {
		C.setPetPixelsDarwin(unsafe.Pointer(&framePix[0]), C.int(frameW), C.int(frameH), C.int(frameStride), C.int(size), C.int(size), darwinBool(faceLeft), genArg)
	} else if sendFace {
		C.setPetFaceDarwin(darwinBool(faceLeft), genArg)
	}
	if moved && size >= 1 {
		C.moveFloatingWindowDarwin(C.int(nx), C.int(ny), genArg)
	}
	return true
}

func darwinBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (w *darwinFloatingWindow) currentPNGLocked() []byte {
	w.pushAnimatorLocked()
	if png := encodeNRGBAToPNG(w.anim.Render(time.Now())); len(png) > 0 {
		return png
	}
	return desktopPetStillPNG(w.petSkin, w.petVariant, w.petRuntimeState, w.pixelSizeLocked(), w.packFrameCache, floatingLogoPNGDarwin)
}

func (w *darwinFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	if w.created {
		w.mu.Unlock()
		return nil
	}
	w.size = width
	w.petRuntimeState = string(petpack.StateIdle)
	w.petStateDeadline = time.Time{}
	w.packFrameCache = petpack.NewFrameCache()
	w.anim.ResetAssets()
	w.invalidateFrameLocked()
	w.loadPetConfigLocked()
	w.anim.SetPos(x, y)
	png := w.currentPNGLocked()
	skin := w.petSkin
	w.mu.Unlock()
	if len(png) == 0 {
		return fmt.Errorf("darwin pet: no pack frame or logo PNG to paint")
	}
	if C.createFloatingWindowDarwin(C.int(x), C.int(y), C.int(width), C.int(height), unsafe.Pointer(&png[0]), C.int(len(png))) == 0 {
		return fmt.Errorf("darwin pet: window was not created")
	}
	w.mu.Lock()
	w.paintGen++
	gen := w.paintGen
	w.created = true
	setGlobalDarwinFloating(w)
	C.darwinSetPaintGen(C.uint(uint32(gen)))
	w.ensureAnimationLocked()
	w.mu.Unlock()
	log.Printf("[floating-window] macOS window created: pos=(%d,%d) size=(%d,%d) skin=%s png=%d", x, y, width, height, skin, len(png))
	return nil
}

// ReloadAppearance applies a new skin or size without destroying the panel.
func (w *darwinFloatingWindow) ReloadAppearance(x, y, size int) error {
	w.mu.Lock()
	if !w.created {
		w.mu.Unlock()
		return fmt.Errorf("darwin pet: window is not created")
	}
	w.size = size
	w.packFrameCache = petpack.NewFrameCache()
	w.anim.ResetAssets()
	w.invalidateFrameLocked()
	w.loadPetConfigLocked()
	w.anim.SetPos(x, y)
	w.pushAnimatorLocked()
	w.ensureAnimationLocked()
	gen := w.paintGen
	w.mu.Unlock()
	C.resizeFloatingWindowDarwin(C.int(size), C.int(size), C.uint(uint32(gen)))
	C.moveFloatingWindowDarwin(C.int(x), C.int(y), C.uint(uint32(gen)))
	w.paintAnimatedFrame()
	return nil
}

func (w *darwinFloatingWindow) Show() {
	w.mu.Lock()
	created := w.created
	w.mu.Unlock()
	if !created {
		return
	}
	C.showFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Hide() {
	w.mu.Lock()
	created := w.created
	w.mu.Unlock()
	if !created {
		return
	}
	C.hideFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Destroy() {
	w.mu.Lock()
	if !w.created {
		w.mu.Unlock()
		return
	}
	w.created = false
	w.paintGen++
	gen := w.paintGen
	w.stopAnimationLocked()
	clearGlobalDarwinFloating(w)
	C.darwinSetPaintGen(C.uint(uint32(gen)))
	w.mu.Unlock()
	C.destroyFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	w.anim.SetPos(x, y)
	created := w.created
	gen := w.paintGen
	w.mu.Unlock()
	if !created {
		return
	}
	C.moveFloatingWindowDarwin(C.int(x), C.int(y), C.uint(uint32(gen)))
}

func (w *darwinFloatingWindow) IsCreated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.created
}

func (w *darwinFloatingWindow) UpdateSoundConfig(soundEnabled bool, preset string) {
}

func (w *darwinFloatingWindow) UpdateMotionConfig(motionEnabled, quiet, reducedMotion bool, interactionMode, skin, variant string) {
	w.mu.Lock()
	w.petMotionEnabled = motionEnabled && !reducedMotion
	w.petQuietMode = quiet
	w.petReducedMotion = reducedMotion
	if interactionMode != "" {
		w.petInteraction = interactionMode
	}
	changed := false
	if skin != "" && skin != w.petSkin {
		w.petSkin = skin
		changed = true
	}
	if variant != "" {
		resolved := petpack.ResolveVariantForRuntime(variant)
		if resolved != w.petVariant {
			w.petVariant = resolved
			changed = true
		}
	}
	if changed {
		w.packFrameCache = petpack.NewFrameCache()
		w.anim.ResetAssets()
	}
	w.invalidateFrameLocked()
	w.pushAnimatorLocked()
	created := w.created
	w.mu.Unlock()
	if created {
		w.paintAnimatedFrame()
	}
}

func (w *darwinFloatingWindow) InvalidatePetPackAssets() {
	w.mu.Lock()
	w.packFrameCache = petpack.NewFrameCache()
	w.anim.ResetAssets()
	w.invalidateFrameLocked()
	created := w.created
	w.mu.Unlock()
	if created {
		w.paintAnimatedFrame()
	}
}

func (w *darwinFloatingWindow) SetPetRuntimeState(state string, ttlMs int) {
	w.mu.Lock()
	next := string(petpack.NormalizeState(state))
	if next == w.petRuntimeState && ttlMs == 0 && w.petStateDeadline.IsZero() {
		w.mu.Unlock()
		return
	}
	w.petRuntimeState = next
	if ttlMs > 0 {
		w.petStateDeadline = time.Now().Add(time.Duration(ttlMs) * time.Millisecond)
	} else {
		w.petStateDeadline = time.Time{}
	}
	w.invalidateFrameLocked()
	created := w.created
	w.mu.Unlock()
	if created {
		w.paintAnimatedFrame()
	}
}

func (w *darwinFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.petRuntimeState = string(petpack.StateIdle)
		w.petStateDeadline = time.Time{}
	}
	if w.petRuntimeState == "" {
		return string(petpack.StateIdle)
	}
	return w.petRuntimeState
}

func (w *darwinFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	w.mu.Lock()
	w.pushAnimatorLocked()
	w.mu.Unlock()
	return w.anim.RuntimeLevel(declared)
}

//export maclawDarwinPetClicked
func maclawDarwinPetClicked() {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		return
	}
	go w.app.onFloatingButtonClicked()
}

//export maclawDarwinPetDragged
func maclawDarwinPetDragged(x, y C.int) {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		C.darwinPetDragClear()
		return
	}
	xi, yi := int(x), int(y)
	app := w.app
	go func() {
		defer C.darwinPetDragClear()
		app.onFloatingButtonDragged(xi, yi)
	}()
}

//export maclawDarwinPetMenu
func maclawDarwinPetMenu(action C.int) {
	w := currentDarwinFloating()
	if w == nil || w.app == nil {
		return
	}
	switch int(action) {
	case 1:
		go w.app.openPetSettingsFromMenu()
	case 2:
		go w.app.DisablePetFromMenu()
	case 3:
		go w.app.QuitApp()
	case 4:
		go w.app.showMainWindowFromPet()
	case 5:
		go w.app.openPetConversationFromMenu()
	}
}
