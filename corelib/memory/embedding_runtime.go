package memory

import "github.com/RapidAI/CodeClaw/corelib/embedding"

// RuntimeEmbedderForHost returns the currently active non-noop embedder for
// host runtime wiring. Hosts use this instead of reaching into Store.Embedder
// so the active/noop policy stays owned by corelib/memory.
type RuntimeEmbedderStatus struct {
	Active          bool
	Dim             int
	TotalEntries    int
	EmbeddedEntries int
}

// SetRuntimeEmbedderForHost wires the host-provided embedder into the memory
// store. Keeping this runtime hook in corelib/memory makes host bootstraps use
// the same embedder policy surface as status and read-side wiring.
func (s *Store) SetRuntimeEmbedderForHost(emb embedding.Embedder) {
	if s == nil {
		return
	}
	s.SetEmbedder(emb)
}
func (s *Store) RuntimeEmbedderForHost() (embedding.Embedder, bool) {
	if s == nil {
		return nil, false
	}
	emb := s.Embedder()
	if emb == nil || embedding.IsNoop(emb) {
		return nil, false
	}
	return emb, true
}

// RuntimeEmbedderStatusForHost returns the runtime vector-search projection for
// host UI/status surfaces. It keeps active/noop and entry-count policy in
// corelib/memory instead of duplicating it in host adapters.
func (s *Store) RuntimeEmbedderStatusForHost() RuntimeEmbedderStatus {
	if s == nil {
		return RuntimeEmbedderStatus{}
	}
	embedStatus := s.EmbedStatusForTool()
	status := RuntimeEmbedderStatus{
		TotalEntries:    embedStatus.TotalEntries,
		EmbeddedEntries: embedStatus.WithEmbeddings,
	}
	if emb, ok := s.RuntimeEmbedderForHost(); ok {
		status.Active = true
		status.Dim = emb.Dim()
	}
	return status
}
