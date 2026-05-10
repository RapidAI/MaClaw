package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type agentLoopScreenshotResult struct {
	Response                 *IMAgentResponse
	PostStreamReturnPrepTime bool
}

func (h *IMMessageHandler) handleAgentLoopScreenshotArtifact(
	userID string,
	iteration int,
	platform string,
	pendingImageKey string,
	screenshotAlreadySent bool,
	screenshotIsOnlyAction bool,
	totalToolCallsInLoop int,
	toolCallCount int,
	history []agent.ConversationEntry,
	visibleArtifacts *pendingVisibleArtifacts,
	streamDone bool,
	attachLLMTelemetry func(*IMAgentResponse),
) agentLoopScreenshotResult {
	result := agentLoopScreenshotResult{}
	if pendingImageKey != "" && screenshotIsOnlyAction {
		resp := &IMAgentResponse{}
		result.PostStreamReturnPrepTime = streamDone
		attachLLMTelemetry(resp)
		h.saveConversationHistoryTimed(userID, history, resp)
		if normalizeIMMessagePlatformKind(platform).IsDesktop() {
			filePath, err := h.saveScreenshotToFile(pendingImageKey)
			if err != nil {
				result.Response = &IMAgentResponse{Text: fmt.Sprintf("Failed to save screenshot: %s", err.Error()), ResponseSource: imResponseSourceScreenshot.String()}
				return result
			}
			resp.Text = "Screenshot saved."
			resp.ResponseSource = imResponseSourceScreenshot.String()
			resp.LocalFilePath = filePath
			resp.ThumbnailBase64 = downsizeScreenshotThumbnail(pendingImageKey)
			result.Response = resp
			return result
		}
		resp.ResponseSource = imResponseSourceScreenshot.String()
		resp.ImageKey = pendingImageKey
		result.Response = resp
		return result
	}
	if pendingImageKey != "" {
		h.saveIntermediateScreenshotArtifact(iteration, platform, pendingImageKey, toolCallCount, visibleArtifacts)
	}
	if screenshotAlreadySent && screenshotIsOnlyAction {
		resp := &IMAgentResponse{Text: "Screenshot sent.", ResponseSource: imResponseSourceScreenshot.String()}
		attachLLMTelemetry(resp)
		h.saveConversationHistoryTimed(userID, history, resp)
		result.Response = resp
		return result
	}
	if screenshotAlreadySent {
		log.Printf("[screenshot] intermediate screenshot via session.image, loop continues (iteration=%d totalToolCalls=%d)", iteration, totalToolCallsInLoop)
	}
	return result
}

func (h *IMMessageHandler) saveIntermediateScreenshotArtifact(iteration int, platform, pendingImageKey string, toolCallCount int, visibleArtifacts *pendingVisibleArtifacts) {
	if normalizeIMMessagePlatformKind(platform).IsDesktop() {
		if filePath, err := h.saveScreenshotToFile(pendingImageKey); err == nil {
			if visibleArtifacts != nil {
				visibleArtifacts.LocalPreviewPath = filePath
				visibleArtifacts.LocalPreviewThumbnail = downsizeScreenshotThumbnail(pendingImageKey)
			}
			log.Printf("[screenshot] intermediate screenshot saved to %s, loop continues (iteration=%d toolCalls=%d)", filePath, iteration, toolCallCount)
		}
		return
	}
	if filePath, err := h.saveScreenshotToFile(pendingImageKey); err == nil {
		log.Printf("[screenshot] intermediate screenshot (IM) saved to %s, loop continues (iteration=%d toolCalls=%d)", filePath, iteration, toolCallCount)
	}
}

func downsizeScreenshotThumbnail(base64Data string) string {
	if len(base64Data) <= 50000 {
		return base64Data
	}
	if downsized, err := remote.DownsizeScreenshotBase64(base64Data, 10000); err == nil {
		return downsized
	}
	return base64Data
}

type agentLoopFileArtifactResult struct {
	Response                 *IMAgentResponse
	PostStreamReturnPrepTime bool
}

func (h *IMMessageHandler) handleAgentLoopFileArtifacts(
	userID string,
	platform string,
	pendingFiles []pendingFile,
	voiceData string,
	voiceFileName string,
	voiceMimeType string,
	history []agent.ConversationEntry,
	streamDone bool,
	attachLLMTelemetry func(*IMAgentResponse),
) agentLoopFileArtifactResult {
	result := agentLoopFileArtifactResult{}
	if len(pendingFiles) == 0 {
		return result
	}
	resp := &IMAgentResponse{}
	result.PostStreamReturnPrepTime = streamDone
	attachLLMTelemetry(resp)
	h.saveConversationHistoryTimed(userID, history, resp)
	if normalizeIMMessagePlatformKind(platform).IsDesktop() {
		h.populateDesktopFileArtifactResponse(resp, pendingFiles)
		attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
		result.Response = resp
		return result
	}
	last := pendingFiles[len(pendingFiles)-1]
	resp.ResponseSource = imResponseSourceFileDelivery.String()
	resp.FileData = last.data
	resp.FileName = last.name
	resp.FileMimeType = last.mimeType
	attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
	result.Response = resp
	return result
}

func (h *IMMessageHandler) populateDesktopFileArtifactResponse(resp *IMAgentResponse, pendingFiles []pendingFile) {
	fileMaterializeStartedAt := time.Now()
	var savedPaths []string
	var failLines []string
	var imForwardedCount int
	for _, pf := range pendingFiles {
		filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
		if err != nil {
			failLines = append(failLines, fmt.Sprintf("Failed to save %s: %s", pf.name, err.Error()))
			continue
		}
		savedPaths = append(savedPaths, filePath)
		if !pf.forwardIM {
			continue
		}
		if h.imFileSender == nil {
			failLines = append(failLines, fmt.Sprintf("Could not forward %s to IM: sender is not configured", pf.name))
			continue
		}
		if err := h.imFileSender(pf.data, pf.name, pf.mimeType, pf.message); err != nil {
			log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, err)
			failLines = append(failLines, fmt.Sprintf("Could not forward %s to IM: %s", pf.name, err.Error()))
			continue
		}
		imForwardedCount++
	}
	text := strings.Join(failLines, "\n")
	if imForwardedCount > 0 {
		imNote := fmt.Sprintf("Forwarded %d file(s) to IM.", imForwardedCount)
		if text != "" {
			text = imNote + "\n" + text
		} else {
			text = imNote
		}
	}
	resp.Text = text
	resp.ResponseSource = imResponseSourceFileDelivery.String()
	resp.LocalFilePaths = savedPaths
	resp.FileMaterializeNanos = time.Since(fileMaterializeStartedAt).Nanoseconds()
	if len(savedPaths) > 0 {
		resp.LocalFilePath = savedPaths[0]
	}
}

func attachVoiceArtifact(resp *IMAgentResponse, voiceData, voiceFileName, voiceMimeType string) {
	if resp == nil || voiceData == "" {
		return
	}
	resp.VoiceData = voiceData
	resp.VoiceFileName = voiceFileName
	resp.VoiceMimeType = voiceMimeType
}
