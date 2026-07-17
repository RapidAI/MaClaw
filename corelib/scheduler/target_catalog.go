package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// TargetRef is a channel-agnostic delivery destination for listing / resolution.
type TargetRef struct {
	// Kind is group or user (see DeliveryKind*).
	Kind string `json:"kind"`
	// ID is the platform identifier used when sending (group_id / user_id).
	ID string `json:"id"`
	// Name is a human-readable label when available.
	Name string `json:"name,omitempty"`
	// Channel echoes the catalog channel for multi-channel result sets.
	Channel string `json:"channel,omitempty"`
}

// TargetCatalog lists addressable destinations for one delivery channel.
// Implementations are registered per runtime (desktop / server); the tool layer
// stays channel-agnostic so new IM platforms do not need new agent tools.
type TargetCatalog interface {
	// Channel returns the stable channel key (e.g. "lansenger", "weixin").
	Channel() string
	// ListTargets returns destinations matching optional query (empty = all).
	// Query may match id or display name; catalogs may pre-filter or return all
	// and let MatchGroupQuery refine.
	ListTargets(ctx context.Context, query string) ([]TargetRef, error)
}

// TargetCatalogFunc adapts a function to TargetCatalog.
type TargetCatalogFunc struct {
	ChannelName string
	List        func(ctx context.Context, query string) ([]TargetRef, error)
}

func (f TargetCatalogFunc) Channel() string { return f.ChannelName }

func (f TargetCatalogFunc) ListTargets(ctx context.Context, query string) ([]TargetRef, error) {
	if f.List == nil {
		return nil, fmt.Errorf("scheduler: catalog %q has no list implementation", f.ChannelName)
	}
	return f.List(ctx, query)
}

// TargetCatalogRegistry maps channel → catalog for a process (GUI or Srv).
type TargetCatalogRegistry struct {
	byChannel map[string]TargetCatalog
}

// NewTargetCatalogRegistry creates an empty registry.
func NewTargetCatalogRegistry() *TargetCatalogRegistry {
	return &TargetCatalogRegistry{byChannel: make(map[string]TargetCatalog)}
}

// Register adds or replaces a catalog for its Channel().
func (r *TargetCatalogRegistry) Register(c TargetCatalog) {
	if r == nil || c == nil {
		return
	}
	// Canonical keys only (蓝信/wechat → lansenger/weixin) so Get aliases resolve.
	ch := DefaultDeliveryChannel(c.Channel())
	if ch == "" {
		return
	}
	if r.byChannel == nil {
		r.byChannel = make(map[string]TargetCatalog)
	}
	r.byChannel[ch] = c
}

// Get returns the catalog for channel (aliases like 蓝信/wechat accepted).
func (r *TargetCatalogRegistry) Get(channel string) (TargetCatalog, bool) {
	if r == nil {
		return nil, false
	}
	ch := DefaultDeliveryChannel(channel)
	c, ok := r.byChannel[ch]
	return c, ok
}

// Channels returns registered channel keys (sorted; lansenger first when present).
func (r *TargetCatalogRegistry) Channels() []string {
	if r == nil || len(r.byChannel) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.byChannel))
	for ch := range r.byChannel {
		out = append(out, ch)
	}
	sort.Strings(out)
	// Prefer default channel at the front for agent prompts.
	for i, ch := range out {
		if ch == DeliveryChannelLansenger && i > 0 {
			copy(out[1:i+1], out[0:i])
			out[0] = DeliveryChannelLansenger
			break
		}
	}
	return out
}

// ListTargets is a convenience that errors if the channel is unknown.
func (r *TargetCatalogRegistry) ListTargets(ctx context.Context, channel, query string) ([]TargetRef, error) {
	c, ok := r.Get(channel)
	if !ok {
		known := r.Channels()
		if len(known) == 0 {
			return nil, fmt.Errorf("scheduler: no delivery target catalogs registered")
		}
		return nil, fmt.Errorf("scheduler: unknown delivery channel %q (available: %s)", DefaultDeliveryChannel(channel), strings.Join(known, ", "))
	}
	return c.ListTargets(ctx, query)
}

// ResolveDeliveryNames fills missing group/user ids from catalog names for d.Channel.
func (r *TargetCatalogRegistry) ResolveDeliveryNames(ctx context.Context, d *TaskDelivery) error {
	if d == nil || !d.Enabled {
		return nil
	}
	d.Normalize()
	if !d.NeedsGroupNameResolution() {
		// Still may want display names for ids — optional, skip for now.
		return d.EnsureResolved()
	}
	refs, err := r.ListTargets(ctx, d.Channel, "")
	if err != nil {
		return err
	}
	groups := TargetRefsToGroupRefs(refs, DeliveryKindGroup)
	if err := ResolveDeliveryGroupNames(d, groups); err != nil {
		return err
	}
	return d.EnsureResolved()
}

// TargetRefsToGroupRefs filters catalog entries for group matching.
func TargetRefsToGroupRefs(refs []TargetRef, kind string) []GroupRef {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		kind = DeliveryKindGroup
	}
	out := make([]GroupRef, 0, len(refs))
	for _, r := range refs {
		k := strings.TrimSpace(strings.ToLower(r.Kind))
		if k == "" {
			k = DeliveryKindGroup
		}
		if k != kind {
			continue
		}
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		out = append(out, GroupRef{ID: id, Name: strings.TrimSpace(r.Name)})
	}
	return out
}

// FilterTargetRefs applies optional query (id/name substring) client-side.
func FilterTargetRefs(refs []TargetRef, query string) []TargetRef {
	q := normalizeGroupQuery(query)
	if q == "" {
		return refs
	}
	out := make([]TargetRef, 0, len(refs))
	for _, r := range refs {
		id := normalizeGroupQuery(r.ID)
		name := normalizeGroupQuery(r.Name)
		if id == q || name == q || strings.Contains(name, q) || strings.Contains(id, q) {
			out = append(out, r)
		}
	}
	return out
}

// FormatTargetList builds a compact agent-readable listing.
func FormatTargetList(channel string, refs []TargetRef, query string) string {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = DeliveryChannelLansenger
	}
	refs = FilterTargetRefs(refs, query)
	if len(refs) == 0 {
		if strings.TrimSpace(query) != "" {
			return fmt.Sprintf("通道 %s：没有匹配 %q 的投递目标。", channel, query)
		}
		return fmt.Sprintf("通道 %s：暂无可用投递目标（请确认机器人已入群 / 通道已配置）。", channel)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("通道 %s 可用投递目标（%d）：\n", channel, len(refs)))
	limit := len(refs)
	if limit > 50 {
		limit = 50
	}
	for i := 0; i < limit; i++ {
		r := refs[i]
		kind := r.Kind
		if kind == "" {
			kind = DeliveryKindGroup
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = "(无名称)"
		}
		// Field names match delivery JSON so the agent can copy them.
		switch kind {
		case DeliveryKindUser:
			b.WriteString(fmt.Sprintf("  - kind=user  name=%s  user_id=%s\n", name, r.ID))
		default:
			b.WriteString(fmt.Sprintf("  - kind=group  name=%s  group_id=%s\n", name, r.ID))
		}
	}
	if len(refs) > limit {
		b.WriteString(fmt.Sprintf("  …另有 %d 个未列出，请缩小 query\n", len(refs)-limit))
	}
	b.WriteString("创建定时任务时 delivery.targets 使用 group_id/user_id；也可只填 group_name，系统会自动解析。")
	return strings.TrimSpace(b.String())
}
