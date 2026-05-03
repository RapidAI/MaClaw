package dingtalk

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

func TestSendVoiceRejectsUnsupportedFormatBeforeUpload(t *testing.T) {
	plugin := &Plugin{configProvider: func() Config { return Config{Enabled: true, ClientID: "app", ClientSecret: "secret"} }}
	voice := base64.StdEncoding.EncodeToString([]byte("not audio"))
	if err := plugin.SendVoice(context.Background(), im.UserTarget{PlatformUID: "staff-1"}, voice, "voice.wav", "audio/wav"); err == nil {
		t.Fatal("SendVoice(unsupported) error = nil, want error")
	}
}

func TestCapabilitiesSupportVoiceWithSendVoice(t *testing.T) {
	plugin := &Plugin{configProvider: func() Config { return Config{} }}
	if !plugin.Capabilities().SupportsVoice {
		t.Fatal("SupportsVoice = false, want true")
	}
	if _, ok := any(plugin).(im.VoiceSender); !ok {
		t.Fatal("DingTalk plugin does not implement im.VoiceSender")
	}
}
