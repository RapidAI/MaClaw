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
}

func TestRemoteGatewayPendingVoiceTextIncludesArmingMetadata(t *testing.T) {
	sender := &captureMachineSender{}
	plugin := &RemoteGatewayPlugin{
		platform: "thirdparty", sender: sender,
		owner: &gatewayOwner{TenantID: "tenant_default", MachineID: "machine-1"},
	}
	if err := plugin.SendTextWithPendingVoiceParts(context.Background(), UserTarget{PlatformUID: "pet-1"}, "完整结果", 3); err != nil {
		t.Fatalf("SendTextWithPendingVoiceParts() error = %v", err)
	}
	msg := sender.msg.(map[string]any)
	inner := msg["payload"].(map[string]any)["payload"].(map[string]any)
	metadata := inner["metadata"].(map[string]any)
	if inner["reply_type"] != "text" || inner["final"] != true || metadata["acp_turn"] != "final" || metadata["speech_parts_pending"] != 3 {
		t.Fatalf("pending voice text payload = %#v", inner)
	}
}

func TestRemoteGatewayPendingVoiceEndPreservesCorrelation(t *testing.T) {
	sender := &captureMachineSender{}
	plugin := &RemoteGatewayPlugin{
		platform: "thirdparty", sender: sender,
		owner: &gatewayOwner{TenantID: "tenant_default", MachineID: "machine-1"},
	}
	ctx := WithReplyMeta(context.Background(), "pet-1", "command-42")
	if err := plugin.SendPendingVoiceEnd(ctx, UserTarget{PlatformUID: "pet-1"}, 3, 1); err != nil {
		t.Fatalf("SendPendingVoiceEnd() error = %v", err)
	}
	msg := sender.msg.(map[string]any)
	inner := msg["payload"].(map[string]any)["payload"].(map[string]any)
	extra := inner["extra"].(map[string]any)
	if inner["reply_type"] != "speech_end" || inner["source_message_id"] != "command-42" ||
		extra["speech_parts_expected"] != 3 || extra["speech_parts_sent"] != 1 {
		t.Fatalf("speech end payload = %#v", inner)
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
