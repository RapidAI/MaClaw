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
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

//go:embed build/appicon.png
var floatingLogoPNGDarwin []byte

// darwinFloatingWindow is the macOS desktop pet. It shows a pack still on a
// clear window and can be dragged. Full skeleton/character animation remains
// a Windows path; state changes swap the matching still.
type darwinFloatingWindow struct {
	app              *App
	created          bool
	mu               sync.Mutex
	size             int
	petSkin          string
	petVariant       string
	petRuntimeState  string
	petStateDeadline time.Time
	packFrameCache   *petpack.FrameCache
}

func newFloatingWindow(app *App) floatingWindow {
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

func (w *darwinFloatingWindow) petPNGBytesLocked() []byte {
	sz := w.size
	scale := int(C.darwinBackingScale())
	if scale < 1 {
		scale = 1
	}
	if scale > 3 {
		scale = 3
	}
	if sz > 0 {
		sz *= scale
	}
	return desktopPetStillPNG(w.petSkin, w.petVariant, w.petRuntimeState, sz, w.packFrameCache, floatingLogoPNGDarwin)
}

func (w *darwinFloatingWindow) applyPetImageLocked() {
	if !w.created {
		return
	}
	png := w.petPNGBytesLocked()
	if len(png) == 0 {
		return
	}
	C.setPetImageDarwin(unsafe.Pointer(&png[0]), C.int(len(png)))
}

func (w *darwinFloatingWindow) Create(x, y, width, height int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.created {
		return nil
	}

	w.size = width
	w.petSkin = petpack.DefaultPackID
	w.petVariant = petpack.VariantDefault
	w.petRuntimeState = string(petpack.StateIdle)
	w.petStateDeadline = time.Time{}
	w.packFrameCache = petpack.NewFrameCache()
	if w.app != nil {
		if cfg, err := w.app.LoadConfig(); err == nil {
			if cfg.PetSkin != "" {
				w.petSkin = cfg.PetSkin
			}
			w.petVariant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
		}
	}

	png := w.petPNGBytesLocked()
	if len(png) == 0 {
		return fmt.Errorf("darwin pet: no pack frame or logo PNG to paint")
	}
	if C.createFloatingWindowDarwin(C.int(x), C.int(y), C.int(width), C.int(height), unsafe.Pointer(&png[0]), C.int(len(png))) == 0 {
		return fmt.Errorf("darwin pet: window was not created")
	}
	w.created = true
	setGlobalDarwinFloating(w)
	log.Printf("[floating-window] macOS window created: pos=(%d,%d) size=(%d,%d) skin=%s png=%d", x, y, width, height, w.petSkin, len(png))
	return nil
}

func (w *darwinFloatingWindow) Show() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.showFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Hide() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.hideFloatingWindowDarwin()
}

func (w *darwinFloatingWindow) Destroy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	clearGlobalDarwinFloating(w)
	C.destroyFloatingWindowDarwin()
	w.created = false
}

func (w *darwinFloatingWindow) MoveTo(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.created {
		return
	}
	C.moveFloatingWindowDarwin(C.int(x), C.int(y))
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
	defer w.mu.Unlock()
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
		w.applyPetImageLocked()
	}
}

func (w *darwinFloatingWindow) InvalidatePetPackAssets() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.packFrameCache = petpack.NewFrameCache()
	w.applyPetImageLocked()
}

func (w *darwinFloatingWindow) SetPetRuntimeState(state string, ttlMs int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := string(petpack.NormalizeState(state))
	if next == w.petRuntimeState && ttlMs == 0 && w.petStateDeadline.IsZero() {
		return
	}
	w.petRuntimeState = next
	if ttlMs > 0 {
		w.petStateDeadline = time.Now().Add(time.Duration(ttlMs) * time.Millisecond)
	} else {
		w.petStateDeadline = time.Time{}
	}
	w.applyPetImageLocked()
}

func (w *darwinFloatingWindow) CurrentPetRuntimeState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.petStateDeadline.IsZero() && time.Now().After(w.petStateDeadline) {
		w.petRuntimeState = string(petpack.StateIdle)
		w.petStateDeadline = time.Time{}
		w.applyPetImageLocked()
	}
	if w.petRuntimeState == "" {
		return string(petpack.StateIdle)
	}
	return w.petRuntimeState
}

func (w *darwinFloatingWindow) PetPackRuntimeLevel(declared string) (string, string) {
	if declared == petpack.RendererNative {
		return declared, ""
	}
	return petpack.RendererNative, "当前平台暂不支持宠物动画，仅显示静态图像"
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
		return
	}
	go w.app.onFloatingButtonDragged(int(x), int(y))
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
