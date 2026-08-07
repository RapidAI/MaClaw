package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/telegram"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// deliverScheduledTaskResult pushes agent output according to task.Delivery.
func (app *TUIApp) deliverScheduledTaskResult(task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if app == nil || task == nil || task.Delivery == nil || !task.Delivery.Active() {
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
	if err := app.DeliverIMFromTaskDelivery(ctx, d, body); err != nil {
		log.Printf("[TUI-ScheduledTask] delivery failed task=%s: %v", task.ID, err)
		return err
	}
	return nil
}

// DeliverIMText immediately pushes text to IM channel targets (shared by schedule + im_message).
func (app *TUIApp) DeliverIMText(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, text string) error {
	if app == nil {
		return fmt.Errorf("app unavailable")
	}
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
	send, err := app.newScheduleChannelSender(channel)
	if err != nil {
		return err
	}
	_, err = scheduler.FanOutDeliveryTargets(targets, func(i int, target scheduler.DeliveryTarget) error {
		if err := send(ctx, target, text); err != nil {
			log.Printf("[TUI-IM] delivery failed channel=%s target=%d: %v", channel, i, err)
			return err
		}
		return nil
	})
	return err
}

// DeliverIMFromTaskDelivery sends using an active TaskDelivery config.
func (app *TUIApp) DeliverIMFromTaskDelivery(ctx context.Context, d *scheduler.TaskDelivery, text string) error {
	if d == nil || !d.Active() {
		return fmt.Errorf("delivery not active")
	}
	return app.DeliverIMText(ctx, d.Channel, d.Targets, text)
}

// DeliverIMFile immediately uploads a local file to IM channel targets.
// caption, when non-empty, is delivered as a text message to the same target first
// (Lansenger media messages carry no caption field). Currently lansenger only.
func (app *TUIApp) DeliverIMFile(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, path, fileName, caption string) (string, int64, error) {
	if app == nil {
		return "", 0, fmt.Errorf("app unavailable")
	}
	if len(targets) == 0 {
		return "", 0, fmt.Errorf("no delivery targets")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", 0, fmt.Errorf("file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("file not found or inaccessible: %w", err)
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("path is a directory: %s", path)
	}
	if info.Size() == 0 {
		// Fail before any caption text goes out so an unsendable file never
		// leaves a half-delivered caption behind.
		return "", 0, fmt.Errorf("file is empty: %s", path)
	}
	if info.Size() > agent.SendFileMaxSize {
		return "", 0, fmt.Errorf("file too large (%d bytes, max %d)", info.Size(), agent.SendFileMaxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read file failed: %w", err)
	}
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = filepath.Base(path)
	}
	caption = scheduler.TruncateDeliveryBody(caption)
	channel = scheduler.DefaultDeliveryChannel(channel)
	if channel != scheduler.DeliveryChannelLansenger {
		return "", 0, fmt.Errorf("channel %q 暂不支持 im_message 文件发送（目前仅蓝信 lansenger）", channel)
	}
	ctx, cancel := scheduler.WithDeliveryTimeout(ctx, scheduler.DefaultIMFileDeliveryTimeout)
	defer cancel()
	gw, err := app.newLansengerGatewayForSend()
	if err != nil {
		return "", 0, err
	}
	store := app.deliveryStateStore()
	_, err = scheduler.FanOutDeliveryTargets(targets, func(i int, target scheduler.DeliveryTarget) error {
		peer, sendErr := app.sendLansengerFileTarget(ctx, gw, store, target, data, name, caption)
		if sendErr != nil {
			log.Printf("[TUI-IM] file delivery failed channel=%s target=%d: %v", channel, i, sendErr)
			return sendErr
		}
		if peer != "" && scheduler.CanRememberAsSelfPeer(target) {
			store.RememberPeer(channel, peer)
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return name, info.Size(), nil
}

// sendLansengerFileTarget uploads one file to a single lansenger target and
// returns the concrete peer id used, mirroring sendLansengerScheduleTarget.
func (app *TUIApp) sendLansengerFileTarget(ctx context.Context, gw *lansenger.Gateway, store *scheduler.DeliveryStateStore, target scheduler.DeliveryTarget, data []byte, name, caption string) (string, error) {
	if gw == nil {
		return "", fmt.Errorf("lansenger gateway is nil")
	}
	send := func(peer string, isGroup bool) error {
		if strings.TrimSpace(caption) != "" {
			text := lansenger.OutgoingText{ToUserID: peer, Text: caption, IsGroup: isGroup}
			if isGroup {
				if target.MentionAll {
					text.Reminder = &lansenger.OutgoingReminder{All: true}
				} else if len(target.MentionUserIDs) > 0 {
					text.Reminder = &lansenger.OutgoingReminder{UserIDs: append([]string(nil), target.MentionUserIDs...)}
				}
			}
			if err := gw.SendText(ctx, text); err != nil {
				return fmt.Errorf("caption send failed: %w", err)
			}
		}
		return gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  peer,
			FileData:  data,
			FileName:  name,
			MediaType: "file",
			IsGroup:   isGroup,
			Strict:    true,
		})
	}
	switch target.Kind {
	case scheduler.DeliveryKindGroup:
		if strings.TrimSpace(target.GroupID) == "" {
			return "", fmt.Errorf("lansenger group target missing group_id")
		}
		if err := send(target.GroupID, true); err != nil {
			return "", err
		}
		return target.GroupID, nil
	case scheduler.DeliveryKindUser:
		userID := strings.TrimSpace(target.UserID)
		if userID == "" {
			return "", fmt.Errorf("lansenger user target missing user_id")
		}
		userID = store.ResolveSelfPeer(scheduler.DeliveryChannelLansenger, userID)
		if scheduler.IsSelfPeerID(userID) {
			return "", fmt.Errorf("lansenger: user_id=self 需要 staffId，或先成功私聊推送一次以记住对方")
		}
		if err := send(userID, false); err != nil {
			return "", err
		}
		return userID, nil
	default:
		return "", fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

func (app *TUIApp) deliveryStateStore() *scheduler.DeliveryStateStore {
	if app == nil {
		return nil
	}
	app.deliveryStateStoreOnce.Do(func() {
		app.deliveryStateStoreCached = scheduler.NewDeliveryStateStore(commands.ResolveDataDir())
	})
	return app.deliveryStateStoreCached
}

type tuiChannelSendFunc func(ctx context.Context, target scheduler.DeliveryTarget, text string) error

func (app *TUIApp) newScheduleChannelSender(channel string) (tuiChannelSendFunc, error) {
	channel = scheduler.DefaultDeliveryChannel(channel)
	switch channel {
	case scheduler.DeliveryChannelLansenger:
		gw, err := app.newLansengerGatewayForSend()
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
			return app.sendLansengerScheduleTarget(ctx, gw, target, text)
		}, nil
	case scheduler.DeliveryChannelWeixin:
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
			return app.deliverWeixinScheduledTarget(target, text)
		}, nil
	case scheduler.DeliveryChannelTelegram:
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
			return app.deliverTelegramScheduledTarget(ctx, target, text)
		}, nil
	case scheduler.DeliveryChannelQQ:
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
			return app.deliverQQScheduledTarget(ctx, target, text)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported channel %q", channel)
	}
}

// deliverWeixinScheduledTarget uses the TUI weixin gateway last-active private session.
func (app *TUIApp) deliverWeixinScheduledTarget(target scheduler.DeliveryTarget, text string) error {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return fmt.Errorf("weixin delivery only supports kind=user (owner private session)")
	}
	uid := strings.TrimSpace(target.UserID)
	if uid != "" && !scheduler.IsSelfPeerID(uid) {
		return fmt.Errorf("weixin: only owner private push is supported (use user_id=self); got %q", target.UserID)
	}
	if app.weixinGateway == nil {
		return fmt.Errorf("weixin gateway unavailable — enable WeChat bot and chat once first")
	}
	// Reuse same proactive path as desktop when available via gateway wrapper.
	if err := app.weixinGateway.SendProactiveText(text); err != nil {
		return err
	}
	return nil
}

func (app *TUIApp) newLansengerGatewayForSend() (*lansenger.Gateway, error) {
	cfg := app.appConfig
	if store := commands.NewFileConfigStore(commands.ResolveDataDir()); store != nil {
		if fresh, err := store.LoadConfig(); err == nil {
			cfg = fresh
		}
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

func (app *TUIApp) sendLansengerScheduleTarget(ctx context.Context, gw *lansenger.Gateway, target scheduler.DeliveryTarget, text string) error {
	if gw == nil {
		return fmt.Errorf("lansenger gateway is nil")
	}
	switch target.Kind {
	case scheduler.DeliveryKindGroup:
		if strings.TrimSpace(target.GroupID) == "" {
			return fmt.Errorf("lansenger group target missing group_id")
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
		return gw.SendText(ctx, msg)
	case scheduler.DeliveryKindUser:
		userID := strings.TrimSpace(target.UserID)
		if userID == "" {
			return fmt.Errorf("lansenger user target missing user_id")
		}
		store := app.deliveryStateStore()
		userID = store.ResolveSelfPeer(scheduler.DeliveryChannelLansenger, userID)
		if scheduler.IsSelfPeerID(userID) {
			return fmt.Errorf("lansenger: user_id=self 需要 staffId，或先成功私聊推送一次以记住对方")
		}
		if err := gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: userID,
			Text:     text,
			IsGroup:  false,
		}); err != nil {
			return err
		}
		// Private peers only — never store group ids into self memory.
		store.RememberPeer(scheduler.DeliveryChannelLansenger, userID)
		return nil
	default:
		return fmt.Errorf("unknown target kind %q", target.Kind)
	}
}

func (app *TUIApp) deliverTelegramScheduledTarget(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
	idStr := strings.TrimSpace(target.UserID)
	if idStr == "" {
		idStr = strings.TrimSpace(target.GroupID)
	}
	store := app.deliveryStateStore()
	idStr = store.ResolveSelfPeer(scheduler.DeliveryChannelTelegram, idStr)
	if idStr == "" {
		return fmt.Errorf("telegram: 需要 chat_id，或先成功推送过一次以记住 self")
	}
	chatID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: chat_id 必须是数字: %q", idStr)
	}
	gw, err := app.newTelegramGatewayForSend()
	if err != nil {
		return err
	}
	if err := gw.SendText(ctx, telegram.OutgoingText{ChatID: chatID, Text: text}); err != nil {
		return err
	}
	if scheduler.CanRememberAsSelfPeer(target) {
		store.RememberPeer(scheduler.DeliveryChannelTelegram, idStr)
	}
	return nil
}

func (app *TUIApp) deliverQQScheduledTarget(ctx context.Context, target scheduler.DeliveryTarget, text string) error {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return fmt.Errorf("qq delivery only supports kind=user")
	}
	openID := strings.TrimSpace(target.UserID)
	store := app.deliveryStateStore()
	openID = store.ResolveSelfPeer(scheduler.DeliveryChannelQQ, openID)
	if openID == "" {
		return fmt.Errorf("qq: 需要 openid，或先成功推送过一次以记住 self")
	}
	gw, err := app.newQQGatewayForSend()
	if err != nil {
		return err
	}
	if err := gw.SendText(ctx, qqbot.OutgoingText{OpenID: openID, Text: text}); err != nil {
		return err
	}
	if scheduler.CanRememberAsSelfPeer(target) {
		store.RememberPeer(scheduler.DeliveryChannelQQ, openID)
	}
	return nil
}

func (app *TUIApp) newTelegramGatewayForSend() (*telegram.Gateway, error) {
	cfg := app.appConfig
	if store := commands.NewFileConfigStore(commands.ResolveDataDir()); store != nil {
		if fresh, err := store.LoadConfig(); err == nil {
			cfg = fresh
		}
	}
	if !cfg.TelegramBotEnabled {
		// allow send with token even if toggle off
	}
	token := strings.TrimSpace(cfg.TelegramBotToken)
	if token == "" {
		return nil, fmt.Errorf("Telegram 未配置 Bot Token")
	}
	return telegram.NewGateway(telegram.Config{BotToken: token}, nil), nil
}

func (app *TUIApp) newQQGatewayForSend() (*qqbot.Gateway, error) {
	cfg := app.appConfig
	if store := commands.NewFileConfigStore(commands.ResolveDataDir()); store != nil {
		if fresh, err := store.LoadConfig(); err == nil {
			cfg = fresh
		}
	}
	appID := strings.TrimSpace(cfg.QQBotAppID)
	secret := strings.TrimSpace(cfg.QQBotAppSecret)
	if appID == "" || secret == "" {
		return nil, fmt.Errorf("QQ 未配置 AppID / AppSecret")
	}
	return qqbot.NewGateway(qqbot.Config{AppID: appID, AppSecret: secret}, nil), nil
}
