package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestCurrentRuntimeTaskTextDoesNotUseLegacyLastUser(t *testing.T) {
	h := &IMMessageHandler{lastUserID: desktopUserID, lastUserText: "desktop secret task"}

	text, ownerID := h.currentRuntimeTaskTextOrLegacy()
	if text != "" || ownerID != "" {
		t.Fatalf("currentRuntimeTaskTextOrLegacy() = (%q, %q), want empty without explicit runtime", text, ownerID)
	}
}

func TestCurrentRuntimeTaskTextUsesExplicitOwner(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("desktop", 1, nil)
	ctx.Runtime = RuntimeContext{RequestID: "req-owner", PolicyOwnerID: "owner-1"}
	h.currentLoopCtx = ctx
	h.setSessionLoopCtx("owner-1", ctx)
	state := h.getSessionLoop("owner-1")
	state.stateMu.Lock()
	state.userText = "owner task"
	state.stateMu.Unlock()

	text, ownerID := h.currentRuntimeTaskTextOrLegacy()
	if text != "owner task" || ownerID != "owner-1" {
		t.Fatalf("currentRuntimeTaskTextOrLegacy() = (%q, %q), want explicit owner task", text, ownerID)
	}
}

func TestOlderLoopCleanupDoesNotClearReplacementLoopState(t *testing.T) {
	const userID = "im:replacement"
	h := &IMMessageHandler{}
	oldCtx := NewLoopContext("old", 1, nil)
	oldCleanup := h.beginAgentLoopRuntime(oldCtx, userID, "old task", "weixin")

	newCtx := NewLoopContext("new", 1, nil)
	newCleanup := h.beginAgentLoopRuntime(newCtx, userID, "new task", "weixin")
	h.accumulateInjection(userID, "[用户补充] only for replacement")

	oldCleanup()
	if got := h.getSessionLoopCtx(userID); got != newCtx {
		t.Fatalf("active loop after old cleanup = %p, want replacement %p", got, newCtx)
	}
	if got := h.sessionLoopTaskText(userID); got != "new task" {
		t.Fatalf("replacement task text = %q, want new task", got)
	}
	if raw, ok := h.pendingInjection.Load(userID); !ok || raw == "" {
		t.Fatal("old cleanup discarded replacement steering injection")
	}

	newCleanup()
	if got := h.getSessionLoopCtx(userID); got != nil {
		t.Fatalf("active loop after replacement cleanup = %p, want nil", got)
	}
}

func TestHostOwnedCurrentChannelDeliveryTargetBindsLocalChatOnly(t *testing.T) {
	desktop := hostOwnedCurrentChannelDeliveryTarget("desktop", "desktop-user")
	if desktop == nil || desktop.ChannelScope != "desktop" || desktop.DestinationID != "user:desktop-user" {
		t.Fatalf("desktop target = %+v", desktop)
	}
	tui := hostOwnedCurrentChannelDeliveryTarget("tui", "tui-user")
	if tui == nil || tui.ChannelScope != "desktop" || tui.DestinationID != "user:tui-user" {
		t.Fatalf("tui target = %+v", tui)
	}
	if got := hostOwnedCurrentChannelDeliveryTarget("weixin", "wx-user"); got != nil {
		t.Fatalf("weixin must not invent a destination: %+v", got)
	}
	if got := hostOwnedCurrentChannelDeliveryTarget("lansenger_local", "ls-user"); got != nil {
		t.Fatalf("lansenger must not invent a destination: %+v", got)
	}
	if got := hostOwnedCurrentChannelDeliveryTarget("desktop", "  "); got != nil {
		t.Fatalf("empty desktop user must stay fail-closed: %+v", got)
	}
	if got := copyInboundDeliveryTarget(&agent.DeliveryTarget{ChannelScope: "desktop"}, "desktop"); got != nil {
		t.Fatalf("incomplete inbound must be treated as missing: %+v", got)
	}
	if got := copyInboundDeliveryTarget(&agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "user:"}, "weixin"); got != nil {
		t.Fatalf("typed-empty destination must be treated as missing: %+v", got)
	}
	if got := copyInboundDeliveryTarget(&agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "wx-user"}, "weixin"); got != nil {
		t.Fatalf("untyped destination must be treated as missing: %+v", got)
	}
	normalized := copyInboundDeliveryTarget(&agent.DeliveryTarget{ChannelScope: "weixin_local", DestinationID: "user:wx-user"}, "weixin_local")
	if normalized == nil || normalized.ChannelScope != "weixin" || normalized.DestinationID != "user:wx-user" {
		t.Fatalf("weixin_local inbound must canonicalize to weixin: %+v", normalized)
	}
	if got := copyInboundDeliveryTarget(&agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "user:wx-user"}, "desktop"); got != nil {
		t.Fatalf("weixin inbound must not match desktop: %+v", got)
	}
	if got := trustedLoopDeliveryTarget(&LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "desktop-user"}}); got != nil {
		t.Fatalf("current-channel dest still requires user:/group: prefix: %+v", got)
	}
	canonical := trustedLoopDeliveryTarget(&LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "weixin_local", DestinationID: "user:wx-user"}})
	if canonical == nil || canonical.ChannelScope != "weixin" || canonical.DestinationID != "user:wx-user" {
		t.Fatalf("execute-time target must canonicalize weixin_local: %+v", canonical)
	}
}

func TestPrepareIMLoopContextBindsDesktopDeliveryTarget(t *testing.T) {
	h := &IMMessageHandler{}
	desktop := h.prepareIMLoopContext(nil, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "南京天气，生成pdf报告"}, nil, false, false)
	if desktop.DeliveryTarget == nil || desktop.DeliveryTarget.ChannelScope != "desktop" || desktop.DeliveryTarget.DestinationID != "user:desktop-user" {
		t.Fatalf("desktop DeliveryTarget = %+v", desktop.DeliveryTarget)
	}

	tui := h.prepareIMLoopContext(nil, IMUserMessage{UserID: "tui-user", Platform: "tui", Text: "hi"}, nil, false, false)
	if tui.DeliveryTarget == nil || tui.DeliveryTarget.ChannelScope != "desktop" || tui.DeliveryTarget.DestinationID != "user:tui-user" {
		t.Fatalf("tui DeliveryTarget = %+v", tui.DeliveryTarget)
	}

	inbound := &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:explicit"}
	preserved := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", Text: "hi", DeliveryTarget: inbound,
	}, nil, false, false)
	if preserved.DeliveryTarget == nil || preserved.DeliveryTarget.DestinationID != "user:explicit" {
		t.Fatalf("explicit inbound target was overwritten: %+v", preserved.DeliveryTarget)
	}

	emptyInbound := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", Text: "hi", DeliveryTarget: &agent.DeliveryTarget{},
	}, nil, false, false)
	if emptyInbound.DeliveryTarget == nil || emptyInbound.DeliveryTarget.DestinationID != "user:desktop-user" {
		t.Fatalf("empty inbound desktop target must fall through to host-owned: %+v", emptyInbound.DeliveryTarget)
	}

	mismatched := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", Text: "hi",
		DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "user:wx-user"},
	}, nil, false, false)
	if mismatched.DeliveryTarget == nil || mismatched.DeliveryTarget.ChannelScope != "desktop" || mismatched.DeliveryTarget.DestinationID != "user:desktop-user" {
		t.Fatalf("cross-channel inbound must not stick on desktop: %+v", mismatched.DeliveryTarget)
	}

	weixin := h.prepareIMLoopContext(nil, IMUserMessage{UserID: "wx-user", Platform: "weixin", Text: "hi"}, nil, false, false)
	if weixin.DeliveryTarget != nil {
		t.Fatalf("weixin without inbound target must stay nil: %+v", weixin.DeliveryTarget)
	}

	weixinEmpty := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "wx-user", Platform: "weixin", Text: "hi", DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "weixin"},
	}, nil, false, false)
	if weixinEmpty.DeliveryTarget != nil {
		t.Fatalf("incomplete weixin inbound must stay fail-closed: %+v", weixinEmpty.DeliveryTarget)
	}

	weixinInbound := &agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "user:wx-user"}
	weixinKept := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "wx-user", Platform: "weixin_local", Text: "hi", DeliveryTarget: weixinInbound,
	}, nil, false, false)
	if weixinKept.DeliveryTarget == nil || weixinKept.DeliveryTarget.ChannelScope != "weixin" || weixinKept.DeliveryTarget.DestinationID != "user:wx-user" {
		t.Fatalf("weixin_local inbound = %+v", weixinKept.DeliveryTarget)
	}

	leftover := NewLoopContext("chat", 1, nil)
	leftover.Platform = "weixin"
	leftover.UserID = "wx-user"
	rebound := h.prepareIMLoopContext(leftover, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "南京天气，生成pdf报告"}, nil, false, false)
	if rebound.DeliveryTarget == nil || rebound.DeliveryTarget.ChannelScope != "desktop" || rebound.DeliveryTarget.DestinationID != "user:desktop-user" {
		t.Fatalf("desktop message must not inherit leftover weixin dest: %+v", rebound.DeliveryTarget)
	}
}

func TestPrepareIMLoopContextClearsStaleLeftoverFlags(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	provided.Runtime.RequestID = "req-reused"
	provided.Runtime.RoutingMissFallback = true
	provided.Runtime.HostAdapterLeftover = true
	provided.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
		Reason: "chat projection; routing miss fallback; host adapter leftover",
	}
	got := h.prepareIMLoopContext(provided, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "北京天所"}, nil, false, false)
	if got.Runtime.RoutingMissFallback || got.Runtime.HostAdapterLeftover {
		t.Fatalf("reused Runtime must drop leftover flags: %#v", got.Runtime)
	}
	if got.Runtime.SemanticIntent != nil {
		t.Fatalf("reused leftover projection must not classify this turn: %#v", got.Runtime.SemanticIntent)
	}
}

func TestPrepareIMLoopContextAnonymousReplacementRotatesSemanticEnvelope(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "first-user", Platform: "desktop", Lang: "zh", RequestID: "request-a", Text: "capture primary screen",
	}, nil, false, false)
	firstRoot, firstTurn := semanticRoutingIdentity(first, "first-user", "capture primary screen")
	firstRunID := first.RunID
	first.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: .99}

	second := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "second-user", Platform: "weixin", Lang: "en", Text: "what time is it",
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "second-user", "what time is it")
	if firstRoot == secondRoot || firstTurn == secondTurn {
		t.Fatalf("anonymous replacement retained semantic identity: first=(%q,%q) second=(%q,%q)", firstRoot, firstTurn, secondRoot, secondTurn)
	}
	if second.Runtime.RequestID == "" || second.Runtime.RequestID == "request-a" {
		t.Fatalf("anonymous replacement request ID=%q, want fresh host request ID", second.Runtime.RequestID)
	}
	if second.Runtime.SemanticIntent != nil {
		t.Fatalf("anonymous replacement retained prior semantic intent: %#v", second.Runtime.SemanticIntent)
	}
	if second.Platform != "weixin" || second.UserID != "second-user" || second.Lang != "en" {
		t.Fatalf("anonymous replacement retained stale ingress envelope: platform=%q user=%q lang=%q", second.Platform, second.UserID, second.Lang)
	}
	if second.RunID != "" && second.RunID == firstRunID {
		t.Fatalf("anonymous replacement retained prior trace run %q", second.RunID)
	}
	if !strings.Contains(second.Runtime.Conversation.SessionKey, "second-user") || strings.Contains(second.Runtime.Conversation.SessionKey, "first-user") {
		t.Fatalf("anonymous replacement runtime session=%q", second.Runtime.Conversation.SessionKey)
	}
}

func TestPrepareIMLoopContextSameRequestIDCannotCrossIngressBoundary(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user-a", Platform: "desktop", RequestID: "transport-retry-id", Text: "capture primary screen",
	}, nil, false, false)
	firstRoot, firstTurn := semanticRoutingIdentity(first, "desktop-user-a", "capture primary screen")
	first.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: .99}
	first.Runtime.PolicyOwnerID = "workflow-owner-a"
	first.Runtime.WorkflowOwnerID = "workflow-owner-a"

	second := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "weixin-user-b", Platform: "weixin", RequestID: "transport-retry-id", Text: "what time is it",
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "weixin-user-b", "what time is it")
	if firstRoot == secondRoot || firstTurn == secondTurn {
		t.Fatalf("cross-ingress same request ID retained semantic identity: first=(%q,%q) second=(%q,%q)", firstRoot, firstTurn, secondRoot, secondTurn)
	}
	if second.Runtime.SemanticIntent != nil {
		t.Fatalf("cross-ingress request retained semantic intent: %#v", second.Runtime.SemanticIntent)
	}
	if second.Runtime.PolicyOwnerID != "weixin-user-b" || second.Runtime.WorkflowOwnerID != "weixin-user-b" {
		t.Fatalf("cross-ingress request retained prior policy owner: runtime=%+v", second.Runtime)
	}
	if second.Runtime.Source.Channel != "im" || second.Runtime.Source.Provider != "weixin" || second.Runtime.Conversation.ConversationID != "weixin-user-b" {
		t.Fatalf("cross-ingress runtime envelope=%+v", second.Runtime)
	}
}

func TestPrepareIMLoopContextSameIngressRequestRetryRetainsHostPolicyOwner(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "capture primary screen",
	}, nil, false, false)
	first.Runtime.PolicyOwnerID = "verified-workflow-owner"
	first.Runtime.WorkflowOwnerID = "verified-workflow-owner"
	first.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: .99}
	root, turn := semanticRoutingIdentity(first, "desktop-user", "capture primary screen")

	second := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "capture primary screen",
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "desktop-user", "capture primary screen")
	if secondRoot != root || secondTurn != turn {
		t.Fatalf("same-ingress retry unnecessarily rotated semantic identity: first=(%q,%q) second=(%q,%q)", root, turn, secondRoot, secondTurn)
	}
	if second.Runtime.PolicyOwnerID != "verified-workflow-owner" || second.Runtime.WorkflowOwnerID != "verified-workflow-owner" {
		t.Fatalf("same-ingress retry lost verified policy owner: runtime=%+v", second.Runtime)
	}
}

func TestPrepareIMLoopContextSameRequestIDWithChangedPayloadRotatesSurface(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "capture primary screen",
		ClientToolContext: &agent.ClientToolContext{ClientID: "device-a", ConversationID: "main"},
		ClientTools:       []agent.ClientToolDefinition{{Name: "alarm_list", InputSchema: map[string]any{"type": "object"}}},
	}, nil, false, false)
	first.Runtime.PolicyOwnerID = "verified-workflow-owner"
	first.Runtime.WorkflowOwnerID = "verified-workflow-owner"
	first.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: .99}
	root, turn := semanticRoutingIdentity(first, "desktop-user", "capture primary screen")

	second := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "what time is it",
		ClientToolContext: &agent.ClientToolContext{ClientID: "device-b", ConversationID: "main"},
		ClientTools:       []agent.ClientToolDefinition{{Name: "unlock_door", InputSchema: map[string]any{"type": "object"}}},
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "desktop-user", "what time is it")
	if secondRoot == root || secondTurn == turn {
		t.Fatalf("changed same-ID payload retained semantic identity: first=(%q,%q) second=(%q,%q)", root, turn, secondRoot, secondTurn)
	}
	if second.Runtime.SemanticIntent != nil {
		t.Fatalf("changed same-ID payload retained semantic intent: %#v", second.Runtime.SemanticIntent)
	}
	if second.Runtime.PolicyOwnerID != "desktop-user" || second.Runtime.WorkflowOwnerID != "desktop-user" {
		t.Fatalf("changed same-ID payload retained policy owner: runtime=%+v", second.Runtime)
	}
	if second.ClientToolContext == nil || second.ClientToolContext.ClientID != "device-b" || len(second.ClientTools) != 1 || second.ClientTools[0].Name != "unlock_door" {
		t.Fatalf("changed same-ID payload retained client surface: context=%#v tools=%#v", second.ClientToolContext, second.ClientTools)
	}
}

func TestPrepareIMLoopContextSameRequestIDWithChangedControlPlaneRotatesSurface(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "continue",
		ClientCapabilities: &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{Modalities: []string{"text"}}},
	}, nil, false, false)
	first.Runtime.PolicyOwnerID = "verified-workflow-owner"
	first.Runtime.WorkflowOwnerID = "verified-workflow-owner"
	root, turn := semanticRoutingIdentity(first, "desktop-user", "continue")

	second := h.prepareIMLoopContext(provided, IMUserMessage{
		UserID: "desktop-user", Platform: "desktop", RequestID: "transport-retry-id", Text: "continue", StartNewTask: true,
		ClientCapabilities: &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{Modalities: []string{"text", "audio"}}},
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "desktop-user", "continue")
	if secondRoot == root || secondTurn == turn {
		t.Fatalf("changed same-ID control plane retained semantic identity: first=(%q,%q) second=(%q,%q)", root, turn, secondRoot, secondTurn)
	}
	if second.Runtime.PolicyOwnerID != "desktop-user" || second.Runtime.WorkflowOwnerID != "desktop-user" {
		t.Fatalf("changed same-ID control plane retained policy owner: runtime=%+v", second.Runtime)
	}
}
