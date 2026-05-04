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
	tpmReturncmd = 0x0100

	menuIdHide = 1001
	menuIdQuit = 1002

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
	Size   int
	Skin   string
	Mode   string
	Bucket int
}

const petAnimationFrameBuckets = 72

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

	// Halo animation
	haloPhase          float64 // 0..2*pi, advances each timer tick
	petEnabled         bool
	petMotionEnabled   bool
	petMotionSound     bool
	lastPetSoundBucket int
	petQuietMode       bool
	petInteractionMode string
	petSkin            string

	// Pre-rendered base image (logo + circle clip, without halo)
	baseImg *image.NRGBA

	// Quantized pet animation frames avoid rerasterizing the supersampled pet every timer tick.
	petFrameCache map[petFrameCacheKey]*image.NRGBA

	// Pre-computed distance from center for each pixel (avoids sqrt per frame)
	distMap []float64

	// Stop signal for the message loop
	stopCh chan struct{}
}

func newFloatingWindow(app *App) floatingWindow {
	return &windowsFloatingWindow{app: app}
}

var globalFloatingWin *windowsFloatingWindow

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
	petInteractionMode := "balanced"
	petSkin := "clawmate"
	if w.app != nil {
		if cfg, err := w.app.LoadConfig(); err == nil {
			petEnabled = cfg.PetEnabled
			petQuietMode = cfg.PetQuietMode
			petMotionEnabled = isPetMotionEnabled(cfg)
			petMotionSound = petMotionSoundEnabled(cfg)
			if cfg.PetInteractionMode != "" {
				petInteractionMode = cfg.PetInteractionMode
			}
			if cfg.PetSkin != "" {
				petSkin = cfg.PetSkin
			}
		}
	}

	base, err := renderFloatingBase(sz, petEnabled, petSkin)
	if err != nil {
		return fmt.Errorf("renderFloatingBase: %w", err)
	}

	w.mu.Lock()
	w.baseImg = base
	w.size = sz
	w.haloPhase = 0
	w.petEnabled = petEnabled
	w.petMotionEnabled = petMotionEnabled
	w.petMotionSound = petMotionSound
	w.lastPetSoundBucket = 0
	w.petQuietMode = petQuietMode
	w.petInteractionMode = petInteractionMode
	w.petSkin = petSkin
	w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
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
	globalFloatingWin = w
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
			if globalFloatingWin == w {
				globalFloatingWin = nil
			}
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

		// Start halo animation timer (50ms = 20fps).
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
			if globalFloatingWin == w {
				globalFloatingWin = nil
			}
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

func renderFloatingBase(sz int, petEnabled bool, petSkin string) (*image.NRGBA, error) {
	if petEnabled {
		return renderClawMatePet(sz, petSkin), nil
	}
	return renderCircularLogo(sz)
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
	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
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
	antBaseX, antBaseY := rotatePetPoint(headCx, headCy, headCx, headCy-headR+float64(sz)*0.02, pose.HeadTilt)
	antTipX, antTipY := rotatePetPoint(headCx, headCy, headCx, headCy-headR-float64(sz)*0.11, pose.HeadTilt)
	barLeftX, barLeftY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.1, headCy-headR-float64(sz)*0.06, pose.HeadTilt)
	barRightX, barRightY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.1, headCy-headR-float64(sz)*0.06, pose.HeadTilt)
	drawLine(out, int(antTipX), int(antTipY), int(antBaseX), int(antBaseY), accent, int(math.Max(3, float64(sz)*0.04)))
	drawLine(out, int(barLeftX), int(barLeftY), int(barRightX), int(barRightY), accent, int(math.Max(3, float64(sz)*0.04)))

	switch skin {
	case "mini-claw":
		drawLine(out, int(bodyCx-float64(sz)*0.22), int(bodyY+float64(sz)*0.18), int(bodyCx+float64(sz)*0.22), int(bodyY+float64(sz)*0.18), accent, int(math.Max(3, float64(sz)*0.05)))
	case "dev-claw":
		visorLeftX, visorLeftY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.28, headCy-float64(sz)*0.03, pose.HeadTilt)
		visorRightX, visorRightY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.28, headCy-float64(sz)*0.03, pose.HeadTilt)
		drawLine(out, int(visorLeftX), int(visorLeftY), int(visorRightX), int(visorRightY), color.NRGBA{R: 15, G: 23, B: 42, A: 245}, int(math.Max(4, float64(sz)*0.08)))
		drawLine(out, int(bodyCx-float64(sz)*0.12), int(bodyY), int(bodyCx-float64(sz)*0.03), int(bodyY+float64(sz)*0.07), accent, int(math.Max(2, float64(sz)*0.03)))
		drawLine(out, int(bodyCx+float64(sz)*0.12), int(bodyY), int(bodyCx+float64(sz)*0.03), int(bodyY+float64(sz)*0.07), accent, int(math.Max(2, float64(sz)*0.03)))
	case "focus-claw":
		browLeftAX, browLeftAY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.17, headCy-float64(sz)*0.02, pose.HeadTilt)
		browLeftBX, browLeftBY := rotatePetPoint(headCx, headCy, headCx-float64(sz)*0.07, headCy-float64(sz)*0.02, pose.HeadTilt)
		browRightAX, browRightAY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.07, headCy-float64(sz)*0.02, pose.HeadTilt)
		browRightBX, browRightBY := rotatePetPoint(headCx, headCy, headCx+float64(sz)*0.17, headCy-float64(sz)*0.02, pose.HeadTilt)
		drawLine(out, int(browLeftAX), int(browLeftAY), int(browLeftBX), int(browLeftBY), eye, int(math.Max(3, float64(sz)*0.04)))
		drawLine(out, int(browRightAX), int(browRightAY), int(browRightBX), int(browRightBY), eye, int(math.Max(3, float64(sz)*0.04)))
	}
	return out
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

func (w *windowsFloatingWindow) cachedPetFrame(sz int, skin, interactionMode string, phase float64) *image.NRGBA {
	if skin == "" {
		skin = "clawmate"
	}
	if interactionMode == "" {
		interactionMode = "balanced"
	}
	phase = math.Mod(phase, 2*math.Pi)
	if phase < 0 {
		phase += 2 * math.Pi
	}
	bucket := int(math.Round(phase/(2*math.Pi)*petAnimationFrameBuckets)) % petAnimationFrameBuckets
	key := petFrameCacheKey{Size: sz, Skin: skin, Mode: interactionMode, Bucket: bucket}

	w.mu.Lock()
	if frame := w.petFrameCache[key]; frame != nil {
		w.mu.Unlock()
		return frame
	}
	w.mu.Unlock()

	bucketPhase := float64(bucket) / float64(petAnimationFrameBuckets) * 2 * math.Pi
	frame := renderClawMatePetWithPose(sz, skin, petFacePoseForPhase(bucketPhase, interactionMode))

	w.mu.Lock()
	if w.petFrameCache == nil || len(w.petFrameCache) > petAnimationFrameBuckets*6 {
		w.petFrameCache = make(map[petFrameCacheKey]*image.NRGBA)
	}
	if cached := w.petFrameCache[key]; cached != nil {
		w.mu.Unlock()
		return cached
	}
	w.petFrameCache[key] = frame
	w.mu.Unlock()
	return frame
}

func playPetMotionSound(interactionMode, skin string) {
	go func() {
		if interactionMode == "quiet" {
			return
		}
		type petTone struct {
			hz uintptr
			ms uintptr
		}
		toneSet := []petTone{{880, 16}, {1175, 20}}
		switch skin {
		case "mini-claw":
			toneSet = []petTone{{1175, 12}, {1568, 16}, {1760, 12}}
		case "dev-claw":
			toneSet = []petTone{{988, 12}, {740, 14}, {1175, 16}}
		case "focus-claw":
			toneSet = []petTone{{659, 14}, {880, 16}}
		}
		pitch := 1.0
		if interactionMode == "active" {
			pitch = 1.12
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
	petInteractionMode := w.petInteractionMode
	petSkin := w.petSkin
	w.mu.Unlock()
	if playPetSound {
		playPetMotionSound(petInteractionMode, petSkin)
	}

	if hwnd == 0 || base == nil || distMap == nil {
		return
	}

	sz := w.currentSize()
	frame := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	if petEnabled && petMotionEnabled && !petQuietMode {
		petScale := 1.0 + 0.018*math.Sin(phase)
		petYOffset := math.Sin(phase+math.Pi/5) * float64(sz) * 0.012
		if petInteractionMode == "active" {
			petScale = 1.0 + 0.026*math.Sin(phase*1.35)
			petYOffset = math.Sin(phase*1.35+math.Pi/5) * float64(sz) * 0.018
		} else if petInteractionMode == "quiet" {
			petScale = 1.0 + 0.01*math.Sin(phase*0.75)
			petYOffset = math.Sin(phase*0.75+math.Pi/5) * float64(sz) * 0.006
		}
		petFrame := w.cachedPetFrame(sz, petSkin, petInteractionMode, phase)
		renderAnimatedPetFrame(frame, petFrame, petScale, petYOffset)
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

func renderAnimatedPetFrame(dst, src *image.NRGBA, scale, offsetY float64) {
	b := dst.Bounds()
	sz := b.Dx()
	if sz <= 0 || src == nil {
		return
	}
	target := int(math.Round(float64(sz) * scale))
	if target < 1 {
		target = 1
	}
	x := (sz - target) / 2
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
	xdraw.CatmullRom.Scale(dst, rect, src, src.Bounds(), xdraw.Over, nil)
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
	w := globalFloatingWin

	switch msg {
	case wmNchittest:
		return 1 // HTCLIENT

	case wmTimer:
		if w != nil && wParam == timerIdHalo {
			w.mu.Lock()
			w.haloPhase += 0.15 // ~2s full cycle at 20fps
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
		return 0

	case wmMousemove:
		if w == nil || !w.dragging {
			break
		}
		var pt point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		dx := pt.X - w.dragStartX
		dy := pt.Y - w.dragStartY
		if !w.dragMoved {
			if abs32(dx) > 5 || abs32(dy) > 5 {
				w.dragMoved = true
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
		if !w.dragMoved {
			go func() {
				if w.app != nil {
					w.app.OnFloatingButtonClicked()
				}
			}()
		} else {
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
		hideText, _ := syscall.UTF16PtrFromString("\u9690\u85cf")
		quitText, _ := syscall.UTF16PtrFromString("\u9000\u51fa")
		procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(menuIdHide), uintptr(unsafe.Pointer(hideText)))
		procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(menuIdQuit), uintptr(unsafe.Pointer(quitText)))
		procSetForegroundWindow.Call(hwnd)
		cmd, _, _ := procTrackPopupMenu.Call(hMenu, uintptr(tpmReturncmd), uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
		procDestroyMenu.Call(hMenu)
		switch cmd {
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
				if globalFloatingWin == w {
					globalFloatingWin = nil
				}
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
