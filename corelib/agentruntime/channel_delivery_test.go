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

func TestResolveHostFileDestination(t *testing.T) {
	kind, ok := ResolveHostFileDestination("发到微信")
	if !ok || kind != IMMessagePlatformWeixin {
		t.Fatalf("发到微信 = %q ok=%v", kind, ok)
	}
	kind, ok = ResolveHostFileDestination("  发送到 WeChat。 ")
	if !ok || kind != IMMessagePlatformWeixin {
		t.Fatalf("发送到 WeChat = %q ok=%v", kind, ok)
	}
	kind, ok = ResolveHostFileDestination("转发到蓝信")
	if !ok || kind != IMMessagePlatformLansenger {
		t.Fatalf("转发到蓝信 = %q ok=%v", kind, ok)
	}
	kind, ok = ResolveHostFileDestination("Send to weixin")
	if !ok || kind != IMMessagePlatformWeixin {
		t.Fatalf("Send to weixin = %q ok=%v", kind, ok)
	}
	for _, utterance := range []string{"继续上次未完成任务", "北京天气，生成pdf", "把崇州pdf发到微信", "发到微信群", "发到微信吧"} {
		if _, ok := ResolveHostFileDestination(utterance); ok {
			t.Fatalf("%q must not resolve to a file destination", utterance)
		}
	}
}

func TestSemanticCrossChannelFileDelivery(t *testing.T) {
	if !SemanticFileDeliveryPublished("weixin") {
		t.Fatal("weixin own-channel file receipt must stay published")
	}
	if SemanticCrossChannelFileDeliveryPublished("desktop", "weixin") {
		t.Fatal("desktop must not push a file to weixin")
	}
	if SemanticCrossChannelFileDeliveryPublished("tui", "weixin_local") {
		t.Fatal("tui must not push a file to weixin")
	}
	if !SemanticCrossChannelFileDeliveryPublished("weixin", "weixin") || !SemanticCrossChannelFileDeliveryPublished("weixin_local", "weixin") {
		t.Fatal("a weixin turn may receive a file on its own channel")
	}
	if !SemanticCrossChannelFileDeliveryPublished("desktop", "lansenger") {
		t.Fatal("desktop may push a file to lansenger")
	}
	if SemanticCrossChannelFileDeliveryPublished("weixin", "lansenger") {
		t.Fatal("weixin must not cross-push a file to lansenger")
	}
	if SemanticCrossChannelFileDeliveryPublished("unknown-origin", "weixin") {
		t.Fatal("an unknown origin must not push a file to weixin")
	}
	if !SemanticCrossChannelFileDeliveryPublished("unknown-origin", "lansenger") {
		t.Fatal("an unknown origin may still push a file to lansenger")
	}
	if SemanticCrossChannelFileDeliveryPublished("ve_group_executor", "lansenger") {
		t.Fatal("ve_group_executor is not a cross-channel file origin")
	}
	if SemanticCrossChannelFileDeliveryPublished("ve_group_executor", "weixin") {
		t.Fatal("ve_group_executor must not push a file to weixin")
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
