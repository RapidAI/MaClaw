package main

import (
	"fmt"
	"log"
	"path/filepath"
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
	resp.Text = last.message
	resp.FileData = last.data
	resp.FileName = last.name
	resp.FileMimeType = last.mimeType
	attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
	result.Response = resp
	return result
}

// toolFileMaterializeResult is the outcome of turning a [file_base64|...] tool
// payload into a local file (+ optional IM forward).
type toolFileMaterializeResult struct {
	// Text is a short, honest status for the model (never the raw base64 body).
	Text string
	// Handled is true when raw was a file payload and materialize ran.
	Handled bool
	// Forwarded is true when at least one file was sent via imFileSender.
	Forwarded bool
	// LocalPaths are absolute paths written under ~/.maclaw/data/files.
	LocalPaths []string
	// MaterializeNanos is time spent saving (+ forwarding when requested).
	MaterializeNanos int64
}

// materializeToolFilePayloadIfNeeded handles [file_base64|...] tool results on
// the shared agent loop. Shared RunLoop only returns tool text and never runs
// the legacy post-tool file branch, so without this, send_to_im stages the
// payload but never calls imFileSender → WeChat never receives the file.
//
// Non-file payloads return Handled=false with Text=raw.
func (h *IMMessageHandler) materializeToolFilePayloadIfNeeded(raw string) toolFileMaterializeResult {
	trimmed := strings.TrimSpace(raw)
	if h == nil || !strings.HasPrefix(trimmed, toolPayloadFilePrefix) {
		return toolFileMaterializeResult{Text: raw}
	}
	obs := parseToolPayloadResult(trimmed)
	if obs.File == nil {
		return toolFileMaterializeResult{Text: raw}
	}
	if strings.TrimSpace(obs.File.data) == "" {
		msg := fmt.Sprintf("文件 %s 数据为空，无法保存或转发到微信/IM。", obs.File.name)
		log.Printf("[file-delivery] shared/materialize empty payload name=%q", obs.File.name)
		return toolFileMaterializeResult{Text: msg, Handled: true}
	}
	resp := &IMAgentResponse{}
	forwardedCount := h.populateDesktopFileArtifactResponse(resp, []pendingFile{*obs.File})
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		text = strings.TrimSpace(obs.ToolContent)
	}
	if text == "" {
		text = "文件交付完成。"
	}
	forwarded := forwardedCount > 0
	log.Printf("[file-delivery] shared/materialize name=%q forwardIM=%v forwarded=%v paths=%d text=%q",
		obs.File.name, obs.File.forwardIM, forwarded, len(resp.LocalFilePaths), truncateRunes(text, 120))
	return toolFileMaterializeResult{
		Text:             text,
		Handled:          true,
		Forwarded:        forwarded,
		LocalPaths:       append([]string(nil), resp.LocalFilePaths...),
		MaterializeNanos: resp.FileMaterializeNanos,
	}
}

// populateDesktopFileArtifactResponse saves pending files locally and optionally
// forwards them via imFileSender. Returns how many files were successfully forwarded.
func (h *IMMessageHandler) populateDesktopFileArtifactResponse(resp *IMAgentResponse, pendingFiles []pendingFile) int {
	if resp == nil {
		return 0
	}
	fileMaterializeStartedAt := time.Now()
	var savedPaths []string
	var failLines []string
	var imForwardedCount int
	var deliveryMessage string
	for _, pf := range pendingFiles {
		if strings.TrimSpace(pf.data) == "" {
			failLines = append(failLines, fmt.Sprintf("保存 %s 失败：文件数据为空", pf.name))
			continue
		}
		filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
		if err != nil {
			failLines = append(failLines, fmt.Sprintf("保存 %s 失败：%s", pf.name, err.Error()))
			continue
		}
		savedPaths = append(savedPaths, filePath)
		if strings.TrimSpace(pf.message) != "" {
			deliveryMessage = pf.message
		}
		if !pf.forwardIM {
			continue
		}
		if h.imFileSender == nil {
			failLines = append(failLines, fmt.Sprintf("无法转发 %s 到 IM：发送器未配置（请确认微信/飞书已登录）", pf.name))
			continue
		}
		if err := h.imFileSender(pf.data, pf.name, pf.mimeType, pf.message); err != nil {
			log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, err)
			failLines = append(failLines, fmt.Sprintf("无法转发 %s 到微信/IM：%s", pf.name, err.Error()))
			continue
		}
		imForwardedCount++
	}
	text := strings.Join(failLines, "\n")
	if text == "" && imForwardedCount == 0 {
		text = deliveryMessage
	}
	if imForwardedCount > 0 {
		imNote := fmt.Sprintf("已向微信/IM 转发 %d 个文件。", imForwardedCount)
		if text != "" {
			text = imNote + "\n" + text
		} else {
			text = imNote
		}
	}
	// Desktop-only stage with no caption: still give the model/UI a clear status.
	if strings.TrimSpace(text) == "" && len(savedPaths) > 0 && imForwardedCount == 0 {
		if len(savedPaths) == 1 {
			text = fmt.Sprintf("文件已在当前对话中准备好：%s（未转发到微信/IM；若需发送请用 send_to_im）。", filepath.Base(savedPaths[0]))
		} else {
			text = fmt.Sprintf("已在当前对话中准备好 %d 个文件（未转发到微信/IM；若需发送请用 send_to_im）。", len(savedPaths))
		}
	}
	resp.Text = text
	resp.ResponseSource = imResponseSourceFileDelivery.String()
	resp.LocalFilePaths = savedPaths
	resp.FileMaterializeNanos = time.Since(fileMaterializeStartedAt).Nanoseconds()
	if len(savedPaths) > 0 {
		resp.LocalFilePath = savedPaths[0]
	}
	return imForwardedCount
}

func attachVoiceArtifact(resp *IMAgentResponse, voiceData, voiceFileName, voiceMimeType string) {
	if resp == nil || voiceData == "" {
		return
	}
	resp.VoiceData = voiceData
	resp.VoiceFileName = voiceFileName
	resp.VoiceMimeType = voiceMimeType
}
