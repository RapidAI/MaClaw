package main

import (
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
	obs := toolPayloadObservation{
		TraceResult: result,
		ToolContent: result,
	}
	if strings.HasPrefix(result, "[screenshot_base64]") {
		obs.TraceResult = "Tool result prepared for the user."
		obs.ToolContent = obs.TraceResult
		obs.ImageKey = strings.TrimPrefix(result, "[screenshot_base64]")
		return obs
	}
	if result == "[screenshot_sent]" {
		obs.TraceResult = "Tool result prepared for the user."
		obs.ToolContent = obs.TraceResult
		obs.ScreenshotSent = true
		return obs
	}
	if strings.HasPrefix(result, "[file_base64|") {
		obs.TraceResult = "Tool result prepared for the user."
		rest := strings.TrimPrefix(result, "[file_base64|")
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
			if seg == "im" {
				forwardIM = true
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
	}
	if strings.HasPrefix(result, "[voice_base64|") {
		obs.TraceResult = "Tool result prepared for the user."
		rest := strings.TrimPrefix(result, "[voice_base64|")
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
	}
	return obs
}
