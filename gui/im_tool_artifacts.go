package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
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
	platformKind := normalizeIMMessagePlatformKind(platform)
	if platformKind.IsDesktop() {
		h.populateFileArtifactResponse(resp, pendingFiles, platform)
		attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
		result.Response = resp
		return result
	}
	// Multi-file on IM/other channels: deliver via LocalFilePaths only. An
	// explicitly targeted file must also take this materialization path even when
	// it is the only artifact; the single-file shortcut below is only for a reply
	// to the originating gateway.
	// Gateways already send LocalFilePaths; using FileData for the last file
	// as well would double-send that file.
	if len(pendingFiles) > 1 || pendingFilesHaveExplicitTarget(pendingFiles) {
		h.populateFileArtifactResponse(resp, pendingFiles, platform)
		attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
		result.Response = resp
		return result
	}
	// Single file: inline FileData (no disk hop). Active channel gateways send it
	// on the reply; do not also set LocalFilePaths here.
	last := pendingFiles[0]
	resp.ResponseSource = imResponseSourceFileDelivery.String()
	if strings.TrimSpace(last.data) == "" {
		// Match materialize/populate: refuse empty payloads instead of a blank bubble.
		resp.Text = i18n.Tf(i18n.MsgIMFileEmptyPayload, h.imUILangOrZh(), strings.TrimSpace(last.name))
		attachVoiceArtifact(resp, voiceData, voiceFileName, voiceMimeType)
		result.Response = resp
		return result
	}
	resp.FileData = last.data
	resp.FileName = last.name
	resp.FileMimeType = last.mimeType
	resp.Text = fileDeliveryVisibleMessage(last.message, platformKind.IsIMChannel(), h.imUILangOrZh(), last.name)
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
	// Forwarded is true when the file is considered delivered to the user:
	// desktop→IM via imFileSender, or active IM channel via LocalFilePaths
	// (gateway sendLocalFiles / FileData on the reply).
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
	return h.materializeToolFilePayloadForPlatform(raw, "")
}

// materializeToolFilePayloadForPlatform is like materializeToolFilePayloadIfNeeded
// but uses the originating conversation platform so status text is honest when the
// user is already on WeChat/Feishu (LocalFilePaths are delivered by that gateway).
func (h *IMMessageHandler) materializeToolFilePayloadForPlatform(raw, platform string) toolFileMaterializeResult {
	trimmed := strings.TrimSpace(raw)
	if h == nil || !strings.HasPrefix(trimmed, toolPayloadFilePrefix) {
		return toolFileMaterializeResult{Text: raw}
	}
	lang := h.imUILangOrZh()
	obs := parseToolPayloadResultForPlatformLang(trimmed, platform, lang)
	if obs.File == nil {
		return toolFileMaterializeResult{Text: raw}
	}
	if strings.TrimSpace(obs.File.data) == "" {
		msg := i18n.Tf(i18n.MsgIMFileEmptyPayload, lang, obs.File.name)
		log.Printf("[file-delivery] shared/materialize empty payload name=%q", obs.File.name)
		return toolFileMaterializeResult{Text: msg, Handled: true}
	}
	resp := &IMAgentResponse{}
	forwardedCount := h.populateFileArtifactResponse(resp, []pendingFile{*obs.File}, platform)
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		text = strings.TrimSpace(obs.ToolContent)
	}
	if text == "" {
		text = i18n.T(i18n.MsgIMProactiveFileCaptionBare, lang)
	}
	// Delivered: desktop→IM sender, or IM-channel local paths (gateway will send).
	onIMChannel := normalizeIMMessagePlatformKind(platform).IsIMChannel()
	forwarded := forwardedCount > 0 || (onIMChannel && len(resp.LocalFilePaths) > 0)
	log.Printf("[file-delivery] shared/materialize name=%q platform=%q forwardIM=%v forwarded=%v paths=%d text=%q",
		obs.File.name, platform, obs.File.forwardIM, forwarded, len(resp.LocalFilePaths), truncateRunes(text, 120))
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
	return h.populateFileArtifactResponse(resp, pendingFiles, "")
}

// populateFileArtifactResponse saves pending files and optionally forwards them.
//
// platform:
//   - IM channel (weixin/feishu/…): save only. The channel gateway delivers
//     LocalFilePaths / FileData on the reply. Never call imFileSender here —
//     that path is desktop→IM and would double-send into the same chat.
//   - desktop / unknown: when forwardIM is set, push via imFileSender.
//
// Returns how many files were successfully pushed via imFileSender (0 on IM channels).
func (h *IMMessageHandler) populateFileArtifactResponse(resp *IMAgentResponse, pendingFiles []pendingFile, platform string) int {
	if resp == nil {
		return 0
	}
	lang := h.imUILangOrZh()
	onIMChannel := normalizeIMMessagePlatformKind(platform).IsIMChannel()
	originGatewayDeliversFiles := onIMChannel || isThirdPartyFileOriginPlatform(platform)
	fileMaterializeStartedAt := time.Now()
	var savedPaths []string
	var originReplyPaths []string
	var failLines []string
	var imForwardedCount int
	var deliveryMessage string
	for _, pf := range pendingFiles {
		if strings.TrimSpace(pf.data) == "" {
			failLines = append(failLines, i18n.Tf(i18n.MsgIMFileSaveEmpty, lang, pf.name))
			continue
		}
		filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
		if err != nil {
			failLines = append(failLines, i18n.Tf(i18n.MsgIMFileSaveFailed, lang, pf.name, err.Error()))
			continue
		}
		savedPaths = append(savedPaths, filePath)
		// Exact-target delivery is never also attached to the originating IM or
		// hardware reply. This preserves the no-broadcast/no-fallback guarantee.
		if !(originGatewayDeliversFiles && pf.target.Active()) {
			originReplyPaths = append(originReplyPaths, filePath)
		}
		if strings.TrimSpace(pf.message) != "" && !isLegacyBotFileInstruction(pf.message) && !isAutoProactiveCaption(pf.message) {
			deliveryMessage = strings.TrimSpace(pf.message)
		}
		// Active IM session without an explicit target: the current gateway sends
		// LocalFilePaths, so invoking the desktop sender would duplicate media.
		// With an explicit target (for example hardware voice asks for another
		// Lansenger group), the target sender must run even though the origin is an
		// IM/third-party channel.
		if (originGatewayDeliversFiles && !pf.target.Active()) || !pf.forwardIM {
			continue
		}
		if h == nil || (h.structuredIMFileSender == nil && h.imFileSender == nil) {
			failLines = append(failLines, i18n.Tf(i18n.MsgIMFileSenderNotConfigured, lang, pf.name))
			continue
		}
		// Caption on WeChat/Feishu: GUI language, never legacy "Please send … to the user."
		caption := resolveIMProactiveCaption(lang, pf.message, pf.name, pf.mimeType)
		req := agent.IMFileDeliveryRequest{Data: pf.data, FileName: pf.name, MIMEType: pf.mimeType, Message: caption, Target: pf.target}
		var sendErr error
		if h.structuredIMFileSender != nil {
			sendErr = h.structuredIMFileSender(req)
		} else {
			sendErr = h.imFileSender(req.Data, req.FileName, req.MIMEType, req.Message)
		}
		if sendErr != nil {
			log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, sendErr)
			failLines = append(failLines, i18n.Tf(i18n.MsgIMFileForwardFailed, lang, pf.name, sendErr.Error()))
			continue
		}
		log.Printf("[IMMessageHandler] IM forward OK name=%s mime=%s caption=%q", pf.name, pf.mimeType, truncateRunes(caption, 80))
		imForwardedCount++
	}
	resp.Text = buildFileArtifactStatusText(lang, onIMChannel, originReplyPaths, failLines, imForwardedCount, deliveryMessage)
	resp.ResponseSource = imResponseSourceFileDelivery.String()
	resp.LocalFilePaths = originReplyPaths
	resp.FileMaterializeNanos = time.Since(fileMaterializeStartedAt).Nanoseconds()
	if len(originReplyPaths) > 0 {
		resp.LocalFilePath = originReplyPaths[0]
	}
	return imForwardedCount
}

func pendingFilesHaveExplicitTarget(pendingFiles []pendingFile) bool {
	for _, pf := range pendingFiles {
		if pf.target.Active() {
			return true
		}
	}
	return false
}

// Third-party hardware gateways return untargeted artifacts on their own reply
// channel, just like an IM gateway. Keep this predicate narrow instead of making
// thirdparty:* an IM platform globally: that would also change audio formats,
// playback targets, auditing, and unrelated platform-specific behavior.
func isThirdPartyFileOriginPlatform(platform string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	return platform == "thirdparty" || strings.HasPrefix(platform, "thirdparty:")
}

func (h *IMMessageHandler) imUILangOrZh() string {
	if h == nil {
		return "zh"
	}
	if lang := strings.TrimSpace(h.imUILang()); lang != "" {
		return lang
	}
	return "zh"
}

func imChannelFileReadyText(lang, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "file"
	}
	return i18n.Tf(i18n.MsgIMFileChannelReadyOne, lang, name)
}

// fileDeliveryVisibleMessage picks end-user / model-facing delivery text.
// Drops legacy bot placeholders and auto-proactive captions (same filter as
// populateFileArtifactResponse) so IM channels get an honest channel-ready line.
func fileDeliveryVisibleMessage(rawMessage string, onIMChannel bool, lang, fileName string) string {
	msg := strings.TrimSpace(rawMessage)
	if msg != "" && !isLegacyBotFileInstruction(msg) && !isAutoProactiveCaption(msg) {
		return msg
	}
	if onIMChannel {
		return imChannelFileReadyText(lang, fileName)
	}
	return ""
}

// buildFileArtifactStatusText composes model/UI status after save (+ optional desktop forward).
func buildFileArtifactStatusText(lang string, onIMChannel bool, savedPaths, failLines []string, imForwardedCount int, deliveryMessage string) string {
	failText := strings.TrimSpace(strings.Join(failLines, "\n"))
	text := failText
	if text == "" && imForwardedCount == 0 {
		text = strings.TrimSpace(deliveryMessage)
	}
	if imForwardedCount > 0 {
		imNote := i18n.Tf(i18n.MsgIMFileForwardedCount, lang, imForwardedCount)
		if text != "" {
			text = imNote + "\n" + text
		} else {
			text = imNote
		}
	}
	ready := fileArtifactReadyStatus(lang, onIMChannel, savedPaths)
	if text == "" {
		return ready
	}
	// Partial batch: some saves failed, some succeeded — keep both sides honest.
	if failText != "" && ready != "" && imForwardedCount == 0 {
		return text + "\n" + ready
	}
	return text
}

func fileArtifactReadyStatus(lang string, onIMChannel bool, savedPaths []string) string {
	if len(savedPaths) == 0 {
		return ""
	}
	if onIMChannel {
		if len(savedPaths) == 1 {
			return imChannelFileReadyText(lang, filepath.Base(savedPaths[0]))
		}
		return i18n.Tf(i18n.MsgIMFileChannelReadyMany, lang, len(savedPaths))
	}
	if len(savedPaths) == 1 {
		return i18n.Tf(i18n.MsgIMFileDesktopReadyOne, lang, filepath.Base(savedPaths[0]))
	}
	return i18n.Tf(i18n.MsgIMFileDesktopReadyMany, lang, len(savedPaths))
}

func attachVoiceArtifact(resp *IMAgentResponse, voiceData, voiceFileName, voiceMimeType string) {
	if resp == nil || voiceData == "" {
		return
	}
	resp.VoiceData = voiceData
	resp.VoiceFileName = voiceFileName
	resp.VoiceMimeType = voiceMimeType
}
