package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/telegram"
)

var (
	srvDeliveryStateMu    sync.Mutex
	srvDeliveryStateStore *scheduler.DeliveryStateStore
	srvDeliveryStateRoot  string

	// Optional live WeChat proactive sender (registered from HTTPServer / weixin runtime).
	srvWeixinProactiveMu     sync.RWMutex
	srvWeixinProactiveSender func(text string) (peer string, err error)
)

// setSrvWeixinProactiveSender wires live WeChat proactive push for scheduled delivery.
// Safe to call with nil to clear.
func setSrvWeixinProactiveSender(fn func(text string) (peer string, err error)) {
	srvWeixinProactiveMu.Lock()
	defer srvWeixinProactiveMu.Unlock()
	srvWeixinProactiveSender = fn
}

func getSrvWeixinProactiveSender() func(text string) (peer string, err error) {
	srvWeixinProactiveMu.RLock()
	defer srvWeixinProactiveMu.RUnlock()
	return srvWeixinProactiveSender
}

// deliverScheduledTaskResult pushes task output to configured channel targets.
// Supports lansenger (REST), telegram/qq (token APIs), weixin (live runtime session).
func deliverScheduledTaskResult(svc *agentservice.Service, principal agentservice.Principal, task *scheduler.ScheduledTask, resultText string, runErr error) error {
	if task == nil || task.Delivery == nil || !task.Delivery.Active() {
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

	dataRoot := ""
	if svc != nil {
		dataRoot = strings.TrimSpace(svc.DataRoot())
	}

	// Per-target audit while reusing the shared send path.
	send, err := newChannelSender(ctx, svc, principal, scheduler.DefaultDeliveryChannel(d.Channel))
	if err != nil {
		appendDeliveryAudit(dataRoot, DeliveryAuditEntry{
			TaskID:   task.ID,
			TaskName: task.Name,
			Channel:  d.Channel,
			OK:       false,
			Error:    "setup: " + err.Error(),
		})
		return err
	}

	store := srvDeliveryState(svc)
	body = scheduler.TruncateDeliveryBody(body)
	_, err = scheduler.FanOutDeliveryTargets(d.Targets, func(i int, target scheduler.DeliveryTarget) error {
		peer, sendErr := send(ctx, target, body)
		audit := DeliveryAuditEntry{
			TaskID:      task.ID,
			TaskName:    task.Name,
			Channel:     d.Channel,
			TargetIndex: i,
			TargetKind:  target.Kind,
			Peer:        peer,
			OK:          sendErr == nil,
		}
		if sendErr != nil {
			audit.Error = sendErr.Error()
			log.Printf("[MaClawSrv-Scheduler] delivery failed task=%s target=%d: %v", task.ID, i, sendErr)
			appendDeliveryAudit(dataRoot, audit)
			return sendErr
		}
		if peer == "" {
			if id := scheduler.PeerIDFromTarget(target); !scheduler.IsSelfPeerID(id) {
				audit.Peer = id
			}
		}
		appendDeliveryAudit(dataRoot, audit)
		if store != nil && peer != "" && scheduler.CanRememberAsSelfPeer(target) {
			store.RememberPeer(d.Channel, peer)
		}
		return nil
	})
	return err
}

func srvDeliveryState(svc *agentservice.Service) *scheduler.DeliveryStateStore {
	root := ""
	if svc != nil {
		root = strings.TrimSpace(svc.DataRoot())
	}
	if root == "" {
		return nil
	}
	srvDeliveryStateMu.Lock()
	defer srvDeliveryStateMu.Unlock()
	if srvDeliveryStateStore == nil || srvDeliveryStateRoot != root {
		srvDeliveryStateRoot = root
		srvDeliveryStateStore = scheduler.NewDeliveryStateStore(root)
	}
	return srvDeliveryStateStore
}

// channelSendFunc sends one target and returns the concrete peer id for memory.
type channelSendFunc func(ctx context.Context, target scheduler.DeliveryTarget, text string) (peer string, err error)

func newChannelSender(ctx context.Context, svc *agentservice.Service, principal agentservice.Principal, channel string) (channelSendFunc, error) {
	channel = scheduler.DefaultDeliveryChannel(channel)
	store := srvDeliveryState(svc)
	switch channel {
	case scheduler.DeliveryChannelLansenger:
		gw, err := newLansengerGatewayForPrincipal(svc, principal)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
			return sendLansengerTarget(ctx, gw, store, target, text)
		}, nil
	case scheduler.DeliveryChannelTelegram:
		gw, err := newTelegramGatewayForPrincipal(svc, principal)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
			return sendTelegramTarget(ctx, gw, store, target, text)
		}, nil
	case scheduler.DeliveryChannelQQ:
		gw, err := newQQGatewayForPrincipal(svc, principal)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
			return sendQQTarget(ctx, gw, store, target, text)
		}, nil
	case scheduler.DeliveryChannelWeixin:
		return func(ctx context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
			return sendWeixinTarget(ctx, store, target, text)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported channel %q", channel)
	}
}

func sendLansengerTarget(ctx context.Context, gw *lansenger.Gateway, store *scheduler.DeliveryStateStore, target scheduler.DeliveryTarget, text string) (string, error) {
	if gw == nil {
		return "", fmt.Errorf("lansenger gateway is nil")
	}
	switch target.Kind {
	case scheduler.DeliveryKindGroup:
		if strings.TrimSpace(target.GroupID) == "" {
			return "", fmt.Errorf("lansenger group target missing group_id")
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
		userID = store.ResolveSelfPeer(scheduler.DeliveryChannelLansenger, userID)
		if scheduler.IsSelfPeerID(userID) {
			return "", fmt.Errorf("lansenger: user_id=self 需要 staffId，或先成功私聊推送一次以记住对方")
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

func sendTelegramTarget(ctx context.Context, gw *telegram.Gateway, store *scheduler.DeliveryStateStore, target scheduler.DeliveryTarget, text string) (string, error) {
	if gw == nil {
		return "", fmt.Errorf("telegram gateway is nil")
	}
	idStr := strings.TrimSpace(target.UserID)
	if idStr == "" {
		idStr = strings.TrimSpace(target.GroupID)
	}
	idStr = store.ResolveSelfPeer(scheduler.DeliveryChannelTelegram, idStr)
	if idStr == "" || scheduler.IsSelfPeerID(idStr) {
		return "", fmt.Errorf("telegram: 需要 chat_id，或先成功推送过一次以记住 self")
	}
	chatID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("telegram: chat_id 必须是数字: %q", idStr)
	}
	if err := gw.SendText(ctx, telegram.OutgoingText{ChatID: chatID, Text: text}); err != nil {
		return "", err
	}
	return idStr, nil
}

func sendQQTarget(ctx context.Context, gw *qqbot.Gateway, store *scheduler.DeliveryStateStore, target scheduler.DeliveryTarget, text string) (string, error) {
	if gw == nil {
		return "", fmt.Errorf("qq gateway is nil")
	}
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return "", fmt.Errorf("qq delivery only supports kind=user")
	}
	openID := store.ResolveSelfPeer(scheduler.DeliveryChannelQQ, strings.TrimSpace(target.UserID))
	if openID == "" || scheduler.IsSelfPeerID(openID) {
		return "", fmt.Errorf("qq: 需要 openid，或先成功推送过一次以记住 self")
	}
	if err := gw.SendText(ctx, qqbot.OutgoingText{OpenID: openID, Text: text}); err != nil {
		return "", err
	}
	return openID, nil
}

func sendWeixinTarget(_ context.Context, store *scheduler.DeliveryStateStore, target scheduler.DeliveryTarget, text string) (string, error) {
	if target.Kind != "" && target.Kind != scheduler.DeliveryKindUser {
		return "", fmt.Errorf("weixin delivery only supports kind=user (owner private session)")
	}
	uid := strings.TrimSpace(target.UserID)
	if uid != "" && !scheduler.IsSelfPeerID(uid) {
		return "", fmt.Errorf("weixin: only owner private push is supported (use user_id=self); got %q", target.UserID)
	}
	// Prefer remembered peer only as a label; live send uses runtime session tokens.
	_ = store
	sender := getSrvWeixinProactiveSender()
	if sender == nil {
		return "", fmt.Errorf("weixin: 主动推送未接线（需 WeChat 运行时已连接并至少私聊过一次）")
	}
	peer, err := sender(text)
	if err != nil {
		return "", err
	}
	if peer == "" {
		// Session-based success without stable id — do not store "self".
		return "", nil
	}
	return peer, nil
}

func appConfigForPrincipal(svc *agentservice.Service, principal agentservice.Principal) (corelib.AppConfig, error) {
	if svc == nil {
		return corelib.AppConfig{}, fmt.Errorf("agentservice unavailable")
	}
	uc, err := svc.GetUserConfig(context.Background(), principal)
	if err == nil && uc != nil {
		return uc.AppConfig, nil
	}
	def, defErr := svc.GetDefaultClientConfig(context.Background())
	if defErr != nil || def == nil {
		if err != nil {
			return corelib.AppConfig{}, fmt.Errorf("load user config: %w", err)
		}
		return corelib.AppConfig{}, fmt.Errorf("load default client config: %w", defErr)
	}
	return def.AppConfig, nil
}

func newLansengerGatewayForPrincipal(svc *agentservice.Service, principal agentservice.Principal) (*lansenger.Gateway, error) {
	cfg, err := appConfigForPrincipal(svc, principal)
	if err != nil {
		return nil, err
	}
	return newLansengerGatewayFromAppConfig(cfg)
}

func newTelegramGatewayForPrincipal(svc *agentservice.Service, principal agentservice.Principal) (*telegram.Gateway, error) {
	cfg, err := appConfigForPrincipal(svc, principal)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.TelegramBotToken)
	if token == "" {
		return nil, fmt.Errorf("Telegram 未配置 Bot Token")
	}
	return telegram.NewGateway(telegram.Config{BotToken: token}, nil), nil
}

func newQQGatewayForPrincipal(svc *agentservice.Service, principal agentservice.Principal) (*qqbot.Gateway, error) {
	cfg, err := appConfigForPrincipal(svc, principal)
	if err != nil {
		return nil, err
	}
	appID := strings.TrimSpace(cfg.QQBotAppID)
	secret := strings.TrimSpace(cfg.QQBotAppSecret)
	if appID == "" || secret == "" {
		return nil, fmt.Errorf("QQ 未配置 AppID / AppSecret")
	}
	return qqbot.NewGateway(qqbot.Config{AppID: appID, AppSecret: secret}, nil), nil
}

func newLansengerGatewayFromAppConfig(cfg corelib.AppConfig) (*lansenger.Gateway, error) {
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
