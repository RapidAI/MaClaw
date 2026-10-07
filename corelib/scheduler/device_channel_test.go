package scheduler

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalDeliveryChannelRecognizesDevice(t *testing.T) {
	// Every spelling the agent or a hand-written config might use must land on
	// the same stable key; a miss here silently falls through to "unsupported
	// channel" at send time.
	cases := map[string]string{
		"device":        DeliveryChannelDevice,
		"DEVICE":        DeliveryChannelDevice,
		"device_local":  DeliveryChannelDevice,
		"hardware":      DeliveryChannelDevice,
		"esp32":         DeliveryChannelDevice,
		"companion":     DeliveryChannelDevice,
		"maclaw-device": DeliveryChannelDevice,
		"设备":            DeliveryChannelDevice,
		"码卡龙":           DeliveryChannelDevice,
		"硬件设备":          DeliveryChannelDevice,
	}
	for input, want := range cases {
		if got := CanonicalDeliveryChannel(input); got != want {
			t.Fatalf("CanonicalDeliveryChannel(%q)=%q want %q", input, got, want)
		}
		if got := DefaultDeliveryChannel(input); got != want {
			t.Fatalf("DefaultDeliveryChannel(%q)=%q want %q", input, got, want)
		}
	}
	// The new channel must not have swallowed the existing defaults.
	if got := DefaultDeliveryChannel(""); got != DeliveryChannelLansenger {
		t.Fatalf("empty channel must still default to lansenger, got %q", got)
	}
}

func TestSplitScheduledTaskBodyRoundTripsFormatBody(t *testing.T) {
	d := &TaskDelivery{Prefix: "提醒你："}
	body := d.FormatBody("每日站会", "10 点开始", nil)
	name, rest := SplitScheduledTaskBody(body)
	if name != "每日站会" {
		t.Fatalf("name=%q body=%q", name, body)
	}
	// The whole point of the split: the device card shows the name as its title,
	// so the summary must not repeat the marker line.
	if strings.Contains(rest, "【定时任务】") {
		t.Fatalf("rest must not repeat the task line: %q", rest)
	}
	if !strings.Contains(rest, "10 点开始") {
		t.Fatalf("rest must keep the agent result: %q", rest)
	}
	// A prefix is user-visible text that appears before the task line; dropping
	// it would lose part of the message.
	if !strings.Contains(rest, "提醒你：") {
		t.Fatalf("prefix must stay in the body: %q", rest)
	}
}

func TestSplitScheduledTaskBodyKeepsFailureStatus(t *testing.T) {
	d := &TaskDelivery{}
	body := d.FormatBody("备份数据库", "", errors.New("disk full"))
	name, rest := SplitScheduledTaskBody(body)
	if name != "备份数据库" {
		t.Fatalf("name=%q body=%q", name, body)
	}
	if !strings.Contains(rest, "状态: 失败") || !strings.Contains(rest, "disk full") {
		t.Fatalf("rest=%q", rest)
	}
}

func TestSplitScheduledTaskBodyOnUnformattedText(t *testing.T) {
	// An immediate im_message push and the managed dispatch path both reach the
	// sender without a task line.
	name, rest := SplitScheduledTaskBody("楼下快递到了")
	if name != "" {
		t.Fatalf("name=%q", name)
	}
	if rest != "楼下快递到了" {
		t.Fatalf("rest=%q", rest)
	}
	if name, rest := SplitScheduledTaskBody(""); name != "" || rest != "" {
		t.Fatalf("empty body: name=%q rest=%q", name, rest)
	}
}

func TestSplitScheduledTaskBodyPrefersTheFirstMarker(t *testing.T) {
	// FormatBody always writes the task line before the agent result, so a result
	// that happens to contain the marker must not hijack the title.
	body := "【定时任务】真实任务\n结果里也出现了【定时任务】这个词"
	name, rest := SplitScheduledTaskBody(body)
	if name != "真实任务" {
		t.Fatalf("name=%q", name)
	}
	if !strings.Contains(rest, "这个词") {
		t.Fatalf("rest=%q", rest)
	}
}

func TestTaskDeliveryDeviceChannelPassesValidation(t *testing.T) {
	// The device channel is owner-only and has no group targets, so it must
	// validate and resolve without needing a target catalog lookup.
	d := &TaskDelivery{
		Enabled: true,
		Channel: "device",
		Targets: []DeliveryTarget{{Kind: DeliveryKindUser, UserID: "self"}},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if d.Channel != DeliveryChannelDevice {
		t.Fatalf("channel must canonicalize, got %q", d.Channel)
	}
	if d.NeedsGroupNameResolution() {
		t.Fatal("an owner-only channel must never need group name resolution")
	}
	if err := d.EnsureResolved(); err != nil {
		t.Fatalf("EnsureResolved: %v", err)
	}
}
