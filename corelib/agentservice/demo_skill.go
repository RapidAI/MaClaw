package agentservice

import (
	"context"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/demoskill"
)

// ErrDemoSkillDisabled is returned while MACLAW_DEMO_SKILL is off.
var ErrDemoSkillDisabled = errors.New("demo skill is disabled (MACLAW_DEMO_SKILL)")

// DistillDemoSkill turns a recorded demonstration into a knowledge skill and
// installs it through the normal skill store, so the existing skill matcher
// injects it into the agent loop as guidance. The user may edit it later;
// there is no confirm-only gate.
func (s *Service) DistillDemoSkill(ctx context.Context, p Principal, llm demoskill.LLM, tl demoskill.Timeline) (corelib.NLSkillEntry, error) {
	if !demoskill.Enabled() {
		return corelib.NLSkillEntry{}, ErrDemoSkillDisabled
	}
	sk, err := demoskill.Distill(ctx, llm, tl)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	out, err := s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{sk.Entry(time.Now())}, true)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if len(out) == 0 {
		return corelib.NLSkillEntry{}, errors.New("demo skill was not installed")
	}
	return out[0], nil
}
