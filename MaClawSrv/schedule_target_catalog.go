package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

var (
	srvScheduleCatalogOnce  sync.Once
	srvScheduleCatalogReg   *scheduler.TargetCatalogRegistry
	srvScheduleCatalogCache *scheduler.TargetListCache
	srvScheduleCatalogSvc   *agentservice.Service
	srvScheduleWeixinLister func() []scheduler.TargetRef
)

// setSrvScheduleCatalogWeixinLister wires live WeChat peers into list_targets (optional).
func setSrvScheduleCatalogWeixinLister(fn func() []scheduler.TargetRef) {
	srvScheduleWeixinLister = fn
}

func ensureSrvScheduleTargetCatalogs(svc *agentservice.Service) *scheduler.TargetCatalogRegistry {
	srvScheduleCatalogOnce.Do(func() {
		srvScheduleCatalogSvc = svc
		srvScheduleCatalogCache = scheduler.NewTargetListCache(scheduler.DefaultTargetListCacheTTL)
		reg := scheduler.NewTargetCatalogRegistry()
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelLansenger,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return listSrvLansengerDeliveryTargets(ctx, query)
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelWeixin,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return listSrvPeerDeliveryTargets(scheduler.DeliveryChannelWeixin, "微信", query), nil
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelTelegram,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return listSrvPeerDeliveryTargets(scheduler.DeliveryChannelTelegram, "Telegram", query), nil
			},
		})
		reg.Register(scheduler.TargetCatalogFunc{
			ChannelName: scheduler.DeliveryChannelQQ,
			List: func(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
				return listSrvPeerDeliveryTargets(scheduler.DeliveryChannelQQ, "QQ", query), nil
			},
		})
		srvScheduleCatalogReg = reg
	})
	return srvScheduleCatalogReg
}

func listSrvScheduleDeliveryTargets(svc *agentservice.Service, channel, query string) (string, error) {
	reg := ensureSrvScheduleTargetCatalogs(svc)
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

func listSrvPeerDeliveryTargets(channel, label, query string) []scheduler.TargetRef {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    label + " 最近会话 (self)",
		Channel: channel,
	}}
	store := srvDeliveryState(srvScheduleCatalogSvc)
	if peer := store.GetLastPeer(channel); peer != "" && !scheduler.IsSelfPeerID(peer) {
		refs = append(refs, scheduler.TargetRef{
			Kind:    scheduler.DeliveryKindUser,
			ID:      peer,
			Name:    label + " " + peer,
			Channel: channel,
		})
	}
	// Live weixin peers from connected runtimes (if wired).
	if channel == scheduler.DeliveryChannelWeixin && srvScheduleWeixinLister != nil {
		for _, extra := range srvScheduleWeixinLister() {
			id := strings.TrimSpace(extra.ID)
			if id == "" || scheduler.IsSelfPeerID(id) {
				continue
			}
			dup := false
			for _, r := range refs {
				if r.ID == id {
					dup = true
					break
				}
			}
			if !dup {
				refs = append(refs, extra)
			}
		}
	}
	return scheduler.FilterTargetRefs(refs, query)
}

func listSrvLansengerDeliveryTargets(ctx context.Context, query string) ([]scheduler.TargetRef, error) {
	load := func() ([]scheduler.TargetRef, error) {
		svc := srvScheduleCatalogSvc
		if svc == nil {
			return nil, fmt.Errorf("agentservice unavailable")
		}
		// Prefer default client config (scheduler principal often has no user config).
		cfg, err := appConfigForPrincipal(svc, agentservice.Principal{TenantID: "system", UserID: "scheduler"})
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
		} else if _, ok := callCtx.Deadline(); !ok {
			callCtx, cancel = context.WithTimeout(callCtx, 60*time.Second)
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
	if srvScheduleCatalogCache != nil {
		refs, err = srvScheduleCatalogCache.GetOrLoad(scheduler.DeliveryChannelLansenger, load)
	} else {
		refs, err = load()
	}
	if err != nil {
		return nil, err
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

// resolveSrvScheduleDelivery fills group_name → group_id via catalogs.
func resolveSrvScheduleDelivery(svc *agentservice.Service, d *scheduler.TaskDelivery) error {
	if d == nil || !d.Enabled {
		return nil
	}
	reg := ensureSrvScheduleTargetCatalogs(svc)
	if reg == nil {
		return fmt.Errorf("delivery target catalog unavailable")
	}
	return reg.ResolveDeliveryNames(context.Background(), d)
}
