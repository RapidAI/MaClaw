package guiapp

import (
	"image/color"
	"testing"
)

func TestComputerUseHaloRasterMatchesSDF(t *testing.T) {
	sizes := []struct{ w, h int }{{120, 80}, {64, 90}, {90, 64}, {200, 150}, {640, 80}}
	for _, inset := range []bool{false, true} {
		for _, sz := range sizes {
			img := computerUseHaloImage(sz.w, sz.h, inset)
			if img == nil {
				t.Fatalf("inset %v size %dx%d", inset, sz.w, sz.h)
			}
			margin := 0
			if !inset {
				margin = computerUseHaloGap + computerUseHaloBand
			}
			b := img.Bounds()
			for y := 0; y < b.Dy(); y++ {
				for x := 0; x < b.Dx(); x++ {
					wx := float64(x) + 0.5 - float64(margin)
					wy := float64(y) + 0.5 - float64(margin)
					sdf := computerUseHaloSDF(wx, wy, float64(sz.w), float64(sz.h), float64(computerUseHaloRadius))
					wr, wg, wb, wa := computerUseHaloSpanColor(sdf, inset)
					got := img.NRGBAAt(x, y)
					// Alpha truncated to 0 is invisible. Those pixels are left
					// unwritten so a zeroed bitmap stays clear.
					if wa == 0 {
						if got.A != 0 {
							t.Fatalf("inset %v %dx%d at %d,%d alpha %d, want clear", inset, sz.w, sz.h, x, y, got.A)
						}
						continue
					}
					if got.R != wr || got.G != wg || got.B != wb || got.A != wa {
						t.Fatalf("inset %v %dx%d at %d,%d got %+v want R %d G %d B %d A %d", inset, sz.w, sz.h, x, y, got, wr, wg, wb, wa)
					}
				}
			}
		}
	}
}

func TestComputerUseHaloStaysOutsideObserveCrop(t *testing.T) {
	if computerUseHaloGap <= computerUseCropMargin {
		t.Fatalf("halo gap %d overlaps observe crop margin %d", computerUseHaloGap, computerUseCropMargin)
	}
}

func TestComputerUseHaloImageLeavesWindowClear(t *testing.T) {
	const winW, winH = 120, 80
	img := computerUseHaloImage(winW, winH, false)
	if img == nil {
		t.Fatal("missing halo image")
	}
	margin := computerUseHaloGap + computerUseHaloBand
	if img.Bounds().Dx() != winW+2*margin || img.Bounds().Dy() != winH+2*margin {
		t.Fatalf("size %v", img.Bounds())
	}
	midY := margin + winH/2
	if a := img.NRGBAAt(margin+winW/2, midY).A; a != 0 {
		t.Fatalf("interior alpha %d", a)
	}
	if a := img.NRGBAAt(margin-2, midY).A; a != 0 {
		t.Fatalf("gap alpha %d", a)
	}
	peakX, peak := 0, color.NRGBA{}
	for x := 0; x < margin; x++ {
		c := img.NRGBAAt(x, midY)
		if c.A > peak.A {
			peak = c
			peakX = x
		}
	}
	if peak.A < 100 || !computerUseHaloIsLightBlue(peak) {
		t.Fatalf("peak at %d %+v", peakX, peak)
	}
	dist := float64(margin-peakX) - 0.5
	if dist < float64(computerUseHaloGap)-1 || dist > float64(computerUseHaloGap)+6 {
		t.Fatalf("peak sits %.1f px outside the window, want just past the %d px gap", dist, computerUseHaloGap)
	}
	if outer := img.NRGBAAt(0, midY); outer.A >= peak.A {
		t.Fatalf("outer alpha %d is not softer than peak %d", outer.A, peak.A)
	}
}

func TestComputerUseHaloInsetRimsTheInsideEdge(t *testing.T) {
	img := computerUseHaloImage(120, 80, true)
	if img == nil {
		t.Fatal("missing inset halo")
	}
	if img.Bounds().Dx() != 120 || img.Bounds().Dy() != 80 {
		t.Fatalf("size %v", img.Bounds())
	}
	edge := img.NRGBAAt(2, 40)
	if edge.A < 80 || !computerUseHaloIsLightBlue(edge) {
		t.Fatalf("inner edge %+v", edge)
	}
	if c := img.NRGBAAt(60, 40); c.A != 0 {
		t.Fatalf("inset interior alpha %d", c.A)
	}
}

func TestComputerUseHaloStripsMatchImage(t *testing.T) {
	sizes := []struct{ w, h int }{{120, 80}, {64, 90}, {640, 80}, {3840, 2090}}
	for _, inset := range []bool{false, true} {
		for _, sz := range sizes {
			img := computerUseHaloImage(sz.w, sz.h, inset)
			strips, ok := computerUseHaloStrips(sz.w, sz.h, inset)
			ow, oh, dimOK := computerUseHaloDims(sz.w, sz.h, inset)
			if img == nil || !ok || !dimOK {
				t.Fatalf("inset %v size %dx%d", inset, sz.w, sz.h)
			}
			limit := computerUseHaloLimit(inset)
			bufs := make([][]byte, len(strips))
			area := 0
			for i, s := range strips {
				if s.w <= 0 || s.h <= 0 {
					t.Fatalf("inset %v %dx%d strip %d %+v", inset, sz.w, sz.h, i, s)
				}
				bufs[i] = make([]byte, s.w*s.h*4)
				area += s.w * s.h
			}
			// The strips tile the border and leave the interior open. A
			// maximized frame's border is a small fraction of the window.
			interiorW, interiorH := sz.w, sz.h
			if inset {
				interiorW -= 2 * limit
				interiorH -= 2 * limit
			}
			if interiorW <= 0 || interiorH <= 0 || area+interiorW*interiorH != ow*oh {
				t.Fatalf("inset %v %dx%d strips cover %d of %d", inset, sz.w, sz.h, area, ow*oh)
			}
			if sz.w >= 1920 && area*8 > ow*oh {
				t.Fatalf("inset %v %dx%d strips cover %d of %d", inset, sz.w, sz.h, area, ow*oh)
			}
			computerUseHaloPaint(sz.w, sz.h, inset, func(x, y, n int, r, g, b, a uint8) {
				computerUseHaloProject(x, y, n, ow, oh, limit, func(strip, lx, ly, take int) {
					s := strips[strip]
					off := ly*s.w*4 + lx*4
					haloFill4(bufs[strip][off:off+take*4], take, r, g, b, a)
				})
			})
			for y := 0; y < oh; y++ {
				for x := 0; x < ow; x++ {
					got := img.NRGBAAt(x, y)
					strip, lx, ly, on := computerUseHaloStripIndex(x, y, ow, oh, limit)
					if !on {
						if got.A != 0 {
							t.Fatalf("inset %v %dx%d interior %d,%d alpha %d", inset, sz.w, sz.h, x, y, got.A)
						}
						continue
					}
					s := strips[strip]
					off := ly*s.w*4 + lx*4
					pix := bufs[strip][off : off+4]
					if got.A == 0 {
						if pix[3] != 0 {
							t.Fatalf("inset %v %dx%d clear %d,%d strip alpha %d", inset, sz.w, sz.h, x, y, pix[3])
						}
						continue
					}
					if pix[0] != got.R || pix[1] != got.G || pix[2] != got.B || pix[3] != got.A {
						t.Fatalf("inset %v %dx%d %d,%d strip R %d G %d B %d A %d want %+v", inset, sz.w, sz.h, x, y, pix[0], pix[1], pix[2], pix[3], got)
					}
				}
			}
		}
	}
}

func TestComputerUseHaloProjectStopsAtTheInterior(t *testing.T) {
	const ow, oh, limit = 100, 80, 10
	var wrote int
	computerUseHaloProject(0, 40, ow, ow, oh, limit, func(strip, lx, ly, n int) {
		wrote += n
		if strip != 2 || lx != 0 || ly != 30 || n != limit {
			t.Fatalf("strip %d local %d,%d n %d", strip, lx, ly, n)
		}
	})
	if wrote != limit {
		t.Fatalf("wrote %d, want %d", wrote, limit)
	}
}

func TestNamedWindowMissKeepsTheHalo(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	noteComputerUseOperatedWindow(42)
	computerUseForegroundIDFn = func() uint64 { return 99 }
	noteComputerUseNamedWindow("no-such-window-computer-use-zz")
	if got := computerUseHaloTarget(); got != 42 {
		t.Fatalf("missed name took the foreground halo=%d", got)
	}
	noteComputerUseNamedWindow("")
	if got := computerUseHaloTarget(); got != 99 {
		t.Fatalf("empty name should follow the foreground halo=%d", got)
	}
}

func TestComputerUseHaloSkipsOwnProcess(t *testing.T) {
	computerUseHaloMu.Lock()
	prevFn := computerUseHaloOwnsWindow
	prevID := computerUseHaloID
	computerUseHaloOwnsWindow = func(id uint64) bool { return id == 7 }
	computerUseHaloID = 0
	computerUseHaloMu.Unlock()
	t.Cleanup(func() {
		computerUseHaloMu.Lock()
		computerUseHaloOwnsWindow = prevFn
		computerUseHaloID = prevID
		computerUseHaloMu.Unlock()
	})

	noteComputerUseOperatedWindow(7)
	if got := computerUseHaloTarget(); got != 0 {
		t.Fatalf("own window became target %d", got)
	}
	noteComputerUseOperatedWindow(42)
	if got := computerUseHaloTarget(); got != 42 {
		t.Fatalf("target %d", got)
	}
	noteComputerUseOperatedWindow(7)
	if got := computerUseHaloTarget(); got != 42 {
		t.Fatalf("own window replaced target %d", got)
	}
	noteComputerUseOperatedWindow(0)
	if got := computerUseHaloTarget(); got != 42 {
		t.Fatalf("zero id cleared target %d", got)
	}
	hideComputerUseWindowHalo()
	if got := computerUseHaloTarget(); got != 0 {
		t.Fatalf("hide left %d", got)
	}
}
