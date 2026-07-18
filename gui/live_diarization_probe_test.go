package main

import (
	"math"
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/vad"
)

// ttsTo16kWAV converts an arbitrary test WAV (e.g. 22050 Hz TTS output) to the
// 16 kHz mono WAV the ASR/diarization pipelines consume.
func ttsTo16kWAV(data []byte) ([]byte, error) {
	return audioconv.ToWAV(data, "")
}

// TestLiveDiarizationProbeDiagnostics inspects the raw fixtures: duration,
// peak amplitude, VAD speech probability and plain ASR output — to distinguish
// "fixture is silent/garbage" from "diarization pipeline is broken".
func TestLiveDiarizationProbeDiagnostics(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_DIAR_PROBE") == "" {
		t.Skip("set MACLAW_LIVE_DIAR_PROBE=1 to run the live diarization probe")
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{DiarizationEnabled: true, ASREnabled: true}}
	for _, name := range []string{"../beiing_16k.wav", "../zhou_16k.wav"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		pcm, err := asr.WAVToFloat32(data)
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		var peak float64
		for _, s := range pcm {
			if v := math.Abs(float64(s)); v > peak {
				peak = v
			}
		}
		t.Logf("%s: dur=%.2fs samples=%d peak=%.4f", name, float64(len(pcm))/16000, len(pcm), peak)

		vm, err := vad.Load()
		if err != nil {
			t.Fatalf("vad load: %v", err)
		}
		state := vm.NewState()
		speechWins, totalWins := 0, 0
		var maxProb float32
		for off := 0; off+512 <= len(pcm); off += 512 {
			prob, err := vm.Detect(pcm[off:off+512], state)
			if err != nil {
				t.Fatalf("vad detect: %v", err)
			}
			if prob > maxProb {
				maxProb = prob
			}
			totalWins++
			if prob >= 0.5 {
				speechWins++
			}
		}
		t.Logf("%s: VAD speech_windows=%d/%d max_prob=%.3f", name, speechWins, totalWins, maxProb)

		text, err := app.TranscribeWAVBytes(data)
		if err != nil {
			t.Logf("%s: plain ASR error: %v", name, err)
		} else {
			t.Logf("%s: plain ASR text=%q", name, text)
		}
	}
}

// Live end-to-end probe for speaker diarization in the recording transcription
// path (DiarizeAndTranscribeWAVBytes + FormatSpeakerTurnsAuto). Skipped unless
// MACLAW_LIVE_DIAR_PROBE=1. Requires the real CAM++ and SenseVoice artifacts in
// the local models dir and the two 16k voice fixtures at the repo root.
//
//	go test ./gui -run TestLiveDiarizationProbe -v   (with env var set)
func TestLiveDiarizationProbeTwoSpeakers(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_DIAR_PROBE") == "" {
		t.Skip("set MACLAW_LIVE_DIAR_PROBE=1 to run the live diarization probe")
	}
	wavA, err := os.ReadFile("../beiing_16k.wav")
	if err != nil {
		t.Fatalf("read fixture A: %v", err)
	}
	// Fixture B must be a genuinely different voice: the root-level 16k
	// recordings are the same speaker (CAM++ cosine ~0.8), so use a TTS voice
	// from the tts testdata (cosine ~0.0 against fixture A).
	ttsB, err := os.ReadFile("../corelib/tts/testdata/xiao_ya_今天天气不错.wav")
	if err != nil {
		t.Fatalf("read fixture B: %v", err)
	}
	wavB, err := ttsTo16kWAV(ttsB)
	if err != nil {
		t.Fatalf("convert fixture B: %v", err)
	}
	pcmA, err := asr.WAVToFloat32(wavA)
	if err != nil {
		t.Fatalf("decode fixture A: %v", err)
	}
	pcmB, err := asr.WAVToFloat32(wavB)
	if err != nil {
		t.Fatalf("decode fixture B: %v", err)
	}

	// Synthetic two-speaker meeting: A B A B with silence gaps.
	const gapMs = 700
	combined, err := tts.ConcatenateWAVs([][]byte{wavA, wavB, wavA, wavB}, gapMs)
	if err != nil {
		t.Fatalf("concatenate fixtures: %v", err)
	}

	app := &App{configCacheValid: true, configCache: corelib.AppConfig{DiarizationEnabled: true, ASREnabled: true}}
	turns, err := app.DiarizeAndTranscribeWAVBytes(combined, 0)
	if err != nil {
		t.Fatalf("DiarizeAndTranscribeWAVBytes: %v", err)
	}
	for i, turn := range turns {
		t.Logf("turn %d: [%6.2f-%6.2f] speaker=%d text=%q", i, turn.Start, turn.End, turn.Speaker, turn.Text)
	}
	formatted := FormatSpeakerTurnsAuto(turns)
	t.Logf("formatted transcript:\n%s", formatted)

	// Ground-truth layout in seconds.
	dA := float64(len(pcmA)) / 16000.0
	dB := float64(len(pcmB)) / 16000.0
	g := float64(gapMs) / 1000.0
	type seg struct {
		start, end float64
		who        string
	}
	segs := []seg{
		{0, dA, "A"},
		{dA + g, dA + g + dB, "B"},
		{dA + g + dB + g, dA + g + dB + g + dA, "A"},
		{2*dA + 2*g + dB, 2*dA + 2*g + dB + dB, "B"},
	}
	clusterOf := map[string]int{}
	assigned := 0
	for _, turn := range turns {
		mid := (turn.Start + turn.End) / 2
		for _, s := range segs {
			if mid >= s.start && mid <= s.end {
				if c, ok := clusterOf[s.who]; ok && c != turn.Speaker {
					t.Fatalf("source %s split across clusters %d and %d (turn [%.2f-%.2f])", s.who, c, turn.Speaker, turn.Start, turn.End)
				}
				clusterOf[s.who] = turn.Speaker
				assigned++
				break
			}
		}
	}
	if assigned < 3 {
		t.Fatalf("only %d turns assignable to source segments (want >=3)", assigned)
	}
	cA, okA := clusterOf["A"]
	cB, okB := clusterOf["B"]
	if !okA || !okB {
		t.Fatalf("missing source coverage: clusterOf=%v", clusterOf)
	}
	if cA == cB {
		t.Fatalf("two distinct voices merged into one speaker cluster %d; formatted output would carry no speaker labels", cA)
	}
	t.Logf("OK: voice A -> cluster %d, voice B -> cluster %d, %d/%d turns assigned", cA, cB, assigned, len(turns))
}

// TestLiveDiarizationProbeSingleSpeaker feeds one voice twice; report-only —
// over-clustering a single speaker is a known diarization failure mode, so this
// logs the cluster count instead of failing.
func TestLiveDiarizationProbeSingleSpeaker(t *testing.T) {
	if os.Getenv("MACLAW_LIVE_DIAR_PROBE") == "" {
		t.Skip("set MACLAW_LIVE_DIAR_PROBE=1 to run the live diarization probe")
	}
	wavA, err := os.ReadFile("../beiing_16k.wav")
	if err != nil {
		t.Fatalf("read fixture A: %v", err)
	}
	combined, err := tts.ConcatenateWAVs([][]byte{wavA, wavA}, 700)
	if err != nil {
		t.Fatalf("concatenate fixtures: %v", err)
	}
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{DiarizationEnabled: true, ASREnabled: true}}
	turns, err := app.DiarizeAndTranscribeWAVBytes(combined, 0)
	if err != nil {
		t.Fatalf("DiarizeAndTranscribeWAVBytes: %v", err)
	}
	speakers := map[int]int{}
	for i, turn := range turns {
		t.Logf("turn %d: [%6.2f-%6.2f] speaker=%d text=%q", i, turn.Start, turn.End, turn.Speaker, turn.Text)
		if turn.Text != "" {
			speakers[turn.Speaker]++
		}
	}
	formatted := FormatSpeakerTurnsAuto(turns)
	t.Logf("distinct speakers (non-empty text): %d", len(speakers))
	t.Logf("formatted transcript:\n%s", formatted)
	if len(speakers) > 1 {
		t.Logf("WARNING: single voice over-clustered into %d speakers; output would show Speaker labels for one person", len(speakers))
	}
}
