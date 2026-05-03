package wecom

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

func TestSendVoiceRejectsNonAMRBeforeUpload(t *testing.T) {
	plugin := &Plugin{configProvider: func() Config { return Config{Enabled: true, BotID: "bot", Secret: "secret"} }}
	voice := base64.StdEncoding.EncodeToString([]byte("OggS\x00OpusHead"))
	if err := plugin.SendVoice(context.Background(), im.UserTarget{PlatformUID: "user-1"}, voice, "voice.ogg", "audio/ogg"); err == nil {
		t.Fatal("SendVoice(non-AMR) error = nil, want error")
	}
}

func TestCapabilitiesSupportVoiceWithSendVoice(t *testing.T) {
	plugin := &Plugin{configProvider: func() Config { return Config{} }}
	if !plugin.Capabilities().SupportsVoice {
		t.Fatal("SupportsVoice = false, want true")
	}
	if _, ok := any(plugin).(im.VoiceSender); !ok {
		t.Fatal("WeCom plugin does not implement im.VoiceSender")
	}
}
