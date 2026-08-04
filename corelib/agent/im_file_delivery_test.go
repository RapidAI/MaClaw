package agent

import "testing"

func TestIMFileDeliveryTargetFlagRoundTrip(t *testing.T) {
	flag := EncodeIMFileDeliveryTargetFlag(map[string]interface{}{
		"channel": "lansenger", "group_id": "g-42", "group_name": "研发群",
	})
	if flag == "" {
		t.Fatal("expected target flag")
	}
	got, ok := DecodeIMFileDeliveryTargetFlag(flag)
	if !ok || got.Channel != "lansenger" || got.GroupID != "g-42" || got.GroupName != "研发群" {
		t.Fatalf("decoded target = %#v, ok=%v", got, ok)
	}
}

func TestIMFileDeliveryTargetUsesPlatformDestinationAlias(t *testing.T) {
	got := IMFileDeliveryTargetFromArgs(map[string]interface{}{"destination": "feishu", "user_id": "ou_1"})
	if got.Channel != "feishu" || got.UserID != "ou_1" {
		t.Fatalf("target = %#v", got)
	}
}
