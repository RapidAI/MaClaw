package agent

import (
	"strings"
	"time"
)

// SemanticSessionResidue is the capability remainder of one desktop
// conversation. It is stored with the chat history. It is not a grant and
// contains no tool names, arguments, or route revisions.
type SemanticSessionResidue struct {
	Generation  uint64                       `json:"generation,omitempty"`
	Status      string                       `json:"status,omitempty"`
	Summary     string                       `json:"summary,omitempty"`
	LookupFacts bool                         `json:"lookup_facts,omitempty"`
	Needs       []SemanticSessionResidueNeed `json:"needs,omitempty"`
	Remaining   map[string]int               `json:"remaining,omitempty"`
}

// SemanticSessionResidueNeed is one planner-granted capability.
type SemanticSessionResidueNeed struct {
	ID         string            `json:"id,omitempty"`
	Capability string            `json:"capability,omitempty"`
	Required   bool              `json:"required,omitempty"`
	Qualifiers map[string]string `json:"qualifiers,omitempty"`
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
