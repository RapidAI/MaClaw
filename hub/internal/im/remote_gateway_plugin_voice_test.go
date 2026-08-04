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
		owner:    &gatewayOwner{TenantID: "tenant_default", MachineID: "machine-1"},
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
	if _, ok := inner["voice_part_index"]; ok {
		t.Fatalf("legacy IM voice unexpectedly carries hardware stream metadata: %#v", inner)
	}
}

func TestRemoteGatewaySendVoicePartCarriesStreamMetadata(t *testing.T) {
	sender := &captureMachineSender{}
	plugin := &RemoteGatewayPlugin{
		platform: "thirdparty",
		sender:   sender,
		owner:    &gatewayOwner{TenantID: "tenant_default", MachineID: "machine-1"},
	}
	if err := plugin.SendVoicePart(context.Background(), UserTarget{PlatformUID: "pet"}, "part-2", "reply-2.wav", "audio/wav", 2, 3, false); err != nil {
		t.Fatalf("SendVoicePart() error = %v", err)
	}
	msg := sender.msg.(map[string]any)
	payload := msg["payload"].(map[string]any)
	inner := payload["payload"].(map[string]any)
	if inner["voice_part_index"] != 2 || inner["voice_part_total"] != 3 || inner["voice_part_final"] != false {
		t.Fatalf("voice part metadata = %#v", inner)
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
