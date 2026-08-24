package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

const computerUseCropMargin = 8

type computerUseCapture struct {
	PNG    string
	Meta   computeruse.ScreenMeta
	Width  int
	Height int
}

func captureComputerUseScreen(screenIdx int, windowHint string, cropFocused bool) (computerUseCapture, error) {
	crop, cropOK := resolveObserveWindowCrop(windowHint, cropFocused && screenIdx >= 0)
	if cropOK && runtime.GOOS == "windows" {
		if png, err := remote.NativeScreenshotRect(crop.X, crop.Y, crop.Width, crop.Height); err == nil && png != "" && !remote.IsBlankImage(png) {
			w, h := 0, 0
			if iw, ih, ok := decodeImageSizeB64(png); ok {
				w, h = iw, ih
			}
			meta := computeruse.ScreenMeta{ScreenIndex: screenIdx, CropTitle: crop.Title}
			computeruse.ApplyDisplayGeometry(&meta, crop.X, crop.Y, crop.Width, crop.Height, w, h)
			return computerUseCapture{PNG: png, Meta: meta, Width: w, Height: h}, nil
		}
	}

	png, err := captureDesktopScreenshot(screenIdx)
	if err != nil {
		return computerUseCapture{}, err
	}
	w, h := 0, 0
	if iw, ih, ok := decodeImageSizeB64(png); ok {
		w, h = iw, ih
	}
	meta := screenMetaFromCapture(screenIdx, w, h)
	if cropOK {
		if cropped, ok := cropCaptureToWindow(png, meta, crop); ok {
			return cropped, nil
		}
	}
	return computerUseCapture{PNG: png, Meta: meta, Width: w, Height: h}, nil
}

func resolveObserveWindowCrop(windowHint string, cropFocused bool) (accessibility.WindowBounds, bool) {
	if !cropFocused {
		return accessibility.WindowBounds{}, false
	}
	hint := strings.TrimSpace(windowHint)
	var b accessibility.WindowBounds
	var ok bool
	if hint != "" {
		b, ok = accessibility.NamedWindowBounds(hint)
	}
	if !ok {
		b, ok = accessibility.ForegroundWindowBounds()
	}
	if !ok || b.Width < 64 || b.Height < 64 {
		return accessibility.WindowBounds{}, false
	}
	return expandWindowCrop(b, computerUseCropMargin), true
}

func expandWindowCrop(b accessibility.WindowBounds, margin int) accessibility.WindowBounds {
	if margin < 0 {
		margin = 0
	}
	b.X -= margin
	b.Y -= margin
	b.Width += 2 * margin
	b.Height += 2 * margin
	if b.Width < 64 {
		b.Width = 64
	}
	if b.Height < 64 {
		b.Height = 64
	}
	if b.Width > 16384 {
		b.Width = 16384
	}
	if b.Height > 16384 {
		b.Height = 16384
	}
	return b
}

func cropCaptureToWindow(png string, meta computeruse.ScreenMeta, crop accessibility.WindowBounds) (computerUseCapture, bool) {
	ix, iy := computeruse.MapScreenToCapture(meta, crop.X, crop.Y)
	iw, ih := computeruse.ScaleSize(meta, crop.Width, crop.Height)
	cropped, err := cropPNGBase64(png, ix, iy, iw, ih)
	if err != nil || cropped == "" {
		return computerUseCapture{}, false
	}
	w, h := 0, 0
	if cw, ch, ok := decodeImageSizeB64(cropped); ok {
		w, h = cw, ch
	}
	if w < 64 || h < 64 {
		return computerUseCapture{}, false
	}
	out := computeruse.ScreenMeta{ScreenIndex: meta.ScreenIndex, CropTitle: crop.Title}
	computeruse.ApplyDisplayGeometry(&out, crop.X, crop.Y, crop.Width, crop.Height, w, h)
	return computerUseCapture{PNG: cropped, Meta: out, Width: w, Height: h}, true
}

func captureDesktopScreenshotNative(screenIndex int) (string, error) {
	if screenIndex < 0 {
		return remote.NativeScreenshotVirtual()
	}
	displays, err := remote.EnumDisplays()
	if err == nil && screenIndex >= 0 && screenIndex < len(displays) {
		d := displays[screenIndex]
		if d.Width > 0 && d.Height > 0 {
			return remote.NativeScreenshotRect(d.X, d.Y, d.Width, d.Height)
		}
	}
	return remote.NativeScreenshot()
}

func captureSettleScreenshot() (string, error) {
	last := cuSession().LastObserve()
	if last != nil && last.Meta.Width > 0 && last.Meta.Height > 0 {
		x1, y1 := computeruse.MapCaptureToScreen(last.Meta, 0, 0)
		x2, y2 := computeruse.MapCaptureToScreen(last.Meta, last.Meta.Width, last.Meta.Height)
		w, h := x2-x1, y2-y1
		if w > 0 && h > 0 && runtime.GOOS == "windows" {
			if png, err := remote.NativeScreenshotRect(x1, y1, w, h); err == nil && png != "" && !remote.IsBlankImage(png) {
				return png, nil
			}
		}
		idx := last.Meta.ScreenIndex
		return captureDesktopScreenshot(idx)
	}
	return captureDesktopScreenshot(0)
}

func cuRequireSameWindow(sess *computeruse.Session) string {
	if sess == nil {
		return ""
	}
	last := sess.LastObserve()
	if last == nil || strings.TrimSpace(last.Meta.CropTitle) == "" {
		return ""
	}
	fg := accessibility.ForegroundWindowTitle()
	if computeruse.WindowTitlesMatch(last.Meta.CropTitle, fg) {
		return ""
	}
	sess.InvalidateRefs()
	return fmt.Sprintf("foreground window changed (observed %q, now %q); call computer_observe again", last.Meta.CropTitle, fg)
}
