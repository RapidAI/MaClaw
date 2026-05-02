package im

import (
	"context"
	"testing"
)

type captureMachineSender struct {
	machineID string
	msg       any
}

func (s *captureMachineSender) SendToMachine(machineID string, msg any) error {
	s.machineID = machineID
	s.msg = msg
	return nil
}

func TestRemoteGatewaySendVoiceUsesVoiceReplyType(t *testing.T) {
	sender := &captureMachineSender{}
	plugin := &RemoteGatewayPlugin{
		platform: "weixin",
		sender:   sender,
		owner:    &gatewayOwner{MachineID: "machine-1"},
	}

	if err := plugin.SendVoice(context.Background(), UserTarget{PlatformUID: "user-1"}, "base64-audio", "voice.wav", "audio/wav"); err != nil {
		t.Fatalf("SendVoice() error = %v", err)
	}
	if sender.machineID != "machine-1" {
		t.Fatalf("machineID = %q, want machine-1", sender.machineID)
	}

	msg, ok := sender.msg.(map[string]any)
	if !ok {
		t.Fatalf("sent msg type = %T, want map[string]any", sender.msg)
	}
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", msg["payload"])
	}
	inner, ok := payload["payload"].(map[string]any)
	if !ok {
		t.Fatalf("inner payload type = %T, want map[string]any", payload["payload"])
	}
	if inner["reply_type"] != "voice" {
		t.Fatalf("reply_type = %v, want voice", inner["reply_type"])
	}
	if inner["file_data"] != "base64-audio" || inner["file_name"] != "voice.wav" || inner["mime_type"] != "audio/wav" {
		t.Fatalf("voice payload = %#v", inner)
	}
}
func TestRemoteGatewayVoiceCapabilityIsPlatformAware(t *testing.T) {
	for _, platform := range []string{"weixin", "telegram", "qqbot", "thirdparty"} {
		plugin := &RemoteGatewayPlugin{platform: platform}
		if !plugin.Capabilities().SupportsVoice {
			t.Fatalf("SupportsVoice for %q = false, want true", platform)
		}
	}

	plugin := &RemoteGatewayPlugin{platform: "lansenger"}
	if plugin.Capabilities().SupportsVoice {
		t.Fatal("SupportsVoice for lansenger = true, want false")
	}
}
