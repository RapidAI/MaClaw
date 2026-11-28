// libmaclaw_voice — the desktop's own voice engines compiled for mobile.
//
// Built with `-buildmode=c-shared` (GOOS=android). Kokoro TTS and SenseVoice
// ASR are the exact same corelib packages MaClaw GUI uses.
//
// Android arm64 acceleration:
//   - NEON is always on. A c-shared library often cannot see the auxiliary
//     vector, and treating that as "no SIMD" used to drop every kernel.
//   - SenseVoice matmuls whose shapes are multiples of 32 run as int8×int8
//     SDOT (Snapdragon 855+) or SMMLA (8 Gen 1, 8 Elite). Feature bits are
//     read from /proc/cpuinfo when the auxv is empty. Bias and ReLU are
//     applied in that kernel's 32-wide store, so the encoder does not read
//     the projection back.
//   - Kokoro sequence linears are one GEMM, and 1×1 convs use that GEMM too.
//   - Vocoder k>1 convs use a NEON 4-channel dot. The old arm64 body was scalar Go.
//   - Upsample conv-transpose pairs two output times that share a weight row.
//   - Snake, iSTFT, LSTM gates, PL-BERT GELU, and attention softmax use a
//     float32 sin/exp instead of libm. The iSTFT takes sin and cos from
//     one reduction per bin. Source STFT and iSTFT reuse one frame buffer.
//     The 20-point source transform and its magnitude/phase use float32.
//     The source STFT reads its reflect pad in place. Both vocoder stages
//     share that spectrum. The harmonic bank builds its upsample grid once
//     and mixes straight into the source. After the noise source is added,
//     that buffer is the scratch for the other two resblocks.
//   - PL-BERT attention is two GEMMs per head. Vocoder resblock temps are
//     reused across dilations. Each stage writes the first of its three
//     resblocks into the accumulator and reuses one scratch for the other two.
//     A resblock keeps two temps: the second snake reuses the first, and the
//     first dilation adds the input directly.
//   - LSTM input weights are one GEMM per phoneme sequence. Recurrent
//     weights use the NEON 4-row dot, so each hidden vector is loaded once
//     per four gate rows.
//   - GOMAXPROCS follows the big-core count from cpu_capacity when the chip
//     is heterogeneous. Threads are not pinned; fewer busy threads land on
//     the big cores. A homogeneous chip is left alone.
//
// Hexagon HTP is not used for these layers. The weights are Q8_0 (a scale
// every 32 K elements). NNAPI's fully connected wants one scale per row, and
// a per-layer trip to the NPU is slower than SMMLA at encoder sizes.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"log"
	"sync"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
	"github.com/RapidAI/CodeClaw/mobilebot/native/perfcpus"
)

func init() {
	if procs, total, ok := perfcpus.Apply(); ok {
		log.Printf("[voice] procs=%d/%d", procs, total)
	}
	log.Printf("[voice] tts %s asr-q8p=%s", kokoro.AccelNote(), tensor.Q8PISAName())
}

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
func errString(s string) error    { return simpleErr(s) }

func main() {}
