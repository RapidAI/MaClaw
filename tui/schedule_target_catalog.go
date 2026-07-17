package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ensureScheduleTargetCatalogs registers IM delivery catalogs once per TUIApp.
func (app *TUIApp) ensureScheduleTargetCatalogs() {
	if app == nil {
		return
	}
	app.scheduleTargetCatalogsOnce.Do(func() {
		app.scheduleTargetListCache = scheduler.NewTargetListCache(scheduler.DefaultTargetListCacheTTL)
		reg := scheduler.NewTargetCatalogRegistry()
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelLansenger,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return app.listLansengerDeliveryTargets(ctx, query)
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelWeixin,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				refs := []scheduler.TargetRef{{
					Kind:    scheduler.DeliveryKindUser,
					ID:      "self",
					Name:    "微信私聊（当前会话）",
					Channel: scheduler.DeliveryChannelWeixin,
				}}
				return scheduler.FilterTargetRefs(refs, query), nil
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelTelegram,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return app.listPeerDeliveryTargets(scheduler.DeliveryChannelTelegram, "Telegram", query), nil
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelQQ,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return app.listPeerDeliveryTargets(scheduler.DeliveryChannelQQ, "QQ", query), nil
			},
		})
		app.scheduleTargetCatalogs = reg
	})
}

func (app *TUIApp) listPeerDeliveryTargets(channel, label, query string) []scheduler.TargetRef {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    label + " 最近会话 (self)",
		Channel: channel,
	}}
	if peer := app.deliveryStateStore().GetLastPeer(channel); peer != "" {
		refs = append(refs, scheduler.TargetRef{
			Kind:    scheduler.DeliveryKindUser,
			ID:      peer,
			Name:    label + " " + peer,
			Channel: channel,
		})
	}
	return scheduler.FilterTargetRefs(refs, query)
}

func (app *TUIApp) scheduleTargetCatalogRegistry() *scheduler.TargetCatalogRegistry {
	app.ensureScheduleTargetCatalogs()
	if app == nil {
		return nil
	}
	return app.scheduleTargetCatalogs
}

func (app *TUIApp) listLansengerDeliveryTargets(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
	if app == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	app.ensureScheduleTargetCatalogs()
	load := func() ([]scheduler.TargetRef, error) {
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
		gw := lansenger.NewGateway(lansenger.Config{
			AppID:            appID,
			AppSecret:        appSecret,
			ApiGatewayURL:    apiURL,
			WebSocketBaseURL: strings.TrimSpace(cfg.LansengerWebSocketGatewayURL()),
		}, nil)
		callCtx := ctx
		var cancel context.CancelFunc
		if callCtx == nil {
			callCtx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
		}
		list, err := gw.ListJoinedGroups(callCtx)
		if err != nil {
			return nil, fmt.Errorf("查询蓝信群列表失败: %w", err)
		}
		out := make([]scheduler.TargetRef, 0)
		if list != nil {
			out = make([]scheduler.TargetRef, 0, len(list.Groups))
			for _, g := range list.Groups {
				id := strings.TrimSpace(g.GroupID)
				if id == "" {
					continue
				}
				out = append(out, scheduler.TargetRef{
					Kind:    scheduler.DeliveryKindGroup,
					ID:      id,
					Name:    strings.TrimSpace(g.Name),
					Channel: scheduler.DeliveryChannelLansenger,
				})
			}
		}
		return out, nil
	}
	var refs []scheduler.TargetRef
	var err error
	if app.scheduleTargetListCache != nil {
		refs, err = app.scheduleTargetListCache.GetOrLoad(scheduler.DeliveryChannelLansenger, load)
	} else {
		refs, err = load()
	}
	if err != nil {
		return nil, err
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

func (app *TUIApp) resolveScheduleDelivery(d *scheduler.TaskDelivery) error {
	if d == nil || !d.Enabled {
		return nil
	}
	reg := app.scheduleTargetCatalogRegistry()
	if reg == nil {
		return fmt.Errorf("delivery target catalog unavailable")
	}
	return reg.ResolveDeliveryNames(context.Background(), d)
}

func (app *TUIApp) listScheduleDeliveryTargets(channel, query string) (string, error) {
	reg := app.scheduleTargetCatalogRegistry()
	if reg == nil {
		return "", fmt.Errorf("delivery target catalog unavailable")
	}
	ch := scheduler.DefaultDeliveryChannel(channel)
	refs, err := reg.ListTargets(context.Background(), ch, query)
	if err != nil {
		return "", err
	}
	return scheduler.FormatTargetList(ch, refs, query), nil
}
