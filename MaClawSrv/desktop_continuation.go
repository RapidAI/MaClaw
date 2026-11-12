package main

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// Continuation turns. The plan-or-execute choice lives in
// desktopbot.ResolvePhase: the client's bot_phase is a hint, an ordinary
// task runs, and an answer to a plan-phase ask_user runs in execute.
// Publish approval is remembered here, separate from that phase. A hand-back
// with no phase used to be treated as "not execute" (10-22 run_d8638514).

const desktopTurnMemoryTTL = 3 * time.Hour

type desktopTurnMemory struct {
	approved   bool
	approvedAt time.Time
}

var (
	desktopTurnMemMu sync.Mutex
	desktopTurnMem   = map[string]*desktopTurnMemory{}
)

// desktopPublishIntentRe: the person asked for the post itself ("发到知乎",
// "发布到小红书"); that is the approval to press 发布 once the body is in.
// desktopPublishIntentRe: the same site-free publish-request test as the
// coach's (site names come from the site profiles).
var desktopPublishIntentRe = desktopPublishAskRe

func desktopTurnMessage(request agentruntime.TurnRequest) (string, map[string]string) {
	switch input := request.Input.(type) {
	case agentservice.ExecuteRequest:
		return input.Message.Content, input.Message.Metadata
	case *agentservice.ExecuteRequest:
		if input != nil {
			return input.Message.Content, input.Message.Metadata
		}
	}
	return "", nil
}

func desktopTurnMemKey(request agentruntime.TurnRequest, meta map[string]string) string {
	bot := strings.TrimSpace(meta["bot_id"])
	if bot == "" {
		return ""
	}
	return request.Scope.TenantID + "\x00" + request.Scope.UserID + "\x00" + bot
}

// desktopTurnPhase is the server's plan-or-execute choice for this turn.
// The client's bot_phase is a hint. An ordinary task runs. A plan hint from
// an older GUI does not keep it in plan. An answer to an ask_user from a
// plan turn runs in execute. The choice is stamped on the message so the
// prompt, the ssh gate, and the desktop tool all see the same phase.
func desktopTurnPhase(request agentruntime.TurnRequest) string {
	content, meta := desktopTurnMessage(request)
	if meta != nil && strings.TrimSpace(meta["bot_phase_decided"]) == "1" {
		phase := strings.TrimSpace(meta["bot_phase"])
		if phase == "plan" || phase == "execute" {
			return phase
		}
	}
	hint := botPhaseFromTurn(request)
	bot := ""
	if meta != nil {
		bot = meta["bot_id"]
	}
	phase := desktopbot.ResolvePhase(request.Scope.TenantID, request.Scope.UserID, bot, hint, content)
	if meta != nil && (phase == "plan" || phase == "execute") {
		meta["bot_phase"] = phase
		meta["bot_phase_decided"] = "1"
	}
	return phase
}

// desktopTurnPublishApproved: explicit "publish without asking", or the
// person's own request to post it, in this turn or earlier in the same bot
// conversation. The empty-body / title-only check still runs on every click.
func desktopTurnPublishApproved(request agentruntime.TurnRequest) bool {
	content, meta := desktopTurnMessage(request)
	approved := desktopPreapproveRe.MatchString(content) || desktopPublishIntentRe.MatchString(content)
	key := desktopTurnMemKey(request, meta)
	if key == "" {
		return approved
	}
	now := time.Now()
	desktopTurnMemMu.Lock()
	defer desktopTurnMemMu.Unlock()
	mem := desktopTurnMem[key]
	if mem == nil {
		mem = &desktopTurnMemory{}
		desktopTurnMem[key] = mem
	}
	if approved {
		mem.approved, mem.approvedAt = true, now
		return true
	}
	return mem.approved && now.Sub(mem.approvedAt) < desktopTurnMemoryTTL
}
