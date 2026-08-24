package main

import "strings"

type toolPayloadKind string

const (
	toolPayloadPlain            toolPayloadKind = ""
	toolPayloadScreenshotBase64 toolPayloadKind = "screenshot_base64"
	toolPayloadScreenshotSent   toolPayloadKind = "screenshot_sent"
	toolPayloadFileBase64       toolPayloadKind = "file_base64"
	toolPayloadVoiceBase64      toolPayloadKind = "voice_base64"
	toolPayloadSpeechArtifact   toolPayloadKind = "speech_artifact"
)

const (
	toolPayloadPreparedMessage      = "Tool result prepared for the user."
	toolPayloadScreenshotPrefix     = "[screenshot_base64]"
	toolPayloadScreenshotSentMarker = "[screenshot_sent]"
	toolPayloadFilePrefix           = "[file_base64|"
	toolPayloadVoicePrefix          = "[voice_base64|"
	toolPayloadSpeechArtifactPrefix = "[speech_artifact|audio/wav]"
)

type classifiedToolPayload struct {
	Kind toolPayloadKind
	Body string
}

type toolPayloadFileFlag string

const (
	toolPayloadFileFlagForwardIM toolPayloadFileFlag = "im"
)

func normalizeToolPayloadFileFlag(value string) toolPayloadFileFlag {
	switch toolPayloadFileFlag(strings.TrimSpace(value)) {
	case toolPayloadFileFlagForwardIM:
		return toolPayloadFileFlagForwardIM
	default:
		return ""
	}
}

func classifyToolPayloadResult(result string) classifiedToolPayload {
	switch {
	case strings.HasPrefix(result, toolPayloadScreenshotPrefix):
		return classifiedToolPayload{
			Kind: toolPayloadScreenshotBase64,
			Body: strings.TrimPrefix(result, toolPayloadScreenshotPrefix),
		}
	case result == toolPayloadScreenshotSentMarker:
		return classifiedToolPayload{Kind: toolPayloadScreenshotSent}
	case strings.HasPrefix(result, toolPayloadFilePrefix):
		return classifiedToolPayload{
			Kind: toolPayloadFileBase64,
			Body: strings.TrimPrefix(result, toolPayloadFilePrefix),
		}
	case strings.HasPrefix(result, toolPayloadVoicePrefix):
		return classifiedToolPayload{
			Kind: toolPayloadVoiceBase64,
			Body: strings.TrimPrefix(result, toolPayloadVoicePrefix),
		}
	case strings.HasPrefix(result, toolPayloadSpeechArtifactPrefix):
		return classifiedToolPayload{
			Kind: toolPayloadSpeechArtifact,
			Body: strings.TrimPrefix(result, toolPayloadSpeechArtifactPrefix),
		}
	default:
		return classifiedToolPayload{Kind: toolPayloadPlain, Body: result}
	}
}
