package lansenger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"
)

// StaffBasicInfo is the subset of /v1/staffs/:id/fetch we use for display names.
// Official bot_group_message callbacks only carry `from` (staff openId); the
// person name must be resolved via this directory API (or group members).
// See openapi.lanxin.cn: 获取人员基本信息 / 回调事件 bot_group_message.
type StaffBasicInfo struct {
	StaffID   string
	Name      string
	OrgID     string
	OrgName   string
	AvatarURL string
	Status    int
}

type staffNameCacheEntry struct {
	name string
	at   time.Time
	neg  bool // true = recent lookup failed / empty; avoid hammering
}

const (
	staffNameCacheTTL    = 30 * time.Minute
	staffNameCacheNegTTL = 2 * time.Minute
	staffNameCacheMax    = 2048
	// Bound enrichment on the WS read path so a slow directory call does not
	// stall subsequent inbound messages for long.
	staffNameLookupTimeout = 1500 * time.Millisecond
)

// staffNameCache is a process-local positive/negative cache for staff display names.
type staffNameCache struct {
	mu sync.Mutex
	m  map[string]staffNameCacheEntry
}

func (c *staffNameCache) get(id string, now time.Time) (name string, hit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		return "", false
	}
	ent, found := c.m[id]
	if !found {
		return "", false
	}
	ttl := staffNameCacheTTL
	if ent.neg {
		ttl = staffNameCacheNegTTL
	}
	if now.Sub(ent.at) > ttl {
		delete(c.m, id)
		return "", false
	}
	if ent.neg {
		return "", true // cached miss (name empty)
	}
	return ent.name, true
}

func (c *staffNameCache) set(id, name string, neg bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]staffNameCacheEntry)
	}
	if len(c.m) >= staffNameCacheMax {
		// Drop an arbitrary entry to bound memory under large orgs.
		for k := range c.m {
			delete(c.m, k)
			break
		}
	}
	c.m[id] = staffNameCacheEntry{name: name, at: now, neg: neg}
}

// GetStaffBasicInfo fetches one staff member's basic profile.
// API: GET /v1/staffs/:staffId/fetch?app_token=...
func (g *Gateway) GetStaffBasicInfo(ctx context.Context, staffID string) (*StaffBasicInfo, error) {
	info, err := g.getStaffBasicInfoOnce(ctx, staffID)
	if err != nil && isLansengerTokenExpiredError(err) {
		g.tokens.clear()
		return g.getStaffBasicInfoOnce(ctx, staffID)
	}
	return info, err
}

func (g *Gateway) getStaffBasicInfoOnce(ctx context.Context, staffID string) (*StaffBasicInfo, error) {
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return nil, fmt.Errorf("lansenger: staffId is required")
	}
	token, err := g.getAppToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s?app_token=%s",
		g.apiURL("/v1/staffs/"+url.PathEscape(staffID)+"/fetch"),
		url.QueryEscape(token),
	)
	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			OrgID     string `json:"orgId"`
			Name      string `json:"name"`
			OrgName   string `json:"orgName"`
			AvatarURL string `json:"avatarUrl"`
			Status    int    `json:"status"`
		} `json:"data"`
	}
	if err := g.doGetJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	if result.ErrCode != 0 {
		return nil, &APIError{Code: result.ErrCode, Msg: result.ErrMsg}
	}
	return &StaffBasicInfo{
		StaffID:   staffID,
		Name:      strings.TrimSpace(result.Data.Name),
		OrgID:     strings.TrimSpace(result.Data.OrgID),
		OrgName:   strings.TrimSpace(result.Data.OrgName),
		AvatarURL: strings.TrimSpace(result.Data.AvatarURL),
		Status:    result.Data.Status,
	}, nil
}

// usableStaffDisplayName returns a quote-safe display name, or "" if unusable.
func usableStaffDisplayName(staffID, rawName string) string {
	staffID = strings.TrimSpace(staffID)
	clean := normalizeGroupReplySenderLabel(rawName)
	if clean == "" || (staffID != "" && clean == staffID) {
		return ""
	}
	return clean
}

// isTransientStaffLookupError reports errors that should NOT be negative-cached
// for staffNameCacheNegTTL (retry soon: timeout, network, token race).
// Stable business API errors (not-found / no-permission) are NOT transient.
func isTransientStaffLookupError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// Token expiry is recoverable after refresh — do not lock out names for 2m.
	if isLansengerTokenExpiredError(err) {
		return true
	}
	// Structured business denials are stable for the neg-cache window.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return false
	}
	// Transport / HTTP / decode errors: retry soon rather than hide names.
	return true
}

// canLookupStaff reports whether REST directory calls are configured. Unit tests
// often construct a Gateway with no ApiGatewayURL; skip enrichment there so
// processEvent does not burn staffNameLookupTimeout on every inbound fixture.
func (g *Gateway) canLookupStaff() bool {
	if g == nil {
		return false
	}
	return strings.TrimSpace(g.config.AppID) != "" &&
		strings.TrimSpace(g.config.AppSecret) != "" &&
		strings.TrimSpace(g.config.ApiGatewayURL) != ""
}

// ResolveStaffDisplayName returns a cached or freshly-fetched display name for
// staffID. Empty string means "unknown" (caller should keep using staffId).
// Concurrent lookups for the same id are singleflight-coalesced. Failures are
// negative-cached briefly (except transient errors); never panics.
func (g *Gateway) ResolveStaffDisplayName(ctx context.Context, staffID string) string {
	if g == nil || !g.canLookupStaff() {
		return ""
	}
	staffID = strings.TrimSpace(staffID)
	if staffID == "" {
		return ""
	}
	now := time.Now()
	if name, hit := g.staffNames.get(staffID, now); hit {
		return name
	}

	// Coalesce concurrent cold misses for the same staffId (busy groups).
	// Capture a bounded timeout once so waiters share the same fetch budget
	// regardless of which caller entered singleflight first.
	lookupTimeout := staffNameLookupTimeout
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			if remaining := time.Until(deadline); remaining > 0 && remaining < lookupTimeout {
				lookupTimeout = remaining
			}
		}
	}
	v, _, _ := g.staffSF.Do(staffID, func() (any, error) {
		now := time.Now()
		if name, hit := g.staffNames.get(staffID, now); hit {
			return name, nil
		}

		lookupCtx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()

		info, err := g.GetStaffBasicInfo(lookupCtx, staffID)
		if err != nil {
			transient := isTransientStaffLookupError(err)
			log.Printf("[lansenger] staff name lookup failed: staffId=%s err=%v transient=%v",
				staffID, err, transient)
			if !transient {
				g.staffNames.set(staffID, "", true, now)
			}
			return "", nil
		}
		clean := usableStaffDisplayName(staffID, info.Name)
		if clean == "" {
			log.Printf("[lansenger] staff name lookup unusable: staffId=%s rawName=%q", staffID, info.Name)
			g.staffNames.set(staffID, "", true, now)
			return "", nil
		}
		g.staffNames.set(staffID, clean, false, now)
		log.Printf("[lansenger] staff name resolved: staffId=%s name=%q", staffID, clean)
		return clean, nil
	})
	name, _ := v.(string)
	return name
}

// EnrichIncomingSenderName fills msg.SenderName from the staff directory when
// the inbound payload only has staff openId (official bot_*_message shape).
// Returns the (possibly updated) message and whether a new name was filled in.
// No-op when a usable display name is already present or the gateway is unconfigured.
func (g *Gateway) EnrichIncomingSenderName(ctx context.Context, msg IncomingMessage) (IncomingMessage, bool) {
	if g == nil || !g.canLookupStaff() {
		return msg, false
	}
	if GroupReplyDisplayName(msg) != "" {
		return msg, false
	}
	id := strings.TrimSpace(msg.FromUserID)
	if id == "" {
		return msg, false
	}
	name := g.ResolveStaffDisplayName(ctx, id)
	if name == "" {
		return msg, false
	}
	msg.SenderName = name
	return msg, true
}
