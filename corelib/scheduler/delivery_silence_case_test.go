package scheduler

import (
	"errors"
	"strings"
	"testing"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	d := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g"}},
		On:      DeliveryOnAlways,
	}
	if d.ShouldDeliver(nil, "") || d.ShouldDeliver(nil, "无变化") || d.ShouldDeliver(nil, "(无输出)") {
		t.Fatal("an empty success was delivered")
	}
	if !d.ShouldDeliver(errors.New("disk full"), "") || !d.ShouldDeliver(nil, "首页没打开。") {
		t.Fatal("a failure or a real result was quiet")
	}
	if strings.Contains(d.FormatBody("早报", "", nil), "(无输出)") || strings.Contains(d.FormatBody("早报", "无变化", nil), "无变化") {
		t.Fatal("format wrote a filler")
	}
	if !strings.Contains(d.FormatBody("早报", "首页没打开。", nil), "首页没打开。") {
		t.Fatal("a real result was dropped")
	}
	if !strings.Contains(d.FormatBody("早报", "", errors.New("disk full")), "disk full") {
		t.Fatal("a failure was dropped")
	}
}
