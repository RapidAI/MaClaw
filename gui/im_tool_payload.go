package main

import (
	"encoding/base64"
	"fmt"
	"strings"
)

type pendingFile struct {
	name, mimeType, data string
	forwardIM            bool
	message              string
}

type toolPayloadObservation struct {
	TraceResult    string
	ToolContent    string
	Kind           toolPayloadKind
	ImageKey       string
	ScreenshotSent bool
	File           *pendingFile
	VoiceData      string
	VoiceFileName  string
	VoiceMimeType  string
}

type agentLoopPendingToolArtifacts struct {
	ImageKey       string
	ScreenshotSent bool
	Files          []pendingFile
	VoiceData      string
	VoiceFileName  string
	VoiceMimeType  string
}

func (a *agentLoopPendingToolArtifacts) ApplyObservation(obs toolPayloadObservation) {
	if a == nil {
		return
	}
	if obs.ImageKey != "" {
		a.ImageKey = obs.ImageKey
	}
	if obs.ScreenshotSent {
		a.ScreenshotSent = true
	}
	if obs.File != nil {
		a.Files = append(a.Files, *obs.File)
	}
	if obs.VoiceData != "" {
		a.VoiceData = obs.VoiceData
		a.VoiceFileName = obs.VoiceFileName
		a.VoiceMimeType = obs.VoiceMimeType
	}
}

func parseToolPayloadResult(result string) toolPayloadObservation {
	payload := classifyToolPayloadResult(result)
	obs := toolPayloadObservation{
		TraceResult: result,
		ToolContent: result,
		Kind:        payload.Kind,
	}
	switch payload.Kind {
	case toolPayloadScreenshotBase64:
		obs.TraceResult = toolPayloadPreparedMessage
		obs.ToolContent = obs.TraceResult
		obs.ImageKey = payload.Body
		return obs
	case toolPayloadScreenshotSent:
		obs.TraceResult = toolPayloadPreparedMessage
		obs.ToolContent = obs.TraceResult
		obs.ScreenshotSent = true
		return obs
	case toolPayloadFileBase64:
		obs.TraceResult = toolPayloadPreparedMessage
		rest := payload.Body
		closeBracket := strings.Index(rest, "]")
		if closeBracket <= 0 {
			return obs
		}
		meta := rest[:closeBracket]
		parts := strings.Split(meta, "|")
		if len(parts) < 2 {
			return obs
		}
		forwardIM := false
		mimeType := parts[1]
		var message string
		for i := 2; i < len(parts); i++ {
			seg := parts[i]
			if normalizeToolPayloadFileFlag(seg) == toolPayloadFileFlagForwardIM {
				forwardIM = true
			} else if strings.HasPrefix(seg, "msg64:") {
				if decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(seg, "msg64:")); err == nil {
					message = string(decoded)
				}
			} else if strings.HasPrefix(seg, "msg:") {
				message = strings.TrimPrefix(seg, "msg:")
			} else {
				mimeType += "|" + seg
			}
		}
		if forwardIM && message == "" {
			message = inferFileDeliveryMessage(parts[0])
		}
		obs.File = &pendingFile{
			name:      parts[0],
			mimeType:  mimeType,
			data:      rest[closeBracket+1:],
			forwardIM: forwardIM,
			message:   message,
		}
		if forwardIM {
			obs.ToolContent = fmt.Sprintf("File %s is ready and will be sent through the IM channel.", parts[0])
		} else {
			obs.ToolContent = fmt.Sprintf("File %s is ready and will be sent to the user.", parts[0])
		}
		obs.TraceResult = obs.ToolContent
		return obs
	case toolPayloadVoiceBase64:
		obs.TraceResult = toolPayloadPreparedMessage
		rest := payload.Body
		closeBracket := strings.Index(rest, "]")
		if closeBracket <= 0 {
			return obs
		}
		meta := rest[:closeBracket]
		parts := strings.Split(meta, "|")
		if len(parts) < 2 {
			return obs
		}
		obs.VoiceData = rest[closeBracket+1:]
		obs.VoiceFileName = parts[0]
		obs.VoiceMimeType = parts[1]
		obs.ToolContent = "Voice message is ready and will be sent to the user."
		obs.TraceResult = obs.ToolContent
		return obs
	default:
		return obs
	}
}

func encodeToolPayloadMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	return "msg64:" + base64.RawURLEncoding.EncodeToString([]byte(message))
}
