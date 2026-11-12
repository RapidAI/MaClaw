package main

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// Continuation turns. The plan-or-execute choice is the intake phase
// (desktopbot.ApplyTurnPhase). The client's bot_phase is ignored except
// the arrange/run button. Publish approval is remembered here, separate
// from that phase.

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

// desktopTurnPhase stamps the intake's plan-or-execute choice. A raw client
// phase is not used. bot_phase_button is the arrange/run control.
// bot_ask_id / bot_ask_run match one plan-phase question.
func desktopTurnPhase(request agentruntime.TurnRequest) string {
	_, meta := desktopTurnMessage(request)
	if meta != nil && strings.TrimSpace(meta["bot_phase_decided"]) == "1" {
		phase := strings.TrimSpace(meta["bot_phase"])
		if phase == "plan" || phase == "execute" {
			return phase
		}
	}
	bot := ""
	intake, askID, askRun := "", "", ""
	button := false
	if meta != nil {
		bot = meta["bot_id"]
		intake = meta["bot_intake_phase"]
		askID = meta["bot_ask_id"]
		askRun = meta["bot_ask_run"]
		button = meta["bot_phase_button"] == "1"
	}
	phase := desktopbot.ApplyTurnPhase(desktopbot.TurnPhaseInput{
		Button: button,
		Intake: intake,
		AskID:  askID,
		AskRun: askRun,
		Tenant: request.Scope.TenantID,
		User:   request.Scope.UserID,
		Bot:    bot,
	})
	if meta != nil {
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
