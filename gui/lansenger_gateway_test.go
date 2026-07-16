package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
)

func TestLansengerStatusNeedsWatchdogRestart(t *testing.T) {
	restartStatuses := []gatewayConnectionStatus{
		gatewayConnectionStatusConnecting,
		gatewayConnectionStatusReconnecting,
		gatewayConnectionStatusUnknown,
	}
	for _, status := range restartStatuses {
		if !lansengerStatusNeedsWatchdogRestart(status) {
			t.Fatalf("status %q should trigger watchdog restart", status)
		}
	}

	steadyStatuses := []gatewayConnectionStatus{
		gatewayConnectionStatusConnected,
		gatewayConnectionStatusDisconnected,
		gatewayConnectionStatusError,
	}
	for _, status := range steadyStatuses {
		if lansengerStatusNeedsWatchdogRestart(status) {
			t.Fatalf("status %q should not trigger stale-status watchdog restart", status)
		}
	}
}

func TestLansengerGatewayManagerIgnoresStaleStatus(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	gateway := &lansenger.Gateway{}
	m.gateway = gateway
	m.status = gatewayConnectionStatusConnected
	m.statusSince = time.Now()
	before := m.statusSince

	m.onGatewayStatusChange(nil, "error")

	if m.Status() != gatewayConnectionStatusConnected.String() {
		t.Fatalf("status = %q, want connected", m.Status())
	}
	if !m.statusSince.Equal(before) {
		t.Fatal("stale status update changed statusSince")
	}
}

func TestLansengerRestartCooldown(t *testing.T) {
	now := time.Now()
	if lansengerRestartInCooldown(time.Time{}, now) {
		t.Fatal("zero last restart should not be in cooldown")
	}
	if !lansengerRestartInCooldown(now.Add(-30*time.Second), now) {
		t.Fatal("restart should be in cooldown")
	}
	if lansengerRestartInCooldown(now.Add(-2*time.Minute), now) {
		t.Fatal("restart should not be in cooldown")
	}
}

func TestLansengerReplyRoutePreservesGroupRouting(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-abc", "今天天气？", "mid-1")
	m.rememberReplyRoute("user-1", false, "", "", "")
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group reply route was not retained")
	}
	if m.isGroupReplyTarget("user-1") {
		t.Fatal("private reply route was treated as group")
	}
	if m.isGroupReplyTarget("unknown") {
		t.Fatal("unknown reply route must default to private")
	}
	sender, question, msgID, ok := m.groupReplyQuote("group-1")
	if !ok || sender != "staff-abc" || question != "今天天气？" || msgID != "mid-1" {
		t.Fatalf("group quote cache = (%q, %q, %q, %v)", sender, question, msgID, ok)
	}
	if _, _, _, ok := m.groupReplyQuote("user-1"); ok {
		t.Fatal("private route must not expose a group quote")
	}
}

func TestLansengerReplyRouteHasBoundedCache(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	for i := 0; i < maxLansengerReplyRoutes+1; i++ {
		m.rememberReplyRoute(fmt.Sprintf("target-%d", i), i%2 == 0, "", "", "")
	}
	if got := len(m.replyRoutes); got != maxLansengerReplyRoutes {
		t.Fatalf("reply route cache size = %d, want %d", got, maxLansengerReplyRoutes)
	}
}

func TestLansengerReplyRoutesCanBeClearedOnLifecycleReset(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "A", "q", "m1")
	clear(m.replyRoutes)
	if m.isGroupReplyTarget("group-1") {
		t.Fatal("cleared reply route was retained")
	}
}

func TestLansengerGroupReplyTextQuotesQuestion(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	msg := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "staff-abc",
		SenderName: "张三",
		Text:       "帮我查天气",
	}
	got := m.groupReplyText(msg, "今天晴。")
	want := lansenger.FormatGroupReplyWithQuote("staff-abc", "帮我查天气", "今天晴。")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "staff-abc问：") {
		t.Fatalf("expected staff id prefix, got %q", got)
	}
	private := lansenger.IncomingMessage{ChatType: "p2p", FromUserID: "staff-abc", Text: "帮我查天气"}
	if got := m.groupReplyText(private, "今天晴。"); got != "今天晴。" {
		t.Fatalf("private reply must stay plain, got %q", got)
	}
}

func TestLansengerGroupNeverPublishesIMDetail(t *testing.T) {
	for _, chatType := range []string{"group", "GROUP", " group "} {
		if shouldSendLansengerIMDetail(chatType) {
			t.Fatalf("group chat type %q must suppress IM detail", chatType)
		}
	}
	if !shouldSendLansengerIMDetail("private") {
		t.Fatal("private chat must retain IM detail behavior")
	}
}

func TestLansengerGroupRequiresStructuredBotMention(t *testing.T) {
	base := lansenger.IncomingMessage{ChatType: "group", GroupID: "group-1", Text: "hello"}
	if lansengerGroupMessageMentionsBot(base, "org-bot-1") {
		t.Fatal("group message without a bot mention must not trigger")
	}
	base.MentionedBots = []lansenger.MentionedBot{{ID: "other-bot"}}
	if lansengerGroupMessageMentionsBot(base, "org-bot-1") {
		t.Fatal("mentioning another bot must not trigger")
	}
	base.MentionedBots = nil
	base.IsAtMe = true
	if !lansengerGroupMessageMentionsBot(base, "unrelated-app-id") {
		t.Fatal("Lansenger's explicit isAtMe signal must trigger regardless of ID format")
	}
	base.IsAtMe = false
	base.MentionedBots = []lansenger.MentionedBot{{ID: "1"}}
	if lansengerGroupMessageMentionsBot(base, "org-bot-1") {
		t.Fatal("a trailing App ID fragment must not be accepted as a bot ID")
	}
	base.MentionedBots = []lansenger.MentionedBot{{ID: "bot-1"}}
	if !lansengerGroupMessageMentionsBot(base, "org-bot-1") {
		t.Fatal("the bot component of a composite App ID must trigger")
	}
	base.MentionedBots = []lansenger.MentionedBot{{ID: "ORG-BOT-1"}}
	if !lansengerGroupMessageMentionsBot(base, "org-bot-1") {
		t.Fatal("the full App ID must match case-insensitively")
	}
}

func TestIsLansengerGroupMessage(t *testing.T) {
	if !isLansengerGroupMessage(lansenger.IncomingMessage{ChatType: " GROUP "}) {
		t.Fatal("group chat type must be recognized case-insensitively")
	}
	if isLansengerGroupMessage(lansenger.IncomingMessage{ChatType: "p2p"}) {
		t.Fatal("p2p must not be treated as a group")
	}
}

func TestAgentTextWithGroupContextInjectsMetadataWithoutMutatingQuoteSource(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	msg := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g-42",
		GroupName:  "我的机器人测试",
		FromUserID: "staff-abc",
		SenderName: "王占一",
		IsAtMe:     true,
		Text:       "看下群里还有哪些机器人？",
		MentionedBots: []lansenger.MentionedBot{
			{ID: "self", Name: "M-Wiggins"},
		},
	}
	// No gateway → GetGroupInfo skipped; message-level context still injects.
	got := m.agentTextWithGroupContext(msg, msg.Text)
	if !strings.Contains(got, "[群聊上下文]") {
		t.Fatalf("expected group context prefix, got %q", got)
	}
	if !strings.Contains(got, "我的机器人测试") || !strings.Contains(got, "王占一") {
		t.Fatalf("expected group/sender names, got %q", got)
	}
	if !strings.Contains(got, "完整成员/机器人名册: 不可用") {
		t.Fatalf("expected no-roster marker, got %q", got)
	}
	if !strings.Contains(got, "用户消息:\n看下群里还有哪些机器人？") {
		t.Fatalf("expected original user text section, got %q", got)
	}
	// Quote path must still use pristine msg.Text.
	quoted := m.groupReplyText(msg, "目前无法获取完整成员列表。")
	if !strings.HasPrefix(quoted, "staff-abc问：看下群里还有哪些机器人？") {
		t.Fatalf("group quote must use original question, got %q", quoted)
	}
	if strings.Contains(quoted, "[群聊上下文]") {
		t.Fatalf("group quote must not include agent context prefix, got %q", quoted)
	}
}

func TestAgentTextWithGroupContextPrivateUnchanged(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	msg := lansenger.IncomingMessage{ChatType: "p2p", FromUserID: "u1", Text: "hello"}
	if got := m.agentTextWithGroupContext(msg, msg.Text); got != "hello" {
		t.Fatalf("private text changed: %q", got)
	}
}

func TestLookupGroupInfoUsesCache(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	info := &lansenger.GroupInfo{GroupID: "g-1", Name: "缓存群", TotalMembers: 9}
	m.groupInfoCache["g-1"] = lansengerGroupInfoCacheEntry{info: info, at: time.Now()}
	got := m.lookupGroupInfo("g-1")
	if got == nil || got.Name != "缓存群" || got.TotalMembers != 9 {
		t.Fatalf("cache hit failed: %#v", got)
	}
	// Negative cache should return nil without gateway.
	m.groupInfoCache["g-2"] = lansengerGroupInfoCacheEntry{err: true, at: time.Now()}
	if m.lookupGroupInfo("g-2") != nil {
		t.Fatal("negative cache should return nil")
	}
}

func TestListLansengerGroupsRequiresCredentials(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if _, err := app.ListLansengerGroups(); err == nil || !strings.Contains(err.Error(), "App ID") {
		t.Fatalf("want credentials error, got %v", err)
	}
}

func TestLookupGroupInfoFetchesAndCaches(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	fetchCount := 0
	mux.HandleFunc("/v2/groups/g-live/info/fetch", func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"name":         "直播群",
				"description":  "内部测试",
				"totalMembers": 16,
				"maxMembers":   500,
				"owner":        map[string]any{"staffId": "o1", "name": "群主"},
				"state":        0,
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gw := lansenger.NewGateway(lansenger.Config{
		AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL,
	}, nil)
	m := newLansengerGatewayManager(nil)
	m.gateway = gw

	info := m.lookupGroupInfo("g-live")
	if info == nil || info.Name != "直播群" || info.TotalMembers != 16 {
		t.Fatalf("first fetch = %#v", info)
	}
	// Second lookup must hit cache (no extra HTTP).
	info2 := m.lookupGroupInfo("g-live")
	if info2 == nil || info2.Name != "直播群" {
		t.Fatalf("cached fetch = %#v", info2)
	}
	if fetchCount != 1 {
		t.Fatalf("GetGroupInfo calls = %d, want 1 (cache)", fetchCount)
	}

	agentText := m.agentTextWithGroupContext(lansenger.IncomingMessage{
		ChatType:  "group",
		GroupID:   "g-live",
		GroupName: "直播群",
		Text:      "群里有多少人？",
	}, "群里有多少人？")
	if !strings.Contains(agentText, "成员数: 16 / 500") {
		t.Fatalf("agent text missing member count: %s", agentText)
	}
	if !strings.Contains(agentText, "群描述: 内部测试") {
		t.Fatalf("agent text missing description: %s", agentText)
	}
}

func TestListLansengerGroupsSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": 1,
				"groupIds":      []string{"group-42"},
			},
		})
	})
	mux.HandleFunc("/v2/groups/group-42/info/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"name":         "TigerClaw 测试群",
				"totalMembers": 7,
				"owner":        map[string]any{"staffId": "u1", "name": "Bob"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.LansengerEnabled = true
		cfg.LansengerAppID = "app-id"
		cfg.LansengerAppSecret = "secret"
		cfg.LansengerGatewayURL = srv.URL
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}

	result, err := app.ListLansengerGroups()
	if err != nil {
		t.Fatalf("ListLansengerGroups: %v", err)
	}
	if result.Total != 1 || len(result.Groups) != 1 {
		t.Fatalf("result = %#v", result)
	}
	g := result.Groups[0]
	if g.GroupID != "group-42" || g.Name != "TigerClaw 测试群" || g.OwnerName != "Bob" || g.TotalMembers != 7 {
		t.Fatalf("group = %#v", g)
	}
	if g.Ignored {
		t.Fatal("group should not be ignored by default")
	}
}

func TestSetLansengerGroupIgnoredAndListMarksIgnored(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": 1,
				"groupIds":      []string{"group-42"},
			},
		})
	})
	mux.HandleFunc("/v2/groups/group-42/info/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"name": "G", "totalMembers": 1},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.LansengerAppID = "app"
		cfg.LansengerAppSecret = "sec"
		cfg.LansengerGatewayURL = srv.URL
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if err := app.SetLansengerGroupIgnored("group-42", true); err != nil {
		t.Fatalf("SetLansengerGroupIgnored: %v", err)
	}
	ids := app.GetLansengerIgnoredGroups()
	if len(ids) != 1 || ids[0] != "group-42" {
		t.Fatalf("ignored = %#v", ids)
	}
	result, err := app.ListLansengerGroups()
	if err != nil {
		t.Fatalf("ListLansengerGroups: %v", err)
	}
	if len(result.Groups) != 1 || !result.Groups[0].Ignored {
		t.Fatalf("expected ignored group row, got %#v", result.Groups)
	}
	// Orphan ignore entry still surfaces when not returned by API.
	if err := app.SetLansengerGroupIgnored("ghost-group", true); err != nil {
		t.Fatalf("ignore ghost: %v", err)
	}
	result, err = app.ListLansengerGroups()
	if err != nil {
		t.Fatalf("ListLansengerGroups after ghost: %v", err)
	}
	foundGhost := false
	for _, g := range result.Groups {
		if g.GroupID == "ghost-group" && g.Ignored && g.Orphan {
			foundGhost = true
		}
	}
	if !foundGhost {
		t.Fatalf("ghost ignored group missing: %#v", result.Groups)
	}
	// Platform-returned groups must not be marked orphan.
	for _, g := range result.Groups {
		if g.GroupID == "group-42" && g.Orphan {
			t.Fatalf("platform group marked orphan: %#v", g)
		}
	}
}

func TestLansengerIncomingIgnoresConfiguredGroup(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.LansengerIgnoredGroupIDs = []string{"group-ignore"}
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	m := newLansengerGatewayManager(app)
	// Should return immediately without panicking even when gateway is nil.
	m.onIncomingMessage(lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "group-ignore",
		FromUserID: "u1",
		Text:       "@Bot hello",
		IsAtMe:     true,
	})
}

func TestListLansengerGroupsPermissionDeniedMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 10005, "errMsg": "API服务无权限"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.LansengerAppID = "app"
		cfg.LansengerAppSecret = "sec"
		cfg.LansengerGatewayURL = srv.URL
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	_, err := app.ListLansengerGroups()
	if err == nil || !strings.Contains(err.Error(), "10005") {
		t.Fatalf("want 10005 message, got %v", err)
	}
}
