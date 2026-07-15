package main

import (
	"fmt"
	"testing"
	"time"

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
	m.rememberReplyRoute("group-1", true)
	m.rememberReplyRoute("user-1", false)
	if !m.isGroupReplyTarget("group-1") {
		t.Fatal("group reply route was not retained")
	}
	if m.isGroupReplyTarget("user-1") {
		t.Fatal("private reply route was treated as group")
	}
	if m.isGroupReplyTarget("unknown") {
		t.Fatal("unknown reply route must default to private")
	}
}

func TestLansengerReplyRouteHasBoundedCache(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	for i := 0; i < maxLansengerReplyRoutes+1; i++ {
		m.rememberReplyRoute(fmt.Sprintf("target-%d", i), i%2 == 0)
	}
	if got := len(m.replyRoutes); got != maxLansengerReplyRoutes {
		t.Fatalf("reply route cache size = %d, want %d", got, maxLansengerReplyRoutes)
	}
}

func TestLansengerReplyRoutesCanBeClearedOnLifecycleReset(t *testing.T) {
	m := newLansengerGatewayManager(nil)
	m.rememberReplyRoute("group-1", true)
	clear(m.replyRoutes)
	if m.isGroupReplyTarget("group-1") {
		t.Fatal("cleared reply route was retained")
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
