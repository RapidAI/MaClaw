package main

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// Continuation turns. Hub's hand-back (交还给 bot) posts "请继续刚才的任务"
// with no bot_phase. The plan gate treated that as "not execute", so the
// continued task could only look (10-22 run_d8638514: click and bash refused
// with 这一阶段只核对…, the run ended after 8 steps and asked nobody). The
// same turn also lost the publish approval the person gave in the original
// task. Both are remembered per bot conversation for a while and inherited
// by a turn that names no phase.

const desktopTurnMemoryTTL = 3 * time.Hour

type desktopTurnMemory struct {
	phase      string
	phaseAt    time.Time
	approved   bool
	approvedAt time.Time
}

var (
	desktopTurnMemMu sync.Mutex
	desktopTurnMem   = map[string]*desktopTurnMemory{}
)

// desktopPublishIntentRe: the person asked for the post itself ("发到知乎",
// "发布到小红书"); that is the approval to press 发布 once the body is in.
var desktopPublishIntentRe = regexp.MustCompile(`(?i)(发到|发布到|发表到|发在|发布在|发表在|投到|贴到)\s*(知乎|小红书|微博|公众号|头条|掘金|csdn|b站|网站|博客|专栏)|发(知乎|小红书|微博)|在\s*(知乎|小红书|微博|公众号|头条|掘金|csdn|b站)[^。\n]{0,20}(发布|发表|发一篇|发篇)|在\s*(知乎|小红书|微博|公众号|头条|掘金|csdn|b站)\s*上?\s*发[^现生出给送]|(post|publish) (it |this )?(to|on) `)

var desktopHandBackRe = regexp.MustCompile(`桌面交还给你|请继续刚才的任务|hand(ed)? (the desktop )?back`)

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

// desktopTurnPhase is botPhaseFromTurn with continuation memory.
func desktopTurnPhase(request agentruntime.TurnRequest) string {
	phase := botPhaseFromTurn(request)
	content, meta := desktopTurnMessage(request)
	key := desktopTurnMemKey(request, meta)
	if key == "" {
		return phase
	}
	now := time.Now()
	desktopTurnMemMu.Lock()
	defer desktopTurnMemMu.Unlock()
	mem := desktopTurnMem[key]
	if mem == nil {
		mem = &desktopTurnMemory{}
		desktopTurnMem[key] = mem
	}
	if phase != "" {
		mem.phase, mem.phaseAt = phase, now
		return phase
	}
	if mem.phase == "execute" && now.Sub(mem.phaseAt) < desktopTurnMemoryTTL {
		return "execute"
	}
	if desktopHandBackRe.MatchString(content) {
		// The person handed the desktop back for a task already under way.
		return "execute"
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
