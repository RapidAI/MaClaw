package agent

import (
	"strings"
	"time"
)

// SemanticSessionResidue is the capability remainder of one desktop
// conversation. It is stored with the chat history. It is not a grant and
// contains no tool names, arguments, or route revisions.
type SemanticSessionResidue struct {
	Generation  uint64 `json:"generation,omitempty"`
	Status      string `json:"status,omitempty"`
	Summary     string `json:"summary,omitempty"`
	LookupFacts bool   `json:"lookup_facts,omitempty"`
	// PlanClosed is set when this turn's plan hit its ceiling. The next
	// turn reads it instead of the assistant's wording.
	PlanClosed bool                         `json:"plan_closed,omitempty"`
	Needs      []SemanticSessionResidueNeed `json:"needs,omitempty"`
	Remaining  map[string]int               `json:"remaining,omitempty"`
}

// SemanticSessionResidueNeed is one planner-granted capability.
type SemanticSessionResidueNeed struct {
	ID         string            `json:"id,omitempty"`
	Capability string            `json:"capability,omitempty"`
	Required   bool              `json:"required,omitempty"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
	// EvidenceIDs records why the planner granted the need. Baseline and
	// archetype companions are not the task; the id prefix alone does not
	// mark every companion, and dropping the evidence made them look like
	// the obligation after a restart.
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

func (cm *ConversationMemory) SemanticSessionResidue(userID string) (SemanticSessionResidue, bool) {
	if cm == nil {
		return SemanticSessionResidue{}, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return SemanticSessionResidue{}, false
	}
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	session := sh.sessions[userID]
	if session == nil || session.semanticResidue == nil {
		return SemanticSessionResidue{}, false
	}
	return cloneSemanticSessionResidue(*session.semanticResidue), true
}

func (cm *ConversationMemory) SetSemanticSessionResidue(userID string, residue SemanticSessionResidue) {
	if cm == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	cloned := cloneSemanticSessionResidue(residue)
	sh := cm.shard(userID)
	sh.mu.Lock()
	session := sh.sessions[userID]
	if session == nil {
		session = &conversationSession{lastAccess: time.Now()}
		sh.sessions[userID] = session
	}
	session.semanticResidue = &cloned
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// ParentExecutionTools returns the previous full turn's non-light tool
// names. known is false when this session has never recorded a decision.
// known with an empty slice means the last turn cleared the carry.
func (cm *ConversationMemory) ParentExecutionTools(userID string) (names []string, known bool) {
	if cm == nil {
		return nil, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, false
	}
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	session := sh.sessions[userID]
	if session == nil || !session.parentExecutionKnown {
		return nil, false
	}
	return cloneParentExecutionTools(session.parentExecutionTools), true
}

// SetParentExecutionTools stores non-light tool names so a later process can
// restore a short continuation. Names are not grants and have no arguments.
func (cm *ConversationMemory) SetParentExecutionTools(userID string, names []string) {
	if cm == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	cloned := cloneParentExecutionTools(names)
	if len(cloned) == 0 {
		cm.ClearParentExecutionTools(userID)
		return
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	session := sh.sessions[userID]
	if session != nil && session.parentExecutionKnown && sameParentExecutionTools(session.parentExecutionTools, cloned) {
		sh.mu.Unlock()
		return
	}
	if session == nil {
		session = &conversationSession{lastAccess: time.Now()}
		sh.sessions[userID] = session
	}
	session.parentExecutionTools = cloned
	session.parentExecutionKnown = true
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// ClearParentExecutionTools drops a persisted parent tool list.
func (cm *ConversationMemory) ClearParentExecutionTools(userID string) {
	if cm == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	session := sh.sessions[userID]
	if session == nil || (session.parentExecutionKnown && len(session.parentExecutionTools) == 0) {
		sh.mu.Unlock()
		return
	}
	session.parentExecutionTools = nil
	session.parentExecutionKnown = true
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

func sameParentExecutionTools(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func cloneParentExecutionTools(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		name = strings.TrimSpace(name)
		if !parentExecutionToolNameOK(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
		if len(out) >= 32 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parentExecutionToolNameOK(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func (cm *ConversationMemory) ClearSemanticSessionResidue(userID string) {
	if cm == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	session := sh.sessions[userID]
	cleared := session != nil && session.semanticResidue != nil
	if cleared {
		session.semanticResidue = nil
	}
	sh.mu.Unlock()
	if cleared {
		cm.markDirtyAndScheduleFlush()
	}
}

func clonePersistedSemanticResidue(in *SemanticSessionResidue) *SemanticSessionResidue {
	if in == nil {
		return nil
	}
	cloned := cloneSemanticSessionResidue(*in)
	return &cloned
}

func cloneSemanticSessionResidue(in SemanticSessionResidue) SemanticSessionResidue {
	out := in
	if len(in.Needs) > 0 {
		out.Needs = make([]SemanticSessionResidueNeed, len(in.Needs))
		for i, need := range in.Needs {
			out.Needs[i] = need
			out.Needs[i].Qualifiers = cloneStringMap(need.Qualifiers)
			if len(need.EvidenceIDs) > 0 {
				out.Needs[i].EvidenceIDs = append([]string(nil), need.EvidenceIDs...)
			}
		}
	}
	out.Remaining = cloneIntMap(in.Remaining)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
