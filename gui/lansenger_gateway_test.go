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
	m.rememberReplyRoute("group-1", true, "staff-abc", "张三", "今天天气？", "mid-1", "")
	m.rememberReplyRoute("user-1", false, "", "", "", "", "")
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group reply route was not retained")
	}
	if m.isGroupReplyTarget("user-1") {
		t.Fatal("private reply route was treated as group")
	}
	if m.isGroupReplyTarget("unknown") {
		t.Fatal("unknown reply route must default to private")
	}
	d, ok := m.groupReplyQuote("group-1")
	if !ok || d.senderID != "staff-abc" || d.senderName != "张三" || d.question != "今天天气？" || d.messageID != "mid-1" {
		t.Fatalf("group quote cache = %#v ok=%v", d, ok)
	}
	if _, ok := m.groupReplyQuote("user-1"); ok {
		t.Fatal("private route must not expose a group quote")
	}
}

func TestLansengerReplyRouteHasBoundedCache(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	for i := 0; i < maxLansengerReplyRoutes+1; i++ {
		m.rememberReplyRoute(fmt.Sprintf("target-%d", i), i%2 == 0, "", "", "", "", "")
	}
	if got := len(m.replyRoutes); got != maxLansengerReplyRoutes {
		t.Fatalf("reply route cache size = %d, want %d", got, maxLansengerReplyRoutes)
	}
}

func TestLansengerReplyRoutesCanBeClearedOnLifecycleReset(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "A", "", "q", "m1", "")
	clear(m.replyRoutes)
	if m.isGroupReplyTarget("group-1") {
		t.Fatal("cleared reply route was retained")
	}
}

func TestTakeReplyDecorationsOnlyOnce(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-1", "李四", "q?", "mid-9", "")
	// Empty preferred must never decorate (queue ack / reject would steal slots).
	if _, ok := m.takeReplyDecorations("group-1", ""); ok {
		t.Fatal("empty preferred must not take")
	}
	d, ok := m.takeReplyDecorations("group-1", "mid-9")
	if !ok || d.senderID != "staff-1" || d.senderName != "李四" || d.question != "q?" || d.messageID != "mid-9" {
		t.Fatalf("first take = %#v ok=%v", d, ok)
	}
	if _, ok := m.takeReplyDecorations("group-1", "mid-9"); ok {
		t.Fatal("second take must fail (slot consumed)")
	}
	// Route still usable for isGroup / quote metadata without re-decorating.
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group route must remain after take")
	}
	// Private routes also decorate once (DM auto-@).
	m.rememberReplyRoute("user-9", false, "user-9", "", "", "dm-1", "")
	if d, ok := m.takeReplyDecorations("user-9", "dm-1"); !ok || d.messageID != "dm-1" {
		t.Fatalf("dm first take %#v ok=%v", d, ok)
	}
	if _, ok := m.takeReplyDecorations("user-9", "dm-1"); ok {
		t.Fatal("dm second take must fail")
	}
}

func TestTakeReplyDecorationsRequiresSourceMessageID(t *testing.T) {
	// Concurrent pending slots without preferred id must not FIFO-steal.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-a", "", "问题A", "mid-a", "")
	m.rememberReplyRoute("group-1", true, "staff-b", "", "问题B", "mid-b", "")
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("expected group route")
	}
	if _, ok := m.takeReplyDecorations("group-1", ""); ok {
		t.Fatal("empty preferred must not FIFO-steal under concurrent pending")
	}
	d1, ok := m.takeReplyDecorations("group-1", "mid-a")
	if !ok || d1.senderID != "staff-a" || d1.question != "问题A" || d1.messageID != "mid-a" {
		t.Fatalf("A by id = %#v ok=%v", d1, ok)
	}
	d2, ok := m.takeReplyDecorations("group-1", "mid-b")
	if !ok || d2.senderID != "staff-b" || d2.question != "问题B" || d2.messageID != "mid-b" {
		t.Fatalf("B by id = %#v ok=%v", d2, ok)
	}
	// isGroup must survive drained pending so late hub chunks still route as group.
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group route must survive drained pending")
	}
}

func TestForgetReplyDecorOnHubFallback(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-a", "", "问", "mid-a", "mc-1")
	m.forgetReplyDecor("group-1", "mc-1")
	if _, ok := m.takeReplyDecorations("group-1", "mc-1"); ok {
		t.Fatal("forgotten corr must not decorate")
	}
	// isGroup retained for routing.
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group route should remain after forget")
	}
}

func TestPruneLansengerPendingDecorsTTL(t *testing.T) {
	now := time.Now()
	pending := []lansengerReplyDecor{
		{correlationID: "old", at: now.Add(-lansengerPendingDecorTTL - time.Minute)},
		{correlationID: "fresh", at: now.Add(-time.Minute)},
		{correlationID: "zero-at"}, // zero at kept (tests / legacy)
	}
	got := pruneLansengerPendingDecors(pending, now)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 (fresh+zero)", len(got))
	}
	if got[0].correlationID != "fresh" || got[1].correlationID != "zero-at" {
		t.Fatalf("got %#v", got)
	}
	// Expired slot must not be take-able after remember path prunes.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "a", "", "q", "mid-old", "mid-old")
	route := m.replyRoutes["group-1"]
	route.pending[0].at = now.Add(-lansengerPendingDecorTTL - time.Minute)
	m.replyRoutes["group-1"] = route
	if _, ok := m.takeReplyDecorations("group-1", "mid-old"); ok {
		t.Fatal("TTL-expired pending must not decorate")
	}
}

func TestTakeReplyDecorationsPrefersSourceMessageID(t *testing.T) {
	// Hub finishes B before A — preferred message id must pair correctly.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-a", "", "问题A", "mid-a", "")
	m.rememberReplyRoute("group-1", true, "staff-b", "", "问题B", "mid-b", "")
	d, ok := m.takeReplyDecorations("group-1", "mid-b")
	if !ok || d.senderID != "staff-b" || d.question != "问题B" || d.messageID != "mid-b" {
		t.Fatalf("prefer B = %#v ok=%v", d, ok)
	}
	d, ok = m.takeReplyDecorations("group-1", "mid-a")
	if !ok || d.senderID != "staff-a" || d.question != "问题A" || d.messageID != "mid-a" {
		t.Fatalf("then A = %#v ok=%v", d, ok)
	}
}

func TestTakeReplyDecorationsNoFIFOStealOnMultiChunk(t *testing.T) {
	// First hub chunk consumes mid-a; later chunks still carry source_message_id
	// mid-a and must not steal mid-b via FIFO fallback.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-a", "", "问题A", "mid-a", "")
	m.rememberReplyRoute("group-1", true, "staff-b", "", "问题B", "mid-b", "")
	if _, ok := m.takeReplyDecorations("group-1", "mid-a"); !ok {
		t.Fatal("first chunk of A should decorate")
	}
	if _, ok := m.takeReplyDecorations("group-1", "mid-a"); ok {
		t.Fatal("second chunk of A must not decorate again / steal B")
	}
	d, ok := m.takeReplyDecorations("group-1", "mid-b")
	if !ok || d.senderID != "staff-b" || d.question != "问题B" || d.messageID != "mid-b" {
		t.Fatalf("B still intact = %#v ok=%v", d, ok)
	}
}

func TestTakeReplyDecorationsSyntheticCorrelationKeepsPlatformRefEmpty(t *testing.T) {
	// Platform message id empty; synthetic corr pairs multi-chunk without becoming refMsgId.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-a", "", "问A", "", "mc-synthetic-1")
	m.rememberReplyRoute("group-1", true, "staff-b", "", "问B", "mid-b", "mid-b")
	d, ok := m.takeReplyDecorations("group-1", "mc-synthetic-1")
	if !ok || d.senderID != "staff-a" || d.question != "问A" || d.messageID != "" {
		t.Fatalf("synthetic take = %#v ok=%v", d, ok)
	}
	if _, ok := m.takeReplyDecorations("group-1", "mc-synthetic-1"); ok {
		t.Fatal("second synthetic chunk must not steal B")
	}
	d, ok = m.takeReplyDecorations("group-1", "mid-b")
	if !ok || d.senderID != "staff-b" || d.messageID != "mid-b" {
		t.Fatalf("B = %#v ok=%v", d, ok)
	}
}

func TestHubReplyDecorUsesCachedDisplayName(t *testing.T) {
	// Hub path reconstructs inbound from the route cache; display name must survive.
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true, "staff-abc", "王占一", "帮我查天气", "mid-1", "")
	d, ok := m.takeReplyDecorations("group-1", "mid-1")
	if !ok {
		t.Fatal("expected decor")
	}
	if d.senderName != "王占一" {
		t.Fatalf("cached display name = %q", d.senderName)
	}
	inbound := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "group-1",
		FromUserID: d.senderID,
		SenderName: d.senderName,
		MessageID:  d.messageID,
		Text:       d.question,
	}
	out := buildLansengerOutgoingText(inbound, "今天晴。", lansenger.GroupChatOptions{RequireMention: true})
	if !strings.HasPrefix(out.Text, "王占一问：帮我查天气") {
		t.Fatalf("hub reconstructed quote must use display name, got %q", out.Text)
	}
	// Echoed staffId must not be stored as a "display name".
	m.rememberReplyRoute("group-2", true, "staff-xyz", "staff-xyz", "问题", "mid-2", "")
	d2, ok := m.takeReplyDecorations("group-2", "mid-2")
	if !ok || d2.senderName != "" || d2.senderID != "staff-xyz" {
		t.Fatalf("echoed id must not be cached as name: %#v ok=%v", d2, ok)
	}
	inbound2 := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "group-2",
		FromUserID: d2.senderID,
		SenderName: d2.senderName,
		Text:       d2.question,
	}
	out2 := buildLansengerOutgoingText(inbound2, "答", lansenger.GroupChatOptions{RequireMention: true})
	if !strings.HasPrefix(out2.Text, "staff-xyz问：") {
		t.Fatalf("fallback quote must use staffId, got %q", out2.Text)
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
	want := lansenger.FormatGroupReplyWithQuote("张三", "帮我查天气", "今天晴。")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "张三问：") {
		t.Fatalf("expected display name prefix, got %q", got)
	}
	// When Lansenger omits senderName, fall back to staffId so the header is not blank.
	noName := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "staff-abc",
		Text:       "帮我查天气",
	}
	if got := m.groupReplyText(noName, "今天晴。"); !strings.HasPrefix(got, "staff-abc问：") {
		t.Fatalf("expected staffId fallback prefix, got %q", got)
	}
	private := lansenger.IncomingMessage{ChatType: "p2p", FromUserID: "staff-abc", Text: "帮我查天气"}
	if got := m.groupReplyText(private, "今天晴。"); got != "今天晴。" {
		t.Fatalf("private reply must stay plain, got %q", got)
	}
	// Quote path must strip leading @Bot tokens like buildLansengerOutgoingText.
	withMention := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "staff-abc",
		Text:       "@测试机器人 帮我查天气",
		MentionedBots: []lansenger.MentionedBot{
			{ID: "bot-1", Name: "测试机器人"},
		},
	}
	gotMention := m.groupReplyText(withMention, "今天晴。")
	if strings.Contains(gotMention, "@测试机器人") {
		t.Fatalf("groupReplyText quote must strip @bot, got %q", gotMention)
	}
	if !strings.Contains(gotMention, "帮我查天气") {
		t.Fatalf("groupReplyText quote must keep cleaned question, got %q", gotMention)
	}
}

func TestStripLansengerBotMentionsForAgentPath(t *testing.T) {
	msg := lansenger.IncomingMessage{
		ChatType: "group",
		GroupID:  "g1",
		Text:     "@测试机器人 帮我查天气",
		MentionedBots: []lansenger.MentionedBot{
			{ID: "bot-1", Name: "测试机器人"},
		},
	}
	got := stripLansengerBotMentions(msg)
	if got != "帮我查天气" {
		t.Fatalf("strip = %q, want 帮我查天气", got)
	}
	// Outbound text quote should also prefer cleaned question.
	out := buildLansengerOutgoingText(msg, "晴", lansenger.GroupChatOptions{RequireMention: true})
	if !strings.HasPrefix(out.Text, "问：帮我查天气") && !strings.Contains(out.Text, "问：帮我查天气") {
		// who may be "有人" when FromUserID empty
		if !strings.Contains(out.Text, "帮我查天气") || strings.Contains(out.Text, "@测试机器人") {
			t.Fatalf("quote should use cleaned text, got %q", out.Text)
		}
	}
	if strings.Contains(out.Text, "@测试机器人") {
		t.Fatalf("quote must not retain @bot token, got %q", out.Text)
	}
}

func TestPassthroughSlashDetectedAfterBotMentionStrip(t *testing.T) {
	// Group chats commonly send "@Bot /help" — slash routing must see cleaned text.
	msg := lansenger.IncomingMessage{
		ChatType: "group",
		GroupID:  "g1",
		Text:     "@M-Wiggins /help",
		MentionedBots: []lansenger.MentionedBot{
			{ID: "bot", Name: "M-Wiggins"},
		},
	}
	cleaned := stripLansengerBotMentions(msg)
	if cleaned != "/help" {
		t.Fatalf("cleaned = %q, want /help", cleaned)
	}
	if !isPassthroughSlashText(cleaned) {
		t.Fatal("cleaned /help must be a passthrough slash command")
	}
	if isPassthroughSlashText(msg.Text) {
		t.Fatal("raw @Bot /help must NOT match slash detector (needs strip first)")
	}
	for _, raw := range []string{"@Bot /run demo", "@x /runctl status", "@机器人 /exec echo hi"} {
		m := lansenger.IncomingMessage{Text: raw}
		if !isPassthroughSlashText(stripLansengerBotMentions(m)) {
			t.Fatalf("expected slash after strip for %q -> %q", raw, stripLansengerBotMentions(m))
		}
	}
}

func TestBuildLansengerOutgoingSystemNoticeSkipsGroupQuote(t *testing.T) {
	msg := lansenger.IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "staff-1",
		MessageID:  "mid-1",
		Text:       "原问题",
	}
	opts := lansenger.GroupChatOptions{
		RequireMention:   true,
		AutoMentionReply: true,
		AutoQuoteReply:   true,
	}
	// Status/error notices must not @ or quote (no "xx问：" / reminder / refMsgId).
	out := buildLansengerOutgoingTextEx(msg, "Hub 未连接", opts, true)
	if out.Text != "Hub 未连接" {
		t.Fatalf("system notice text = %q", out.Text)
	}
	if out.Reminder != nil || out.RefMsgID != "" {
		t.Fatalf("system notice must not decorate, rem=%#v ref=%q", out.Reminder, out.RefMsgID)
	}
	// Normal agent replies still quote when native quote is unavailable.
	agent := buildLansengerOutgoingText(msg, "答案", lansenger.GroupChatOptions{RequireMention: true})
	if !strings.HasPrefix(agent.Text, "staff-1问：") {
		t.Fatalf("agent reply should text-quote, got %q", agent.Text)
	}
	// AutoQuote with message id uses native ref, not text prefix.
	quoted := buildLansengerOutgoingText(msg, "答案", opts)
	if strings.Contains(quoted.Text, "问：") {
		t.Fatalf("native quote path must not text-prefix, got %q", quoted.Text)
	}
	if quoted.RefMsgID != "mid-1" || quoted.Reminder == nil || len(quoted.Reminder.UserIDs) != 1 {
		t.Fatalf("decorations = ref=%q rem=%#v", quoted.RefMsgID, quoted.Reminder)
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
	// Quote path must still use pristine msg.Text and prefer display name.
	quoted := m.groupReplyText(msg, "目前无法获取完整成员列表。")
	if !strings.HasPrefix(quoted, "王占一问：看下群里还有哪些机器人？") {
		t.Fatalf("group quote must use display name + original question, got %q", quoted)
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
