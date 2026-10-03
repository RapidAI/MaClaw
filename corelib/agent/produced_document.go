package agent

import (
	"strings"
	"time"
)

// PersistedProducedDocument is the exact file a desktop task materialized.
// The path stays with the host. A later delivery re-reads the bytes and
// refuses a changed or missing file. This is not a model argument and not
// permission to send the file anywhere.
type PersistedProducedDocument struct {
	CanonicalPath string `json:"canonical_path,omitempty"`
	Format        string `json:"format,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	Size          int64  `json:"size,omitempty"`
	ModTimeNS     int64  `json:"mod_time_ns,omitempty"`
	Digest        string `json:"digest,omitempty"`
}

func clonePersistedProducedDocument(in *PersistedProducedDocument) *PersistedProducedDocument {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func producedDocumentUsable(doc PersistedProducedDocument) bool {
	return strings.TrimSpace(doc.CanonicalPath) != "" && len(strings.TrimSpace(doc.Digest)) >= 24
}

// ProducedDocument returns the restart-durable file this session materialized.
func (cm *ConversationMemory) ProducedDocument(userID string) (PersistedProducedDocument, bool) {
	if cm == nil {
		return PersistedProducedDocument{}, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return PersistedProducedDocument{}, false
	}
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	session := sh.sessions[userID]
	if session == nil || session.producedDocument == nil || !producedDocumentUsable(*session.producedDocument) {
		return PersistedProducedDocument{}, false
	}
	return *clonePersistedProducedDocument(session.producedDocument), true
}

// SetProducedDocument stores the exact file this session materialized.
func (cm *ConversationMemory) SetProducedDocument(userID string, doc PersistedProducedDocument) {
	if cm == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	doc.CanonicalPath = strings.TrimSpace(doc.CanonicalPath)
	doc.Digest = strings.TrimSpace(doc.Digest)
	if userID == "" || !producedDocumentUsable(doc) {
		return
	}
	sh := cm.shard(userID)
	sh.mu.Lock()
	session := sh.sessions[userID]
	if session != nil && session.producedDocument != nil && *session.producedDocument == doc {
		sh.mu.Unlock()
		return
	}
	if session == nil {
		session = &conversationSession{lastAccess: time.Now()}
		sh.sessions[userID] = session
	}
	cloned := doc
	session.producedDocument = &cloned
	sh.mu.Unlock()
	cm.markDirtyAndScheduleFlush()
}

// ClearProducedDocument drops the persisted file snapshot.
func (cm *ConversationMemory) ClearProducedDocument(userID string) {
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
	cleared := session != nil && session.producedDocument != nil
	if cleared {
		session.producedDocument = nil
	}
	sh.mu.Unlock()
	if cleared {
		cm.markDirtyAndScheduleFlush()
	}
}
