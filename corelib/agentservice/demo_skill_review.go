package agentservice

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/demoskill"
)

// demoSkillContinueMax bounds the extra repair rounds a demo skill run gets
// from its per-step checks. It is larger than the generic budget because a
// demonstrated task must reach its deliverable, but still finite.
const demoSkillContinueMax = 6

// reviewDemoSkill runs a loaded demo skill's per-step checks and its
// deliverable check through the existing result judge. handled=false leaves
// the decision to the generic check (no demo skill, flag off, budget spent).
func (c *coreAgentCallbacks) reviewDemoSkill(ctx context.Context, userText, draft, evidence string, history []agent.ConversationEntry, image *agent.ToolModelImage) (agent.DesktopOutcomeDecision, bool) {
	if !demoskill.Enabled() {
		return agent.DesktopOutcomeDecision{}, false
	}
	if c.demoProgress == nil {
		sk := demoSkillFromHistory(history)
		if sk == nil {
			return agent.DesktopOutcomeDecision{}, false
		}
		c.demoProgress = demoskill.NewProgress(sk)
	}
	if c.demoContinues >= demoSkillContinueMax {
		return agent.DesktopOutcomeDecision{}, false
	}
	judge := func(requirement string) (bool, string, error) {
		req := strings.TrimSpace(userText) + "\n演示技能要求：" + requirement
		return agent.JudgeDesktopOutcomeWithReply(ctx, c.llmCfg, c.httpClient, req, draft, evidence, image)
	}
	cont, directive := c.demoProgress.Review(judge)
	if cont {
		c.demoContinues++
		log.Printf("[demo-skill] continue=true round=%d skill=%q", c.demoContinues, c.demoProgress.Skill.Name)
		return agent.DesktopOutcomeDecision{Continue: true, Message: fmt.Sprintf("[系统] %s 继续推进，直到交付物有证据。", directive)}, true
	}
	if c.demoProgress.Complete() {
		log.Printf("[demo-skill] delivered skill=%q", c.demoProgress.Skill.Name)
		return agent.DesktopOutcomeDecision{}, true
	}
	return agent.DesktopOutcomeDecision{}, false
}

func demoSkillFromHistory(history []agent.ConversationEntry) *demoskill.Skill {
	for i := len(history) - 1; i >= 0; i-- {
		body := fmt.Sprint(history[i].Content)
		if !strings.Contains(body, demoskill.SpecMarker) {
			continue
		}
		if sk := demoskill.FindSpec(body); sk != nil {
			return sk
		}
	}
	return nil
}
