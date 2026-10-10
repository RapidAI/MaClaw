package agentservice

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// desktopOutcomeEvidenceRunes is one tool result in the check. It stays above
// the longest container bash transcript: the 32KiB tail, the runtime
// "[truncated]" mark, and the exit line. A finished command is judged as the
// model saw it. A longer file read still keeps its start and its exit. The
// omitted middle is marked, so the check does not treat that cut as the
// command's own output.
const desktopOutcomeEvidenceRunes = desktop.BashOutputMax + 64

// An execute-phase desktop bot may finish only when a separate check says the
// tool results and the latest screenshot meet the request. Two failed checks
// keep the same turn going. The next one replaces the draft with what is
// still missing. A login or verification handoff is the person's step.
const desktopOutcomeContinueMax = 2

func (c *coreAgentCallbacks) ReviewDesktopOutcome(userText, draft string, history []agent.ConversationEntry, image *agent.ToolModelImage) agent.DesktopOutcomeDecision {
	if c == nil || strings.TrimSpace(c.messageMetadata["bot_phase"]) != "execute" {
		return agent.DesktopOutcomeDecision{}
	}
	// The draft is the report that can claim a result the screen does not
	// show. The check never reads it.
	_ = draft
	used, ask, remoteOnly, sawSSH, evidence := desktopOutcomeEvidence(history)
	if ask {
		return agent.DesktopOutcomeDecision{}
	}
	if !used {
		if c.desktopOutcomeIdleContinues >= desktopOutcomeContinueMax {
			return agent.DesktopOutcomeDecision{}
		}
		c.desktopOutcomeIdleContinues++
		log.Printf("[desktop-outcome] continue=true idle=%d evidence=none", c.desktopOutcomeIdleContinues)
		return agent.DesktopOutcomeDecision{Continue: true, Message: desktopOutcomeNudge("这一步还没有桌面工具结果，无法确认要求已经达成。", false, false)}
	}
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ok, gap, err := agent.JudgeDesktopOutcome(ctx, c.llmCfg, c.httpClient, userText, evidence, image)
	if err != nil {
		log.Printf("[desktop-outcome] check_err=%v misses=%d", err, c.desktopOutcomeMissContinues)
		gap = ""
	}
	if err != nil || !ok {
		if strings.TrimSpace(gap) == "" {
			gap = "当前结果还没有对照桌面确认。"
		}
		if c.desktopOutcomeMissContinues >= desktopOutcomeContinueMax {
			log.Printf("[desktop-outcome] replace=true misses=%d", c.desktopOutcomeMissContinues)
			return agent.DesktopOutcomeDecision{Replace: gap}
		}
		c.desktopOutcomeMissContinues++
		log.Printf("[desktop-outcome] continue=true misses=%d", c.desktopOutcomeMissContinues)
		return agent.DesktopOutcomeDecision{Continue: true, Message: desktopOutcomeNudge(gap, sawSSH, remoteOnly)}
	}
	log.Printf("[desktop-outcome] ok=true misses=%d", c.desktopOutcomeMissContinues)
	return agent.DesktopOutcomeDecision{}
}

func desktopOutcomeNudge(gap string, sawSSH, remoteOnly bool) string {
	follow := "继续在这个桌面上改，直到工具结果或新的桌面截图能够证明要求已经达成。"
	if remoteOnly {
		follow = "继续用 ssh 工具在那台远程机器上改，直到 ssh 的工具结果能够证明要求已经达成。"
	} else if sawSSH {
		follow = "桌面上的要求继续在这个桌面上改，直到工具结果或新的桌面截图能够证明已经达成。远程机器上的要求继续用 ssh 工具改，直到 ssh 的工具结果能够证明已经达成。桌面截图不能用来判断远程命令。"
	}
	return "[系统] 这一步的结果还没有满足用户要求：" + gap + "。" + follow + "不要把未确认的结果发给用户，也不要让用户自己检查。"
}

func desktopOutcomeEvidence(history []agent.ConversationEntry) (used, ask, remoteOnly, sawSSH bool, text string) {
	var desktopParts []string
	var sshParts []string
	sawDesktop := false
	for _, entry := range history {
		if entry.Role != "tool" {
			continue
		}
		name := strings.TrimSpace(entry.ToolName)
		if name != "desktop" && name != "ssh" {
			continue
		}
		used = true
		body := strings.TrimSpace(fmt.Sprint(entry.Content))
		if name == "desktop" {
			sawDesktop = true
			if agent.IsAskUserResult(body) {
				ask = true
				continue
			}
			desktopParts = append(desktopParts, formatDesktopOutcomeEvidence("desktop", body))
			continue
		}
		sawSSH = true
		sshParts = append(sshParts, formatDesktopOutcomeEvidence("ssh", body))
	}
	remoteOnly = sawSSH && !sawDesktop
	if len(desktopParts) > 4 {
		desktopParts = desktopParts[len(desktopParts)-4:]
	}
	if len(sshParts) > 2 {
		sshParts = sshParts[len(sshParts)-2:]
	}
	parts := append(desktopParts, sshParts...)
	return used, ask, remoteOnly, sawSSH, strings.Join(parts, "\n")
}

// formatDesktopOutcomeEvidence marks one tool result. Lines after the first
// stay indented, so a command transcript cannot look like a second tool.
func formatDesktopOutcomeEvidence(name, body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = clipDesktopOutcomeEvidence(body, desktopOutcomeEvidenceRunes)
	if body == "" {
		return name + ": "
	}
	lines := strings.Split(body, "\n")
	lines[0] = name + ": " + lines[0]
	for i := 1; i < len(lines); i++ {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// clipDesktopOutcomeEvidence keeps a command whole up to limit. Past that it
// keeps the start and the end, because a command puts the exit and the error
// on the last lines. The joined middle is named, so it cannot be read as the
// command's own omission.
func clipDesktopOutcomeEvidence(text string, limit int) string {
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	mark := []rune("\n" + agent.DesktopOutcomeEvidenceOmission + "\n")
	budget := limit - len(mark)
	if budget < 2 {
		return string(runes[:limit])
	}
	head := budget / 3
	if head < 1 {
		head = 1
	}
	tail := budget - head
	if tail < 1 {
		return string(runes[:limit])
	}
	return string(runes[:head]) + string(mark) + string(runes[len(runes)-tail:])
}
