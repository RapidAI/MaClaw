package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func (h *IMMessageHandler) finalizeIMAgentLoopResponse(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool, clearUIAfterContextSwitch bool, confirmedResume bool) *IMAgentResponse {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.ClearUI = resp.ClearUI || clearUIAfterContextSwitch

	if confirmedResume {
		resp.ConfirmedResume = true
	}
	finalizeStartedAt := time.Now()
	resp = h.finalizeTraceResult(loopCtx, resp, firstNonEmptyTraceText(resp.Text, resp.TraceSummary), resp.Error)
	resp.FinalizeTraceNanos = time.Since(finalizeStartedAt).Nanoseconds()
	h.schedulePostLoopSideEffects(msg, loopCtx, resp, workflowAgentLoop)

	// V2 workflow doc phase: attach review action buttons when the phase
	// transitions to WaitingConfirm. This gives the user structured confirm/
	// supplement/abort choices alongside the free-form input box.
	if workflowAgentLoop && loopCtx != nil && loopCtx.WorkflowDocPhase {
		h.appendWorkflowReviewActions(resp, msg.UserID, loopCtx)
	}

	h.maybeAttachVoiceSummary(resp, msg.Platform, isVoiceInputMessage(msg))
	return resp
}

func (h *IMMessageHandler) schedulePostLoopSideEffects(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if h == nil {
		return
	}
	respSnapshot := IMAgentResponse{}
	if resp != nil {
		respSnapshot = *resp
	}

	// V2 workflow doc phase: capture output SYNCHRONOUSLY before returning to the caller.
	// If captured asynchronously in the goroutine, the next user message ("确认") may
	// arrive before the goroutine runs, causing the state machine to advance without
	// the phase output being recorded — SubAgent then sees empty tasks output.
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if workflowAgentLoop && h.isWorkflowV2Active(ownerID) {
		h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
	}

	go func() {
		startedAt := time.Now()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[post-loop] panic user=%s panic=%v", msg.UserID, r)
			}
			log.Printf("[post-loop] done user=%s duration=%s workflow=%v", msg.UserID, time.Since(startedAt).Round(time.Millisecond), workflowAgentLoop)
		}()
		h.runEvidenceCollection(msg.UserID, msg.Text)
		// V2 capture already done synchronously above; skip in goroutine to avoid double-recording.
		if !(workflowAgentLoop && h.isWorkflowV2Active(ownerID)) {
			h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
		}
		h.recordAgentLoopTerminalExperience(loopCtx, &respSnapshot)
	}()
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if !workflowAgentLoop || resp == nil || resp.HardExit {
		return
	}
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if h.isWorkflowV2Active(ownerID) && h.getWorkflowV2() != nil {
		// Skip if the phase output was already recorded (e.g. by SubAgent execution path)
		wf := h.getWorkflowV2()
		completedPhaseID := ""
		if state := wf.machine.GetActive(ownerID); state != nil {
			if p := state.ActivePhase(); p != nil && p.Output != "" {
				log.Printf("[workflow-v2] post-loop doc capture skipped: phase already has output (len=%d)", len([]rune(p.Output)))
				return
			} else if p != nil {
				completedPhaseID = p.ID
			}
		}
		docText, source := resolveWorkflowPhaseDocText(loopCtx, resp)
		if docText != "" {
			if !h.workflowPhaseDocCaptureAllowed(ownerID, completedPhaseID, docText) {
				return
			}
			h.recordWorkflowV2Output(ownerID, docText)
			if completedPhaseID != "" {
				h.recordWorkflowPhaseCompletedExperience(msg, loopCtx, completedPhaseID)
			}
			logSource := source
			if source == "written_files" && loopCtx != nil {
				logSource = fmt.Sprintf("written_files(%s)", strings.Join(loopCtx.WorkflowWrittenFiles, ", "))
			}
			log.Printf("[workflow-v2] post-loop doc capture: user=%s len=%d source=%s", ownerID, len([]rune(docText)), logSource)
			// After recording, check if the next phase is ExecModeAutoFromPrev
			// and auto-complete it (same logic as SubAgent path).
			if wf := h.getWorkflowV2(); wf != nil {
				if updatedState := wf.machine.GetActive(ownerID); updatedState != nil {
					if nextPhase := updatedState.ActivePhase(); nextPhase != nil && nextPhase.ExecMode == v2.ExecModeAutoFromPrev {
						if nextPhasePrompt := v2.BuildPhasePrompt(updatedState); nextPhasePrompt != "" {
							h.stashedPhasePrompt.Store(ownerID, nextPhasePrompt)
							h.workflowAgentLoopMarker.Store(ownerID, true)
						}
						log.Printf("[workflow-v2] post-loop auto-completing phase=%s (ExecMode=auto_from_prev)", nextPhase.ID)
						wf.machine.RecordOutput(ownerID, docText)
						if refreshedState := wf.machine.GetActive(ownerID); refreshedState != nil && h.app != nil && h.app.workflowEngine != nil {
							h.app.workflowEngine.StoreActiveState(ownerID, mapV2StateToV1(refreshedState))
						}
					} else if nextPhase != nil && nextPhase.ExecMode == v2.ExecModeRemoteSubAgent {
						// Next phase uses RemoteExperimentOrchestrator — append a hint to
						// the response so the user knows to send a message to trigger it.
						if resp != nil && resp.Text != "" {
							resp.Text += "\n\n---\n🔬 基线复现已完成。回复「开始迭代」启动自动迭代改进循环。"
						}
						log.Printf("[workflow-v2] post-loop: next phase %s has ExecMode=remote_subagent, appended trigger hint", nextPhase.ID)
					} else if h.app != nil && h.app.workflowEngine != nil {
						h.app.workflowEngine.StoreActiveState(ownerID, mapV2StateToV1(updatedState))
					}
				}
			}
		}
		return
	}
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
		if wf.machine.GetActive(ownerID) == nil {
			return
		}
		docText, source := resolveWorkflowPhaseDocText(loopCtx, resp)
		if docText == "" {
			return
		}
		state := wf.machine.GetActive(ownerID)
		if state == nil {
			return
		}
		phase := state.ActivePhase()
		phaseID := ""
		if phase != nil {
			phaseID = phase.ID
		}
		if !h.workflowPhaseDocCaptureAllowed(ownerID, phaseID, docText) {
			return
		}

		// Record the output via V2 StateMachine.
		if err := wf.machine.RecordOutput(ownerID, docText); err != nil {
			log.Printf("[workflow] post-loop doc capture failed: user=%s err=%v", ownerID, err)
			return
		}

		if phaseID != "" {
			h.recordWorkflowPhaseCompletedExperience(msg, loopCtx, phaseID)
		}

		// For NeedsConfirm=false phases, RecordOutput auto-advances. Check if we need
		// to set up the next phase agent loop.
		if phase != nil && !phase.NeedsConfirm {
			updatedState := wf.machine.GetActive(ownerID)
			if updatedState != nil {
				nextPhase := updatedState.ActivePhase()
				// If the next phase has a special ExecMode (remote_subagent, subagent,
				// auto_from_prev), do NOT set up a standard agent loop. These modes
				// require routing through handleWorkflowV2Action's ExecMode switch on
				// the next user message. Setting stashedPhasePrompt here would bypass
				// that switch and run the phase as a plain agent loop.
				if nextPhase != nil && nextPhase.ExecMode != "" {
					log.Printf("[workflow-v2] post-loop: next phase %s has ExecMode=%s, skipping auto agent loop setup (will route through ExecMode switch on next message)",
						nextPhase.ID, nextPhase.ExecMode)
				} else {
					// Default: set up for immediate next phase agent loop
					nextPhasePrompt := v2.BuildPhasePrompt(updatedState)
					if nextPhasePrompt != "" {
						h.stashedPhasePrompt.Store(ownerID, nextPhasePrompt)
						h.workflowAgentLoopMarker.Store(ownerID, true)
					}
				}
			}
		}
		log.Printf("[workflow] post-loop doc capture: user=%s phase=%s len=%d source=%s", ownerID, phaseID, len([]rune(docText)), source)
	}
}

func (h *IMMessageHandler) workflowPhaseDocCaptureAllowed(ownerID, phaseID, docText string) bool {
	phaseID = strings.TrimSpace(phaseID)
	if phaseID != v2.PhaseCodingTaskBreakdown {
		return true
	}
	if len(v2.ParseTaskList(docText)) > 0 {
		return true
	}
	if h == nil || h.app == nil || h.app.workflowEngine == nil {
		return false
	}
	h.app.workflowEngine.MarkPhasePendingReview(ownerID, phaseID, true)
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
		if state := wf.machine.GetActive(ownerID); state != nil {
			if p := state.ActivePhase(); p != nil && p.ID == phaseID {
				p.Output = ""
				p.Status = v2.PhaseRunning
				_ = wf.machine.GetStore().Save(state)
			}
			if prompt := v2.BuildPhasePrompt(state); prompt != "" {
				h.stashedPhasePrompt.Store(ownerID, prompt)
				h.workflowAgentLoopMarker.Store(ownerID, true)
			}
		}
	}
	log.Printf("[workflow-v2] post-loop doc capture rejected: user=%s phase=%s reason=invalid_task_breakdown", ownerID, phaseID)
	return false
}

func (h *IMMessageHandler) recordAgentLoopTerminalExperience(loopCtx *LoopContext, resp *IMAgentResponse) {
	if event, ok := agentLoopTerminalExperienceEvent(loopCtx, resp); ok {
		h.recordExperienceLifecycleEvent(event)
	}
}

func agentLoopTerminalExperienceEvent(loopCtx *LoopContext, resp *IMAgentResponse) (lifecycle.Event, bool) {
	ctx := experienceContextFromLoop(loopCtx)
	if ctx.TraceID == "" {
		return lifecycle.Event{}, false
	}
	state := LoopStateUnknown
	if loopCtx != nil {
		state = loopCtx.LoopState()
	}
	errorText := ""
	hardExit := false
	if resp != nil {
		errorText = resp.Error
		hardExit = resp.HardExit
	}
	event := ctx.Apply(lifecycle.Event{CreatedAt: time.Now()})
	switch {
	case errorText != "":
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = "failure"
		event.ErrorClass = "agent_loop_error"
		event.Reason = errorText
	case hardExit:
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = "hard_exit"
		event.ErrorClass = "agent_loop_hard_exit"
	case state == LoopStateFailed || state == LoopStateTimeout:
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = state.String()
		event.ErrorClass = "agent_loop_" + state.String()
	case state == LoopStateStopped || state == LoopStatePaused:
		return lifecycle.Event{}, false
	default:
		event.EventType = lifecycle.EventTaskSucceeded
		event.Outcome = "success"
		if state != LoopStateUnknown {
			event.Reason = "loop_state:" + state.String()
		}
	}
	return event, true
}

func (h *IMMessageHandler) recordWorkflowPhaseCompletedExperience(msg IMUserMessage, loopCtx *LoopContext, phaseID string) {
	ctx := experienceContextFromLoop(loopCtx)
	if ctx.TraceID == "" {
		return
	}
	if h != nil {
		ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
		h.workflowReviewExperienceContext.Store(ownerID, workflowReviewExperienceContext{
			EventContext: ctx,
			PhaseID:      phaseID,
			Query:        msg.Text,
		})
	}
	h.recordExperienceLifecycleEvent(ctx.Apply(lifecycle.Event{
		EventType: lifecycle.EventWorkflowPhaseCompleted,
		Outcome:   "success",
		Reason:    phaseID,
		Query:     msg.Text,
		CreatedAt: time.Now(),
	}))
}

type workflowReviewExperienceContext struct {
	lifecycle.EventContext
	PhaseID string
	Query   string
}

func (h *IMMessageHandler) recordExperienceLifecycleEvent(event lifecycle.Event) {
	if h == nil || h.app == nil {
		return
	}
	h.app.ensureExperienceLifecycleSink().RecordExperienceEvent(event)
}

func (h *IMMessageHandler) applyWorkflowAutoAdvanceResponse(userID string, advResp *v2.WorkflowResponse, platform string) {
	return
}

// resolveWorkflowPhaseDocText determines the best available document text
// for a completed workflow phase. Priority cascade:
//  1. WorkflowWrittenFiles (files written via write_file during the loop)
//  2. WorkflowDocBuffer (accumulated no-tool-call LLM text output)
//  3. resp.Text (last iteration's finalized text)
//  4. resp.Error (error message as last resort)
//
// Returns the document text and a source label for logging.
func resolveWorkflowPhaseDocText(loopCtx *LoopContext, resp *IMAgentResponse) (string, string) {
	var docText string
	source := "resp.Text"
	if loopCtx != nil && len(loopCtx.WorkflowWrittenFiles) > 0 {
		if fileContent := readWorkflowWrittenFiles(loopCtx.WorkflowWrittenFiles); fileContent != "" {
			return fileContent, "written_files"
		}
	}
	if loopCtx != nil && loopCtx.WorkflowDocBuffer.Len() > 0 {
		if t := strings.TrimSpace(loopCtx.WorkflowDocBuffer.String()); t != "" {
			docText = t
			source = "buffer"
		}
	}
	if docText == "" && resp != nil {
		docText = strings.TrimSpace(resp.Text)
	}
	if docText == "" && resp != nil && resp.Error != "" {
		docText = "⚠️ 阶段执行出错: " + resp.Error
		source = "error"
	}
	return docText, source
}

// readWorkflowWrittenFiles reads files written during the workflow agent loop
// and concatenates their content. This captures the actual phase document when
// the LLM uses write_file to produce output instead of streaming text.
// Files are read in order and separated by newlines. Only text files <= 100KB
// are read to avoid memory issues with binary or oversized files.
// Script files (.py, .ps1, .sh, etc.) and non-document files (.svg, .xml, .json,
// .html, .css, etc.) are skipped — only document content files (.md, .txt, .markdown,
// .rst, .adoc, .tex) are read as potential phase output for the preview panel.
func readWorkflowWrittenFiles(paths []string) string {
	const maxFileSize = 100 * 1024         // 100KB per file
	const maxTotalRunes = 50000            // cap total output to avoid oversized phase output
	const maxTotalRunesWithImages = 200000 // higher cap when images are inlined

	var parts []string
	totalRunes := 0
	hasInlinedImages := false
	for _, p := range paths {
		// Use the conservative cap for the loop break — if images are inlined
		// later in this iteration, the final truncation uses the higher cap.
		if totalRunes >= maxTotalRunesWithImages {
			break
		}
		if !hasInlinedImages && totalRunes >= maxTotalRunes {
			break
		}
		if !looksLikeDocumentFile(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() > maxFileSize || info.Size() == 0 {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		// Skip binary-looking content (high ratio of non-printable bytes).
		if looksLikeBinary(data) {
			continue
		}
		// Resolve image references in markdown files so the preview panel
		// can render them as inline images (frontend has no filesystem access).
		if strings.HasSuffix(strings.ToLower(p), ".md") || strings.HasSuffix(strings.ToLower(p), ".markdown") {
			before := len(content)
			content = inlineImageReferences(content, p)
			if len(content) > before {
				hasInlinedImages = true
			}
		}
		parts = append(parts, content)
		totalRunes += len([]rune(content))
	}
	if len(parts) == 0 {
		return ""
	}
	result := strings.Join(parts, "\n\n---\n\n")
	// Truncate to cap if needed. Use higher cap when images are inlined
	// because base64 image data expands content significantly but renders
	// as fixed-size UI elements (not scrollable prose).
	// Truncate at a safe boundary — never cut inside a data: URL (which would
	// produce a broken image). Find the last complete line before the cap.
	cap := maxTotalRunes
	if hasInlinedImages {
		cap = maxTotalRunesWithImages
	}
	runes := []rune(result)
	if len(runes) > cap {
		// Find the last newline before the cap to avoid cutting mid-image.
		// We work in rune space to find the boundary, then convert back.
		truncRunes := runes[:cap]
		// Search backwards from the end for a newline rune.
		lastNLRuneIdx := -1
		for j := len(truncRunes) - 1; j >= cap/2; j-- {
			if truncRunes[j] == '\n' {
				lastNLRuneIdx = j
				break
			}
		}
		if lastNLRuneIdx > 0 {
			result = string(truncRunes[:lastNLRuneIdx])
		} else {
			result = string(truncRunes)
		}
	}
	return result
}

// looksLikeBinary returns true if data appears to be binary (>10% non-text bytes).
func looksLikeBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	nonText := 0
	for i := 0; i < checkLen; i++ {
		b := data[i]
		if b == 0 || (b < 0x20 && b != '\n' && b != '\r' && b != '\t') {
			nonText++
		}
	}
	return float64(nonText)/float64(checkLen) > 0.10
}

// looksLikeDocumentFile returns true if path has a document extension that
// should be read as potential phase output for the workflow preview panel.
// This is a whitelist approach — only known document formats are read.
// SVG, XML, HTML, JSON, CSV, and other structured-data or resource files are
// excluded because they are not human-readable document prose.
func looksLikeDocumentFile(path string) bool {
	lower := strings.ToLower(path)
	docExts := []string{
		".md", ".markdown", ".txt", ".text",
		".rst", ".adoc", ".asciidoc", ".tex", ".org",
	}
	for _, ext := range docExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// imageRefRe matches Markdown image references to local image files (case-insensitive extensions).
var imageRefRe = regexp.MustCompile(`(?i)(!\[[^\]]*\])\(([^)]+\.(?:svg|png|jpg|jpeg|gif|webp|bmp))\)`)

// inlineImageReferences resolves Markdown image references (![alt](path.ext))
// in document content by reading the referenced image files and converting them
// to inline data URLs. This allows the frontend preview panel to render images
// without requiring filesystem access from the browser context.
// Only local path references are resolved; URLs (http/https/data:) are unchanged.
// Individual images larger than 100KB are skipped to prevent output bloat.
func inlineImageReferences(content string, basePath string) string {
	if basePath == "" || !strings.Contains(content, "![") {
		return content
	}
	const maxImageSize = 100 * 1024 // skip images larger than 100KB
	const maxInlineCount = 20       // cap inlined images to avoid excessive I/O
	baseDir := filepath.Dir(basePath)
	inlinedCount := 0
	return imageRefRe.ReplaceAllStringFunc(content, func(match string) string {
		if inlinedCount >= maxInlineCount {
			return match
		}
		parts := imageRefRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		altPart := parts[1] // ![alt text]
		imgPath := parts[2]
		// Skip URLs
		if strings.HasPrefix(imgPath, "http://") || strings.HasPrefix(imgPath, "https://") || strings.HasPrefix(imgPath, "data:") {
			return match
		}
		// Resolve relative to the .md file's directory
		fullPath := imgPath
		if !filepath.IsAbs(imgPath) {
			fullPath = filepath.Join(baseDir, imgPath)
		}
		info, err := os.Stat(fullPath)
		if err != nil || info.Size() > maxImageSize {
			return match // skip if file missing or too large
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return match
		}
		mimeType := imageMimeType(imgPath)
		encoded := base64.StdEncoding.EncodeToString(data)
		inlinedCount++
		return altPart + "(data:" + mimeType + ";base64," + encoded + ")"
	})
}

// imageMimeType returns the MIME type for a given image file extension.
func imageMimeType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".bmp"):
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
