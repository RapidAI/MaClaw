package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// ensureScheduleTargetCatalogs registers built-in IM delivery target catalogs once per App.
// New channels add another Register call here — agent tools stay channel-agnostic
// (manage_schedule action=list_targets + delivery.channel).
func (a *App) ensureScheduleTargetCatalogs() {
	if a == nil {
		return
	}
	a.scheduleTargetCatalogsOnce.Do(func() {
		a.scheduleTargetListCache = scheduler.NewTargetListCache(scheduler.DefaultTargetListCacheTTL)
		reg := scheduler.NewTargetCatalogRegistry()
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelLansenger,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return a.listLansengerDeliveryTargets(ctx, query)
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelWeixin,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return a.listWeixinDeliveryTargets(ctx, query)
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelTelegram,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return a.listTelegramDeliveryTargets(ctx, query)
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelQQ,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return a.listQQDeliveryTargets(ctx, query)
			},
		})
		a.scheduleTargetCatalogs = reg
	})
}

func (a *App) scheduleTargetCatalogRegistry() *scheduler.TargetCatalogRegistry {
	a.ensureScheduleTargetCatalogs()
	if a == nil {
		return nil
	}
	return a.scheduleTargetCatalogs
}

func (a *App) listLansengerDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	if a == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	a.ensureScheduleTargetCatalogs()
	load := func() ([]scheduler.TargetRef, error) {
		res, err := a.ListLansengerGroups()
		if err != nil {
			return nil, err
		}
		out := make([]scheduler.TargetRef, 0, len(res.Groups))
		for _, g := range res.Groups {
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
		return out, nil
	}
	var refs []scheduler.TargetRef
	var err error
	if a.scheduleTargetListCache != nil {
		refs, err = a.scheduleTargetListCache.GetOrLoad(scheduler.DeliveryChannelLansenger, load)
	} else {
		refs, err = load()
	}
	if err != nil {
		return nil, err
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

// listWeixinDeliveryTargets exposes the owner private session as the only
// addressable weixin target (platform requires an active contextToken).
func (a *App) listWeixinDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	if a == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	// Always advertise the owner private pathway; send path uses last active session.
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    "微信私聊（当前会话）",
		Channel: scheduler.DeliveryChannelWeixin,
	}}
	return scheduler.FilterTargetRefs(refs, query), nil
}

func (a *App) listTelegramDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    "Telegram 最近会话 (self)",
		Channel: scheduler.DeliveryChannelTelegram,
	}}
	// Surface last chat id when known so agents can pin it.
	if a != nil && a.telegramGateway != nil {
		a.telegramGateway.mu.Lock()
		id := a.telegramGateway.lastChatID
		a.telegramGateway.mu.Unlock()
		if id != 0 {
			refs = append(refs, scheduler.TargetRef{
				Kind:    scheduler.DeliveryKindUser,
				ID:      fmt.Sprintf("%d", id),
				Name:    fmt.Sprintf("Telegram chat %d", id),
				Channel: scheduler.DeliveryChannelTelegram,
			})
		}
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

func (a *App) listQQDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    "QQ 最近私聊 (self)",
		Channel: scheduler.DeliveryChannelQQ,
	}}
	if a != nil && a.qqBotGateway != nil {
		a.qqBotGateway.mu.Lock()
		id := strings.TrimSpace(a.qqBotGateway.lastOpenID)
		a.qqBotGateway.mu.Unlock()
		if id != "" {
			refs = append(refs, scheduler.TargetRef{
				Kind:    scheduler.DeliveryKindUser,
				ID:      id,
				Name:    "QQ openid " + id,
				Channel: scheduler.DeliveryChannelQQ,
			})
		}
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

// resolveScheduleDelivery fills platform ids for name-only delivery targets via the channel catalog.
func (a *App) resolveScheduleDelivery(d *scheduler.TaskDelivery) error {
	if d == nil || !d.Enabled {
		return nil
	}
	reg := a.scheduleTargetCatalogRegistry()
	if reg == nil {
		return fmt.Errorf("delivery target catalog unavailable")
	}
	return reg.ResolveDeliveryNames(context.Background(), d)
}

// listScheduleDeliveryTargets is the generic tool-facing list for any registered channel.
func (a *App) listScheduleDeliveryTargets(channel, query string) (string, error) {
	reg := a.scheduleTargetCatalogRegistry()
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

// invalidateScheduleTargetListCache drops cached group directories (e.g. after bot rejoins).
func (a *App) invalidateScheduleTargetListCache(channel string) {
	if a == nil || a.scheduleTargetListCache == nil {
		return
	}
	a.scheduleTargetListCache.Invalidate(channel)
}
