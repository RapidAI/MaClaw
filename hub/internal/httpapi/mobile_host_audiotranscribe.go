package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// mobileReviewedHostSpeechTranscriber reuses the Hub meeting/pairing ASR
// boundary. The worker owns container decode (including m4a); this adapter
// never imports the GUI asr catalog and never runs minutes.
type mobileReviewedHostSpeechTranscriber struct{}

func (mobileReviewedHostSpeechTranscriber) Ready() bool {
	return mobileSpeechTranscribeAvailable()
}

func (mobileReviewedHostSpeechTranscriber) TranscribeSpeech(ctx context.Context, mime string, data []byte) (string, error) {
	if !mobileSpeechTranscribeAvailable() || len(data) == 0 {
		return "", fmt.Errorf("host_audio_transcribe_unavailable")
	}
	if len(data) > agentservice.ReviewedHostAudioTranscribeMaxBytes {
		return "", fmt.Errorf("trusted_audio_attachment_too_large")
	}
	suffix := mobileTrustedAudioTempSuffix(mime)
	file, err := os.CreateTemp("", "maclaw-host-audio-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("host_audio_transcribe_temp_failed")
	}
	path := file.Name()
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("host_audio_transcribe_temp_failed")
	}
	defer os.Remove(path)
	if ctx == nil {
		ctx = context.Background()
	}
	text, err := TranscribeHardwarePairingWAV(ctx, path, strings.TrimSpace(mime))
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_transcribe_empty")
	}
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_audio_transcribe_delivery_bypass")
	}
	return text, nil
}

func mobileSpeechTranscribeAvailable() bool {
	transcript, _ := mobileMeetingRecordingWorkerAvailability()
	return transcript
}

func mobileTrustedAudioTempSuffix(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])) {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/silk", "audio/x-silk":
		return ".silk"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return ".m4a"
	case "audio/webm":
		return ".webm"
	default:
		return ".wav"
	}
}

func wireMobileReviewedHostSpeechTranscriber(executor *agentservice.CoreAgentExecutor) {
	if executor == nil {
		return
	}
	// Always attach the wrapper. Ready() is the plan-time gate, so a later
	// meeting-worker or env command still becomes usable without restart.
	executor.SetReviewedHostSpeechTranscriber(mobileReviewedHostSpeechTranscriber{})
}
