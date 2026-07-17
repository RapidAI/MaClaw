package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// newIMMessageHandler creates the TUI-side im_message tool (list_targets | send).
func newIMMessageHandler(app *TUIApp) agent.ToolHandler {
	return func(args map[string]interface{}) string {
		return scheduler.RunIMMessageTool(args, app.toolListScheduleDeliveryTargets, app.toolIMMessageSend)
	}
}

func (app *TUIApp) toolIMMessageSend(args map[string]interface{}) string {
	if app == nil {
		return "应用未初始化，无法发送 IM 消息"
	}
	text := scheduler.IMMessageTextFromArgs(args)
	if text == "" {
		return "缺少 text 参数（要发送的消息正文）"
	}
	d, err := parseTUIScheduleDelivery(args)
	if err != nil {
		return err.Error()
	}
	if d == nil || !d.Active() {
		return "缺少投递目标：请提供 group_name / group_id / user_id，或 delivery.targets"
	}
	if err := app.resolveScheduleDelivery(d); err != nil {
		return err.Error()
	}
	ctx, cancel := scheduler.WithDeliveryTimeout(nil, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	if err := app.DeliverIMFromTaskDelivery(ctx, d, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return scheduler.FormatIMMessageSendOK(scheduler.SummarizeDelivery(d), text)
}
