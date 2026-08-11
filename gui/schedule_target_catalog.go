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
	return a.listLansengerDeliveryTargetsForBot(query, "")
}

func lansengerDeliveryTargetCacheKey(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return scheduler.DeliveryChannelLansenger
	}
	return scheduler.DeliveryChannelLansenger + ":" + profileID
}

// listLansengerDeliveryTargetsForBot exposes only groups visible to the
// selected bot. An empty profile retains the legacy/default catalog.
func (a *App) listLansengerDeliveryTargetsForBot(query, profileID string) ([]scheduler.TargetRef, error) {
	if a == nil {
		return nil, fmt.Errorf("app unavailable")
	}
	a.ensureScheduleTargetCatalogs()
	profileID = strings.TrimSpace(profileID)
	load := func() ([]scheduler.TargetRef, error) {
		var (
			res *LansengerGroupListResult
			err error
		)
		if profileID == "" {
			res, err = a.ListLansengerGroups()
		} else {
			res, err = a.ListLansengerGroupsForBot(profileID)
		}
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
	cacheKey := lansengerDeliveryTargetCacheKey(profileID)
	if a.scheduleTargetListCache != nil {
		refs, err = a.scheduleTargetListCache.GetOrLoad(cacheKey, load)
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
	return a.resolveScheduleDeliveryForBot(d, "")
}

func (a *App) resolveScheduleDeliveryForBot(d *scheduler.TaskDelivery, runtimeProfileID string) error {
	if d == nil || !d.Enabled {
		return nil
	}
	runtimeProfileID = strings.TrimSpace(runtimeProfileID)
	if d.Channel == scheduler.DeliveryChannelLansenger {
		if runtimeProfileID != "" {
			// The runtime profile is authoritative even if a stale delivery object
			// happened to carry another value.
			d.BotProfileID = runtimeProfileID
		}
		if d.NeedsGroupNameResolution() {
			refs, err := a.listLansengerDeliveryTargetsForBot("", d.BotProfileID)
			if err != nil {
				return err
			}
			if err := scheduler.ResolveDeliveryGroupNames(d, scheduler.TargetRefsToGroupRefs(refs, scheduler.DeliveryKindGroup)); err != nil {
				return err
			}
		}
		return d.EnsureResolved()
	}
	reg := a.scheduleTargetCatalogRegistry()
	if reg == nil {
		return fmt.Errorf("delivery target catalog unavailable")
	}
	return reg.ResolveDeliveryNames(context.Background(), d)
}

// listScheduleDeliveryTargets is the generic tool-facing list for any registered channel.
func (a *App) listScheduleDeliveryTargets(channel, query string) (string, error) {
	return a.listScheduleDeliveryTargetsForBot(channel, query, "")
}

func (a *App) listScheduleDeliveryTargetsForBot(channel, query, profileID string) (string, error) {
	ch := scheduler.DefaultDeliveryChannel(channel)
	if ch == scheduler.DeliveryChannelLansenger && strings.TrimSpace(profileID) != "" {
		refs, err := a.listLansengerDeliveryTargetsForBot(query, profileID)
		if err != nil {
			return "", err
		}
		return scheduler.FormatTargetList(ch, refs, query), nil
	}
	reg := a.scheduleTargetCatalogRegistry()
	if reg == nil {
		return "", fmt.Errorf("delivery target catalog unavailable")
	}
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
	channel = scheduler.DefaultDeliveryChannel(channel)
	if channel == scheduler.DeliveryChannelLansenger {
		a.scheduleTargetListCache.InvalidatePrefix(channel + ":")
	}
	a.scheduleTargetListCache.Invalidate(channel)
}
