package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/factclaim"
)

var sessionFactUnreachableMarkers = []string{
	"unreachable",
	"timed out",
	"no route to host",
	"network is unreachable",
	"host is down",
	"connection refused",
	"connection timed out",
	"100% packet loss",
	"100% loss",
	"destination host unreachable",
	"不通",
	"无法连接",
	"连不上",
	"连接失败",
	"连接超时",
	"主机不可达",
	"不可达",
	"当前不可达",
	"不能通",
	"不可以通",
	"not connected to",
	"not connected",
	"not reachable",
	"cannot connect",
	"can't connect",
	"failed to connect",
	"无法连通",
}

var sessionFactReachableMarkers = []string{
	"0% packet loss",
	"0% loss",
	"bytes from",
	"reply from",
	"connected to",
	"connection established",
	"is alive",
	" is reachable",
	"currently reachable",
	"当前可达",
	"已连通",
	"能通",
	"可以通",
}

// SessionFactPolarity is the reachability polarity of a claim.
type SessionFactPolarity int

const (
	SessionFactPolarityUnknown SessionFactPolarity = iota
	SessionFactPolarityReachable
	SessionFactPolarityUnreachable
)

// ExtractSessionFactFromTool returns a session fact when a tool result updates
// the reachability of a host or IP. Conservative: no IP/host + no polarity means
// no extraction.
func ExtractSessionFactFromTool(name, argsJSON, result string, outcome ToolExecutionOutcome) (SessionFact, bool) {
	if !shouldExtractReachabilityFact(name, argsJSON, result, outcome) {
		return SessionFact{}, false
	}
	entity, aliases, ok := factclaim.BindToolSubject(name, argsJSON, result)
	if !ok && outcome == ToolExecutionOutcomeTimeout {
		entity, aliases, ok = factclaim.BindTimeoutSubject(name, argsJSON)
	}
	if !ok {
		return SessionFact{}, false
	}
	polarity := DetectSessionFactPolarity(result)
	if polarity == SessionFactPolarityUnknown {
		if outcome == ToolExecutionOutcomeTimeout {
			polarity = SessionFactPolarityUnreachable
		} else {
			return SessionFact{}, false
		}
	}
	claim := sessionFactClaimFor(entity, polarity)
	evidence := sessionFactEvidence(name, result, polarity)
	return SessionFact{
		Entity:    entity,
		Predicate: factclaim.PredicateReachability,
		Claim:     claim,
		Evidence:  evidence,
		Aliases:   aliases,
	}, true
}

// ExtractSessionFactFromMemoryContent builds a session fact from a memory-save
// body when the text states a reachability update for an IP or host.
func ExtractSessionFactFromMemoryContent(content string) (SessionFact, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return SessionFact{}, false
	}
	entity := firstNamedEntity(content)
	if entity == "" {
		return SessionFact{}, false
	}
	polarity := DetectSessionFactPolarity(content)
	if polarity == SessionFactPolarityUnknown {
		return SessionFact{}, false
	}
	return SessionFact{
		Entity:    entity,
		Predicate: factclaim.PredicateReachability,
		Claim:     sessionFactClaimFor(entity, polarity),
		Evidence:  "memory save",
	}, true
}

// DetectSessionFactPolarity classifies reachability language. The rightmost
// match wins so "was up, now down" updates classify as unreachable. When two
// markers share an end index, the longer one wins ("不能通" is not "能通").
func DetectSessionFactPolarity(text string) SessionFactPolarity {
	switch factclaim.DetectPolarity(text) {
	case factclaim.PolarityReachable:
		return SessionFactPolarityReachable
	case factclaim.PolarityUnreachable:
		return SessionFactPolarityUnreachable
	default:
		return SessionFactPolarityUnknown
	}
}

// SessionFactEntities extracts canonical ip:/host: keys from text.
func SessionFactEntities(text string) []string {
	return factclaim.Entities(text)
}

func firstNamedEntity(text string) string {
	ents := factclaim.Entities(text)
	for _, e := range ents {
		if strings.HasPrefix(e, "host:") {
			return e
		}
	}
	if len(ents) > 0 {
		return ents[0]
	}
	return ""
}

func shouldExtractReachabilityFact(name, argsJSON, result string, outcome ToolExecutionOutcome) bool {
	if !factclaim.LooksLikeProbeInvocation(name, argsJSON) {
		return false
	}
	if DetectSessionFactPolarity(result) != SessionFactPolarityUnknown {
		return true
	}
	return outcome == ToolExecutionOutcomeTimeout
}

func sessionFactClaimFor(entity string, polarity SessionFactPolarity) string {
	label := displaySessionFactEntity(entity)
	switch polarity {
	case SessionFactPolarityUnreachable:
		return label + " 当前不可达"
	case SessionFactPolarityReachable:
		return label + " 当前可达"
	default:
		return ""
	}
}

func sessionFactEvidence(toolName, text string, polarity SessionFactPolarity) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "tool"
	}
	markers := sessionFactReachableMarkers
	if polarity == SessionFactPolarityUnreachable {
		markers = sessionFactUnreachableMarkers
	}
	lower := strings.ToLower(text)
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx >= 0 {
			return clipRunes(toolName+": "+marker, sessionFactsEvidenceMax)
		}
	}
	return clipRunes(toolName, sessionFactsEvidenceMax)
}
