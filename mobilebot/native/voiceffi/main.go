// libmaclaw_voice — the desktop's own voice engines compiled for mobile.
//
// Built with `-buildmode=c-shared` (GOOS=android). Kokoro TTS and SenseVoice
// ASR are the exact same corelib packages MaClaw GUI uses; the Go arm64
// kernels (conv_fused_arm64, simd_dot_arm64) carry the NEON acceleration.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

var (
	mu        sync.Mutex
	ttsMgr    *tts.Manager
	asrMgr    *asr.Manager
	lastError string
)

func setErr(err error) *C.char {
	if err == nil {
		lastError = ""
		return nil
	}
	lastError = err.Error()
	return C.CString(lastError)
}

func cbytes(b []byte, outLen *C.int) *C.char {
	if len(b) == 0 {
		if outLen != nil {
			*outLen = 0
		}
		return nil
	}
	p := C.malloc(C.size_t(len(b)))
	copy(unsafe.Slice((*byte)(p), len(b)), b)
	if outLen != nil {
		*outLen = C.int(len(b))
	}
	return (*C.char)(p)
}

//export VoiceLastError
func VoiceLastError() *C.char {
	mu.Lock()
	defer mu.Unlock()
	if lastError == "" {
		return nil
	}
	return C.CString(lastError)
}

//export VoiceFree
func VoiceFree(p unsafe.Pointer) {
	C.free(p)
}

//export VoiceLoadTTS
func VoiceLoadTTS(modelPath, voiceDir, voiceID *C.char) *C.char {
	mu.Lock()
	defer mu.Unlock()
	mp, vd, v := C.GoString(modelPath), C.GoString(voiceDir), C.GoString(voiceID)
	if mp == "" || vd == "" {
		return setErr(errString("voice: model and voice dir are required"))
	}
	if ttsMgr != nil {
		ttsMgr.Unload()
	}
	// Keep the model resident — the voice loop calls back-to-back turns and a
	// mobile reload is seconds of dead air between replies.
	ttsMgr = tts.NewKokoroManager(mp, vd, v)
	ttsMgr.SetUnloadDelay(0)
	return setErr(nil)
}

//export VoiceUnloadTTS
func VoiceUnloadTTS() {
	mu.Lock()
	defer mu.Unlock()
	if ttsMgr != nil {
		ttsMgr.Unload()
		ttsMgr = nil
	}
}

// VoiceTTS synthesizes text to WAV bytes. Long input is split on the same
// semantic chunker the desktop uses so a multi-paragraph reply still speaks.
//
//export VoiceTTS
func VoiceTTS(text *C.char, speed C.double, outLen *C.int) *C.char {
	mu.Lock()
	defer mu.Unlock()
	if ttsMgr == nil {
		return setErr(errString("voice: tts not loaded"))
	}
	if outLen != nil {
		*outLen = 0
	}
	s := C.GoString(text)
	sp := float32(speed)
	if sp <= 0 {
		sp = 1
	}
	var pcm []float32
	for _, chunk := range tts.SplitSpeechChunks(s, tts.MaxSafeSpeechChunkRunes) {
		part, _, err := ttsMgr.SynthesizeAudioAtSpeed(chunk, sp)
		if err != nil {
			setErr(err)
			return nil
		}
		pcm = append(pcm, part...)
	}
	if len(pcm) == 0 {
		setErr(errString("voice: empty synthesis"))
		return nil
	}
	var buf bytes.Buffer
	if err := kokoro.WriteWAVTo(&buf, pcm, kokoro.DefaultSampleRate); err != nil {
		setErr(err)
		return nil
	}
	setErr(nil)
	return cbytes(buf.Bytes(), outLen)
}

//export VoiceLoadASR
func VoiceLoadASR(modelPath *C.char) *C.char {
	mu.Lock()
	defer mu.Unlock()
	mp := C.GoString(modelPath)
	if mp == "" {
		return setErr(errString("voice: asr model path required"))
	}
	if asrMgr != nil {
		asrMgr.Unload()
	}
	asrMgr = asr.NewManager(mp)
	asrMgr.SetUnloadDelay(0)
	return setErr(nil)
}

//export VoiceUnloadASR
func VoiceUnloadASR() {
	mu.Lock()
	defer mu.Unlock()
	if asrMgr != nil {
		asrMgr.Unload()
		asrMgr = nil
	}
}

// VoiceASR transcribes raw PCM — signed 16-bit little-endian mono at the
// SenseVoice rate (16 kHz), which is what the `record` plugin streams.
//
//export VoiceASR
func VoiceASR(pcm *C.char, pcmLen C.int) *C.char {
	mu.Lock()
	defer mu.Unlock()
	if asrMgr == nil {
		return setErr(errString("voice: asr not loaded"))
	}
	raw := C.GoBytes(unsafe.Pointer(pcm), pcmLen)
	if len(raw) < 2 {
		setErr(errString("voice: empty pcm"))
		return nil
	}
	samples := make([]float32, len(raw)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
	}
	text, err := asrMgr.Transcribe(samples)
	if err != nil {
		setErr(err)
		return nil
	}
	setErr(nil)
	return C.CString(text)
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
func errString(s string) error  { return simpleErr(s) }

func main() {}
