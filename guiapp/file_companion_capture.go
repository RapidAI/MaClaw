package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

const fileCompanionWebCaptureEvery = 3 * time.Second

// fileCompanionPlaybackPoll is how often a recording asks the player whether
// this video has finished. The frame grab stays on fileCompanionWebCaptureEvery.
const fileCompanionPlaybackPoll = time.Second

// webPlaybackEndedScript reads the showing player. It does not navigate,
// download, or change the page. YouTube's end screen puts ended-mode on
// #movie_player. ad-showing is an advertisement. A live stream has a
// non-finite duration. webPlaybackHasEnded decides from the JSON.
const webPlaybackEndedScript = `(function(){try{var root=document.getElementById("movie_player");if(!root){var found=document.getElementsByClassName("html5-video-player");if(found&&found.length)root=found[0];}var player=!!root;var ad=!!(root&&root.classList&&root.classList.contains("ad-showing"));var endedMode=!!(root&&root.classList&&root.classList.contains("ended-mode"));var v=document.querySelector("video.html5-main-video")||document.querySelector("video");var ended=false,finite=false,nearEnd=false,atStart=false;if(v){var d=v.duration;finite=typeof d==="number"&&isFinite(d)&&d>0;ended=!!v.ended;var t=v.currentTime;nearEnd=finite&&typeof t==="number"&&(t+1.5)>=d;atStart=finite&&typeof t==="number"&&t<0.75;}return {player:player,ad:ad,endedMode:endedMode,ended:ended,finite:finite,nearEnd:nearEnd,atStart:atStart};}catch(e){return {player:false,ad:false,endedMode:false,ended:false,finite:false,nearEnd:false,atStart:false};}})()`

// webPlaybackRewindScript starts the showing video at the beginning and plays.
// It does not navigate, download, or change the address. An advertisement is
// left alone: seeking it would replay the ad. "ad" means try again once the
// ad class is gone. A seek issued while the player is still ended is dropped,
// so playback starts first and the media element is moved in the same call.
// "seek" and "video" moved the content. "none" means there is no player to
// move, so the recording keeps the current picture.
const webPlaybackRewindScript = `(function(){try{var root=document.getElementById("movie_player");if(!root){var found=document.getElementsByClassName("html5-video-player");if(found&&found.length)root=found[0];}var ad=!!(root&&root.classList&&root.classList.contains("ad-showing"));if(ad)return "ad";var api=false;try{if(root&&typeof root.seekTo==="function"){if(typeof root.playVideo==="function")root.playVideo();root.seekTo(0,true);api=true;}}catch(e){}var v=document.querySelector("video.html5-main-video")||document.querySelector("video");if(v){var play=v.play();if(play&&play.catch)play.catch(function(){});try{v.currentTime=0;}catch(e){}}if(api)return "seek";if(v)return "video";return "none";}catch(e){return "err";}})()`

// fileCompanionWebAdWait is how long a pre-roll may delay the rewind to 0.
// After that the recording takes the picture that is actually showing.
const fileCompanionWebAdWait = 45 * time.Second

// fileCompanionWebSeekRetry is how long a failed player script may delay the
// rewind. The player is on screen; the call did not get an answer. Treating
// that as "no player" records the current position, and on an end screen the
// finished check then writes an empty note.
const fileCompanionWebSeekRetry = 5 * time.Second

// rewindWebPlayer asks the showing player to start at the beginning.
// Windows replaces it. Other platforms leave the player where it is.
var rewindWebPlayer = func(context.Context) string { return "none" }

// fileCompanionRewindWebPlayerHook replaces the rewind in tests.
var fileCompanionRewindWebPlayerHook func(ctx context.Context) string

func fileCompanionRewindWebPlayer(ctx context.Context) string {
	if fileCompanionRewindWebPlayerHook != nil {
		return fileCompanionRewindWebPlayerHook(ctx)
	}
	return rewindWebPlayer(ctx)
}

// fileCompanionWebSeekSettled is false while an advertisement is showing or
// the player script failed. "none" means there is no player to move, so the
// recording keeps the current picture. "err" is tried again.
func fileCompanionWebSeekSettled(result string) bool {
	result = strings.TrimSpace(result)
	return result != "ad" && result != "err"
}

// fileCompanionRewindWait reports whether this rewind result should be tried
// again, and whether the end screen may stop that wait. Advertisement time
// and script-failure time are separate: a long ad must not use up the retries
// for a later script failure.
func fileCompanionRewindWait(result string, adWait, errWait time.Duration) (wait, stopOnEnded bool) {
	switch strings.TrimSpace(result) {
	case "ad":
		return adWait < fileCompanionWebAdWait, true
	case "err":
		return errWait < fileCompanionWebSeekRetry, false
	default:
		return false, false
	}
}

// fileCompanionRewindResult turns one script call into seek, video, ad, none,
// or err. A call that did not return is err, which is tried again. An empty
// answer is none: there is no player to move.
func fileCompanionRewindResult(raw string, callErr error) string {
	if callErr != nil {
		return "err"
	}
	text := fileCompanionScriptString(raw)
	if text == "" {
		return "none"
	}
	return text
}

// fileCompanionScriptString reads the JSON string ExecuteScript returns.
// A bare token is kept so a host that does not encode still works.
func fileCompanionScriptString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal([]byte(raw), &text); err == nil {
		return text
	}
	return strings.Trim(raw, `"`)
}

// fileCompanionPlaybackEndedHook replaces the player probe in tests.
// A nil hook asks the showing player. A true result ends the recording the
// same way 停止 does during capture: frames already taken stay, and the note is written.
var fileCompanionPlaybackEndedHook func(ctx context.Context) bool

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

// fileCompanionCaptureWeb records the WebView until ctx ends, the video
// finishes, or 10800 seconds. note and player stay open while this player
// is still the one on screen. Cancelling either one drops the raster in
// flight and does not wait for it. A finished video stops like 停止 during
// capture: frames already taken stay and the note is written.
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
	// 记笔记 records the whole video. Seek to the start and play before the
	// first grab, so a click in the middle or on the end screen does not
	// leave a note of only the remaining seconds. The frame after a seek can
	// still be the old picture; that grab waits one poll.
	sought := false
	var adWait, errWait time.Duration
	for ctx.Err() == nil && !captureLeft(ctx) && time.Since(start) < fileCompanionNoteMaxDuration {
		if !sought {
			result := fileCompanionRewindWebPlayer(ctx)
			if wait, stopOnEnded := fileCompanionRewindWait(result, adWait, errWait); wait {
				if result == "err" {
					errWait += fileCompanionPlaybackPoll
				} else {
					adWait += fileCompanionPlaybackPoll
				}
				// A failed script is not the end of the video. Stopping on
				// the end-screen flag here writes an empty note before the
				// rewind has been sent.
				if fileCompanionWaitCapture(ctx, fileCompanionPlaybackPoll, playerDone, noteDone, stopOnEnded) {
					break
				}
				continue
			}
			sought = true
			// The end screen stays ended-mode until the seek lands. Wait out
			// that flag before the finished check, or the note stops empty.
			if result == "seek" || result == "video" {
				if fileCompanionWaitForSeek(ctx, playerDone, noteDone) {
					break
				}
				continue
			}
		}
		// This video finished. Stop the same way 停止 does during capture:
		// keep the frames already taken and write the note. Do not grab the
		// end screen or the next autoplayed video.
		if fileCompanionPlaybackEnded(ctx) {
			break
		}
		pngBytes, err := captureCompanionWebViewPNG(ctx, rect)
		// Leaving the player drops this raster: it can already be the next page.
		// 停止 keeps a finished frame of this player. Playback that ended
		// during the grab does not: that picture is the end screen.
		if captureLeft(ctx) {
			break
		}
		ended := fileCompanionPlaybackEnded(ctx)
		if !ended && err == nil && len(pngBytes) > 0 {
			if shot, ok := webShotFromPNG(pngBytes, time.Since(start)); ok && !grayFrameBlack(shot.Gray) {
				frames = fileCompanionKeepWebShot(frames, videoNoteFrame{At: shot.At, Gray: shot.Gray, JPEG: shot.JPEG})
			}
		}
		if ctx.Err() != nil || captureLeft(ctx) || ended {
			break
		}
		wait := fileCompanionWebCaptureEvery
		if remain := fileCompanionNoteMaxDuration - time.Since(start); remain < wait {
			wait = remain
		}
		if wait <= 0 {
			break
		}
		if fileCompanionWaitCapture(ctx, wait, playerDone, noteDone, true) {
			break
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

// fileCompanionWaitForSeek gives a rewind time to leave the end screen.
// It always waits one poll, then up to two more while the player still
// reports that the video has finished. Each of those polls sends the rewind
// again: the first seek is dropped while the player is still ended. A true
// result means the note was cancelled or the player left.
func fileCompanionWaitForSeek(ctx context.Context, playerDone, noteDone <-chan struct{}) bool {
	const polls = 3
	for i := 0; i < polls; i++ {
		if ctx.Err() != nil || captureLeft(ctx) {
			return true
		}
		fileCompanionWaitCapture(ctx, fileCompanionPlaybackPoll, playerDone, noteDone, false)
		if ctx.Err() != nil || captureLeft(ctx) {
			return true
		}
		if !fileCompanionPlaybackEnded(ctx) {
			return false
		}
		if i+1 < polls {
			fileCompanionRewindWebPlayer(ctx)
		}
	}
	return ctx.Err() != nil || captureLeft(ctx)
}

// fileCompanionWaitCapture waits out the gap between frames.
// A true result means this video has finished, so the caller stops recording.
func fileCompanionWaitCapture(ctx context.Context, wait time.Duration, playerDone, noteDone <-chan struct{}, stopWhenEnded bool) bool {
	if wait <= 0 {
		return false
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	pulse := time.NewTimer(fileCompanionPlaybackPoll)
	defer pulse.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-playerDone:
			return false
		case <-noteDone:
			return false
		case <-deadline.C:
			return false
		case <-pulse.C:
			if stopWhenEnded && fileCompanionPlaybackEnded(ctx) {
				return true
			}
			pulse.Reset(fileCompanionPlaybackPoll)
		}
	}
}

func fileCompanionPlaybackEnded(ctx context.Context) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if fileCompanionPlaybackEndedHook != nil {
		return fileCompanionPlaybackEndedHook(ctx)
	}
	return capturePlaybackEnded(ctx)
}

// webPlaybackProbe is the player fact the page script reports.
type webPlaybackProbe struct {
	Player    bool `json:"player"`
	Ad        bool `json:"ad"`
	EndedMode bool `json:"endedMode"`
	Ended     bool `json:"ended"`
	Finite    bool `json:"finite"`
	NearEnd   bool `json:"nearEnd"`
	AtStart   bool `json:"atStart"`
}

// webPlaybackHasEnded is true when the content video has finished.
// YouTube's end screen is ended-mode on #movie_player. That class stays
// up after a rewind until the player repaints, so the playhead has to
// still be at the end. A playhead at the start is the video playing
// again. The video element also reports ended when an advertisement
// finishes, so that flag is not the end while the YouTube player is on
// the page. An advertisement is not the end. A page without that player
// uses a finite video that has ended on its duration. A live stream has
// no finite duration and no ended-mode, so it is not the end.
func webPlaybackHasEnded(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || len(raw) > 512 {
		return false
	}
	var probe webPlaybackProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return false
	}
	if probe.Ad || probe.AtStart {
		return false
	}
	if probe.Player {
		return probe.EndedMode && probe.NearEnd
	}
	return probe.Ended && probe.Finite && probe.NearEnd
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
