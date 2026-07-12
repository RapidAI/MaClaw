package main

import "strings"

// appendWorkflowReviewActions attaches structured confirmation buttons to the
// agent loop response when a V2 workflow doc phase completes and transitions
// to WaitingConfirm state.
//
// 机制：引擎层直接注入按钮，不依赖 LLM 的 ask_user 调用。
// 确认动作是确定性的（fast-path 路由），零 LLM 调用延迟。
//
// 按钮列表：
//   - "确认并推进" (primary) → fast-path ReviewIntentConfirm
//   - "输入补充/修改意见" (secondary) → 前端聚焦输入框，不发消息
//   - "中止" (danger) → 前端二次确认后 fast-path ReviewIntentCancel
func (h *IMMessageHandler) appendWorkflowReviewActions(resp *IMAgentResponse, userID string, loopCtx *LoopContext) {
	if resp == nil {
		return
	}

	// Don't overwrite existing actions (e.g. ask_user options from agent loop).
	if len(resp.Actions) > 0 {
		return
	}

	// Don't attach review buttons on error responses.
	if resp.Error != "" || resp.HardExit {
		return
	}

	wf := h.getWorkflowV2()
	if wf == nil {
		return
	}

	ownerID := h.workflowPolicyOwnerID(userID, loopCtx)
	state := wf.machine.GetActive(ownerID)
	if state == nil || !state.IsWaitingConfirm() {
		return
	}

	phase := state.ActivePhase()
	if phase == nil {
		return
	}

	// Determine user language for i18n.
	lang := "zh"
	if loopCtx != nil && loopCtx.Lang != "" {
		lang = loopCtx.Lang
	}

	// 构建按钮列表：确认 + 补充修改 + 中止
	// 中止按钮 danger 样式（红色文字，不显眼），放在最后
	if strings.HasPrefix(lang, "en") {
		resp.Actions = []IMResponseAction{
			{Label: "Confirm & Proceed", Command: "__wf_review__ confirm", Style: "primary"},
			{Label: "Provide Feedback", Command: "__wf_review__ supplement_focus", Style: "secondary"},
			{Label: "Abort", Command: "__wf_review__ abort", Style: "danger"},
		}
	} else {
		resp.Actions = []IMResponseAction{
			{Label: "确认并推进", Command: "__wf_review__ confirm", Style: "primary"},
			{Label: "输入补充/修改意见", Command: "__wf_review__ supplement_focus", Style: "secondary"},
			{Label: "中止", Command: "__wf_review__ abort", Style: "danger"},
		}
	}

	// Note: we intentionally do NOT append hint text to resp.Text here.
	// In V2 workflow doc phases, resp.Text is often empty (document content lives
	// in WorkflowDocBuffer and was captured earlier by recordWorkflowV2Output).
	// If we set resp.Text to a hint string, the frontend's resolveFinalRoundContent
	// may discard the streamed document content in favor of this shorter text, or
	// vice versa — either way creates inconsistency. The button labels are self-
	// explanatory ("确认并推进" / "输入补充/修改意见" / "中止"), and the
	// frontend always renders them below the message content regardless of resp.Text.
}
