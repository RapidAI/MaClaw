package main

import (
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The last desktop screenshot rides on the reply only when the person asked
// to see the screen; a publish or lookup task answers with its link/text
// (every Zhihu reply carried an unasked desktop.png next to its /p/ link).
var desktopShotAskedRe = regexp.MustCompile(`(?i)截图|截屏|屏幕|看看桌面|screenshot|screen ?shot|show me the (screen|desktop)|desktop image`)

func dropUnaskedDesktopShot(slot *desktopShotSlot, request string, msg *agentservice.Message) {
	// Only a reply that already carries its result as a link drops the
	// image; a bare "done" still shows the screen.
	if slot == nil || msg == nil || desktopShotAskedRe.MatchString(request) || !strings.Contains(msg.Content, "http") {
		return
	}
	slot.mu.Lock()
	slot.shot, slot.ready = agent.MessageAttachment{}, false
	slot.mu.Unlock()
}
