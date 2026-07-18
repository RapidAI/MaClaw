// Command meeting_asr_worker is the no-FFmpeg transcription sidecar for
// mobile meeting recordings. It accepts exactly one JSON request on stdin and
// writes exactly one JSON response on stdout so Hub can supervise it safely.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

type request struct {
	AudioPath   string `json:"audio_path"`
	ContentType string `json:"content_type"`
}

type response struct {
	Transcript string `json:"transcript"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "meeting ASR worker:", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	var req request
	decoder := json.NewDecoder(io.LimitReader(in, 64<<10))
	if err := decoder.Decode(&req); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(strings.Split(req.ContentType, ";")[0])) != "audio/wav" {
		return fmt.Errorf("unsupported content_type %q: this worker accepts audio/wav", req.ContentType)
	}
	path := strings.TrimSpace(req.AudioPath)
	if path == "" || strings.ToLower(filepath.Ext(path)) != ".wav" {
		return fmt.Errorf("audio_path must name a WAV file")
	}
	modelPath := strings.TrimSpace(os.Getenv("MACLAW_MEETING_ASR_MODEL"))
	if _, ok := asr.ModelFileStatus(modelPath); !ok {
		return fmt.Errorf("MACLAW_MEETING_ASR_MODEL is not a usable GGUF model")
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read WAV: %w", err)
	}
	// CoreLib verifies PCM WAV, normalizes it to 16kHz mono float32 PCM, then
	// executes the local model. No container conversion or FFmpeg is involved.
	mgr := asr.NewManager(modelPath)
	defer mgr.Unload()
	transcript, err := mgr.TranscribeWAV(wav)
	if err != nil {
		return fmt.Errorf("transcribe WAV: %w", err)
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return fmt.Errorf("ASR returned an empty transcript")
	}
	return json.NewEncoder(out).Encode(response{Transcript: transcript})
}
