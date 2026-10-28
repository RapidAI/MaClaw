package agentservice

// Entity collections a row-backed store persists individually.
const (
	collTenants     = "tenants"
	collUsers       = "users"
	collCredentials = "credentials"
	collUserConfigs = "user_configs"
	collInstances   = "instances"
	collSessions    = "sessions"
	collRuns        = "runs"
	collMessages    = "messages"
	collRunEvents   = "run_events"
	collAudit       = "audit_events"
)

// storeDirty records what one write transaction changed in a MemoryStore.
// It is only non-nil while SQLiteStore (rows format) runs a write closure, so
// plain MemoryStore/FileStore users pay a nil check per mutation.
type storeDirty struct {
	all          bool
	keys         map[string]map[string]struct{} // map collections: upsert or delete by presence
	msgRewrite   map[string]struct{}
	msgAppend    map[string]int
	evRewrite    map[string]struct{}
	evInsert     map[string][]uint64
	auditRewrite bool
	auditAppend  int
}

func newStoreDirty() *storeDirty {
	return &storeDirty{
		keys:       map[string]map[string]struct{}{},
		msgRewrite: map[string]struct{}{},
		msgAppend:  map[string]int{},
		evRewrite:  map[string]struct{}{},
		evInsert:   map[string][]uint64{},
	}
}

func (d *storeDirty) empty() bool {
	return !d.all && len(d.keys) == 0 && len(d.msgRewrite) == 0 && len(d.msgAppend) == 0 &&
		len(d.evRewrite) == 0 && len(d.evInsert) == 0 && !d.auditRewrite && d.auditAppend == 0
}

func (s *MemoryStore) touch(coll, key string) {
	if s.dirty == nil {
		return
	}
	m := s.dirty.keys[coll]
	if m == nil {
		m = map[string]struct{}{}
		s.dirty.keys[coll] = m
	}
	m[key] = struct{}{}
}

func (s *MemoryStore) touchMessagesRewrite(sessionID string) {
	if s.dirty == nil {
		return
	}
	s.dirty.msgRewrite[sessionID] = struct{}{}
	delete(s.dirty.msgAppend, sessionID)
}

func (s *MemoryStore) touchMessagesAppend(sessionID string) {
	if s.dirty == nil {
		return
	}
	if _, ok := s.dirty.msgRewrite[sessionID]; ok {
		return
	}
	s.dirty.msgAppend[sessionID]++
}

func (s *MemoryStore) touchRunEventsRewrite(scope string) {
	if s.dirty == nil {
		return
	}
	s.dirty.evRewrite[scope] = struct{}{}
	delete(s.dirty.evInsert, scope)
}

func (s *MemoryStore) touchRunEventInsert(scope string, seq uint64) {
	if s.dirty == nil {
		return
	}
	if _, ok := s.dirty.evRewrite[scope]; ok {
		return
	}
	s.dirty.evInsert[scope] = append(s.dirty.evInsert[scope], seq)
}

func (s *MemoryStore) touchAuditAppend() {
	if s.dirty == nil || s.dirty.auditRewrite {
		return
	}
	s.dirty.auditAppend++
}

func (s *MemoryStore) touchAuditRewrite() {
	if s.dirty == nil {
		return
	}
	s.dirty.auditRewrite = true
	s.dirty.auditAppend = 0
}

func (s *MemoryStore) touchAll() {
	if s.dirty == nil {
		return
	}
	s.dirty.all = true
}
