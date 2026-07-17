package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// toolIMMessage is the general-purpose IM push tool (not tied to scheduled tasks).
// Actions: list_targets | send (action may be omitted and inferred).
func (h *IMMessageHandler) toolIMMessage(args map[string]interface{}) string {
	return scheduler.RunIMMessageTool(args, h.toolListScheduleDeliveryTargets, h.toolIMMessageSend)
}

func (h *IMMessageHandler) toolIMMessageSend(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return "应用未初始化，无法发送 IM 消息"
	}
	text := scheduler.IMMessageTextFromArgs(args)
	if text == "" {
		return "缺少 text 参数（要发送的消息正文）"
	}

	d, err := h.parseAndResolveScheduleDelivery(args)
	if err != nil {
		return err.Error()
	}
	if d == nil || !d.Active() {
		return "缺少投递目标：请提供 group_name / group_id / user_id，或 delivery.targets"
	}

	ctx, cancel := scheduler.WithDeliveryTimeout(nil, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	if err := h.app.DeliverIMFromTaskDelivery(ctx, d, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return scheduler.FormatIMMessageSendOK(scheduler.SummarizeDelivery(d), text)
}
