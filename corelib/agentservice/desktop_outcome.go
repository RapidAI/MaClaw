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
	if strings.TrimSpace(c.messageMetadata["bot_wake"]) == "1" {
		// A mailbox wake answers teammates; it has no desktop result to
		// prove. Checking it made the bot take ~25 desktop steps of
		// screenshots and then replaced its reply with the judge's gap text
		// (papers 2026-10-30 run_084b5d76).
		return agent.DesktopOutcomeDecision{}
	}
	// The draft can claim a result the screen does not show, so the check
	// reads it only as the answer text: a chat answer is judged on it, a
	// desktop result never is.
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
	if decision, handled := c.reviewDemoSkill(ctx, userText, draft, evidence, history, image); handled {
		return decision
	}
	ok, gap, err := agent.JudgeDesktopOutcomeWithReply(ctx, c.llmCfg, c.httpClient, userText, draft, evidence, image)
	if err != nil {
		// A failed or unparseable check says nothing about the result:
		// keep the worker's reply (seen live: "not json" replaced a
		// delivered docx report with a gap text).
		log.Printf("[desktop-outcome] check_err=%v misses=%d keep=true", err, c.desktopOutcomeMissContinues)
		return agent.DesktopOutcomeDecision{}
	}
	if !ok {
		if strings.TrimSpace(gap) == "" {
			gap = "当前结果还没有对照桌面确认。"
		}
		if c.desktopOutcomeMissContinues >= desktopOutcomeContinueMax {
			if desktopOutcomeDelivered(history) || strings.TrimSpace(c.connectorAsk.Text) != "" {
				// PR-1: the person sees send_to_user (here the connector
				// ask that fills it) and the delivered files, not the draft.
				// Files already reached the person; a gap text in place
				// of the report would hide what was sent.
				log.Printf("[desktop-outcome] replace=skipped delivered=true misses=%d gap=%q", c.desktopOutcomeMissContinues, desktopOutcomeGapForLog(gap))
				return agent.DesktopOutcomeDecision{}
			}
			log.Printf("[desktop-outcome] replace=true misses=%d gap=%q", c.desktopOutcomeMissContinues, desktopOutcomeGapForLog(gap))
			return agent.DesktopOutcomeDecision{Replace: gap}
		}
		c.desktopOutcomeMissContinues++
		log.Printf("[desktop-outcome] continue=true misses=%d gap=%q", c.desktopOutcomeMissContinues, desktopOutcomeGapForLog(gap))
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
	var deliverIndexes []int
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
			if desktopOutcomeDeliverBody(body) {
				deliverIndexes = append(deliverIndexes, len(desktopParts))
			}
			desktopParts = append(desktopParts, formatDesktopOutcomeEvidence("desktop", body))
			continue
		}
		sawSSH = true
		sshParts = append(sshParts, formatDesktopOutcomeEvidence("ssh", body))
	}
	remoteOnly = sawSSH && !sawDesktop
	if len(desktopParts) > 4 {
		// Keep the file delivery even when later checks pushed it out of
		// the window: the judge otherwise reports "not delivered" for a
		// file that is already attached (seen live twice).
		// Every delivery in the run is kept, not only the latest: a later
		// screenshot delivery otherwise hid the document (seen live).
		var delivered []string
		for _, i := range deliverIndexes {
			if i < len(desktopParts)-4 {
				delivered = append(delivered, desktopParts[i])
			}
		}
		if len(delivered) > 8 {
			delivered = delivered[len(delivered)-8:]
		}
		desktopParts = append(delivered, desktopParts[len(desktopParts)-4:]...)
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
	vision := lastScreenVisionBlock(body)
	body = clipDesktopOutcomeEvidence(body, desktopOutcomeEvidenceRunes)
	if vision != "" && !screenVisionBlockKept(body, vision) {
		body = strings.TrimSpace(body) + "\n" + vision
	}
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

func screenVisionBlockKept(body, block string) bool {
	head, tail := screenVisionKeepParts(block)
	if head == "" || !strings.Contains(body, head) {
		return false
	}
	return tail == "" || strings.Contains(body, tail)
}

// screenVisionKeepParts splits the latest reading into an opening and a
// closing slice. The next screenshot often repeats the first control, and
// the click instruction is identical every time, so either slice alone can
// match an older screen that the clip still holds.
func screenVisionKeepParts(block string) (head, tail string) {
	cut := block
	for _, footer := range []string{
		"\napp_run click uses these pixel coordinates.",
		"\nThese pixels describe the page.",
	} {
		if i := strings.Index(cut, footer); i >= 0 {
			cut = cut[:i]
		}
	}
	runes := []rune(strings.TrimSpace(cut))
	if len(runes) <= 96 {
		return string(runes), ""
	}
	return string(runes[:48]), string(runes[len(runes)-48:])
}

func lastScreenVisionBlock(body string) string {
	idx := strings.LastIndex(body, "screen_vision:")
	if idx < 0 {
		return ""
	}
	lines := strings.Split(body[idx:], "\n")
	end := 1
	for ; end < len(lines); end++ {
		line := lines[end]
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "ocr: ") ||
			strings.HasPrefix(line, "app_run click uses these pixel coordinates.") ||
			strings.HasPrefix(line, "These pixels describe the page.") {
			continue
		}
		break
	}
	block := strings.TrimSpace(strings.Join(lines[:end], "\n"))
	runes := []rune(block)
	if len(runes) > 2500 {
		block = string(runes[:2500])
	}
	return block
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

// desktopOutcomeGapForLog keeps the judge's reason short in the log; it says
// why a finished-looking run was sent back, which logs never showed.
func desktopOutcomeGapForLog(gap string) string {
	r := []rune(strings.Join(strings.Fields(gap), " "))
	if len(r) > 100 {
		return string(r[:100]) + "…"
	}
	return string(r)
}

// desktopOutcomeDelivered reports a successful desktop deliver in the run.
func desktopOutcomeDelivered(history []agent.ConversationEntry) bool {
	for _, entry := range history {
		if entry.Role != "tool" {
			continue
		}
		switch strings.TrimSpace(entry.ToolName) {
		case "desktop":
			if desktopOutcomeDeliverBody(strings.TrimSpace(fmt.Sprint(entry.Content))) {
				return true
			}
		case "send_to_user":
			// PR-1's single exit: a sentence already reached the person.
			if strings.TrimSpace(fmt.Sprint(entry.Content)) != "" {
				return true
			}
		}
	}
	return false
}

// desktopOutcomeDeliverBody reports a deliver result. A long result moved to
// a container file keeps the path on the first line and the result's first
// line after it, so "Delivered " can be on the second line.
func desktopOutcomeDeliverBody(body string) bool {
	if strings.HasPrefix(body, "Delivered ") {
		return true
	}
	first, rest, ok := strings.Cut(body, "\n")
	return ok && strings.HasPrefix(strings.TrimSpace(first), "/home/desktop/tasks/") &&
		strings.HasPrefix(strings.TrimSpace(rest), "Delivered ")
}
