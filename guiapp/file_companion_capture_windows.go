//go:build windows

package guiapp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
	"golang.org/x/sys/windows"
)

// VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK is the device id string
// VAD\Process_Loopback. It is not the default render endpoint.
const virtualAudioDeviceProcessLoopback = `VAD\Process_Loopback`

const (
	// AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK
	audioClientActivationTypeProcessLoopback = 1
	// PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE
	processLoopbackModeIncludeTargetProcessTree = 0
	windowsProcessLoopbackMinBuild              = 19041
	waveFormatPCM                               = 1
	waveFormatIEEEFloat                         = 3
	waveFormatExtensibleTag                     = 0xFFFE
	vtBlob                                      = 65
)

// KSDATAFORMAT_SUBTYPE_IEEE_FLOAT and KSDATAFORMAT_SUBTYPE_PCM.
// Process loopback does not implement IAudioClient vtable slot 8, so the
// mix format is one of these extensible layouts instead of a queried header.
var (
	ksDataFormatSubtypeIEEEFloat = windows.GUID{
		Data1: 0x00000003,
		Data2: 0x0000,
		Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
	}
	ksDataFormatSubtypePCM = windows.GUID{
		Data1: 0x00000001,
		Data2: 0x0000,
		Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
	}
)

// waveFormatExtensible is WAVEFORMATEXTENSIBLE: the 18-byte WAVEFORMATEX plus
// the 22-byte tail (valid bits, channel mask, SubFormat GUID).
type waveFormatExtensible struct {
	waveFormatEx
	ValidBits   uint16
	ChannelMask uint32
	SubFormat   windows.GUID
}

// processLoopbackPlan is the activation passed to ActivateAudioInterfaceAsync.
// StreamFlags stays 0. The machine-wide render endpoint is a different
// device. AUDCLNT_STREAMFLAGS_LOOPBACK belongs on IAudioClient.Initialize
// of this virtual device, which the activation already limits to TargetPID.
type processLoopbackPlan struct {
	DeviceID       string
	ActivationType uint32
	TargetPID      uint32
	Mode           uint32
	StreamFlags    uint32
}

func windowsProcessLoopbackPlan(act processAudioActivate) processLoopbackPlan {
	mode := uint32(processLoopbackModeIncludeTargetProcessTree)
	if !act.IncludeProcessTree {
		mode = 1
	}
	return processLoopbackPlan{
		DeviceID:       virtualAudioDeviceProcessLoopback,
		ActivationType: audioClientActivationTypeProcessLoopback,
		TargetPID:      act.TargetPID,
		Mode:           mode,
		StreamFlags:    act.StreamFlags,
	}
}

type audioClientActivationParams struct {
	ActivationType      uint32
	TargetProcessID     uint32
	ProcessLoopbackMode uint32
}

// captureCompanionWebViewPNG captures one PNG of the visible player.
// A top-level player page is already the video, so the companion-window
// rectangle is not a crop of that picture. Otherwise the app WebView is
// captured and cropped to rect.
// No bound view returns immediately and does not call CapturePreview.
func captureCompanionWebViewPNG(ctx context.Context, rect image.Rectangle) ([]byte, error) {
	fileCompanionCaptureViewCalls.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if maclawplayer.Showing() {
		view := maclawplayer.View()
		if view == nil || view.Pointer() == 0 || !maclawcapture.HasUIRunner() {
			return nil, errCompanionPlayerNotReady
		}
		return captureWebView2CapturePreview(ctx, unsafe.Pointer(view.Pointer()))
	}
	view := maclawcapture.Current()
	if view == nil || view.Pointer() == 0 || !maclawcapture.HasUIRunner() {
		return nil, errCompanionWebViewUnbound
	}
	raw, err := captureWebView2CapturePreview(ctx, unsafe.Pointer(view.Pointer()))
	if err != nil {
		return nil, err
	}
	return cropCompanionPNG(raw, rect)
}

var errCompanionPlayerNotReady = errors.New("player page is not ready")

// loopbackProcess is one row from a process snapshot.
type loopbackProcess struct {
	PID   uint32
	PPID  uint32
	Image string
}

// webViewLoopbackPID is the process whose direct children render the player.
//
// Include-tree captures the target and its direct children, not grandchildren.
// This host's child is the WebView2 browser. The browser's child is
// msedgewebview2 --type=utility --utility-sub-type=audio.mojom.AudioService,
// and that utility is what plays YouTube and Bilibili. Capturing the host
// therefore records a zero waveform while the picture still moves.
// The browser is chosen because it is that utility's parent. A service that
// starts when playback starts is a new direct child and is included.
// Several direct WebView2 children are not expected; the one with the most
// children is the browser. No such child keeps the host.
func webViewLoopbackPID(host uint32, rows []loopbackProcess) uint32 {
	if host == 0 {
		return 0
	}
	children := make(map[uint32]int, len(rows))
	for _, row := range rows {
		if row.PPID != 0 {
			children[row.PPID]++
		}
	}
	best := uint32(0)
	bestChildren := -1
	for _, row := range rows {
		if row.PPID != host || !strings.EqualFold(row.Image, "msedgewebview2.exe") {
			continue
		}
		n := children[row.PID]
		if n > bestChildren || (n == bestChildren && (best == 0 || row.PID < best)) {
			best = row.PID
			bestChildren = n
		}
	}
	if best == 0 {
		return host
	}
	return best
}

func loopbackProcessSnapshot() ([]loopbackProcess, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	rows := make([]loopbackProcess, 0, 64)
	for {
		rows = append(rows, loopbackProcess{
			PID:   entry.ProcessID,
			PPID:  entry.ParentProcessID,
			Image: windows.UTF16ToString(entry.ExeFile[:]),
		})
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return rows, nil
}

func companionLoopbackPID(host uint32) uint32 {
	rows, err := loopbackProcessSnapshot()
	if err != nil {
		return host
	}
	return webViewLoopbackPID(host, rows)
}

// captureProcessAudio records the WebView2 browser tree as 16 kHz mono WAV.
// The activation plan keeps StreamFlags at 0. Initialize of
// VAD\Process_Loopback sets AUDCLNT_STREAMFLAGS_LOOPBACK and
// AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM on that already process-scoped device.
// Those flags do not open the machine-wide render endpoint.
// No bound WebView returns refused immediately and does not open a device.
func captureProcessAudio(ctx context.Context) ([]byte, bool) {
	fileCompanionCaptureAudioCalls.Add(1)
	host := uint32(os.Getpid())
	target := companionLoopbackPID(host)
	act := companionProcessAudioActivate(target)
	if act.StreamFlags&audclntStreamFlagsLoopback != 0 || !act.IncludeProcessTree || act.TargetPID == 0 {
		return nil, true
	}
	if ctx.Err() != nil {
		return nil, true
	}
	if !companionCaptureViewBound() {
		return nil, true
	}
	if target != host {
		logFileCompanion("process loopback browser=%d host=%d", target, host)
	}
	plan := windowsProcessLoopbackPlan(act)
	wav, err := activateProcessLoopback(ctx, plan)
	if err != nil || len(wav) < 44 {
		// Stopping the note cancels this context. That is not a device failure.
		if err != nil && ctx.Err() == nil {
			logFileCompanion("process loopback pid=%d: %v", plan.TargetPID, err)
		}
		return nil, true
	}
	return wav, false
}

type previewResult struct {
	png []byte
	err error
}

// capturePreviewHandler is an ICoreWebView2CapturePreviewCompletedHandler.
// The first field is the vtable so the COM this pointer can be cast back.
// Callbacks stay package-level: windows.NewCallback rejects some generated
// go-webview2 signatures, and that package's init panics in this process.
type capturePreviewHandler struct {
	vtbl   *capturePreviewVtbl
	ch     chan previewResult
	stream uintptr
	refs   uintptr
	// creator is the reference taken at creation. WebView2 AddRef/Release
	// does not include it, so Invoke drops it once or the handler stays pinned.
	creator sync.Once
}

type capturePreviewVtbl struct {
	query uintptr
	add   uintptr
	rel   uintptr
	done  uintptr
}

var (
	capturePreviewVtblOnce sync.Once
	capturePreviewV        capturePreviewVtbl
	capturePreviewHandlers sync.Map
)

func capturePreviewCallbacks() *capturePreviewVtbl {
	capturePreviewVtblOnce.Do(func() {
		capturePreviewV.query = syscall.NewCallback(capturePreviewQuery)
		capturePreviewV.add = syscall.NewCallback(capturePreviewAddRef)
		capturePreviewV.rel = syscall.NewCallback(capturePreviewRelease)
		capturePreviewV.done = syscall.NewCallback(capturePreviewDone)
	})
	return &capturePreviewV
}

func capturePreviewQuery(this, _, object uintptr) uintptr {
	if object == 0 {
		return uintptr(windows.E_POINTER)
	}
	h := (*capturePreviewHandler)(unsafe.Pointer(this))
	*(*uintptr)(unsafe.Pointer(object)) = this
	h.refs++
	return uintptr(windows.S_OK)
}

func capturePreviewAddRef(this uintptr) uintptr {
	h := (*capturePreviewHandler)(unsafe.Pointer(this))
	h.refs++
	return h.refs
}

func capturePreviewRelease(this uintptr) uintptr {
	h := (*capturePreviewHandler)(unsafe.Pointer(this))
	if h.refs > 0 {
		h.refs--
	}
	if h.refs == 0 {
		capturePreviewHandlers.Delete(this)
	}
	return h.refs
}

// releaseCreator drops the reference taken when the handler was created.
// CapturePreview holds its own reference across Invoke and releases that
// after Invoke returns, so this must run once from Invoke and not from the waiter.
func (h *capturePreviewHandler) releaseCreator() {
	h.creator.Do(func() {
		capturePreviewRelease(uintptr(unsafe.Pointer(h)))
	})
}

func capturePreviewDone(this, errorCode uintptr) uintptr {
	h := (*capturePreviewHandler)(unsafe.Pointer(this))
	stream := h.stream
	h.stream = 0
	defer releaseCOM(stream)
	defer h.releaseCreator()
	if int32(errorCode) < 0 {
		h.ch <- previewResult{err: fmt.Errorf("CapturePreview completed 0x%x", errorCode)}
		return 0
	}
	png, err := readAllStream(stream)
	h.ch <- previewResult{png: png, err: err}
	return 0
}

func captureWebView2CapturePreview(ctx context.Context, this unsafe.Pointer) ([]byte, error) {
	if this == nil {
		return nil, errCompanionWebViewUnbound
	}
	stream, err := shCreateMemStream()
	if err != nil {
		return nil, err
	}
	ch := make(chan previewResult, 1)
	handler := &capturePreviewHandler{
		vtbl:   capturePreviewCallbacks(),
		ch:     ch,
		stream: stream,
		refs:   1,
	}
	key := uintptr(unsafe.Pointer(handler))
	capturePreviewHandlers.Store(key, handler)

	started := make(chan error, 1)
	ok := maclawcapture.PostUI(func() {
		started <- callCapturePreview(uintptr(this), stream, key)
	})
	if !ok {
		capturePreviewHandlers.Delete(key)
		releaseCOM(stream)
		return nil, errCompanionWebViewUnbound
	}
	if callErr := <-started; callErr != nil {
		capturePreviewHandlers.Delete(key)
		releaseCOM(stream)
		return nil, callErr
	}
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	return finishedCapturePreview(ctx, ch, timer.C)
}

// finishedCapturePreview returns one preview.
// 停止 cancels ctx while the recorded player is still on screen, so the
// preview already started is waited for. player and note are the drop
// signals copied onto ctx. They stay open across that stop. A leave or a
// note cancel that arrives during the wait is selected here, including
// after ctx is already done, so the raster is not held until the bitmap
// or the timeout.
func finishedCapturePreview(ctx context.Context, ch <-chan previewResult, timeout <-chan time.Time) ([]byte, error) {
	playerDone, noteDone := captureDropChans(ctx)
	select {
	case got := <-ch:
		if captureLeft(ctx) {
			return nil, errCompanionPlayerLeft
		}
		return got.png, got.err
	case <-playerDone:
		return nil, errCompanionPlayerLeft
	case <-noteDone:
		return nil, errCompanionPlayerLeft
	case <-ctx.Done():
		if captureLeft(ctx) {
			return nil, errCompanionPlayerLeft
		}
		select {
		case got := <-ch:
			if captureLeft(ctx) {
				return nil, errCompanionPlayerLeft
			}
			return got.png, got.err
		case <-playerDone:
			return nil, errCompanionPlayerLeft
		case <-noteDone:
			return nil, errCompanionPlayerLeft
		case <-timeout:
			return nil, errors.New("CapturePreview timed out")
		}
	case <-timeout:
		return nil, errors.New("CapturePreview timed out")
	}
}

// iCoreWebView2ExecuteScript is ICoreWebView2.ExecuteScript.
// IUnknown occupies slots 0-2. CapturePreview is the next slot, 30.
const iCoreWebView2ExecuteScript = 29

// capturePlaybackEnded asks the showing player whether this video has finished.
// No player page returns false and does not run a script. A script failure
// is not the end of the video.
func capturePlaybackEnded(ctx context.Context) bool {
	if !maclawplayer.Showing() {
		return false
	}
	view := maclawplayer.View()
	if view == nil || view.Pointer() == 0 || !maclawcapture.HasUIRunner() {
		return false
	}
	raw, err := executePlayerScript(ctx, unsafe.Pointer(view.Pointer()), webPlaybackEndedScript)
	if err != nil || raw == "" {
		return false
	}
	return webPlaybackHasEnded(raw)
}

type playbackScriptResult struct {
	raw string
	err error
}

// playbackScriptHandler is an ICoreWebView2ExecuteScriptCompletedHandler.
// The first field is the vtable so the COM this pointer can be cast back.
// Callbacks stay package-level: the generated go-webview2 handler panics in this process.
type playbackScriptHandler struct {
	vtbl    *playbackScriptVtbl
	ch      chan playbackScriptResult
	refs    uintptr
	creator sync.Once
}

type playbackScriptVtbl struct {
	query uintptr
	add   uintptr
	rel   uintptr
	done  uintptr
}

var (
	playbackScriptVtblOnce sync.Once
	playbackScriptV        playbackScriptVtbl
	playbackScriptHandlers sync.Map
)

func playbackScriptCallbacks() *playbackScriptVtbl {
	playbackScriptVtblOnce.Do(func() {
		playbackScriptV.query = syscall.NewCallback(playbackScriptQuery)
		playbackScriptV.add = syscall.NewCallback(playbackScriptAddRef)
		playbackScriptV.rel = syscall.NewCallback(playbackScriptRelease)
		playbackScriptV.done = syscall.NewCallback(playbackScriptDone)
	})
	return &playbackScriptV
}

// guidPlaybackIUnknown is IUnknown. guidExecuteScriptCompleted is
// ICoreWebView2ExecuteScriptCompletedHandler {49511172-CC67-4BCA-9923-137112F4C4CC}.
// Accepting any other IID would claim IAgileObject, and WebView2 could
// then call Invoke off the UI thread.
var (
	guidPlaybackIUnknown = windows.GUID{
		Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	guidExecuteScriptCompleted = windows.GUID{
		Data1: 0x49511172, Data2: 0xCC67, Data3: 0x4BCA,
		Data4: [8]byte{0x99, 0x23, 0x13, 0x71, 0x12, 0xF4, 0xC4, 0xCC},
	}
)

func playbackScriptQuery(this, riid, object uintptr) uintptr {
	if this == 0 || riid == 0 || object == 0 {
		return uintptr(windows.E_POINTER)
	}
	*(*uintptr)(unsafe.Pointer(object)) = 0
	id := *(*windows.GUID)(unsafe.Pointer(riid))
	if id != guidPlaybackIUnknown && id != guidExecuteScriptCompleted {
		return uintptr(windows.E_NOINTERFACE)
	}
	*(*uintptr)(unsafe.Pointer(object)) = this
	playbackScriptAddRef(this)
	return uintptr(windows.S_OK)
}

func playbackScriptAddRef(this uintptr) uintptr {
	if this == 0 {
		return 1
	}
	h := (*playbackScriptHandler)(unsafe.Pointer(this))
	return atomic.AddUintptr(&h.refs, 1)
}

func playbackScriptRelease(this uintptr) uintptr {
	if this == 0 {
		return 0
	}
	h := (*playbackScriptHandler)(unsafe.Pointer(this))
	refs := atomic.AddUintptr(&h.refs, ^uintptr(0))
	if refs == 0 {
		playbackScriptHandlers.Delete(this)
	}
	return refs
}

func (h *playbackScriptHandler) releaseCreator() {
	h.creator.Do(func() {
		playbackScriptRelease(uintptr(unsafe.Pointer(h)))
	})
}

func playbackScriptDone(this, errorCode, resultPtr uintptr) uintptr {
	h := (*playbackScriptHandler)(unsafe.Pointer(this))
	defer h.releaseCreator()
	raw := ""
	var err error
	if int32(errorCode) < 0 {
		err = fmt.Errorf("ExecuteScript completed 0x%x", uint32(errorCode))
	} else if resultPtr != 0 {
		raw = windows.UTF16PtrToString((*uint16)(unsafe.Pointer(resultPtr)))
	}
	select {
	case h.ch <- playbackScriptResult{raw: raw, err: err}:
	default:
	}
	return 0
}

func executePlayerScript(ctx context.Context, this unsafe.Pointer, js string) (string, error) {
	if this == nil {
		return "", errCompanionWebViewUnbound
	}
	script, err := windows.UTF16PtrFromString(js)
	if err != nil {
		return "", err
	}
	ch := make(chan playbackScriptResult, 1)
	handler := &playbackScriptHandler{
		vtbl: playbackScriptCallbacks(),
		ch:   ch,
		refs: 1,
	}
	key := uintptr(unsafe.Pointer(handler))
	playbackScriptHandlers.Store(key, handler)
	started := make(chan error, 1)
	ok := maclawcapture.PostUI(func() {
		started <- callExecuteScript(uintptr(this), script, key)
		runtime.KeepAlive(script)
	})
	if !ok {
		playbackScriptHandlers.Delete(key)
		return "", errCompanionWebViewUnbound
	}
	if callErr := <-started; callErr != nil {
		playbackScriptHandlers.Delete(key)
		return "", callErr
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case got := <-ch:
		return got.raw, got.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("ExecuteScript timed out")
	}
}

// callExecuteScript calls ICoreWebView2.ExecuteScript at vtable slot 29.
func callExecuteScript(webview uintptr, script *uint16, handler uintptr) error {
	if webview == 0 || script == nil {
		return errCompanionWebViewUnbound
	}
	vtbl := *(*uintptr)(unsafe.Pointer(webview))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + iCoreWebView2ExecuteScript*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, webview, uintptr(unsafe.Pointer(script)), handler)
	if int32(hr) < 0 {
		return fmt.Errorf("ExecuteScript 0x%x", uint32(hr))
	}
	return nil
}

// callCapturePreview calls ICoreWebView2.CapturePreview at vtable slot 30.
// PNG format is COREWEBVIEW2_CAPTURE_PREVIEW_IMAGE_FORMAT_PNG (0).
func callCapturePreview(webview, stream, handler uintptr) error {
	if webview == 0 {
		return errCompanionWebViewUnbound
	}
	vtbl := *(*uintptr)(unsafe.Pointer(webview))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 30*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, webview, 0, stream, handler)
	if int32(hr) < 0 {
		return fmt.Errorf("CapturePreview 0x%x", hr)
	}
	return nil
}

func shCreateMemStream() (uintptr, error) {
	proc := windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHCreateMemStream")
	r, _, err := proc.Call(0, 0)
	if r == 0 {
		if err != nil {
			return 0, err
		}
		return 0, errors.New("SHCreateMemStream failed")
	}
	return r, nil
}

func releaseCOM(p uintptr) {
	if p == 0 {
		return
	}
	vtbl := *(*uintptr)(unsafe.Pointer(p))
	rel := *(*uintptr)(unsafe.Pointer(vtbl + 2*unsafe.Sizeof(uintptr(0))))
	_, _, _ = syscall.SyscallN(rel, p)
}

func readAllStream(p uintptr) ([]byte, error) {
	if p == 0 {
		return nil, errors.New("empty preview stream")
	}
	vtbl := *(*uintptr)(unsafe.Pointer(p))
	seek := *(*uintptr)(unsafe.Pointer(vtbl + 5*unsafe.Sizeof(uintptr(0))))
	var newPos uint64
	hr, _, _ := syscall.SyscallN(seek, p, 0, 0, uintptr(unsafe.Pointer(&newPos)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("IStream.Seek 0x%x", hr)
	}
	read := *(*uintptr)(unsafe.Pointer(vtbl + 3*unsafe.Sizeof(uintptr(0))))
	var out []byte
	buf := make([]byte, 64<<10)
	for {
		var n uint32
		hr, _, _ := syscall.SyscallN(read, p, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
		if int32(hr) < 0 {
			return nil, fmt.Errorf("IStream.Read 0x%x", hr)
		}
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if n == 0 {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("CapturePreview returned an empty PNG")
	}
	return out, nil
}

func activateProcessLoopback(ctx context.Context, plan processLoopbackPlan) ([]byte, error) {
	if plan.StreamFlags&audclntStreamFlagsLoopback != 0 {
		return nil, errors.New("refusing machine-wide loopback")
	}
	if plan.DeviceID != virtualAudioDeviceProcessLoopback || plan.ActivationType != audioClientActivationTypeProcessLoopback || plan.Mode != processLoopbackModeIncludeTargetProcessTree {
		return nil, errors.New("process loopback activation was not include-tree")
	}
	info := windows.RtlGetVersion()
	if info == nil || info.BuildNumber < windowsProcessLoopbackMinBuild {
		return nil, errors.New("process loopback needs Windows 10.0.19041")
	}
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	params := audioClientActivationParams{
		ActivationType:      plan.ActivationType,
		TargetProcessID:     plan.TargetPID,
		ProcessLoopbackMode: plan.Mode,
	}
	device, err := windows.UTF16PtrFromString(plan.DeviceID)
	if err != nil {
		return nil, err
	}
	iid := windows.GUID{
		Data1: 0x1CB9AD4C,
		Data2: 0xDBFA,
		Data3: 0x4c32,
		Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2},
	}
	pv := propVariantBlob{
		VT:      vtBlob,
		CBSize:  uint32(unsafe.Sizeof(params)),
		BlobPtr: uintptr(unsafe.Pointer(&params)),
	}
	handler, wait := newAudioActivateHandler()
	defer handler.release()
	var asyncOp uintptr
	proc := windows.NewLazySystemDLL("mmdevapi.dll").NewProc("ActivateAudioInterfaceAsync")
	hr, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(device)),
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&pv)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(&asyncOp)),
	)
	if hr != 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return nil, callErr
		}
		return nil, fmt.Errorf("ActivateAudioInterfaceAsync 0x%x", hr)
	}
	defer releaseCOM(asyncOp)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-wait:
		if got.err != nil {
			return nil, got.err
		}
		defer releaseCOM(got.client)
		return readProcessLoopback(ctx, got.client)
	case <-time.After(5 * time.Second):
		return nil, errors.New("ActivateAudioInterfaceAsync timed out")
	}
}

type propVariantBlob struct {
	VT        uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	CBSize    uint32
	_         uint32
	BlobPtr   uintptr
}

type audioActivateResult struct {
	client uintptr
	err    error
}

type audioActivateHandler struct {
	vtbl *audioActivateVtbl
	ch   chan audioActivateResult
	refs uintptr
}

type audioActivateVtbl struct {
	query uintptr
	add   uintptr
	rel   uintptr
	done  uintptr
}

func newAudioActivateHandler() (*audioActivateHandler, <-chan audioActivateResult) {
	ch := make(chan audioActivateResult, 1)
	h := &audioActivateHandler{ch: ch, refs: 1}
	h.vtbl = &audioActivateVtbl{
		query: windows.NewCallback(audioActivateQuery),
		add:   windows.NewCallback(audioActivateAdd),
		rel:   windows.NewCallback(audioActivateRelease),
		done:  windows.NewCallback(audioActivateDone),
	}
	return h, ch
}

func (h *audioActivateHandler) release() {
	if h == nil {
		return
	}
	h.refs = 0
}

func audioActivateQuery(this, _, object uintptr) uintptr {
	if object == 0 {
		return uintptr(windows.E_POINTER)
	}
	h := (*audioActivateHandler)(unsafe.Pointer(this))
	*(*uintptr)(unsafe.Pointer(object)) = this
	h.refs++
	return uintptr(windows.S_OK)
}

func audioActivateAdd(this uintptr) uintptr {
	h := (*audioActivateHandler)(unsafe.Pointer(this))
	h.refs++
	return h.refs
}

func audioActivateRelease(this uintptr) uintptr {
	h := (*audioActivateHandler)(unsafe.Pointer(this))
	if h.refs > 0 {
		h.refs--
	}
	return h.refs
}

func audioActivateDone(this, op uintptr) uintptr {
	h := (*audioActivateHandler)(unsafe.Pointer(this))
	hr, client := getActivateResult(op)
	if int32(hr) < 0 || client == 0 {
		h.ch <- audioActivateResult{err: fmt.Errorf("process loopback activate 0x%x", hr)}
		return 0
	}
	h.ch <- audioActivateResult{client: client}
	return 0
}

func getActivateResult(op uintptr) (uintptr, uintptr) {
	if op == 0 {
		return uintptr(windows.E_POINTER), 0
	}
	vtbl := *(*uintptr)(unsafe.Pointer(op))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 3*unsafe.Sizeof(uintptr(0))))
	var activateHR uintptr
	var iface uintptr
	r, _, _ := syscall.SyscallN(proc, op, uintptr(unsafe.Pointer(&activateHR)), uintptr(unsafe.Pointer(&iface)))
	if int32(r) < 0 {
		return r, 0
	}
	return activateHR, iface
}

func readProcessLoopback(ctx context.Context, client uintptr) ([]byte, error) {
	format, err := initializeProcessLoopbackClient(client)
	if err != nil {
		return nil, err
	}
	capture, err := audioGetCaptureClient(client)
	if err != nil {
		return nil, err
	}
	defer releaseCOM(capture)
	if err := audioClientStart(client); err != nil {
		return nil, err
	}
	defer audioClientStop(client)
	// The note keeps 16 kHz mono. A 48 kHz float-stereo device buffer reaches
	// 512 MiB in about 23 minutes, which would drop the rest of a 10800s note.
	// Mix each packet first and apply that ceiling to the mono PCM.
	sink := loopbackPCMSink{format: format}
	for ctx.Err() == nil {
		var packets uint32
		if err := audioNextPacket(capture, &packets); err != nil {
			return nil, err
		}
		if packets == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(15 * time.Millisecond):
			}
			continue
		}
		chunk, frames, flags, err := audioGetBuffer(capture, format.BlockAlign)
		if err != nil {
			_ = audioReleaseBuffer(capture, 0)
			return nil, err
		}
		if flags&1 != 0 {
			sink.addSilence(frames)
		} else if len(chunk) > 0 {
			sink.addRaw(chunk)
		}
		if err := audioReleaseBuffer(capture, frames); err != nil {
			return nil, err
		}
		if loopbackAudioStopped(len(sink.pcm)) {
			break
		}
	}
	pcm := sink.bytes()
	if len(pcm) == 0 {
		return nil, errors.New("process loopback recorded no samples")
	}
	return pcm16MonoWAV(pcm, 16000), nil
}

// loopbackKeptPCMCeiling bounds the 16 kHz mono recording, not the device buffer.
const loopbackKeptPCMCeiling = 512 << 20

func loopbackAudioStopped(mixedPCM int) bool {
	return mixedPCM > loopbackKeptPCMCeiling
}

func processLoopbackFormatCandidates() []waveFormatExtensible {
	return []waveFormatExtensible{
		loopbackExtensibleFormat(48000, 32, ksDataFormatSubtypeIEEEFloat),
		loopbackExtensibleFormat(44100, 32, ksDataFormatSubtypeIEEEFloat),
		loopbackExtensibleFormat(48000, 16, ksDataFormatSubtypePCM),
	}
}

func loopbackExtensibleFormat(rate uint32, bits uint16, sub windows.GUID) waveFormatExtensible {
	channels := uint16(2)
	block := channels * (bits / 8)
	return waveFormatExtensible{
		waveFormatEx: waveFormatEx{
			FormatTag:      waveFormatExtensibleTag,
			Channels:       channels,
			SamplesPerSec:  rate,
			AvgBytesPerSec: rate * uint32(block),
			BlockAlign:     block,
			BitsPerSample:  bits,
			Size:           22,
		},
		ValidBits:   bits,
		ChannelMask: 0x3,
		SubFormat:   sub,
	}
}

func loopbackExtensibleBytes(format waveFormatExtensible) []byte {
	buf := make([]byte, 40)
	binary.LittleEndian.PutUint16(buf[0:], format.FormatTag)
	binary.LittleEndian.PutUint16(buf[2:], format.Channels)
	binary.LittleEndian.PutUint32(buf[4:], format.SamplesPerSec)
	binary.LittleEndian.PutUint32(buf[8:], format.AvgBytesPerSec)
	binary.LittleEndian.PutUint16(buf[12:], format.BlockAlign)
	binary.LittleEndian.PutUint16(buf[14:], format.BitsPerSample)
	binary.LittleEndian.PutUint16(buf[16:], format.Size)
	binary.LittleEndian.PutUint16(buf[18:], format.ValidBits)
	binary.LittleEndian.PutUint32(buf[20:], format.ChannelMask)
	binary.LittleEndian.PutUint32(buf[24:], format.SubFormat.Data1)
	binary.LittleEndian.PutUint16(buf[28:], format.SubFormat.Data2)
	binary.LittleEndian.PutUint16(buf[30:], format.SubFormat.Data3)
	copy(buf[32:], format.SubFormat.Data4[:])
	return buf
}

func initializeProcessLoopbackClient(client uintptr) (*waveFormatExtensible, error) {
	candidates := processLoopbackFormatCandidates()
	var last error
	for i := range candidates {
		format := candidates[i]
		err := audioClientInitialize(client, &format)
		if err == nil {
			chosen := format
			return &chosen, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("process loopback has no format")
	}
	return nil, last
}

// audclntStreamFlagsAutoConvertPCM is AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM.
// The process-loopback device converts into the format passed to Initialize.
const audclntStreamFlagsAutoConvertPCM uint32 = 0x80000000

// audclntStreamFlagsEventCallback is AUDCLNT_STREAMFLAGS_EVENTCALLBACK.
// The capture loop polls GetNextPacketSize and never calls SetEventHandle.
// Setting this bit makes Start fail with AUDCLNT_E_EVENTHANDLE_NOT_SET.
const audclntStreamFlagsEventCallback uint32 = 0x00040000

// processLoopbackInit is the IAudioClient.Initialize pair for VAD\Process_Loopback.
// The virtual device rejects a zero stream-flag word with
// AUDCLNT_E_INVALID_STREAM_FLAG (0x88890021), so capture never starts and a
// web note keeps only pictures. LOOPBACK here opens a capture buffer on this
// already process-scoped render device. It is not the default render endpoint,
// so it does not record other programs. Buffer duration 0 lets the engine
// choose the period. Event callbacks are not used; the capture loop polls.
func processLoopbackInit() (flags uint32, duration uintptr) {
	return audclntStreamFlagsLoopback | audclntStreamFlagsAutoConvertPCM, 0
}

func audioClientInitialize(client uintptr, format *waveFormatExtensible) error {
	if client == 0 || format == nil || format.FormatTag != waveFormatExtensibleTag || format.Size < 22 || guidZero(format.SubFormat) {
		return errors.New("process loopback format is not WAVEFORMATEXTENSIBLE")
	}
	encoded := loopbackExtensibleBytes(*format)
	if len(encoded) < 40 {
		return errors.New("process loopback format dropped the extensible tail")
	}
	flags, duration := processLoopbackInit()
	if flags&audclntStreamFlagsLoopback == 0 || flags&audclntStreamFlagsAutoConvertPCM == 0 {
		return errors.New("process loopback Initialize is missing AUDCLNT_STREAMFLAGS_LOOPBACK or AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM")
	}
	if flags&audclntStreamFlagsEventCallback != 0 {
		return errors.New("process loopback Initialize set AUDCLNT_STREAMFLAGS_EVENTCALLBACK without an event handle")
	}
	vtbl := *(*uintptr)(unsafe.Pointer(client))
	// IAudioClient::Initialize is vtable slot 3.
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 3*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, client, 0, uintptr(flags), duration, 0, uintptr(unsafe.Pointer(&encoded[0])), 0)
	runtime.KeepAlive(encoded)
	if int32(hr) < 0 {
		return fmt.Errorf("IAudioClient.Initialize 0x%x", hr)
	}
	return nil
}

func audioGetCaptureClient(client uintptr) (uintptr, error) {
	vtbl := *(*uintptr)(unsafe.Pointer(client))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 14*unsafe.Sizeof(uintptr(0))))
	iid := windows.GUID{
		Data1: 0xC8ADBD64,
		Data2: 0xE71E,
		Data3: 0x48a0,
		Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17},
	}
	var capture uintptr
	hr, _, _ := syscall.SyscallN(proc, client, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&capture)))
	if int32(hr) < 0 || capture == 0 {
		return 0, fmt.Errorf("GetService IAudioCaptureClient 0x%x", hr)
	}
	return capture, nil
}

func audioClientStart(client uintptr) error {
	return audioClientCall0(client, 10, "Start")
}

func audioClientStop(client uintptr) {
	_ = audioClientCall0(client, 11, "Stop")
}

func audioClientCall0(client uintptr, index int, name string) error {
	vtbl := *(*uintptr)(unsafe.Pointer(client))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, client)
	if int32(hr) < 0 {
		return fmt.Errorf("IAudioClient.%s 0x%x", name, hr)
	}
	return nil
}

func audioNextPacket(capture uintptr, packets *uint32) error {
	vtbl := *(*uintptr)(unsafe.Pointer(capture))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 5*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, capture, uintptr(unsafe.Pointer(packets)))
	if int32(hr) < 0 {
		return fmt.Errorf("GetNextPacketSize 0x%x", hr)
	}
	return nil
}

func audioGetBuffer(capture uintptr, blockAlign uint16) ([]byte, uint32, uint32, error) {
	vtbl := *(*uintptr)(unsafe.Pointer(capture))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 3*unsafe.Sizeof(uintptr(0))))
	var data uintptr
	var frames uint32
	var flags uint32
	hr, _, _ := syscall.SyscallN(proc, capture, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0)
	if int32(hr) < 0 {
		return nil, 0, 0, fmt.Errorf("GetBuffer 0x%x", hr)
	}
	if data == 0 || frames == 0 || flags&1 != 0 || blockAlign == 0 {
		return nil, frames, flags, nil
	}
	return audioCopyFrames(data, frames, uint32(blockAlign)), frames, flags, nil
}

func audioCopyFrames(data uintptr, frames, blockAlign uint32) []byte {
	if data == 0 || frames == 0 || blockAlign == 0 {
		return nil
	}
	n := int(frames * blockAlign)
	if n <= 0 || n > 1<<20 {
		return nil
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(data)), n)
	out := make([]byte, n)
	copy(out, src)
	return out
}

func audioReleaseBuffer(capture uintptr, frames uint32) error {
	vtbl := *(*uintptr)(unsafe.Pointer(capture))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + 4*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, capture, uintptr(frames))
	if int32(hr) < 0 {
		return fmt.Errorf("ReleaseBuffer 0x%x", hr)
	}
	return nil
}

func mixTo16kMono(raw []byte, format *waveFormatExtensible) []byte {
	mono := downmixLoopback(raw, format)
	if len(mono) == 0 || format == nil || format.SamplesPerSec == 0 {
		return nil
	}
	return resampleMono(mono, int(format.SamplesPerSec), 16000)
}

// downmixLoopback averages channels to one int16 per source frame.
func downmixLoopback(raw []byte, format *waveFormatExtensible) []int16 {
	if format == nil || format.Channels == 0 || format.BlockAlign == 0 || format.SamplesPerSec == 0 {
		return nil
	}
	kind := loopbackSampleKind(format)
	if kind == 0 {
		return nil
	}
	channels := int(format.Channels)
	block := int(format.BlockAlign)
	if block < channels {
		return nil
	}
	frames := len(raw) / block
	if frames == 0 {
		return nil
	}
	mono := make([]int16, frames)
	switch kind {
	case loopbackKindPCM16:
		if block < channels*2 {
			return nil
		}
		for i := 0; i < frames; i++ {
			base := i * block
			var sum int
			for c := 0; c < channels; c++ {
				sum += int(int16(binary.LittleEndian.Uint16(raw[base+c*2:])))
			}
			mono[i] = int16(sum / channels)
		}
	case loopbackKindFloat32:
		if block < channels*4 {
			return nil
		}
		for i := 0; i < frames; i++ {
			base := i * block
			var sum float32
			for c := 0; c < channels; c++ {
				sum += float32FromBits(binary.LittleEndian.Uint32(raw[base+c*4:]))
			}
			mono[i] = floatToS16(sum / float32(channels))
		}
	case loopbackKindPCM32:
		if block < channels*4 {
			return nil
		}
		for i := 0; i < frames; i++ {
			base := i * block
			var sum int64
			for c := 0; c < channels; c++ {
				sum += int64(int32(binary.LittleEndian.Uint32(raw[base+c*4:])))
			}
			mono[i] = int16((sum / int64(channels)) >> 16)
		}
	default:
		return nil
	}
	return mono
}

// loopbackPCMSink mixes device packets into 16 kHz mono as they arrive.
// The point-sample index is the same one resampleMono uses on the whole buffer,
// so a packet boundary does not drop or repeat a sample.
type loopbackPCMSink struct {
	format   *waveFormatExtensible
	srcIndex int
	nextI    int
	pending  []int16
	pcm      []byte
	saw      bool
}

func (s *loopbackPCMSink) addRaw(raw []byte) {
	s.addMono(downmixLoopback(raw, s.format))
}

func (s *loopbackPCMSink) addSilence(frames uint32) {
	if frames == 0 {
		return
	}
	s.addMono(make([]int16, frames))
}

func (s *loopbackPCMSink) addMono(mono []int16) {
	if s == nil || s.format == nil || s.format.SamplesPerSec == 0 || len(mono) == 0 {
		return
	}
	s.saw = true
	s.pending = append(s.pending, mono...)
	srcRate := int(s.format.SamplesPerSec)
	const dstRate = 16000
	total := s.srcIndex + len(s.pending)
	outFrames := total
	if srcRate != dstRate {
		outFrames = total * dstRate / srcRate
	}
	for s.nextI < outFrames {
		pos := s.nextI
		if srcRate != dstRate {
			pos = s.nextI * srcRate / dstRate
		}
		if pos >= total {
			pos = total - 1
		}
		local := pos - s.srcIndex
		if local < 0 || local >= len(s.pending) {
			break
		}
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(s.pending[local]))
		s.pcm = append(s.pcm, buf[:]...)
		s.nextI++
	}
	nextPos := s.nextI
	if srcRate != dstRate {
		nextPos = s.nextI * srcRate / dstRate
	}
	drop := nextPos - s.srcIndex
	if drop >= len(s.pending) {
		s.srcIndex += len(s.pending)
		s.pending = nil
		return
	}
	if drop > 0 {
		s.pending = append([]int16(nil), s.pending[drop:]...)
		s.srcIndex += drop
	}
}

func (s *loopbackPCMSink) bytes() []byte {
	if s == nil {
		return nil
	}
	// resampleMono emits one sample when the buffer is shorter than one output frame.
	if len(s.pcm) == 0 && s.saw && len(s.pending) > 0 {
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(s.pending[0]))
		s.pcm = append(s.pcm, buf[:]...)
	}
	return s.pcm
}

const (
	loopbackKindPCM16 = iota + 1
	loopbackKindFloat32
	loopbackKindPCM32
)

func loopbackSampleKind(format *waveFormatExtensible) int {
	if format == nil {
		return 0
	}
	switch format.FormatTag {
	case waveFormatPCM:
		if format.BitsPerSample == 16 {
			return loopbackKindPCM16
		}
		return 0
	case waveFormatIEEEFloat:
		if format.BitsPerSample == 32 {
			return loopbackKindFloat32
		}
		return 0
	case waveFormatExtensibleTag:
		if format.Size < 22 || guidZero(format.SubFormat) {
			return 0
		}
		switch {
		case guidEqual(format.SubFormat, ksDataFormatSubtypeIEEEFloat):
			if format.BitsPerSample < 32 {
				return 0
			}
			return loopbackKindFloat32
		case guidEqual(format.SubFormat, ksDataFormatSubtypePCM):
			bits := format.ValidBits
			if bits == 0 {
				bits = format.BitsPerSample
			}
			switch bits {
			case 16:
				return loopbackKindPCM16
			case 32:
				return loopbackKindPCM32
			default:
				return 0
			}
		default:
			return 0
		}
	default:
		return 0
	}
}

func guidEqual(a, b windows.GUID) bool {
	return a == b
}

func guidZero(g windows.GUID) bool {
	return g == (windows.GUID{})
}

func float32FromBits(u uint32) float32 {
	return *(*float32)(unsafe.Pointer(&u))
}

func floatToS16(v float32) int16 {
	if v > 1 {
		v = 1
	}
	if v < -1 {
		v = -1
	}
	return int16(v * 32767)
}

func resampleMono(in []int16, srcRate, dstRate int) []byte {
	if len(in) == 0 || srcRate <= 0 || dstRate <= 0 {
		return nil
	}
	if srcRate == dstRate {
		out := make([]byte, len(in)*2)
		for i, s := range in {
			binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
		}
		return out
	}
	outFrames := len(in) * dstRate / srcRate
	if outFrames < 1 {
		outFrames = 1
	}
	out := make([]byte, outFrames*2)
	for i := 0; i < outFrames; i++ {
		pos := i * srcRate / dstRate
		if pos >= len(in) {
			pos = len(in) - 1
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(in[pos]))
	}
	return out
}
