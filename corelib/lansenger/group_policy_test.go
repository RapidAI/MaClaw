package lansenger

import "testing"

func TestIsGroupChatAndNormalizeChatType(t *testing.T) {
	if !IsGroupChat("group") || !IsGroupChat(" GROUP ") || !IsGroupChat("Group") {
		t.Fatal("expected group variants to match")
	}
	if IsGroupChat("p2p") || IsGroupChat("") {
		t.Fatal("non-group must not match")
	}
	if got := NormalizeChatType(" GROUP "); got != "group" {
		t.Fatalf("NormalizeChatType group = %q", got)
	}
	if got := NormalizeChatType("Private"); got != "p2p" {
		t.Fatalf("NormalizeChatType private = %q", got)
	}
	if got := NormalizeChatType("dm"); got != "p2p" {
		t.Fatalf("NormalizeChatType dm = %q", got)
	}
}

func TestGroupMessageAllowed(t *testing.T) {
	base := IncomingMessage{
		ChatType:   "group",
		GroupID:    "g1",
		FromUserID: "u1",
		Text:       "hello",
	}
	opts := GroupChatOptions{
		Policy:         GroupPolicyOpen,
		RequireMention: true,
		AppID:          "org-bot1",
	}
	if ok, reason := GroupMessageAllowed(base, opts); ok || reason != "require_mention" {
		t.Fatalf("expected require_mention drop, got ok=%v reason=%q", ok, reason)
	}
	base.IsAtMe = true
	if ok, reason := GroupMessageAllowed(base, opts); !ok || reason != "" {
		t.Fatalf("at-me should pass, got ok=%v reason=%q", ok, reason)
	}

	base.IsAtMe = false
	base.IsAtAll = true
	if ok, _ := GroupMessageAllowed(base, opts); ok {
		t.Fatal("@all without RespondToAtAll must not pass")
	}
	opts.RespondToAtAll = true
	if ok, _ := GroupMessageAllowed(base, opts); !ok {
		t.Fatal("@all with RespondToAtAll should pass")
	}

	opts.RequireMention = false
	base.IsAtAll = false
	if ok, _ := GroupMessageAllowed(base, opts); !ok {
		t.Fatal("requireMention=false should allow any group message")
	}

	opts.Policy = GroupPolicyDisabled
	if ok, reason := GroupMessageAllowed(base, opts); ok || reason != "group_policy_disabled" {
		t.Fatalf("disabled policy, got ok=%v reason=%q", ok, reason)
	}

	opts.Policy = GroupPolicyAllowlist
	opts.AllowedGroupIDs = []string{"g1"}
	if ok, _ := GroupMessageAllowed(base, opts); !ok {
		t.Fatal("allowlist with matching group should pass")
	}
	base.GroupID = "g2"
	if ok, reason := GroupMessageAllowed(base, opts); ok || reason != "group_not_in_allowlist" {
		t.Fatalf("allowlist miss, got ok=%v reason=%q", ok, reason)
	}

	base.GroupID = "g1"
	opts.Policy = GroupPolicyOpen
	opts.IgnoredGroupIDs = []string{"g1"}
	if ok, reason := GroupMessageAllowed(base, opts); ok || reason != "group_ignored" {
		t.Fatalf("ignored group, got ok=%v reason=%q", ok, reason)
	}

	// Private chat always allowed.
	if ok, _ := GroupMessageAllowed(IncomingMessage{ChatType: "p2p"}, opts); !ok {
		t.Fatal("private should always pass group policy")
	}
}

func TestBuildReplyDecorations(t *testing.T) {
	msg := IncomingMessage{FromUserID: "staff-1", MessageID: "mid-9", ChatType: "group"}
	opts := GroupChatOptions{AutoMentionReply: true, AutoQuoteReply: true}
	rem, ref := BuildReplyDecorations(msg, opts)
	if rem == nil || len(rem.UserIDs) != 1 || rem.UserIDs[0] != "staff-1" {
		t.Fatalf("reminder = %#v", rem)
	}
	if ref != "mid-9" {
		t.Fatalf("ref = %q", ref)
	}
	if !PreferNativeGroupQuote(opts, ref) {
		t.Fatal("expected native quote preference")
	}

	for _, chatType := range []string{"p2p", "private", "dm", "direct", ""} {
		private := msg
		private.ChatType = chatType
		rem, ref = BuildReplyDecorations(private, opts)
		if rem != nil {
			t.Fatalf("private chat type %q must not @mention, reminder = %#v", chatType, rem)
		}
		if ref != "mid-9" {
			t.Fatalf("private chat type %q changed native quote = %q", chatType, ref)
		}
	}

	// Chat type normalization inside IsGroupChat must also apply to decorations.
	spacedGroup := msg
	spacedGroup.ChatType = " GROUP "
	rem, ref = BuildReplyDecorations(spacedGroup, opts)
	if rem == nil || len(rem.UserIDs) != 1 || rem.UserIDs[0] != "staff-1" || ref != "mid-9" {
		t.Fatalf("normalized group decorations = reminder %#v ref %q", rem, ref)
	}
}
