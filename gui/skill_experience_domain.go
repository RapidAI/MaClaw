package main

// skill_experience_domain.go partitions self-learned skills into experience
// pools so that coding work and general assistant work accumulate separately.
//
// Task context is already isolated per tab (conversation history and memory are
// keyed by owner). The skill registry is deliberately shared instead, because
// the point of a learned skill is reuse across tasks. Sharing the pool machine
// wide, however, let a skill distilled from a chat ("北京天气") surface as a
// capability inside a coding turn, where it reads like a pending request.
//
// The domain splits that shared pool without giving up accumulation: every
// coding task sees what other coding tasks learned, and nothing else. Skills
// the user deliberately installed (manual, hub, market, github) carry no domain
// and stay visible everywhere.
//
// The boundary is advertising, not access. Filtering applies where an agent is
// told unprompted what it can do:
//
//   - the assistant's system-prompt skill section and knowledge-skill docs
//   - the loop's preferred-skill steering
//   - the coding subagent's task-matched skill selection
//
// Explicit surfaces stay complete on purpose: the settings UI and
// manage_skill(action="list") must show every skill or a user cannot find and
// manage one, and running a skill by name is never blocked by domain. A user
// who asks for a skill gets it; the domain only decides what gets volunteered.
//
// Tool routing (skillExecutorProvider feeding tool.Router / DynamicToolBuilder)
// is deliberately left unfiltered for the same reason. It only names a skill in
// the manage_skill description after that skill BM25-matches the user's current
// message, so it is the user's own words doing the asking, not the pool
// volunteering. Its BM25 index is also process-wide rather than per-owner, so
// filtering there would mean rebuilding the index per turn for no gain.

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// experienceDomainForTrajectoryKind resolves the domain implied by a loop kind
// alone: these kinds are coding work regardless of which tab owns them. Kinds
// that carry no signal ("main", "shared") return the universal domain, leaving
// the decision to the session owner.
func experienceDomainForTrajectoryKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "coding_subagent", "remote_coding_subagent":
		return corelib.SkillDomainCoding
	default:
		return corelib.SkillDomainUniversal
	}
}

// skillExperienceDomainForOwner resolves the domain an agent is working in from
// the session owner. A pure coding workbench tab is coding work even when its
// outer loop kind is the generic "main"; everything else is general assistant
// work.
func (h *IMMessageHandler) skillExperienceDomainForOwner(userID string) string {
	if h != nil && h.isPureCodingWorkbenchSession(userID) {
		return corelib.SkillDomainCoding
	}
	return corelib.SkillDomainGeneral
}

// resolveTrajectoryExperienceDomain combines both signals: an explicitly coding
// loop kind wins, otherwise the owner decides.
func (h *IMMessageHandler) resolveTrajectoryExperienceDomain(kind, userID string) string {
	if domain := experienceDomainForTrajectoryKind(kind); domain != corelib.SkillDomainUniversal {
		return domain
	}
	return h.skillExperienceDomainForOwner(userID)
}

// filterSkillsForExperienceDomain drops skills belonging to a different
// experience pool. An empty agentDomain disables filtering so a call site that
// cannot resolve a domain degrades to today's behavior instead of hiding
// installed capabilities.
func filterSkillsForExperienceDomain(agentDomain string, skills []NLSkillDefinition) []NLSkillDefinition {
	agentDomain = corelib.NormalizeSkillExperienceDomain(agentDomain)
	if agentDomain == corelib.SkillDomainUniversal || len(skills) == 0 {
		return skills
	}
	out := make([]NLSkillDefinition, 0, len(skills))
	var dropped []string
	for _, s := range skills {
		if corelib.SkillVisibleInExperienceDomain(s.ExperienceDomain, agentDomain) {
			out = append(out, s)
			continue
		}
		if len(dropped) < experienceDomainLogSampleSize {
			dropped = append(dropped, s.Name)
		}
	}
	// Hiding a capability is invisible from the outside, so leave a trail: this
	// is the only answer to "why did my skill stop showing up". Logged only when
	// something was actually dropped, which keeps quiet the common case where
	// every skill belongs to the current pool.
	if n := len(skills) - len(out); n > 0 {
		log.Printf("[skill-domain] hid %d/%d skill(s) outside the %q experience pool (e.g. %s)",
			n, len(skills), agentDomain, strings.Join(dropped, ", "))
	}
	return out
}

// experienceDomainLogSampleSize bounds how many skill names a single filter log
// names, so a large cross-pool registry cannot produce an unreadable line.
const experienceDomainLogSampleSize = 3
