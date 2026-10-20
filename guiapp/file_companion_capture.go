package guiapp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

const fileCompanionWebCaptureEvery = 3 * time.Second

// errCompanionWebViewUnbound means this process has no live Wails WebView.
// Capture returns immediately and does not open a sound device.
var errCompanionWebViewUnbound = errors.New("companion webview is not bound")

// fileCompanionCaptureViewCalls counts entries into captureCompanionWebViewPNG.
// A nil test hook still increments it, because that is the shipping path.
var fileCompanionCaptureViewCalls atomic.Int64

// fileCompanionCaptureAudioCalls counts entries into captureProcessAudio.
var fileCompanionCaptureAudioCalls atomic.Int64

func companionCaptureViewBound() bool {
	view := maclawcapture.Current()
	return view != nil && view.Pointer() != 0
}

func fileCompanionWebRect(x, y, w, h float64) image.Rectangle {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	x0 := int(x)
	y0 := int(y)
	x1 := int(x + w)
	y1 := int(y + h)
	if x1 <= x0 || y1 <= y0 {
		return image.Rectangle{}
	}
	return image.Rect(x0, y0, x1, y1)
}

// captureDrop is the pair of contexts that mean this preview is not the note.
// player ends when the recorded player leaves the screen.
// note ends when the tab closes or /clear runs.
// 停止 cancels the capture context and neither of these.
type captureDrop struct {
	note   context.Context
	player context.Context
}

type captureDropKey struct{}

// errCompanionPlayerLeft means this raster is not part of the note.
// The recorded player left, or the note itself was cancelled.
var errCompanionPlayerLeft = errors.New("player left")

func withCaptureDrop(ctx, note, player context.Context) context.Context {
	if ctx == nil || (note == nil && player == nil) {
		return ctx
	}
	return context.WithValue(ctx, captureDropKey{}, captureDrop{note: note, player: player})
}

func captureDropOf(ctx context.Context) captureDrop {
	if ctx == nil {
		return captureDrop{}
	}
	drop, _ := ctx.Value(captureDropKey{}).(captureDrop)
	return drop
}

// captureDropChans are selectable. A nil channel never fires.
// A polled predicate cannot wake a select that is already waiting.
func captureDropChans(ctx context.Context) (playerDone, noteDone <-chan struct{}) {
	drop := captureDropOf(ctx)
	if drop.player != nil {
		playerDone = drop.player.Done()
	}
	if drop.note != nil {
		noteDone = drop.note.Done()
	}
	return playerDone, noteDone
}

func captureLeft(ctx context.Context) bool {
	drop := captureDropOf(ctx)
	if drop.player != nil && drop.player.Err() != nil {
		return true
	}
	return drop.note != nil && drop.note.Err() != nil
}

// fileCompanionCaptureWeb records the WebView until ctx ends or 10800 seconds.
// note and player stay open while this player is still the one on screen.
// Cancelling either one drops the raster in flight and does not wait for it.
// A non-nil fileCompanionWebCaptureHook replaces the whole recording for tests.
// A nil hook calls captureCompanionWebViewPNG and captureProcessAudio.
func fileCompanionCaptureWeb(ctx context.Context, rect image.Rectangle, note, player context.Context) (shots []webNoteShot, audio []byte, refused bool) {
	ctx = withCaptureDrop(ctx, note, player)
	if ctx != nil && ctx.Err() != nil {
		return nil, nil, false
	}
	if fileCompanionWebCaptureHook != nil {
		return fileCompanionWebCaptureHook()
	}
	audioCtx, audioCancel := context.WithCancel(ctx)
	defer audioCancel()
	type audioResult struct {
		pcm     []byte
		refused bool
	}
	audioCh := make(chan audioResult, 1)
	go func() {
		pcm, ref := captureProcessAudio(audioCtx)
		audioCh <- audioResult{pcm, ref}
	}()

	start := time.Now()
	var frames []videoNoteFrame
	playerDone, noteDone := captureDropChans(ctx)
	for ctx.Err() == nil && !captureLeft(ctx) && time.Since(start) < fileCompanionNoteMaxDuration {
		pngBytes, err := captureCompanionWebViewPNG(ctx, rect)
		// Leaving the player drops this raster: it can already be the next page.
		// 停止 only ends the loop below, so a finished frame of this player stays.
		if captureLeft(ctx) {
			break
		}
		if err == nil && len(pngBytes) > 0 {
			if shot, ok := webShotFromPNG(pngBytes, time.Since(start)); ok && !grayFrameBlack(shot.Gray) {
				frames = fileCompanionKeepWebShot(frames, videoNoteFrame{At: shot.At, Gray: shot.Gray, JPEG: shot.JPEG})
			}
		}
		if ctx.Err() != nil || captureLeft(ctx) {
			break
		}
		wait := fileCompanionWebCaptureEvery
		if remain := fileCompanionNoteMaxDuration - time.Since(start); remain < wait {
			wait = remain
		}
		if wait <= 0 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-playerDone:
			timer.Stop()
		case <-noteDone:
			timer.Stop()
		case <-timer.C:
		}
	}
	audioCancel()
	got := <-audioCh
	shots = make([]webNoteShot, 0, len(frames))
	for _, frame := range frames {
		shots = append(shots, webNoteShot{At: frame.At, Gray: frame.Gray, JPEG: frame.JPEG})
	}
	return shots, got.pcm, got.refused
}

func webShotFromPNG(raw []byte, at time.Duration) (webNoteShot, bool) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return webNoteShot{}, false
	}
	gray := gray16FromImage(img)
	if len(gray) != 256 {
		return webNoteShot{}, false
	}
	jpg := jpegLongEdge(img, 768)
	if len(jpg) == 0 || len(jpg) > 200<<10 {
		return webNoteShot{}, false
	}
	return webNoteShot{At: at, Gray: gray, JPEG: jpg}, true
}

func cropCompanionPNG(raw []byte, rect image.Rectangle) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if rect.Empty() {
		return raw, nil
	}
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return nil, errors.New("iframe rect is outside the webview")
	}
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(cropped, cropped.Bounds(), img, rect.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gray16FromImage(src image.Image) []byte {
	b := src.Bounds()
	if b.Empty() {
		return nil
	}
	dst := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			sx := b.Min.X
			sy := b.Min.Y
			if b.Dx() > 0 {
				sx += (x * b.Dx()) / 16
			}
			if b.Dy() > 0 {
				sy += (y * b.Dy()) / 16
			}
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			r, g, bl, _ := src.At(sx, sy).RGBA()
			y8 := uint8((r*299 + g*587 + bl*114) / 1000 >> 8)
			dst.SetGray(x, y, color.Gray{Y: y8})
		}
	}
	if dst.Stride != 16 || len(dst.Pix) < 256 {
		return nil
	}
	return append([]byte(nil), dst.Pix[:256]...)
}

func jpegLongEdge(src image.Image, edge int) []byte {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || edge < 1 {
		return nil
	}
	nw, nh := w, h
	if w >= h && w > edge {
		nw = edge
		nh = h * edge / w
	} else if h > w && h > edge {
		nh = edge
		nw = w * edge / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			sy := b.Min.Y + y*h/nh
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			if sy >= b.Max.Y {
				sy = b.Max.Y - 1
			}
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 60}); err != nil {
		return nil
	}
	return buf.Bytes()
}

func pcm16MonoWAV(pcm []byte, rate int) []byte {
	if rate <= 0 {
		rate = 16000
	}
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	n := uint32(len(pcm))
	out := make([]byte, 44+len(pcm))
	copy(out[0:], "RIFF")
	putU32(out[4:], uint32(36+n))
	copy(out[8:], "WAVEfmt ")
	putU32(out[16:], 16)
	putU16(out[20:], 1)
	putU16(out[22:], 1)
	putU32(out[24:], uint32(rate))
	putU32(out[28:], uint32(rate)*2)
	putU16(out[32:], 2)
	putU16(out[34:], 16)
	copy(out[36:], "data")
	putU32(out[40:], n)
	copy(out[44:], pcm)
	return out
}

func putU16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func putU32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
