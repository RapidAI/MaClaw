package main

import (
	"context"
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// newSrvIMMessageHandler builds im_message for CoreAgentExecutor (list_targets | send | send_file).
func newSrvIMMessageHandler(svc *agentservice.Service) func(args map[string]interface{}) string {
	h := newSrvIMMessageHandlerContext(svc)
	return func(args map[string]interface{}) string {
		return h(context.Background(), agentservice.Principal{TenantID: "system", UserID: "scheduler"}, args)
	}
}

func newSrvIMMessageHandlerContext(svc *agentservice.Service) func(context.Context, agentservice.Principal, map[string]interface{}) string {
	return func(ctx context.Context, principal agentservice.Principal, args map[string]interface{}) string {
		return scheduler.RunIMMessageTool(args,
			func(a map[string]interface{}) string {
				text, err := srvListIMTargetsForPrincipal(ctx, svc, principal, a)
				if err != nil {
					return err.Error()
				}
				return text
			},
			func(a map[string]interface{}) string { return srvToolIMMessageSendForPrincipal(ctx, svc, principal, a) },
			func(a map[string]interface{}) string { return srvToolIMMessageSendFileForPrincipal(ctx, svc, principal, a) },
		)
	}
}

func srvListIMTargetsForPrincipal(ctx context.Context, svc *agentservice.Service, principal agentservice.Principal, args map[string]interface{}) (string, error) {
	channel := firstNonEmpty(stringArg(args, "channel"), stringArg(args, "platform"))
	query := firstNonEmpty(stringArg(args, "query"), stringArg(args, "group_name"), stringArg(args, "name"))
	return listSrvScheduleDeliveryTargetsForPrincipal(ctx, svc, principal, channel, query)
}

func srvToolIMMessageSend(svc *agentservice.Service, args map[string]interface{}) string {
	return srvToolIMMessageSendForPrincipal(context.Background(), svc, agentservice.Principal{TenantID: "system", UserID: "scheduler"}, args)
}

func srvToolIMMessageSendForPrincipal(parent context.Context, svc *agentservice.Service, principal agentservice.Principal, args map[string]interface{}) string {
	text := scheduler.IMMessageTextFromArgs(args)
	if text == "" {
		return "缺少 text 参数（要发送的消息正文）"
	}
	d, err := parseSrvScheduleDelivery(args)
	if err != nil {
		return err.Error()
	}
	if err := resolveSrvScheduleDeliveryForPrincipal(parent, svc, principal, d); err != nil {
		return err.Error()
	}
	if d == nil || !d.Active() {
		return "缺少投递目标：请提供 group_name / group_id / user_id，或 delivery.targets"
	}
	ctx, cancel := scheduler.WithDeliveryTimeout(parent, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	if err := deliverSrvIMText(ctx, svc, principal, d.Channel, d.Targets, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return scheduler.FormatIMMessageSendOK(scheduler.SummarizeDelivery(d), text)
}

// srvToolIMMessageSendFileForPrincipal uploads a local file to IM targets
// (action=send_file). Lansenger media messages carry no caption field, so a
// caption text is delivered as a normal message first on that channel;
// telegram/qq attach it natively.
func srvToolIMMessageSendFileForPrincipal(parent context.Context, svc *agentservice.Service, principal agentservice.Principal, args map[string]interface{}) string {
	path := scheduler.IMMessageFilePathFromArgs(args)
	if path == "" {
		return "缺少 path 参数（要发送的文件路径）"
	}
	d, err := parseSrvScheduleDelivery(args)
	if err != nil {
		return err.Error()
	}
	if err := resolveSrvScheduleDeliveryForPrincipal(parent, svc, principal, d); err != nil {
		return err.Error()
	}
	if d == nil || !d.Active() {
		return "缺少投递目标：请提供 group_name / group_id / user_id，或 delivery.targets"
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("文件不可读: %s", err.Error())
	}
	if info.IsDir() {
		return "path 是目录，无法发送"
	}
	if info.Size() == 0 {
		// Fail before any caption text goes out so an unsendable file never
		// leaves a half-delivered caption behind.
		return "文件为空，无法发送"
	}
	if info.Size() > agent.SendFileMaxSize {
		return fmt.Sprintf("文件过大（%d 字节），最大允许 %d 字节", info.Size(), agent.SendFileMaxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %s", err.Error())
	}
	name := scheduler.IMMessageFileNameFromArgs(args)
	if name == "" {
		name = filepath.Base(path)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(name))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	caption := scheduler.IMMessageTextFromArgs(args)
	channel := scheduler.DefaultDeliveryChannel(d.Channel)

	ctx, cancel := scheduler.WithDeliveryTimeout(parent, scheduler.DefaultIMFileDeliveryTimeout)
	defer cancel()
	if caption != "" && channel == scheduler.DeliveryChannelLansenger {
		if err := deliverSrvIMText(ctx, svc, principal, channel, d.Targets, caption); err != nil {
			return fmt.Sprintf("发送失败: %s", err.Error())
		}
		caption = ""
	}
	_, err = scheduler.FanOutDeliveryTargets(d.Targets, func(_ int, target scheduler.DeliveryTarget) error {
		return deliverSrvIMFileToTarget(ctx, svc, principal, channel, target, data, name, mimeType, caption)
	})
	if err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return scheduler.FormatIMMessageSendFileOK(scheduler.SummarizeDelivery(d), name, info.Size())
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
	store := srvDeliveryStateForPrincipal(svc, principal)
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
