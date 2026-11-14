package guiapp

import (
	"image"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// petAnimConfig is the slice of pet settings the desktop animator needs.
// Window code copies it in under its own lock; the animator then renders
// without holding that lock.
type petAnimConfig struct {
	Skin          string
	Variant       string
	State         string
	PixelSize     int
	WindowSize    int
	MotionEnabled bool
	Quiet         bool
	Reduced       bool
}

// petDesktopAnimator plays a character or skeleton pack and walks it across
// the desktop. Quiet mode, reduced motion, and motion-off stay on one still.
type petDesktopAnimator struct {
	mu              sync.Mutex
	skin            string
	variant         string
	state           string
	pixelSize       int
	windowSize      int
	motionEnabled   bool
	quiet           bool
	reduced         bool
	character       *petpack.CharacterRenderer
	characterKey    string
	characterFailed map[string]bool
	rig             *petpack.RigRenderer
	rigKey          string
	rigFailed       map[string]bool
	rigClock        time.Time
	cache           *petpack.FrameCache
	roam            petpack.DesktopRoam
	travelAt        time.Time
	x               int
	y               int
	still           *image.NRGBA
	stillSkin       string
	stillVariant    string
	stillState      string
	stillSize       int
}

func (a *petDesktopAnimator) Configure(cfg petAnimConfig) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	variant := petpack.ResolveVariantForRuntime(cfg.Variant)
	if cfg.Skin != a.skin || variant != a.variant {
		a.resetAssetsLocked()
	}
	a.skin = cfg.Skin
	a.variant = variant
	a.state = cfg.State
	if cfg.PixelSize > 0 {
		a.pixelSize = cfg.PixelSize
	}
	if cfg.WindowSize > 0 {
		a.windowSize = cfg.WindowSize
	}
	a.motionEnabled = cfg.MotionEnabled
	a.quiet = cfg.Quiet
	a.reduced = cfg.Reduced
	if a.cache == nil {
		a.cache = petpack.NewFrameCache()
	}
}

func (a *petDesktopAnimator) ResetAssets() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.resetAssetsLocked()
	a.mu.Unlock()
}

func (a *petDesktopAnimator) resetAssetsLocked() {
	a.character = nil
	a.characterKey = ""
	a.characterFailed = nil
	a.rig = nil
	a.rigKey = ""
	a.rigFailed = nil
	a.rigClock = time.Time{}
	a.cache = petpack.NewFrameCache()
	a.roam = petpack.DesktopRoam{}
	a.travelAt = time.Time{}
	a.still = nil
	a.stillSkin = ""
	a.stillVariant = ""
	a.stillState = ""
	a.stillSize = 0
}

func (a *petDesktopAnimator) SetPos(x, y int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.x, a.y = x, y
	a.mu.Unlock()
}

func (a *petDesktopAnimator) staticLocked() bool {
	return a.quiet || a.reduced || !a.motionEnabled
}

// Render draws the current pose. Character and skeleton packs advance with
// now. A still pack, or a frozen motion mode, returns the pack's static frame.
// Pixels stay right-facing. The host flips the window when FacingLeft is set.
// The animator lock is not held across the rasterizer, so a skin change can
// drop the old renderer while a frame is still drawing.
func (a *petDesktopAnimator) Render(now time.Time) *image.NRGBA {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	skin := a.skin
	if skin == "" {
		skin = petpack.DefaultPackID
	}
	variant := petpack.ResolveVariantForRuntime(a.variant)
	state := petpack.NormalizeState(a.state)
	if a.quiet {
		state = petpack.StateQuiet
	}
	sz := a.pixelSize
	if sz < minPetSize {
		sz = defaultPetSize
	}
	if a.staticLocked() {
		frame := a.staticFrameLocked(skin, variant, string(state), sz)
		a.mu.Unlock()
		return frame
	}
	a.still = nil
	if renderer := a.characterRendererLocked(skin, variant); renderer != nil {
		a.mu.Unlock()
		return renderer.RenderState(state, now.UnixMilli(), sz)
	}
	if renderer, elapsed := a.rigRendererLocked(skin, variant, now); renderer != nil {
		a.mu.Unlock()
		return renderer.Render(state, elapsed, sz)
	}
	frame := tryLoadPackFrame(skin, variant, string(state), sz, a.cache)
	a.mu.Unlock()
	return frame
}

// darwinRenderScale is the pixel multiplier for a macOS pet. The window stays
// in points. 3x rasters and uploads are too heavy for a 20fps character, so
// the render stops at 2x.
func darwinRenderScale(screen int) int {
	if screen < 1 {
		return 1
	}
	if screen > 2 {
		return 2
	}
	return screen
}

func (a *petDesktopAnimator) staticFrameLocked(skin, variant, state string, sz int) *image.NRGBA {
	if a.still != nil && a.stillSize == sz && a.stillSkin == skin && a.stillVariant == variant && a.stillState == state {
		return a.still
	}
	frame := tryLoadPackFrame(skin, variant, state, sz, a.cache)
	if frame == nil {
		return nil
	}
	a.still = frame
	a.stillSkin = skin
	a.stillVariant = variant
	a.stillState = state
	a.stillSize = sz
	return frame
}

// FacingLeft reports the roam heading's horizontal facing.
func (a *petDesktopAnimator) FacingLeft() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.roam.FaceLeft
}

func (a *petDesktopAnimator) characterRendererLocked(skin, variant string) *petpack.CharacterRenderer {
	key := skin + "\x00" + variant
	if a.characterFailed[key] {
		return nil
	}
	if a.character != nil && a.characterKey == key {
		return a.character
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil
	}
	resolved, err := reg.Resolve(skin, variant)
	if err != nil || resolved == nil || resolved.Renderer != petpack.RendererCharacter {
		return nil
	}
	renderer, err := petpack.NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		if a.characterFailed == nil {
			a.characterFailed = make(map[string]bool)
		}
		a.characterFailed[key] = true
		return nil
	}
	a.character = renderer
	a.characterKey = key
	return renderer
}

func (a *petDesktopAnimator) rigRendererLocked(skin, variant string, now time.Time) (*petpack.RigRenderer, int) {
	key := skin + "\x00" + variant
	if a.rigFailed[key] {
		return nil, 0
	}
	if a.rig == nil || a.rigKey != key {
		reg := petpack.EnsureGlobal()
		if reg == nil {
			return nil, 0
		}
		resolved, err := reg.Resolve(skin, variant)
		if err != nil || resolved == nil || resolved.Renderer != petpack.RendererSkeleton {
			return nil, 0
		}
		renderer, err := petpack.NewRigRenderer(resolved)
		if err != nil || renderer == nil {
			if a.rigFailed == nil {
				a.rigFailed = make(map[string]bool)
			}
			a.rigFailed[key] = true
			return nil, 0
		}
		a.rig = renderer
		a.rigKey = key
		a.rigClock = now
	}
	elapsed := int(now.Sub(a.rigClock).Milliseconds())
	if a.rigClock.IsZero() || elapsed < 0 {
		elapsed = 0
	}
	return a.rig, elapsed
}

// Advance moves the window from the active clip's speed. Dragging, a still
// pose, and frozen motion modes leave the position unchanged. maxX and maxY
// are the inclusive travel limits.
func (a *petDesktopAnimator) Advance(now time.Time, dragging bool, maxX, maxY int) (x, y int, moved bool) {
	if a == nil {
		return 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	if dragging || a.staticLocked() {
		a.travelAt = now
		a.roam.RemX, a.roam.RemY = 0, 0
		return a.x, a.y, false
	}
	vx, vy := 0.0, 0.0
	if a.character != nil {
		vx, vy = a.character.DesktopTravelDesignPerMS(now.UnixMilli())
	}
	dt := now.Sub(a.travelAt)
	if a.travelAt.IsZero() {
		dt = 0
	}
	a.travelAt = now
	scale := 1.0
	if a.windowSize > 0 {
		scale = float64(a.windowSize) / 256
	}
	next, nx, ny := petpack.StepDesktopRoam(a.roam, vx, vy, scale, dt, a.x, a.y, 0, maxX, 0, maxY, now)
	a.roam = next
	moved = nx != a.x || ny != a.y
	a.x, a.y = nx, ny
	return nx, ny, moved
}

// RuntimeLevel reports the renderer the animator can actually play.
func (a *petDesktopAnimator) RuntimeLevel(declared string) (string, string) {
	if a == nil {
		return declared, ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	staticReason := ""
	switch {
	case a.quiet:
		staticReason = "安静模式已启用，动画已暂停"
	case a.reduced:
		staticReason = "系统“减少动态效果”已开启，动画已停用"
	case !a.motionEnabled:
		staticReason = "宠物动画已关闭"
	}
	if staticReason != "" {
		if declared == petpack.RendererNative {
			return declared, ""
		}
		return petpack.RendererNative, staticReason
	}
	key := a.skin + "\x00" + petpack.ResolveVariantForRuntime(a.variant)
	switch declared {
	case petpack.RendererCharacter:
		if a.characterFailed[key] {
			return petpack.RendererNative, "角色动画加载失败，已回退到静态图像"
		}
	case petpack.RendererSkeleton:
		if a.rigFailed[key] {
			return petpack.RendererNative, "骨骼动画加载失败，已回退到静态图像"
		}
	}
	return declared, ""
}

// petFrameSig covers every byte of a painted frame. Identical stills share a
// signature so the host can skip uploading them. A stride sample misses idle
// motion that only touches the gaps, and the pet stays on the previous pose.
func petFrameSig(pix []byte) uint64 {
	if len(pix) == 0 {
		return 0
	}
	const prime = 1099511628211
	h := uint64(len(pix))
	i := 0
	for ; i+4 <= len(pix); i += 4 {
		v := uint64(pix[i]) | uint64(pix[i+1])<<8 | uint64(pix[i+2])<<16 | uint64(pix[i+3])<<24
		h = h*prime + v
	}
	for ; i < len(pix); i++ {
		h = h*prime + uint64(pix[i])
	}
	return h
}

// nrgbaBytes returns the packed view of frame. pix points at the first pixel
// and stride is the row step in bytes.
func nrgbaBytes(frame *image.NRGBA) (pix []byte, width, height, stride int, ok bool) {
	if frame == nil {
		return nil, 0, 0, 0, false
	}
	b := frame.Bounds()
	width, height = b.Dx(), b.Dy()
	stride = frame.Stride
	if width < 1 || height < 1 || stride < width*4 {
		return nil, 0, 0, 0, false
	}
	offset := frame.PixOffset(b.Min.X, b.Min.Y)
	need := offset + (height-1)*stride + width*4
	if offset < 0 || need > len(frame.Pix) {
		return nil, 0, 0, 0, false
	}
	return frame.Pix[offset:], width, height, stride, true
}
