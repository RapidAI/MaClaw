//go:build windows

package guiapp

import (
	"bytes"
	"encoding/base64"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestComputerUseHaloStripBitmapHasColor(t *testing.T) {
	if !haloInstall(760, 520, false) {
		t.Fatal("install")
	}
	t.Cleanup(func() { haloReleaseSurface() })
	names := []string{"top", "bottom", "left", "right"}
	for i, name := range names {
		s := haloStrips[i]
		if s.bits == nil || s.w < 8 || s.h < 8 {
			t.Fatalf("%s surface %+v", name, s)
		}
		buf := unsafe.Slice((*byte)(s.bits), s.w*s.h*4)
		best := 0
		for p := 3; p < len(buf); p += 4 {
			if int(buf[p]) > best {
				best = int(buf[p])
			}
		}
		t.Logf("%s alpha %d size %dx%d", name, best, s.w, s.h)
		if best < 80 {
			t.Fatalf("%s alpha %d", name, best)
		}
	}
}

// TestComputerUseHaloPreviewOnNotepad flashes the real overlay on a private
// Notepad window. It is opt-in so the suite does not paint on the desktop.
func TestComputerUseHaloPreviewOnNotepad(t *testing.T) {
	if os.Getenv("MACLAW_HALO_PREVIEW") != "1" {
		t.Skip("set MACLAW_HALO_PREVIEW=1 to draw the halo on a Notepad window")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "maclaw-halo-preview.txt")
	if err := os.WriteFile(path, []byte("halo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("notepad.exe", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		hideComputerUseWindowHalo()
		// Let the overlay thread hide the strips before a later test
		// touches the strip handles.
		time.Sleep(300 * time.Millisecond)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		computerUseHaloMu.Lock()
		computerUseHaloForceLive = false
		computerUseHaloSkipAffinity = false
		computerUseHaloMu.Unlock()
	})

	// Store Notepad titles the file ("maclaw-halo-preview.txt - Notepad").
	// The matcher treats that file name as the document, not a substring.
	const hint = "maclaw-halo-preview.txt"
	var id uint64
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		id = accessibility.NamedWindowID(hint)
		if id != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if id == 0 {
		t.Fatal("notepad window did not appear")
	}
	t.Cleanup(func() { killWindowProcess(id) })
	_ = accessibility.FocusWindow(hint)
	placeWindow(uintptr(id), 240, 160, 760, 520)
	time.Sleep(200 * time.Millisecond)

	computerUseHaloMu.Lock()
	computerUseHaloForceLive = true
	computerUseHaloSkipAffinity = true
	computerUseHaloMu.Unlock()
	noteComputerUseOperatedWindow(id)
	time.Sleep(900 * time.Millisecond)

	bounds, ok := accessibility.NamedWindowBounds(hint)
	if !ok {
		t.Fatal("notepad bounds unavailable")
	}
	margin := computerUseHaloGap + computerUseHaloBand
	png, err := remote.NativeScreenshotRect(bounds.X-margin, bounds.Y-margin, bounds.Width+2*margin, bounds.Height+2*margin)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(png)
	if err != nil {
		t.Fatal(err)
	}
	shotPath := filepath.Join(os.TempDir(), "maclaw-halo-preview.png")
	if err := os.WriteFile(shotPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("preview shot %s window %q %d,%d %dx%d", shotPath, bounds.Title, bounds.X, bounds.Y, bounds.Width, bounds.Height)

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() < margin*2+64 || b.Dy() < margin*2+64 {
		t.Fatalf("shot %v", b)
	}
	rim := maxBlueness(img, 0, margin, margin, b.Dy()-margin)
	center := maxBlueness(img, margin+bounds.Width/2-8, margin+bounds.Height/2-8, margin+bounds.Width/2+8, margin+bounds.Height/2+8)
	t.Logf("rim blueness %d center %d excluded %v", rim, center, haloExcluded.Load())
	if rim < center+25 {
		t.Fatalf("halo rim blueness %d is not above the window center %d (see %s)", rim, center, shotPath)
	}

	// The person sees the rim. Screen capture must not, or the model treats
	// the glow as part of the app. Every strip has to be excluded; one
	// leftover border would still land in the shot.
	if n, ok := setHaloStripAffinity(0x11); n != 4 {
		t.Fatalf("halo strips %d, want 4", n)
	} else if !ok {
		t.Log("capture exclusion unavailable; the outer gap still keeps the rim out of a window crop")
		return
	}
	time.Sleep(200 * time.Millisecond)
	png2, err := remote.NativeScreenshotRect(bounds.X-margin, bounds.Y-margin, bounds.Width+2*margin, bounds.Height+2*margin)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := base64.StdEncoding.DecodeString(png2)
	if err != nil {
		t.Fatal(err)
	}
	img2, _, err := image.Decode(bytes.NewReader(raw2))
	if err != nil {
		t.Fatal(err)
	}
	rim2 := maxBlueness(img2, 0, margin, margin, img2.Bounds().Dy()-margin)
	t.Logf("rim blueness after capture exclusion %d", rim2)
	// The desktop behind the rim can already be slightly blue. Exclusion
	// has to take a clear bite out of the glow, not zero the background.
	if rim-rim2 < 12 {
		excludedPath := filepath.Join(os.TempDir(), "maclaw-halo-preview-excluded.png")
		_ = os.WriteFile(excludedPath, raw2, 0o644)
		t.Fatalf("screen capture still includes the halo (visible %d excluded %d, see %s)", rim, rim2, excludedPath)
	}

	// Maximized windows have no outer pixels. The inset rim is drawn only
	// while capture exclusion is on; turn the flag on, then drop affinity
	// just long enough to photograph it.
	haloExcluded.Store(true)
	procShowWindowFloat.Call(uintptr(id), 3) // SW_MAXIMIZE
	time.Sleep(700 * time.Millisecond)
	if !haloZoomed(uintptr(id)) {
		t.Fatal("notepad did not maximize")
	}
	dx, dy, dw, dh, dok := haloDwmRect(uintptr(id))
	if !dok {
		t.Fatal("maximized frame bounds unavailable")
	}
	if _, ok := setHaloStripAffinity(0); !ok {
		t.Fatal("could not clear capture exclusion to photograph the inset rim")
	}
	time.Sleep(250 * time.Millisecond)
	inRim := shotMaxBlueness(t, dx+dw/2-20, dy+1, 40, 8)
	inCenter := shotMaxBlueness(t, dx+dw/2-8, dy+dh/2-8, 16, 16)
	t.Logf("inset rim %d center %d bounds %d,%d %dx%d", inRim, inCenter, dx, dy, dw, dh)
	if inRim < inCenter+15 {
		t.Fatalf("maximized inset rim blueness %d is not above the center %d", inRim, inCenter)
	}
	if _, ok := setHaloStripAffinity(0x11); ok {
		time.Sleep(200 * time.Millisecond)
		inRim2 := shotMaxBlueness(t, dx+dw/2-20, dy+1, 40, 8)
		t.Logf("inset rim after capture exclusion %d", inRim2)
		if inRim-inRim2 < 12 {
			t.Fatalf("maximized capture still includes the inset halo (visible %d excluded %d)", inRim, inRim2)
		}
	}
}

// setHaloStripAffinity sets WDA on every rim strip. A partial failure clears
// them all so a later shot cannot keep one border and drop the others.
func setHaloStripAffinity(affinity uintptr) (n int, ok bool) {
	setAffinity := syscall.NewLazyDLL("user32.dll").NewProc("SetWindowDisplayAffinity")
	var hwnds []uintptr
	computerUseHaloVisitWindows(func(hwnd uintptr) {
		hwnds = append(hwnds, hwnd)
	})
	n = len(hwnds)
	if n != haloStripCount {
		return n, false
	}
	ok = true
	for _, hwnd := range hwnds {
		if r, _, _ := setAffinity.Call(hwnd, affinity); r == 0 {
			ok = false
		}
	}
	if !ok && affinity != 0 {
		for _, hwnd := range hwnds {
			setAffinity.Call(hwnd, 0)
		}
	}
	return n, ok
}

func TestComputerUseHaloOverlayKeepsPreviousTarget(t *testing.T) {
	var saved [haloStripCount]uintptr
	for i := range haloStripHwnd {
		saved[i] = haloStripHwnd[i].Swap(0)
	}
	computerUseHaloMu.Lock()
	prevID := computerUseHaloID
	computerUseHaloID = 0
	computerUseHaloMu.Unlock()
	t.Cleanup(func() {
		for i := range haloStripHwnd {
			haloStripHwnd[i].Store(saved[i])
		}
		computerUseHaloMu.Lock()
		computerUseHaloID = prevID
		computerUseHaloMu.Unlock()
	})
	haloStripHwnd[0].Store(99)
	haloStripHwnd[2].Store(100)
	noteComputerUseOperatedWindow(42)
	noteComputerUseOperatedWindow(99)
	noteComputerUseOperatedWindow(100)
	if got := computerUseHaloTarget(); got != 42 {
		t.Fatalf("overlay replaced target %d", got)
	}
}

func shotMaxBlueness(t *testing.T, x, y, w, h int) int {
	t.Helper()
	png, err := remote.NativeScreenshotRect(x, y, w, h)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(png)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	return maxBlueness(img, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y)
}

func placeWindow(hwnd uintptr, x, y, w, h int) {
	setPos := syscall.NewLazyDLL("user32.dll").NewProc("SetWindowPos")
	const swpNoActivate = 0x0010
	setPos.Call(hwnd, 0, uintptr(int32(x)), uintptr(int32(y)), uintptr(w), uintptr(h), swpNoActivate)
}

func killWindowProcess(hwnd uint64) {
	if hwnd == 0 {
		return
	}
	var pid uint32
	proc := syscall.NewLazyDLL("user32.dll").NewProc("GetWindowThreadProcessId")
	proc.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return
	}
	p, err := os.FindProcess(int(pid))
	if err != nil || p == nil {
		return
	}
	_ = p.Kill()
}

func maxBlueness(img image.Image, x0, y0, x1, y1 int) int {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	best := -1000
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, _, bl, _ := img.At(x, y).RGBA()
			score := int(bl>>8) - int(r>>8)
			if score > best {
				best = score
			}
		}
	}
	return best
}
