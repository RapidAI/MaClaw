package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// deliverScheduledTaskResult pushes the agent result according to task.Delivery.
// No-op when delivery is inactive or ShouldDeliver is false.
// Returns a non-nil error only when delivery was attempted and at least one target failed.
func (a *App) deliverScheduledTaskResult(task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if a == nil || task == nil || task.Delivery == nil || !task.Delivery.Active() {
		return nil
	}
	d := task.Delivery
	if !d.ShouldDeliver(runErr, resultText) {
		return nil
	}
	body := d.FormatBody(task.Name, resultText, runErr)
	if strings.TrimSpace(body) == "" {
		return nil
	}

	ctx, cancel := scheduler.WithDeliveryTimeout(nil, scheduler.DefaultIMDeliveryTimeout)
	defer cancel()
	if err := a.DeliverIMFromTaskDelivery(ctx, d, body); err != nil {
		log.Printf("[scheduled-task] delivery failed task=%s: %v", task.ID, err)
		return err
	}
	return nil
}

func (a *App) deliveryStateStore() *scheduler.DeliveryStateStore {
	if a == nil {
		return nil
	}
	a.deliveryStateStoreOnce.Do(func() {
		base := a.getMaclawBaseDir()
		if base != "" {
			a.deliveryStateStoreCached = scheduler.NewDeliveryStateStore(base)
		}
	})
	return a.deliveryStateStoreCached
}

// deliverScheduledTaskTarget sends one target and returns the concrete peer id used (for memory).
func (a *App) deliverScheduledTaskTarget(ctx context.Context, channel string, target scheduler.DeliveryTarget, text string) (peer string, err error) {
	channel = scheduler.DefaultDeliveryChannel(channel)
	switch channel {
	case scheduler.DeliveryChannelLansenger:
		return a.deliverLansengerScheduledTarget(ctx, target, text)
	case scheduler.DeliveryChannelWeixin:
		return a.deliverWeixinScheduledTarget(ctx, target, text)
	case scheduler.DeliveryChannelTelegram:
		return a.deliverTelegramScheduledTarget(ctx, target, text)
	case scheduler.DeliveryChannelQQ:
		return a.deliverQQScheduledTarget(ctx, target, text)
	default:
		return "", fmt.Errorf("unsupported channel %q", channel)
	}
}

func (a *App) deliverWeixinScheduledTarget(_ context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return "", fmt.Errorf("weixin delivery only supports kind=user (owner private session)")
	}
	if !scheduler.IsSelfPeerID(target.UserID) {
		return "", fmt.Errorf("weixin: only owner private push is supported (use user_id=self); got %q", target.UserID)
	}
	a.ensureWeixinGateway()
	if a.weixinGateway == nil {
		return "", fmt.Errorf("weixin gateway unavailable")
	}
	if err := a.weixinGateway.SendProactiveText(text); err != nil {
		return "", err
	}
	// Session-based: no durable platform peer id (RememberPeer rejects "self").
	return "", nil
}

func (a *App) deliverTelegramScheduledTarget(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	idStr := strings.TrimSpace(target.UserID)
	if idStr == "" {
		idStr = strings.TrimSpace(target.GroupID)
	}
	idStr = a.deliveryStateStore().ResolveSelfPeer(scheduler.DeliveryChannelTelegram, idStr)
	var chatID int64
	if scheduler.IsSelfPeerID(idStr) {
		chatID = 0 // live gateway last-active fallback
	} else {
		n, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return "", fmt.Errorf("telegram: user_id/group_id must be chat_id integer or self: %q", idStr)
		}
		chatID = n
	}
	a.ensureTelegramGateway()
	if a.telegramGateway == nil {
		return "", fmt.Errorf("telegram gateway unavailable")
	}
	used, err := a.telegramGateway.SendProactiveText(chatID, text)
	if err != nil {
		return "", err
	}
	_ = ctx
	if used != 0 {
		return strconv.FormatInt(used, 10), nil
	}
	if !scheduler.IsSelfPeerID(idStr) {
		return idStr, nil
	}
	return "", nil
}

func (a *App) deliverQQScheduledTarget(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return "", fmt.Errorf("qq delivery only supports kind=user (openid)")
	}
	openID := a.deliveryStateStore().ResolveSelfPeer(scheduler.DeliveryChannelQQ, strings.TrimSpace(target.UserID))
	a.ensureQQBotGateway()
	if a.qqBotGateway == nil {
		return "", fmt.Errorf("qqbot gateway unavailable")
	}
	used, err := a.qqBotGateway.SendProactiveText(openID, text)
	if err != nil {
		return "", err
	}
	_ = ctx
	return used, nil
}

func (a *App) deliverLansengerScheduledTarget(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	switch target.Kind {
	case scheduler.DeliveryKindGroup:
		if strings.TrimSpace(target.GroupID) == "" {
			return "", fmt.Errorf("lansenger group target missing group_id")
		}
		gw, err := a.lansengerGatewayForSend()
		if err != nil {
			return "", err
		}
		msg := lansenger.OutgoingText{
			ToUserID: target.GroupID,
			Text:     text,
			IsGroup:  true,
		}
		if target.MentionAll {
			msg.Reminder = &lansenger.OutgoingReminder{All: true}
		} else if len(target.MentionUserIDs) > 0 {
			msg.Reminder = &lansenger.OutgoingReminder{UserIDs: append([]string(nil), target.MentionUserIDs...)}
		}
		if err := gw.SendText(ctx, msg); err != nil {
			return "", err
		}
		return target.GroupID, nil
	case scheduler.DeliveryKindUser:
		userID := strings.TrimSpace(target.UserID)
		if userID == "" {
			return "", fmt.Errorf("lansenger user target missing user_id")
		}
		// user_id=self → remembered peer, else live private session.
		userID = a.deliveryStateStore().ResolveSelfPeer(scheduler.DeliveryChannelLansenger, userID)
		if scheduler.IsSelfPeerID(userID) {
			a.ensureLansengerGateway()
			if a.lansengerGateway == nil {
				return "", fmt.Errorf("lansenger: user_id=self 需要 staffId，或先用蓝信私聊机器人一次")
			}
			if err := a.lansengerGateway.SendProactiveText(text); err != nil {
				return "", err
			}
			if peer := a.lansengerGateway.LastPrivatePeerID(); peer != "" {
				return peer, nil
			}
			return "", nil
		}
		gw, err := a.lansengerGatewayForSend()
		if err != nil {
			return "", err
		}
		if err := gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: userID,
			Text:     text,
			IsGroup:  false,
		}); err != nil {
			return "", err
		}
		return userID, nil
	default:
		return "", fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

// lansengerGatewayForSend returns a live gateway, or a short-lived REST client
// built from saved credentials (same pattern as ListLansengerGroups).
func (a *App) lansengerGatewayForSend() (*lansenger.Gateway, error) {
	if a == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	if a.lansengerGateway != nil {
		a.lansengerGateway.mu.Lock()
		gw := a.lansengerGateway.gateway
		a.lansengerGateway.mu.Unlock()
		if gw != nil {
			return gw, nil
		}
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	appID := strings.TrimSpace(cfg.LansengerAppID)
	appSecret := strings.TrimSpace(cfg.LansengerAppSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("蓝信未配置 App ID / App Secret")
	}
	apiURL := strings.TrimSpace(cfg.LansengerApiGatewayURL())
	if apiURL == "" {
		return nil, fmt.Errorf("蓝信网关地址为空")
	}
	return lansenger.NewGateway(lansenger.Config{
		AppID:            appID,
		AppSecret:        appSecret,
		ApiGatewayURL:    apiURL,
		WebSocketBaseURL: strings.TrimSpace(cfg.LansengerWebSocketGatewayURL()),
	}, nil), nil
}
