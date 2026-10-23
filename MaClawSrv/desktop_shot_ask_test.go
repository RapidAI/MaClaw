package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestUnaskedShotDroppedWhenReplyHasLink(t *testing.T) {
	mk := func() *desktopShotSlot {
		return &desktopShotSlot{shot: agent.MessageAttachment{Data: "x"}, ready: true}
	}
	s := mk()
	dropUnaskedDesktopShot(s, "发到知乎上", &agentservice.Message{Content: "链接：https://zhuanlan.zhihu.com/p/1"})
	if s.ready {
		t.Fatal("kept")
	}
	s = mk()
	dropUnaskedDesktopShot(s, "发完给我截图", &agentservice.Message{Content: "https://x/p/1"})
	if !s.ready {
		t.Fatal("asked shot dropped")
	}
	s = mk()
	dropUnaskedDesktopShot(s, "打开页面", &agentservice.Message{Content: "桌面操作完成"})
	if !s.ready {
		t.Fatal("bare reply lost its screen")
	}
}
