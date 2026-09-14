package agent

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/factclaim"
)

// SessionFactsMarker is the line-start heading of the spliced system tail that
// carries in-task fact updates for the current agent instance.
const SessionFactsMarker = "[会话事实]"

const (
	sessionFactsMaxLive       = 8
	sessionFactsClaimMaxRunes = 80
	sessionFactsEvidenceMax   = 60
)

// SessionFact is one entity-keyed claim discovered or updated during this
// agent instance. It is the working overlay that must win over conversation
// history and the long-term memory warehouse until the session is reset.
type SessionFact struct {
	Entity string `json:"entity"`
	// Predicate is the aspect being updated (reachability, 是, 地址, 使用, ...).
	// Empty means reachability, matching older overlays.
	Predicate string `json:"predicate,omitempty"`
	Claim     string `json:"claim"`
	Evidence  string `json:"evidence,omitempty"`
	Prior     string `json:"prior,omitempty"`
	// Aliases are extra ip:/host: keys from the same probe (resolved
	// addresses) so warehouse write-back can supersede both forms.
	Aliases []string  `json:"aliases,omitempty"`
	Updated time.Time `json:"updated"`
}

// SessionFactOverlay is the loop-owned set of current-task facts.
type SessionFactOverlay struct {
	Facts []SessionFact `json:"facts,omitempty"`
}

// SessionFactHolder is an optional host callback, same shape as WorkingStateHolder.
type SessionFactHolder interface {
	LoadSessionFacts() *SessionFactOverlay
	SaveSessionFacts(*SessionFactOverlay)
}

// VerifiedFactSink is an optional host hook invoked when a session fact is
// admitted from tool evidence. Hosts should persist the update into memory
// and knowledge stores so the next retrieval does not return the stale claim.
// The in-task overlay already covers the current agent instance.
type VerifiedFactSink interface {
	OnVerifiedSessionFact(fact SessionFact)
}

// NewSessionFactOverlay returns an empty overlay.
func NewSessionFactOverlay() *SessionFactOverlay {
	return &SessionFactOverlay{}
}

// CloneSessionFactOverlay returns a deep copy, or nil.
func CloneSessionFactOverlay(o *SessionFactOverlay) *SessionFactOverlay {
	if o == nil {
		return nil
	}
	cp := *o
	if o.Facts != nil {
		cp.Facts = make([]SessionFact, len(o.Facts))
		for i, fact := range o.Facts {
			cp.Facts[i] = fact
			if fact.Aliases != nil {
				cp.Facts[i].Aliases = append([]string(nil), fact.Aliases...)
			}
		}
	}
	return &cp
}

// Len returns the number of live facts.
func (o *SessionFactOverlay) Len() int {
	if o == nil {
		return 0
	}
	return len(o.Facts)
}

// EnsureSessionFactOverlay returns overlay, creating an empty one when nil.
func EnsureSessionFactOverlay(overlay *SessionFactOverlay) *SessionFactOverlay {
	if overlay != nil {
		return overlay
	}
	return NewSessionFactOverlay()
}

func polarityFromValue(v string) SessionFactPolarity {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "reachable":
		return SessionFactPolarityReachable
	case "unreachable":
		return SessionFactPolarityUnreachable
	default:
		return SessionFactPolarityUnknown
	}
}

func sessionFactSlot(f SessionFact) string {
	p := strings.TrimSpace(f.Predicate)
	if p == "" {
		p = factclaim.PredicateReachability
	}
	return p + "\x00" + f.Entity
}

// ClaimsFromText turns stored or saved text into overlay facts (reachability
// and subject-predicate-object assertions).
func ClaimsFromText(text, evidence string) []SessionFact {
	var out []SessionFact
	for _, c := range factclaim.Extract(text) {
		claim := clipRunes(c.Text, sessionFactsClaimMaxRunes)
		if c.Predicate == factclaim.PredicateReachability {
			if pol := polarityFromValue(c.Value); pol != SessionFactPolarityUnknown {
				claim = sessionFactClaimFor(c.Subject, pol)
			}
		}
		out = append(out, SessionFact{
			Entity:    c.Subject,
			Predicate: c.Predicate,
			Claim:     claim,
			Evidence:  evidence,
			Aliases:   append([]string(nil), c.Aliases...),
		})
	}
	return out
}

// AdmitSessionFact inserts or replaces a fact for the same entity. A new entity
// evicts the oldest fact when the overlay is full. Returns true when the
// overlay changed.
func AdmitSessionFact(overlay *SessionFactOverlay, fact SessionFact) bool {
	if overlay == nil {
		return false
	}
	fact.Entity = strings.TrimSpace(fact.Entity)
	fact.Predicate = strings.TrimSpace(fact.Predicate)
	fact.Claim = strings.TrimSpace(fact.Claim)
	fact.Evidence = strings.TrimSpace(fact.Evidence)
	fact.Prior = strings.TrimSpace(fact.Prior)
	if fact.Entity == "" || fact.Claim == "" {
		return false
	}
	fact.Claim = clipRunes(fact.Claim, sessionFactsClaimMaxRunes)
	fact.Evidence = clipRunes(fact.Evidence, sessionFactsEvidenceMax)
	if fact.Updated.IsZero() {
		fact.Updated = time.Now()
	}
	slot := sessionFactSlot(fact)
	for i, existing := range overlay.Facts {
		if sessionFactSlot(existing) != slot {
			continue
		}
		if existing.Claim == fact.Claim && existing.Evidence == fact.Evidence {
			return false
		}
		if fact.Prior == "" && existing.Claim != fact.Claim {
			fact.Prior = existing.Claim
		}
		kept := append(append([]SessionFact(nil), overlay.Facts[:i]...), overlay.Facts[i+1:]...)
		overlay.Facts = append(kept, fact)
		return true
	}
	overlay.Facts = append(overlay.Facts, fact)
	if len(overlay.Facts) > sessionFactsMaxLive {
		overlay.Facts = append([]SessionFact(nil), overlay.Facts[len(overlay.Facts)-sessionFactsMaxLive:]...)
	}
	return true
}

// LookupSessionFact returns the current claim for entity, if any.
func LookupSessionFact(overlay *SessionFactOverlay, entity string) (SessionFact, bool) {
	if overlay == nil {
		return SessionFact{}, false
	}
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return SessionFact{}, false
	}
	for i := len(overlay.Facts) - 1; i >= 0; i-- {
		if overlay.Facts[i].Entity == entity {
			return overlay.Facts[i], true
		}
	}
	return SessionFact{}, false
}

// RenderSessionFacts formats a non-empty overlay. Empty overlays render nothing.
func RenderSessionFacts(overlay *SessionFactOverlay) string {
	if overlay == nil || len(overlay.Facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(SessionFactsMarker)
	b.WriteByte('\n')
	b.WriteString("以下为当前任务中已核实的最新事实，覆盖历史记录与记忆仓库中的旧结论。\n")
	for _, fact := range overlay.Facts {
		line := "- " + displaySessionFactEntity(fact.Entity) + ": " + fact.Claim
		if fact.Evidence != "" {
			line += "（证据: " + fact.Evidence + "）"
		}
		if fact.Prior != "" && fact.Prior != fact.Claim {
			line += "；旧结论作废: " + fact.Prior
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func displaySessionFactEntity(entity string) string {
	entity = strings.TrimSpace(entity)
	if strings.HasPrefix(entity, "ip:") {
		return strings.TrimSpace(entity[3:])
	}
	if strings.HasPrefix(entity, "host:") {
		return strings.TrimSpace(entity[5:])
	}
	return entity
}

// ApplySessionFactSection splices or strips the session-facts tail on conversation[0].
// attach=false or a nil/empty render deletes a previous section.
func ApplySessionFactSection(conversation []interface{}, overlay *SessionFactOverlay, attach bool) []interface{} {
	if len(conversation) == 0 {
		return conversation
	}
	role, content, kind := systemPromptContent(conversation[0])
	if role != "system" {
		return conversation
	}
	stripped := stripSessionFactsSection(content)
	section := ""
	if attach {
		section = RenderSessionFacts(overlay)
	}
	next := stripped
	if section != "" {
		if strings.TrimSpace(stripped) == "" {
			next = section
		} else {
			next = strings.TrimRight(stripped, "\n") + "\n\n" + section
		}
	}
	conversation[0] = replaceSystemContent(conversation[0], kind, next)
	return conversation
}

// StripSessionFactsFromVisible removes a line-start session-facts block from
// user-visible text. Inline mentions stay.
func StripSessionFactsFromVisible(text string) string {
	if text == "" || !strings.Contains(text, SessionFactsMarker) {
		return text
	}
	return strings.TrimRight(stripSessionFactsSection(text), "\n")
}

func stripSessionFactsSection(content string) string {
	idx := lastLineStartMarker(content, SessionFactsMarker)
	if idx < 0 {
		return content
	}
	return strings.TrimRight(content[:idx], "\n")
}
