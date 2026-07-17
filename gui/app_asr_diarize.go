package main

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/diarization"
)

var (
	camPlusInstance *diarization.CAMPlus
	camPlusPath     string
	camPlusMu       sync.Mutex
)

// SpeakerTranscript is a speaker-labelled portion of a meeting transcript.
// Speaker IDs are local to the input recording: Speaker 0 does not identify a
// person across separate meetings unless an enrolment layer is added.
type SpeakerTranscript struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
	Text    string  `json:"text"`
}

// FormatSpeakerTranscript preserves local speaker identity and source timing
// in a transcript that can safely feed the meeting-minutes map-reduce flow.
// Speaker IDs are intentionally local to this one recording.
func FormatSpeakerTranscript(turns []SpeakerTranscript) string {
	ordered := make([]SpeakerTranscript, 0, len(turns))
	for _, turn := range turns {
		if strings.TrimSpace(turn.Text) == "" || math.IsNaN(turn.Start) || math.IsNaN(turn.End) || math.IsInf(turn.Start, 0) || math.IsInf(turn.End, 0) {
			continue
		}
		// Do not emit malformed or negative local labels/timestamps into a
		// meeting record. The diarizer itself returns valid values, but this is
		// a Wails-facing boundary and callers can construct these turns directly.
		if turn.Start < 0 {
			turn.Start = 0
		}
		if turn.End < turn.Start {
			continue
		}
		if turn.Speaker < 0 {
			turn.Speaker = 0
		}
		ordered = append(ordered, turn)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	var b strings.Builder
	for _, turn := range ordered {
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[%02d:%02d-%02d:%02d] Speaker %d: %s",
			int(turn.Start)/60, int(turn.Start)%60,
			int(turn.End)/60, int(turn.End)%60,
			turn.Speaker+1, text)
	}
	return b.String()
}

// DiarizeAndTranscribeAudioBase64 is the Wails-facing counterpart used by the
// desktop assistant microphone. Each returned item is an independently
// transcribed, locally labelled speaker turn in recording order.
func (a *App) DiarizeAndTranscribeAudioBase64(wavBase64 string, knownSpeakers int) ([]SpeakerTranscript, error) {
	if wavBase64 == "" {
		return nil, fmt.Errorf("empty audio data")
	}
	wavData, err := base64.StdEncoding.DecodeString(wavBase64)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	return a.DiarizeAndTranscribeWAVBytes(wavData, knownSpeakers)
}

// CheckDiarizationModel reports whether the separately cached CAM++ weights
// are ready. The artifact is downloaded into the same durable model cache as
// ASR and can be supplied by the configured Hub cache when GitHub is absent.
func (a *App) CheckDiarizationModel() map[string]interface{} {
	dir, err := embeddingModelsDir()
	if err != nil {
		return map[string]interface{}{"exists": false, "size": int64(0)}
	}
	path := filepath.Join(dir, diarization.DefaultCAMPlusFilename)
	info, err := osStatNonEmpty(path)
	if err != nil || diarization.ValidateCAMPlusFile(path) != nil {
		return map[string]interface{}{"exists": false, "size": int64(0), "model": "cam++"}
	}
	return map[string]interface{}{"exists": true, "size": info, "model": "cam++", "path": path}
}

func osStatNonEmpty(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		if err == nil {
			err = fmt.Errorf("model file is empty")
		}
		return 0, err
	}
	return info.Size(), nil
}

func (a *App) getCAMPlusModel() (*diarization.CAMPlus, error) {
	camPlusMu.Lock()
	defer camPlusMu.Unlock()
	dir, err := embeddingModelsDir()
	if err != nil {
		return nil, fmt.Errorf("models dir: %w", err)
	}
	path := filepath.Join(dir, diarization.DefaultCAMPlusFilename)
	if camPlusInstance != nil && camPlusPath == path {
		return camPlusInstance, nil
	}
	if _, err := osStatNonEmpty(path); err != nil {
		return nil, fmt.Errorf("CAM++ diarization model not downloaded: %s", path)
	}
	model, err := diarization.LoadCAMPlus(path)
	if err != nil {
		return nil, err
	}
	camPlusInstance = model
	camPlusPath = path
	return model, nil
}

// resetCAMPlusModel drops the cached immutable weights after a replacement or
// an invalid-cache cleanup. Existing calls retain their local model pointer;
// the next call loads the artifact currently on disk.
func resetCAMPlusModel(path string) {
	camPlusMu.Lock()
	defer camPlusMu.Unlock()
	if path == "" || camPlusPath == path {
		camPlusInstance = nil
		camPlusPath = ""
	}
}

// DiarizeAndTranscribeWAVBytes returns independently transcribed, local
// speaker-labelled meeting spans. knownSpeakers is optional; pass the actual
// attendee count when known to make CAM++ clustering substantially steadier.
func (a *App) DiarizeAndTranscribeWAVBytes(wavData []byte, knownSpeakers int) ([]SpeakerTranscript, error) {
	if a == nil {
		return nil, fmt.Errorf("ASR app is nil")
	}
	if !a.GetASREnabled() {
		return nil, fmt.Errorf("ASR is not enabled")
	}
	if !a.GetDiarizationEnabled() {
		return nil, fmt.Errorf("speaker diarization is not enabled")
	}
	pcm, err := asr.WAVToFloat32(wavData)
	if err != nil {
		return nil, fmt.Errorf("WAV to PCM: %w", err)
	}
	model, err := a.getCAMPlusModel()
	if err != nil {
		return nil, err
	}
	spans, err := diarization.Diarize(pcm, model, diarization.Config{KnownSpeakers: knownSpeakers})
	if err != nil {
		return nil, err
	}
	asrModel, err := a.getASRModel()
	if err != nil {
		return nil, err
	}
	out := make([]SpeakerTranscript, 0, len(spans))
	for _, span := range spans {
		start := int(span.Start * diarization.SampleRate)
		end := int(span.End * diarization.SampleRate)
		if start < 0 {
			start = 0
		}
		if end > len(pcm) {
			end = len(pcm)
		}
		if end <= start {
			continue
		}
		text, err := asrModel.Transcribe(gentleNormalizePCM(pcm[start:end]))
		if err != nil {
			return nil, fmt.Errorf("transcribe speaker %d [%.2f, %.2f]: %w", span.Speaker, span.Start, span.End, err)
		}
		if drop, _ := shouldDropASRText(text, end-start); drop {
			text = ""
		}
		out = append(out, SpeakerTranscript{Start: span.Start, End: span.End, Speaker: span.Speaker, Text: text})
	}
	return out, nil
}
