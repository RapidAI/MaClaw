package agentruntime

import "testing"

func TestNormalizeIMMessagePlatformKind(t *testing.T) {
	if got := NormalizeIMMessagePlatformKind(" Lansenger_Local "); got != IMMessagePlatformLansengerLocal {
		t.Fatalf("lansenger_local = %q", got)
	}
	if got := NormalizeIMMessagePlatformKind("WEIXIN"); got != IMMessagePlatformWeixin {
		t.Fatalf("weixin = %q", got)
	}
	if got := NormalizeIMMessagePlatformKind("nope"); got != IMMessagePlatformUnknown {
		t.Fatalf("unknown = %q", got)
	}
}

func TestChannelDeliveryPolicies(t *testing.T) {
	if !SemanticFileDeliveryPublished("desktop") || !SemanticFileDeliveryPublished("weixin") {
		t.Fatal("desktop/weixin must allow file delivery")
	}
	if SemanticFileDeliveryPublished("feishu") {
		t.Fatal("feishu must not allow file delivery")
	}
	if !SemanticTrustedDispatchDestination("group:ops") || !SemanticTrustedDispatchDestination("user:u1") {
		t.Fatal("group/user destinations must be trusted")
	}
	if SemanticTrustedDispatchDestination("group:") || SemanticTrustedDispatchDestination("") {
		t.Fatal("empty targets must not be trusted")
	}
	if !SemanticScheduleDispatchPublished("lansenger", "group:ops") {
		t.Fatal("lansenger group dispatch must be publishable")
	}
	if SemanticScheduleDispatchPublished("weixin", "group:ops") {
		t.Fatal("weixin scheduled dispatch must not be publishable")
	}
	if SemanticScheduleDispatchPublished("desktop", "") {
		t.Fatal("dispatch without destination must not be publishable")
	}
	if !SemanticVoiceDeliveryPublished("lansenger_local", "user:u1") {
		t.Fatal("lansenger voice delivery must be publishable")
	}
	if SemanticVoiceDeliveryPublished("desktop", "user:u1") {
		t.Fatal("desktop voice delivery must not be publishable")
	}
	if !SemanticAudioSynthesizeLocalPublished("tui") || SemanticAudioSynthesizeLocalPublished("lansenger") {
		t.Fatal("local audio synthesis only on desktop/tui")
	}
	if !SemanticImageDeliveryPublished("qqbot") || SemanticImageDeliveryPublished("  ") {
		t.Fatal("image delivery requires a non-empty channel")
	}
}

func TestIMMessagePlatformKindMethods(t *testing.T) {
	if !IMMessagePlatformLansengerLocal.IsIMChannel() || IMMessagePlatformDesktop.IsIMChannel() {
		t.Fatal("IM channel classification drifted")
	}
	if IMMessagePlatformLansengerLocal.ChannelScope() != "lansenger" {
		t.Fatal("local runtime spelling must map to canonical scope")
	}
	if IMMessagePlatformTUI.ChannelScope() != "desktop" {
		t.Fatal("tui must share the desktop channel scope")
	}
	if !IMMessagePlatformWecom.PrefersAMRVoice() || !IMMessagePlatformWeixin.PrefersWAVVoice() || !IMMessagePlatformFeishu.PrefersOGGVoice() {
		t.Fatal("voice format preferences drifted")
	}
	if !IMMessagePlatformUnknown.IsDesktopPlaybackTarget() || !IMMessagePlatformVEGroupExecutor.IsDesktop() {
		t.Fatal("desktop playback/classification drifted")
	}
}
