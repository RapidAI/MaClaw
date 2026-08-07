package scheduler

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeIMMessageAction(t *testing.T) {
	cases := map[string]string{
		"list_groups": "list_targets",
		"LIST":        "list_targets",
		"push":        "send",
		"SEND":        "send",
		"send_file":   "send_file",
		"upload":      "send_file",
		"SendFile":    "send_file",
		"explode":     "explode",
		"":            "",
	}
	for in, want := range cases {
		if got := NormalizeIMMessageAction(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestResolveIMMessageActionInfer(t *testing.T) {
	if got := ResolveIMMessageAction(map[string]interface{}{"text": "hi", "group_name": "g"}); got != "send" {
		t.Fatalf("infer send: %q", got)
	}
	if got := ResolveIMMessageAction(map[string]interface{}{"path": "a.pdf", "group_id": "g"}); got != "send_file" {
		t.Fatalf("infer send_file: %q", got)
	}
	// A file path wins over text so a captioned file send does not degrade to text-only.
	if got := ResolveIMMessageAction(map[string]interface{}{"path": "a.pdf", "text": "看看"}); got != "send_file" {
		t.Fatalf("path beats text: %q", got)
	}
	if got := ResolveIMMessageAction(map[string]interface{}{"file_path": "a.pdf"}); got != "send_file" {
		t.Fatalf("file_path alias: %q", got)
	}
	if got := ResolveIMMessageAction(map[string]interface{}{"query": "校友"}); got != "list_targets" {
		t.Fatalf("infer list: %q", got)
	}
	if got := ResolveIMMessageAction(map[string]interface{}{"action": "send"}); got != "send" {
		t.Fatalf("explicit: %q", got)
	}
	if got := ResolveIMMessageAction(map[string]interface{}{"action": "send_file"}); got != "send_file" {
		t.Fatalf("explicit send_file: %q", got)
	}
	if got := ResolveIMMessageAction(nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestIsIMMessageSendIntent(t *testing.T) {
	if !IsIMMessageSendIntent(map[string]interface{}{"text": "weather", "group_name": "x"}) {
		t.Fatal("inferred send must be send intent for security")
	}
	if !IsIMMessageSendIntent(map[string]interface{}{"path": "a.pdf", "group_id": "g"}) {
		t.Fatal("inferred send_file must be send intent for security")
	}
	if !IsIMMessageSendIntent(map[string]interface{}{"action": "send_file", "path": "a.pdf"}) {
		t.Fatal("explicit send_file must be send intent for security")
	}
	if IsIMMessageSendIntent(map[string]interface{}{"action": "list_targets"}) {
		t.Fatal("list is not send")
	}
}

func TestFormatIMMessageSendOKTruncationNote(t *testing.T) {
	long := strings.Repeat("字", MaxDeliveryBodyRunes+10)
	msg := FormatIMMessageSendOK("lansenger → 群:test", long)
	if !strings.Contains(msg, "截断") {
		t.Fatalf("want truncate note: %s", msg)
	}
	short := FormatIMMessageSendOK("lansenger → 群:test", "ok")
	if strings.Contains(short, "截断") {
		t.Fatalf("no truncate note for short: %s", short)
	}
}

func TestDefaultDeliveryChannel(t *testing.T) {
	if DefaultDeliveryChannel("") != DeliveryChannelLansenger {
		t.Fatal("default")
	}
	if DefaultDeliveryChannel(" Weixin ") != "weixin" {
		t.Fatal("lower")
	}
	if DefaultDeliveryChannel("蓝信") != DeliveryChannelLansenger {
		t.Fatal("蓝信")
	}
	if DefaultDeliveryChannel("蓝信 IM") != DeliveryChannelLansenger {
		t.Fatal("蓝信 IM")
	}
	if DefaultDeliveryChannel("微信") != DeliveryChannelWeixin {
		t.Fatal("微信")
	}
	if DefaultDeliveryChannel("wechat") != DeliveryChannelWeixin {
		t.Fatal("wechat")
	}
	for input, want := range map[string]string{
		"lansenger_local": DeliveryChannelLansenger,
		"weixin_local":    DeliveryChannelWeixin,
		"telegram_local":  DeliveryChannelTelegram,
		"qqbot_local":     DeliveryChannelQQ,
	} {
		if got := DefaultDeliveryChannel(input); got != want {
			t.Fatalf("%q -> %q, want %q", input, got, want)
		}
	}
}

func TestRunIMMessageToolDispatch(t *testing.T) {
	var saw string
	out := RunIMMessageTool(map[string]interface{}{"text": "hi", "group_id": "g"},
		func(map[string]interface{}) string { saw = "list"; return "L" },
		func(map[string]interface{}) string { saw = "send"; return "S" },
		func(map[string]interface{}) string { saw = "send_file"; return "F" },
	)
	if saw != "send" || out != "S" {
		t.Fatalf("saw=%q out=%q", saw, out)
	}
	out = RunIMMessageTool(map[string]interface{}{"query": "x"},
		func(map[string]interface{}) string { saw = "list"; return "L" },
		func(map[string]interface{}) string { saw = "send"; return "S" },
		func(map[string]interface{}) string { saw = "send_file"; return "F" },
	)
	if saw != "list" || out != "L" {
		t.Fatalf("saw=%q out=%q", saw, out)
	}
	out = RunIMMessageTool(map[string]interface{}{"path": "a.pdf", "group_id": "g"},
		func(map[string]interface{}) string { saw = "list"; return "L" },
		func(map[string]interface{}) string { saw = "send"; return "S" },
		func(map[string]interface{}) string { saw = "send_file"; return "F" },
	)
	if saw != "send_file" || out != "F" {
		t.Fatalf("saw=%q out=%q", saw, out)
	}
	// Nil send_file handler degrades to a clear message instead of a panic.
	out = RunIMMessageTool(map[string]interface{}{"path": "a.pdf"}, nil, nil, nil)
	if !strings.Contains(out, "send_file") {
		t.Fatalf("nil send_file handler: %q", out)
	}
}

func TestFormatIMMessageSendFileOK(t *testing.T) {
	msg := FormatIMMessageSendFileOK("lansenger → 群:test", "report.pdf", 123)
	for _, want := range []string{"report.pdf", "123", "lansenger → 群:test"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("want %q in %q", want, msg)
		}
	}
	if got := FormatIMMessageSendFileOK("", "  ", 0); !strings.Contains(got, "(unnamed file)") || !strings.Contains(got, "(unknown target)") {
		t.Fatalf("defaults: %q", got)
	}
}

func TestTaskDeliveryNormalizeCanonicalizesChannel(t *testing.T) {
	d := &TaskDelivery{Enabled: true, Channel: "蓝信", Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g1"}}}
	d.Normalize()
	if d.Channel != DeliveryChannelLansenger {
		t.Fatalf("channel=%q", d.Channel)
	}
}

func TestTargetCatalogRegistryGetAcceptsAliases(t *testing.T) {
	reg := NewTargetCatalogRegistry()
	reg.Register(TargetCatalogFunc{
		ChannelName: DeliveryChannelLansenger,
		List: func(ctx context.Context, query string) ([]TargetRef, error) {
			return []TargetRef{{Kind: DeliveryKindGroup, ID: "g1", Name: "校友群"}}, nil
		},
	})
	if _, ok := reg.Get("蓝信"); !ok {
		t.Fatal("Get(蓝信) should resolve lansenger catalog")
	}
	if _, ok := reg.Get("lanxin"); !ok {
		t.Fatal("Get(lanxin)")
	}
	refs, err := reg.ListTargets(context.Background(), "蓝信", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ID != "g1" {
		t.Fatalf("refs=%#v", refs)
	}
}
