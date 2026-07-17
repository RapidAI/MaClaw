package main

import (
	"context"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// newSrvIMMessageHandler builds im_message for CoreAgentExecutor (list_targets | send).
func newSrvIMMessageHandler(svc *agentservice.Service) func(args map[string]interface{}) string {
	return func(args map[string]interface{}) string {
		return scheduler.RunIMMessageTool(args,
			func(a map[string]interface{}) string { return srvToolListScheduleDeliveryTargets(svc, a) },
			func(a map[string]interface{}) string { return srvToolIMMessageSend(svc, a) },
		)
	}
}

func srvToolIMMessageSend(svc *agentservice.Service, args map[string]interface{}) string {
	text := scheduler.IMMessageTextFromArgs(args)
	if text == "" {
		return "缺少 text 参数（要发送的消息正文）"
	}
	d, err := parseAndResolveSrvDelivery(svc, args)
	if err != nil {
		return err.Error()
	}
	if d == nil || !d.Active() {
		return "缺少投递目标：请提供 group_name / group_id / user_id，或 delivery.targets"
	}
	principal := agentservice.Principal{TenantID: "system", UserID: "scheduler"}
	ctx, cancel := scheduler.WithDeliveryTimeout(nil, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	if err := deliverSrvIMText(ctx, svc, principal, d.Channel, d.Targets, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return scheduler.FormatIMMessageSendOK(scheduler.SummarizeDelivery(d), text)
}

// deliverSrvIMText is the shared immediate send path (schedule delivery + im_message).
func deliverSrvIMText(ctx context.Context, svc *agentservice.Service, principal agentservice.Principal, channel string, targets []scheduler.DeliveryTarget, text string) error {
	text = scheduler.TruncateDeliveryBody(text)
	if text == "" {
		return fmt.Errorf("message text is empty")
	}
	if len(targets) == 0 {
		return fmt.Errorf("no delivery targets")
	}
	channel = scheduler.DefaultDeliveryChannel(channel)
	ctx, cancel := scheduler.WithDeliveryTimeout(ctx, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	send, err := newChannelSender(ctx, svc, principal, channel)
	if err != nil {
		return err
	}
	store := srvDeliveryState(svc)
	_, err = scheduler.FanOutDeliveryTargets(targets, func(i int, target scheduler.DeliveryTarget) error {
		peer, sendErr := send(ctx, target, text)
		if sendErr != nil {
			log.Printf("[MaClawSrv-IM] delivery failed channel=%s target=%d: %v", channel, i, sendErr)
			return sendErr
		}
		if store != nil && peer != "" && scheduler.CanRememberAsSelfPeer(target) {
			store.RememberPeer(channel, peer)
		}
		return nil
	})
	return err
}
