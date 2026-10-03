package agentruntime

import "strings"

// IMMessagePlatformKind identifies the delivery channel a turn is serving.
// The vocabulary is shared by every host so channel-governance rules (which
// channel may receive files, voice, scheduled dispatches, ...) are decided
// once here instead of drifting per host.
type IMMessagePlatformKind string

const (
	IMMessagePlatformUnknown         IMMessagePlatformKind = ""
	IMMessagePlatformDesktop         IMMessagePlatformKind = "desktop"
	IMMessagePlatformTUI             IMMessagePlatformKind = "tui"
	IMMessagePlatformFeishu          IMMessagePlatformKind = "feishu"
	IMMessagePlatformWecom           IMMessagePlatformKind = "wecom"
	IMMessagePlatformQQBot           IMMessagePlatformKind = "qqbot"
	IMMessagePlatformQQBotLocal      IMMessagePlatformKind = "qqbot_local"
	IMMessagePlatformDingTalk        IMMessagePlatformKind = "dingtalk"
	IMMessagePlatformTelegram        IMMessagePlatformKind = "telegram"
	IMMessagePlatformTelegramLocal   IMMessagePlatformKind = "telegram_local"
	IMMessagePlatformWeixin          IMMessagePlatformKind = "weixin"
	IMMessagePlatformWeixinLocal     IMMessagePlatformKind = "weixin_local"
	IMMessagePlatformLansenger       IMMessagePlatformKind = "lansenger"
	IMMessagePlatformLansengerLocal  IMMessagePlatformKind = "lansenger_local"
	IMMessagePlatformVEGroupExecutor IMMessagePlatformKind = "ve_group_executor"
	IMMessagePlatformScheduler       IMMessagePlatformKind = "scheduler"
)

// NormalizeIMMessagePlatformKind maps a free-form channel string onto the
// known platform vocabulary; anything unrecognized is Unknown.
func NormalizeIMMessagePlatformKind(value string) IMMessagePlatformKind {
	switch IMMessagePlatformKind(strings.ToLower(strings.TrimSpace(value))) {
	case IMMessagePlatformDesktop:
		return IMMessagePlatformDesktop
	case IMMessagePlatformTUI:
		return IMMessagePlatformTUI
	case IMMessagePlatformFeishu:
		return IMMessagePlatformFeishu
	case IMMessagePlatformWecom:
		return IMMessagePlatformWecom
	case IMMessagePlatformQQBot:
		return IMMessagePlatformQQBot
	case IMMessagePlatformQQBotLocal:
		return IMMessagePlatformQQBotLocal
	case IMMessagePlatformDingTalk:
		return IMMessagePlatformDingTalk
	case IMMessagePlatformTelegram:
		return IMMessagePlatformTelegram
	case IMMessagePlatformTelegramLocal:
		return IMMessagePlatformTelegramLocal
	case IMMessagePlatformWeixin:
		return IMMessagePlatformWeixin
	case IMMessagePlatformWeixinLocal:
		return IMMessagePlatformWeixinLocal
	case IMMessagePlatformLansenger:
		return IMMessagePlatformLansenger
	case IMMessagePlatformLansengerLocal:
		return IMMessagePlatformLansengerLocal
	case IMMessagePlatformVEGroupExecutor:
		return IMMessagePlatformVEGroupExecutor
	case IMMessagePlatformScheduler:
		return IMMessagePlatformScheduler
	default:
		return IMMessagePlatformUnknown
	}
}

// SemanticFileDeliveryPublished reports whether the channel may receive file
// artifacts produced during a managed semantic turn on that same channel.
// A true result for weixin means a weixin turn may receive a file it produced.
// It does not authorize a desktop turn to push a file to WeChat.
func SemanticFileDeliveryPublished(channel string) bool {
	switch NormalizeIMMessagePlatformKind(channel) {
	case IMMessagePlatformDesktop, IMMessagePlatformTUI, IMMessagePlatformLansenger, IMMessagePlatformLansengerLocal, IMMessagePlatformWeixin, IMMessagePlatformWeixinLocal:
		return true
	default:
		return false
	}
}

// ResolveHostFileDestination resolves one whole utterance onto a file-delivery
// platform. The utterance must be only a closed frame plus an exact channel
// alias. Extra words, partial sentences, and earlier history are not scanned.
func ResolveHostFileDestination(utterance string) (IMMessagePlatformKind, bool) {
	text := strings.TrimSpace(utterance)
	text = strings.TrimRight(text, "。．.！!？?")
	text = strings.TrimSpace(text)
	if text == "" {
		return IMMessagePlatformUnknown, false
	}
	lower := strings.ToLower(text)
	frames := []string{"发送到", "转发到", "发到", "发给", "send to ", "deliver to "}
	rest := ""
	matched := false
	for _, frame := range frames {
		if strings.HasPrefix(lower, strings.ToLower(frame)) {
			rest = strings.TrimSpace(text[len(frame):])
			matched = true
			break
		}
	}
	if !matched || rest == "" {
		return IMMessagePlatformUnknown, false
	}
	switch strings.ToLower(rest) {
	case "微信", "weixin", "wechat":
		return IMMessagePlatformWeixin, true
	case "蓝信", "lansenger":
		return IMMessagePlatformLansenger, true
	default:
		return IMMessagePlatformUnknown, false
	}
}

// SemanticCrossChannelFileDeliveryPublished reports whether origin may push a
// file to dest. The same channel scope uses SemanticFileDeliveryPublished.
// A desktop, TUI, or unknown origin may push a file only to lansenger, which
// is the transport DeliverIMFile implements. Desktop to weixin is false.
// ve_group_executor shares the desktop scope but is not a file-delivery origin.
func SemanticCrossChannelFileDeliveryPublished(origin, dest string) bool {
	originKind := NormalizeIMMessagePlatformKind(origin)
	destKind := NormalizeIMMessagePlatformKind(dest)
	if destKind == IMMessagePlatformUnknown {
		return false
	}
	if originKind.ChannelScope() != "" && originKind.ChannelScope() == destKind.ChannelScope() {
		return SemanticFileDeliveryPublished(origin)
	}
	switch originKind {
	case IMMessagePlatformDesktop, IMMessagePlatformTUI, IMMessagePlatformUnknown:
		return destKind.ChannelScope() == "lansenger"
	default:
		return false
	}
}

// SemanticTrustedDispatchDestination reports whether destination names a
// concrete, non-empty group or user target.
func SemanticTrustedDispatchDestination(destination string) bool {
	destination = strings.TrimSpace(destination)
	switch {
	case strings.HasPrefix(destination, "group:") && len(destination) > len("group:"):
		return true
	case strings.HasPrefix(destination, "user:") && len(destination) > len("user:"):
		return true
	default:
		return false
	}
}

// SemanticScheduleDispatchPublished reports whether a scheduled dispatch may
// be published on the channel towards the destination.
func SemanticScheduleDispatchPublished(channel, destination string) bool {
	if !SemanticTrustedDispatchDestination(destination) {
		return false
	}
	switch NormalizeIMMessagePlatformKind(channel) {
	case IMMessagePlatformDesktop, IMMessagePlatformTUI, IMMessagePlatformLansenger, IMMessagePlatformLansengerLocal:
		return true
	default:
		return false
	}
}

// SemanticVoiceDeliveryPublished reports whether voice messages may be
// delivered on the channel towards the destination.
func SemanticVoiceDeliveryPublished(channel, destination string) bool {
	switch NormalizeIMMessagePlatformKind(channel) {
	case IMMessagePlatformLansenger, IMMessagePlatformLansengerLocal:
		return SemanticTrustedDispatchDestination(destination)
	default:
		return false
	}
}

// SemanticAudioSynthesizeLocalPublished reports whether local audio
// synthesis may be published on the channel.
func SemanticAudioSynthesizeLocalPublished(channel string) bool {
	switch NormalizeIMMessagePlatformKind(channel) {
	case IMMessagePlatformDesktop, IMMessagePlatformTUI:
		return true
	default:
		return false
	}
}

// SemanticImageDeliveryPublished reports whether image delivery may be
// published on the channel. Unlike file/voice/schedule delivery, images are
// intentionally allowed on any non-empty channel: the artifact has already
// been produced and approved, so the delivery gate is not the place to
// second-guess platform capability.
func SemanticImageDeliveryPublished(channel string) bool {
	return strings.TrimSpace(channel) != ""
}

func (kind IMMessagePlatformKind) String() string {
	return string(kind)
}

// IsDesktop reports whether the kind is a desktop-class runtime surface.
func (kind IMMessagePlatformKind) IsDesktop() bool {
	return kind == IMMessagePlatformDesktop || kind == IMMessagePlatformVEGroupExecutor
}

// IsKnown reports whether the kind belongs to the shared platform vocabulary.
func (kind IMMessagePlatformKind) IsKnown() bool {
	return kind != IMMessagePlatformUnknown
}

// IsDesktopPlaybackTarget reports whether audio playback targets the local
// desktop (including an unspecified channel, which is desktop by default).
func (kind IMMessagePlatformKind) IsDesktopPlaybackTarget() bool {
	return kind == IMMessagePlatformUnknown || kind == IMMessagePlatformDesktop || kind == IMMessagePlatformTUI
}

// IsIMChannel reports whether the kind is an instant-messaging channel.
func (kind IMMessagePlatformKind) IsIMChannel() bool {
	switch kind {
	case IMMessagePlatformFeishu,
		IMMessagePlatformWecom,
		IMMessagePlatformQQBot,
		IMMessagePlatformQQBotLocal,
		IMMessagePlatformDingTalk,
		IMMessagePlatformTelegram,
		IMMessagePlatformTelegramLocal,
		IMMessagePlatformWeixin,
		IMMessagePlatformWeixinLocal,
		IMMessagePlatformLansenger,
		IMMessagePlatformLansengerLocal:
		return true
	default:
		return false
	}
}

// ChannelScope returns the canonical channel identity used by capability
// providers and delivery records. Runtime platforms intentionally retain their
// transport detail (for example, "lansenger_local"), while a semantic plan
// describes the user-visible delivery channel ("lansenger"). Keeping this
// conversion at the typed platform boundary prevents a provider binding from
// depending on a particular local-vs-hub runtime spelling.
func (kind IMMessagePlatformKind) ChannelScope() string {
	switch kind {
	case IMMessagePlatformLansenger, IMMessagePlatformLansengerLocal:
		return "lansenger"
	case IMMessagePlatformWeixin, IMMessagePlatformWeixinLocal:
		return "weixin"
	case IMMessagePlatformTelegram, IMMessagePlatformTelegramLocal:
		return "telegram"
	case IMMessagePlatformQQBot, IMMessagePlatformQQBotLocal:
		return "qqbot"
	case IMMessagePlatformFeishu:
		return "feishu"
	case IMMessagePlatformWecom:
		return "wecom"
	case IMMessagePlatformDingTalk:
		return "dingtalk"
	case IMMessagePlatformScheduler:
		return "scheduler"
	case IMMessagePlatformDesktop, IMMessagePlatformTUI, IMMessagePlatformVEGroupExecutor:
		return "desktop"
	default:
		return ""
	}
}

// PrefersAMRVoice reports whether the channel prefers AMR voice messages.
func (kind IMMessagePlatformKind) PrefersAMRVoice() bool {
	return kind == IMMessagePlatformWecom
}

// PrefersWAVVoice reports whether the channel prefers WAV voice messages.
func (kind IMMessagePlatformKind) PrefersWAVVoice() bool {
	switch kind {
	case IMMessagePlatformWeixin, IMMessagePlatformWeixinLocal, IMMessagePlatformQQBot, IMMessagePlatformQQBotLocal:
		return true
	default:
		return false
	}
}

// PrefersOGGVoice reports whether the channel prefers OGG voice messages.
func (kind IMMessagePlatformKind) PrefersOGGVoice() bool {
	switch kind {
	case IMMessagePlatformFeishu, IMMessagePlatformTelegram, IMMessagePlatformTelegramLocal, IMMessagePlatformDingTalk:
		return true
	default:
		return false
	}
}
