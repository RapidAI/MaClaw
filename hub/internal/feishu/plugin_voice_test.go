package feishu

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

func TestSendVoiceRejectsNonOpusBeforeBotUse(t *testing.T) {
	plugin := NewPlugin(New("", "", nil, nil, nil))
	voice := base64.StdEncoding.EncodeToString([]byte("not opus"))
	if err := plugin.SendVoice(context.Background(), im.UserTarget{PlatformUID: "open-id-1"}, voice, "voice.wav", "audio/wav"); err == nil {
		t.Fatal("SendVoice(non-opus) error = nil, want error")
	}
}

func TestCapabilitiesSupportVoiceWithSendVoice(t *testing.T) {
	plugin := NewPlugin(New("", "", nil, nil, nil))
	if !plugin.Capabilities().SupportsVoice {
		t.Fatal("SupportsVoice = false, want true")
	}
	if _, ok := any(plugin).(im.VoiceSender); !ok {
		t.Fatal("FeishuPlugin does not implement im.VoiceSender")
	}
}
