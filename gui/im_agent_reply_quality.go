package main

import (
	"regexp"
	"strings"
)

func looksLikeNoToolStallReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	stallHints := []string{
		"鎴戝厛鎯虫兂",
		"let me think", "i'll think", "think first", "organize the steps", "plan this out", "analyze first", "check first",
	}
	for _, hint := range stallHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	// Detect "blocked on one track but intending to continue another" pattern.
	// When the LLM reports a blocker (login required, waiting for approval, etc.)
	// AND also mentions continuing with other work, it should not finalize 鈥?	// the agent loop should force another round so the LLM can proceed with
	// the unblocked subtask via tool calls.
	blockerHints := []string{
		"绛夊緟鐧诲綍", "绛夊緟鎵爜",
		"requires login", "needs login", "need to log in", "waiting for approval",
	}
	// These hints indicate the LLM intends to work on a different subtask.
	// Deliberately excludes bare "继续" — it often appears in blocker context
	// ("需要登录才能继续") rather than indicating a parallel work track.
	continueHints := []string{
		"同时准备", "同时处理", "与此同时", "另一方面",
		"meanwhile", "in the meantime", "continue with", "proceed with", "at the same time",
	}
	hasBlocker := false
	for _, hint := range blockerHints {
		if strings.Contains(lower, hint) {
			hasBlocker = true
			break
		}
	}
	if hasBlocker {
		for _, hint := range continueHints {
			if strings.Contains(lower, hint) {
				return true
			}
		}
	}
	return false
}

// Compiled regexes for isSubstantivePhaseDocument 鈥?package-level for performance.
var (
	substantiveHeadingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	substantiveNumberedRe   = regexp.MustCompile(`(?m)^(?:\d+[\.\x{3001}\)]\s*)\S`)
	substantiveBulletLineRe = regexp.MustCompile(`(?m)^[-*]\s+\S`)
)

// isSubstantivePhaseDocument checks whether the LLM output constitutes a
// substantive phase document (vs a short transitional preamble).
// It returns true if ANY of the following conditions hold:
// 1. Text is 200+ runes long (sufficient length for a document)
// 2. Text contains Markdown heading markers (# , ## , ### , etc.)
// 3. Text contains numbered list patterns (1. , 2. , 1銆? etc.)
// 4. Text contains 3+ bullet list lines (- item or * item)
func isSubstantivePhaseDocument(text string) bool {
	if len([]rune(text)) >= 200 {
		return true
	}
	if substantiveHeadingRe.MatchString(text) {
		return true
	}
	if substantiveNumberedRe.MatchString(text) {
		return true
	}
	if len(substantiveBulletLineRe.FindAllStringIndex(text, 3)) >= 3 {
		return true
	}
	return false
}

func hasFutureDeliveryPromise(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	futureDeliveryHints := []string{
		"马上发你", "继续发你", "继续生成", "继续整理",
		"will send", "send it to you shortly", "send you shortly", "about to send", "going to send",
	}
	for _, hint := range futureDeliveryHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func looksLikeCompletedOrSummaryDeliverableReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	completedHints := []string{
		"宸茬粡完成", "宸茬粡整理", "整理濂戒簡",
		"宸叉矇娣€", "娌夋穩完成", "娌夋穩瀹屾瘯",
		"缁撴灉濡備笅", "鎬荤粨濡備笅", "缁撹濡備笅", "鎶ュ憡濡備笅", "鏂囨。濡備笅",
		"completed", "done", "here is", "here's", "results below", "summary below", "below is",
		"saved", "recorded",
	}
	hasCompletedHint := false
	for _, hint := range completedHints {
		if strings.Contains(lower, hint) {
			hasCompletedHint = true
			break
		}
	}
	if !hasCompletedHint {
		return false
	}
	if hasFutureDeliveryPromise(trimmed) {
		return false
	}
	return true
}

func looksLikePromiseOnlyDeliverableReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	// Negative patterns: self-introduction / capability-listing context.
	// When the model describes what it *can* do (e.g. "甯綘鍐欐枃妗ｃ€佸仛整理"),
	// the deliverable keywords appear in a descriptive context, not as an
	// actual promise to deliver a specific file. Skip these.
	selfIntroHints := []string{
		"鎴戝彨", "鎴戠殑鍚嶅瓧", "骞虫椂我会", "鎴戣兘甯綘",
		"i'm ", "my name is", "i can help you", "nice to meet",
	}
	for _, hint := range selfIntroHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	// "鎴戞槸" is very common in Chinese; only treat it as self-intro when it
	// appears at the very beginning of the response (first 10 chars).
	if len([]rune(lower)) >= 2 && strings.HasPrefix(lower, "鎴戞槸") {
		return false
	}

	deliverableHints := []string{
		"pdf", "生成pdf", "生成 pdf", "鎶ュ憡", "鏂囨。", "鏂囦欢", "缁艰堪", "发你",
		"report", "document", "file", "send you", "deliver", "summary",
	}
	hasDeliverableIntent := false
	for _, hint := range deliverableHints {
		if strings.Contains(lower, hint) {
			hasDeliverableIntent = true
			break
		}
	}
	if !hasDeliverableIntent {
		if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "锛") {
			hasDeliverableIntent = true
		}
	}
	if !hasDeliverableIntent {
		return false
	}
	promiseHints := []string{
		"我来", "我会", "马上", "立刻", "直接", "继续", "执行", "生成", "整理", "添加", "补充",
		"i will", "i'll", "let me", "going to", "about to", "right away", "prepare", "generate", "send", "continue", "append",
	}
	hasPromiseHint := false
	for _, hint := range promiseHints {
		if strings.Contains(lower, hint) {
			hasPromiseHint = true
			break
		}
	}
	if !hasPromiseHint {
		return false
	}
	if looksLikeCompletedOrSummaryDeliverableReply(trimmed) {
		return false
	}
	failureHints := []string{"澶辫触", "鏃犳硶", "鍑洪敊", "鎶ラ敊", "error", "failed", "unable", "cannot"}
	for _, hint := range failureHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	futureDeliveryPromise := hasFutureDeliveryPromise(trimmed)
	completionHints := []string{
		"灏嗗彂閫佺粰鐢ㄦ埛", "宸插噯澶囧ソ", "澶辫触鍘熷洜", "鏃犳硶生成", "localfile", "[file_base64|", "[voice_base64|",
		"宸茬粡完成", "缁撴灉濡備笅", "鎬荤粨濡備笅", "here is", "here's", "results below", "summary below",
	}
	for _, hint := range completionHints {
		if strings.Contains(lower, hint) {
			if futureDeliveryPromise {
				break
			}
			return false
		}
	}
	if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "锛") {
		return true
	}
	return true
}

func shouldRecoverForPendingSkillRunNoToolReply(text string, runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false
	}
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return true
	}
	if looksLikeCompletedOrSummaryDeliverableReply(trimmed) {
		return false
	}
	lower := strings.ToLower(trimmed)
	failureHints := []string{"澶辫触", "鏃犳硶", "鍑洪敊", "鎶ラ敊", "error", "failed", "unable", "cannot"}
	for _, hint := range failureHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	if looksLikePromiseOnlyDeliverableReply(trimmed) || looksLikeNoToolStallReply(trimmed) {
		return true
	}
	pendingRunContinuationHints := []string{
		"继续添加", "继续补充", "继续整理", "继续生成",
		"append more", "continue writing", "continue generating",
	}
	for _, hint := range pendingRunContinuationHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "锛") {
		return true
	}
	return false
}

func looksLikePromiseOnlyPDFReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	hasPDFIntent := strings.Contains(lower, "pdf") || strings.Contains(lower, "生成pdf") || strings.Contains(lower, "生成 pdf")
	if !hasPDFIntent {
		return false
	}
	return looksLikePromiseOnlyDeliverableReply(text)
}

func shouldForceAnotherRoundForDeliverable(text string, toolCalls int, pendingFiles int) bool {
	if toolCalls > 0 || pendingFiles > 0 {
		return false
	}
	return looksLikePromiseOnlyDeliverableReply(text)
}

func shouldForceAnotherRoundForPDF(text string, toolCalls int, pendingFiles int) bool {
	if toolCalls > 0 || pendingFiles > 0 {
		return false
	}
	return looksLikePromiseOnlyPDFReply(text)
}
