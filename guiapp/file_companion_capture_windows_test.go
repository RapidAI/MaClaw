//go:build windows

package guiapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestExecuteScriptSlotSitsBeforeCapturePreview(t *testing.T) {
	if iCoreWebView2ExecuteScript != 29 {
		t.Fatalf("ExecuteScript slot = %d", iCoreWebView2ExecuteScript)
	}
}

func TestPlaybackScriptQueryRejectsAgile(t *testing.T) {
	h := &playbackScriptHandler{vtbl: playbackScriptCallbacks(), refs: 1}
	this := uintptr(unsafe.Pointer(h))
	playbackScriptHandlers.Store(this, h)
	t.Cleanup(func() { playbackScriptHandlers.Delete(this) })
	agile := windows.GUID{
		Data1: 0x94EA2B94, Data2: 0xE9CC, Data3: 0x49E0,
		Data4: [8]byte{0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90},
	}
	var out uintptr = 1
	hr := playbackScriptQuery(this, uintptr(unsafe.Pointer(&agile)), uintptr(unsafe.Pointer(&out)))
	if hr == uintptr(windows.S_OK) || out != 0 {
		t.Fatalf("agile query hr=0x%x out=%x", hr, out)
	}
	out = 0
	hr = playbackScriptQuery(this, uintptr(unsafe.Pointer(&guidExecuteScriptCompleted)), uintptr(unsafe.Pointer(&out)))
	if hr != uintptr(windows.S_OK) || out != this {
		t.Fatalf("handler query hr=0x%x out=%x", hr, out)
	}
}

func TestFinishedCapturePreviewKeepsAReadyFrameWhenStopWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan previewResult, 1)
	ch <- previewResult{png: []byte{1, 2, 3}}
	png, err := finishedCapturePreview(ctx, ch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(png, []byte{1, 2, 3}) {
		t.Fatalf("png = %v", png)
	}
}

func TestFinishedCapturePreviewWaitsForTheFrameWhenThePlayerStays(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan previewResult, 1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- previewResult{png: []byte{7}}
	}()
	png, err := finishedCapturePreview(ctx, ch, time.After(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(png, []byte{7}) {
		t.Fatalf("png = %v", png)
	}
}

func TestFinishedCapturePreviewDoesNotWaitWhenThePlayerLeft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gone, cancelGone := context.WithCancel(context.Background())
	cancelGone()
	ctx = withCaptureDrop(ctx, nil, gone)
	ch := make(chan previewResult, 1)
	ch <- previewResult{png: []byte{7}}
	start := time.Now()
	_, err := finishedCapturePreview(ctx, ch, nil)
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("waited for a preview after the player left")
	}
	if !errors.Is(err, errCompanionPlayerLeft) {
		t.Fatalf("err = %v", err)
	}
}

func TestFinishedCapturePreviewLeavesDuringTheStopWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	player, playerCancel := context.WithCancel(context.Background())
	defer playerCancel()
	ctx = withCaptureDrop(ctx, nil, player)
	ch := make(chan previewResult, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		playerCancel()
	}()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	start := time.Now()
	_, err := finishedCapturePreview(ctx, ch, timeout.C)
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("kept waiting after the player left")
	}
	if !errors.Is(err, errCompanionPlayerLeft) {
		t.Fatalf("err = %v", err)
	}
}

func TestFinishedCapturePreviewNoteClosesDuringTheStopWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	note, noteCancel := context.WithCancel(context.Background())
	defer noteCancel()
	ctx = withCaptureDrop(ctx, note, nil)
	ch := make(chan previewResult, 1)
	go func() {
		time.Sleep(30 * time.Millisecond)
		noteCancel()
	}()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	start := time.Now()
	_, err := finishedCapturePreview(ctx, ch, timeout.C)
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("kept waiting after the note closed")
	}
	if !errors.Is(err, errCompanionPlayerLeft) {
		t.Fatalf("err = %v", err)
	}
}

func TestWindowsProcessLoopbackPlan(t *testing.T) {
	act := companionProcessAudioActivate(uint32(os.Getpid()))
	plan := windowsProcessLoopbackPlan(act)
	if plan.DeviceID != virtualAudioDeviceProcessLoopback {
		t.Fatalf("device = %q", plan.DeviceID)
	}
	if plan.DeviceID != `VAD\Process_Loopback` {
		t.Fatalf("device id = %q", plan.DeviceID)
	}
	if plan.ActivationType != audioClientActivationTypeProcessLoopback {
		t.Fatalf("activation type = %d", plan.ActivationType)
	}
	if plan.Mode != processLoopbackModeIncludeTargetProcessTree {
		t.Fatalf("mode = %d", plan.Mode)
	}
	if plan.TargetPID != uint32(os.Getpid()) || plan.StreamFlags != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.StreamFlags&audclntStreamFlagsLoopback != 0 {
		t.Fatal("plan set AUDCLNT_STREAMFLAGS_LOOPBACK")
	}
	params := audioClientActivationParams{
		ActivationType:      plan.ActivationType,
		TargetProcessID:     plan.TargetPID,
		ProcessLoopbackMode: plan.Mode,
	}
	if unsafe.Sizeof(params) != 12 {
		t.Fatalf("activation params size = %d", unsafe.Sizeof(params))
	}
}

func TestWebViewLoopbackPIDIsTheBrowserNotTheHost(t *testing.T) {
	rows := []loopbackProcess{
		{PID: 10, Image: "MaClaw.exe"},
		{PID: 20, PPID: 10, Image: "msedgewebview2.exe"},
		{PID: 21, PPID: 20, Image: "msedgewebview2.exe"},
		{PID: 22, PPID: 20, Image: "msedgewebview2.exe"},
	}
	if got := webViewLoopbackPID(10, rows); got != 20 {
		t.Fatalf("browser pid = %d", got)
	}
	if got := webViewLoopbackPID(10, nil); got != 10 {
		t.Fatalf("no browser = %d", got)
	}
	if got := webViewLoopbackPID(0, rows); got != 0 {
		t.Fatalf("host 0 = %d", got)
	}
	unrelated := []loopbackProcess{
		{PID: 10, Image: "MaClaw.exe"},
		{PID: 20, PPID: 99, Image: "msedgewebview2.exe"},
		{PID: 21, PPID: 20, Image: "MSEdgeWebView2.EXE"},
	}
	if got := webViewLoopbackPID(10, unrelated); got != 10 {
		t.Fatalf("unrelated browser = %d", got)
	}
	two := []loopbackProcess{
		{PID: 10, Image: "MaClaw.exe"},
		{PID: 30, PPID: 10, Image: "msedgewebview2.exe"},
		{PID: 31, PPID: 30, Image: "msedgewebview2.exe"},
		{PID: 40, PPID: 10, Image: "msedgewebview2.exe"},
		{PID: 41, PPID: 40, Image: "msedgewebview2.exe"},
		{PID: 42, PPID: 40, Image: "msedgewebview2.exe"},
	}
	if got := webViewLoopbackPID(10, two); got != 40 {
		t.Fatalf("busiest browser = %d", got)
	}
	if got := companionLoopbackPID(uint32(os.Getpid())); got != uint32(os.Getpid()) {
		t.Fatalf("test process loopback pid = %d, want itself", got)
	}
}

func TestProcessLoopbackExtensibleMix(t *testing.T) {
	candidates := processLoopbackFormatCandidates()
	if len(candidates) != 3 {
		t.Fatalf("candidates = %d", len(candidates))
	}
	float48 := candidates[0]
	float441 := candidates[1]
	pcm48 := candidates[2]
	if float48.SamplesPerSec != 48000 || float441.SamplesPerSec != 44100 || pcm48.SamplesPerSec != 48000 {
		t.Fatalf("rates = %d %d %d", float48.SamplesPerSec, float441.SamplesPerSec, pcm48.SamplesPerSec)
	}
	if float48.FormatTag != 0xFFFE || float48.Channels != 2 || float48.BitsPerSample != 32 || float48.Size < 22 {
		t.Fatalf("float format = %+v", float48.waveFormatEx)
	}
	if pcm48.FormatTag != 0xFFFE || pcm48.BitsPerSample != 16 || pcm48.SubFormat != ksDataFormatSubtypePCM {
		t.Fatalf("pcm format = %+v sub=%+v", pcm48.waveFormatEx, pcm48.SubFormat)
	}
	if float48.SubFormat != ksDataFormatSubtypeIEEEFloat || float441.SubFormat != ksDataFormatSubtypeIEEEFloat {
		t.Fatalf("float subformat = %+v", float48.SubFormat)
	}
	if unsafe.Sizeof(float48) < 40 {
		t.Fatalf("sizeof WAVEFORMATEXTENSIBLE = %d", unsafe.Sizeof(float48))
	}
	encoded := loopbackExtensibleBytes(float48)
	if len(encoded) < 40 {
		t.Fatalf("format bytes = %d", len(encoded))
	}
	if binary.LittleEndian.Uint16(encoded[0:]) != 0xFFFE {
		t.Fatalf("tag = %x", encoded[:2])
	}
	if binary.LittleEndian.Uint16(encoded[16:]) != 22 {
		t.Fatalf("cbSize = %d", binary.LittleEndian.Uint16(encoded[16:]))
	}
	wantGUID := []byte{
		0x03, 0x00, 0x00, 0x00,
		0x00, 0x00,
		0x10, 0x00,
		0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71,
	}
	if !bytes.Equal(encoded[24:40], wantGUID) {
		t.Fatalf("subformat bytes = %x", encoded[24:40])
	}
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint32(raw[0:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(raw[4:], math.Float32bits(1))
	binary.LittleEndian.PutUint32(raw[8:], math.Float32bits(float32(-1)))
	binary.LittleEndian.PutUint32(raw[12:], math.Float32bits(float32(-1)))
	pcm := mixTo16kMono(raw, &float48)
	if len(pcm) == 0 {
		t.Fatal("extensible float mix was empty")
	}
	if got := int16(binary.LittleEndian.Uint16(pcm)); got != 32767 {
		t.Fatalf("mixed sample = %d", got)
	}
	var blank waveFormatExtensible
	zeroed := float48
	zeroed.SubFormat = blank.SubFormat
	if mixTo16kMono(raw, &zeroed) != nil {
		t.Fatal("zero SubFormat was mixed")
	}
	shortTail := float48
	shortTail.Size = 0
	if mixTo16kMono(raw, &shortTail) != nil {
		t.Fatal("extensible format without a tail was mixed")
	}
	pcmRaw := make([]byte, 8)
	neg := int16(-32767)
	binary.LittleEndian.PutUint16(pcmRaw[0:], 32767)
	binary.LittleEndian.PutUint16(pcmRaw[2:], 32767)
	binary.LittleEndian.PutUint16(pcmRaw[4:], uint16(neg))
	binary.LittleEndian.PutUint16(pcmRaw[6:], uint16(neg))
	if len(mixTo16kMono(pcmRaw, &pcm48)) == 0 {
		t.Fatal("extensible pcm16 mix was empty")
	}
}

func TestLoopbackPCMSinkMatchesWholeBuffer(t *testing.T) {
	shortFmt := processLoopbackFormatCandidates()[0]
	shortRaw := make([]byte, int(shortFmt.BlockAlign)*2)
	fillLoopbackFrame(shortRaw[:shortFmt.BlockAlign], shortFmt, 0)
	fillLoopbackFrame(shortRaw[shortFmt.BlockAlign:], shortFmt, 1)
	shortWant := mixTo16kMono(shortRaw, &shortFmt)
	shortSink := loopbackPCMSink{format: &shortFmt}
	shortSink.addRaw(shortRaw[:shortFmt.BlockAlign])
	shortSink.addRaw(shortRaw[shortFmt.BlockAlign:])
	if !bytes.Equal(shortSink.bytes(), shortWant) {
		t.Fatalf("short packet mix = %v, whole = %v", shortSink.bytes(), shortWant)
	}
	for _, format := range processLoopbackFormatCandidates() {
		block := int(format.BlockAlign)
		frames := 4800
		if format.SamplesPerSec == 44100 {
			frames = 4410
		}
		raw := make([]byte, frames*block)
		for i := 0; i < frames; i++ {
			fillLoopbackFrame(raw[i*block:(i+1)*block], format, i)
		}
		// A silent packet in the middle must land on the same samples as zeros in the device buffer.
		silentAt := frames / 2
		silentN := 50
		for i := silentAt; i < silentAt+silentN && i < frames; i++ {
			for j := 0; j < block; j++ {
				raw[i*block+j] = 0
			}
		}
		want := mixTo16kMono(raw, &format)
		sink := loopbackPCMSink{format: &format}
		for i := 0; i < frames; i++ {
			if i >= silentAt && i < silentAt+silentN {
				sink.addSilence(1)
				continue
			}
			sink.addRaw(raw[i*block : (i+1)*block])
		}
		got := sink.bytes()
		if !bytes.Equal(got, want) {
			t.Fatalf("rate %d packet mix = %d bytes, whole buffer = %d", format.SamplesPerSec, len(got), len(want))
		}
	}
}

func TestLoopbackCeilingKeepsFullWebNote(t *testing.T) {
	sec := int(fileCompanionNoteMaxDuration / time.Second)
	mixed := sec * 16000 * 2
	rawFloatStereo := sec * 48000 * 8
	if loopbackAudioStopped(mixed) {
		t.Fatalf("16 kHz mono for %ds is %d bytes and hits the ceiling", sec, mixed)
	}
	if rawFloatStereo <= loopbackKeptPCMCeiling {
		t.Fatalf("48 kHz float stereo for %ds is %d bytes, which does not exceed the ceiling", sec, rawFloatStereo)
	}
	if !loopbackAudioStopped(loopbackKeptPCMCeiling + 1) {
		t.Fatal("pcm past the ceiling kept recording")
	}
}

func TestCapturePreviewCreatorRefDropsWhenInvokeFinishes(t *testing.T) {
	for _, code := range []uintptr{0, 0x80004005} {
		h := &capturePreviewHandler{
			vtbl: capturePreviewCallbacks(),
			ch:   make(chan previewResult, 1),
			refs: 1,
		}
		key := uintptr(unsafe.Pointer(h))
		capturePreviewHandlers.Store(key, h)
		t.Cleanup(func() { capturePreviewHandlers.Delete(key) })

		if got := capturePreviewAddRef(key); got != 2 {
			t.Fatalf("code 0x%x addref = %d", code, got)
		}
		capturePreviewDone(key, code)
		if _, ok := capturePreviewHandlers.Load(key); !ok {
			t.Fatalf("code 0x%x dropped the handler while WebView2 still held it", code)
		}
		if h.refs != 1 {
			t.Fatalf("code 0x%x refs after invoke = %d", code, h.refs)
		}
		h.releaseCreator()
		if h.refs != 1 {
			t.Fatalf("code 0x%x second creator release refs = %d", code, h.refs)
		}
		if _, ok := capturePreviewHandlers.Load(key); !ok {
			t.Fatalf("code 0x%x second creator release unpinned the handler", code)
		}
		if got := capturePreviewRelease(key); got != 0 {
			t.Fatalf("code 0x%x webview release = %d", code, got)
		}
		if _, ok := capturePreviewHandlers.Load(key); ok {
			t.Fatalf("code 0x%x handler stayed pinned", code)
		}
		select {
		case got := <-h.ch:
			if got.err == nil || len(got.png) != 0 {
				t.Fatalf("code 0x%x result = png %d err %v", code, len(got.png), got.err)
			}
		default:
			t.Fatalf("code 0x%x did not deliver a result", code)
		}
	}
}

func TestProcessLoopbackInitializeArmsTheVirtualDevice(t *testing.T) {
	flags, duration := processLoopbackInit()
	if flags&audclntStreamFlagsLoopback == 0 {
		t.Fatal("Initialize must set AUDCLNT_STREAMFLAGS_LOOPBACK on the process-loopback device")
	}
	if flags&audclntStreamFlagsAutoConvertPCM == 0 {
		t.Fatal("Initialize must set AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM so the engine accepts the capture format")
	}
	if flags&audclntStreamFlagsEventCallback != 0 {
		t.Fatal("polling capture must not require an event handle")
	}
	if duration != 0 {
		t.Fatalf("buffer duration = %d, want the engine default", duration)
	}
	plan := windowsProcessLoopbackPlan(companionProcessAudioActivate(uint32(os.Getpid())))
	if plan.StreamFlags != 0 || plan.Mode != processLoopbackModeIncludeTargetProcessTree {
		t.Fatalf("activation = %+v", plan)
	}
}

func fillLoopbackFrame(dst []byte, format waveFormatExtensible, i int) {
	amp := float32((i%11)-5) / 5
	if amp == 0 {
		amp = 0.25
	}
	switch loopbackSampleKind(&format) {
	case loopbackKindFloat32:
		bits := math.Float32bits(amp)
		for off := 0; off+4 <= len(dst); off += 4 {
			binary.LittleEndian.PutUint32(dst[off:], bits)
		}
	case loopbackKindPCM16:
		sample := uint16(int16(amp * 32767))
		for off := 0; off+2 <= len(dst); off += 2 {
			binary.LittleEndian.PutUint16(dst[off:], sample)
		}
	default:
		for off := range dst {
			dst[off] = byte(i + off)
		}
	}
}
