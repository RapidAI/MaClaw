package knowledge

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// recordingRoleEmbedder captures the task role every embedding call was made
// under. It implements embedding.RoleEmbedder, so EmbedAs / EmbedBatchAs route
// through the role-aware methods instead of the plain fallback.
//
// The knowledge pipeline itself never concatenates a prompt — that happens
// inside GemmaEmbedder — so asserting on the recorded role is exactly the
// contract the call sites own.
type recordingRoleEmbedder struct {
	mu    sync.Mutex
	roles []embedding.Role
	texts []string
	// modelID overrides the reported identity. Tests that need a second,
	// incompatible embedder set it so stored vectors become stale.
	modelID string
}

func (e *recordingRoleEmbedder) record(role embedding.Role, texts ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.roles = append(e.roles, role)
	e.texts = append(e.texts, texts...)
}

func (e *recordingRoleEmbedder) snapshot() []embedding.Role {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]embedding.Role(nil), e.roles...)
}

func (e *recordingRoleEmbedder) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.roles = nil
	e.texts = nil
}

func (e *recordingRoleEmbedder) sawRole(role embedding.Role) bool {
	for _, got := range e.snapshot() {
		if got == role {
			return true
		}
	}
	return false
}

func (e *recordingRoleEmbedder) EmbedWithRole(text string, role embedding.Role) ([]float32, error) {
	e.record(role, text)
	return []float32{1, 0}, nil
}

func (e *recordingRoleEmbedder) EmbedBatchWithRole(texts []string, role embedding.Role) ([][]float32, error) {
	e.record(role, texts...)
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

// Plain Embedder surface, kept so the type also satisfies embedding.Embedder.
func (e *recordingRoleEmbedder) Embed(text string) ([]float32, error) {
	e.record(embedding.RoleNone, text)
	return []float32{1, 0}, nil
}

func (e *recordingRoleEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.record(embedding.RoleNone, texts...)
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{1, 0}
	}
	return vectors, nil
}

func (*recordingRoleEmbedder) Dim() int { return 2 }
func (*recordingRoleEmbedder) Close()   {}

func (e *recordingRoleEmbedder) ModelID() string {
	if e.modelID != "" {
		return e.modelID
	}
	return "recording-role-embedder:2"
}

// newRoleRecordingStore opens a fresh store wired to a role-recording embedder.
func newRoleRecordingStore(t *testing.T) (*SQLiteStore, *recordingRoleEmbedder) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rec := &recordingRoleEmbedder{}
	store.SetEmbedder(rec)
	return store, rec
}

// TestAllQuerySitesUseQueryRole pins every query-side call site at once.
//
// All three must embed under RoleQuery, because the rows they are compared
// against were embedded under RoleDocument. One site left on the plain Embed
// silently compares across two spaces — and a cosine still comes back, so
// nothing looks broken. Covering the sites individually (rather than trusting
// one representative) is the point: they live in three different files.
func TestAllQuerySitesUseQueryRole(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		query func(*SQLiteStore) error
	}{
		{
			name: "vector_search.searchByEmbedding",
			query: func(s *SQLiteStore) error {
				_, err := s.searchByEmbedding(ctx, SearchOptions{Query: "如何接入官方 prompt", Limit: 5})
				return err
			},
		},
		{
			name: "structured_search.searchTableRowsByEmbedding",
			query: func(s *SQLiteStore) error {
				_, err := s.searchTableRowsByEmbedding(ctx, StructuredSearchOptions{Query: "季度营收"}, 5)
				return err
			},
		},
		{
			name: "image_search.SearchImages",
			query: func(s *SQLiteStore) error {
				// An English query keeps bm25.ShortEntityMention false, so the
				// lexical-anchor shortcut does not skip the embedding branch.
				_, err := s.SearchImages(ctx, ImageSearchOptions{
					SearchOptions: SearchOptions{Query: "sunset over the harbour", Limit: 5},
				})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, rec := newRoleRecordingStore(t)
			rec.reset()
			if err := tc.query(store); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !rec.sawRole(embedding.RoleQuery) {
				t.Fatalf("%s embedded the query under %v, want RoleQuery", tc.name, rec.snapshot())
			}
			if rec.sawRole(embedding.RoleDocument) {
				t.Fatalf("%s embedded the query under RoleDocument: %v", tc.name, rec.snapshot())
			}
			assertNoRolelessEmbedding(t, rec, tc.name)
		})
	}
}

// TestKnowledgeRetrievalUsesRolePrompts covers the retrieval entry point end to
// end: a stored document must be reachable by a query, and the query half must
// be embedded under RoleQuery while the stored half used RoleDocument.
func TestKnowledgeRetrievalUsesRolePrompts(t *testing.T) {
	ctx := context.Background()
	store, rec := newRoleRecordingStore(t)

	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "角色前缀", Text: "检索链路必须按角色走 prompt。"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	store.WaitBackground()

	rec.reset()
	if _, err := store.searchByEmbedding(ctx, SearchOptions{Query: "如何接入官方 prompt", Limit: 5}); err != nil {
		t.Fatalf("searchByEmbedding: %v", err)
	}
	if !rec.sawRole(embedding.RoleQuery) {
		t.Fatalf("searchByEmbedding embedded the query under %v, want RoleQuery", rec.snapshot())
	}
	if rec.sawRole(embedding.RoleDocument) {
		t.Fatalf("searchByEmbedding embedded the query under RoleDocument: %v", rec.snapshot())
	}
	assertNoRolelessEmbedding(t, rec, "retrieval")
}

// assertNoRolelessEmbedding fails when any call reached the embedder without a
// task role. That is the exact fingerprint of a call site that forgot to pass
// one: EmbedAs silently falls back to the plain Embedder surface, the cosine
// still returns a number, and the only symptom is degraded ranking. Asserting
// "some call used RoleDocument" is not enough — it is satisfied by any one
// correct site while another stays roleless.
func assertNoRolelessEmbedding(t *testing.T, rec *recordingRoleEmbedder, phase string) {
	t.Helper()
	if rec.sawRole(embedding.RoleNone) {
		t.Fatalf("%s hit a call site that supplied no role: %v", phase, rec.snapshot())
	}
}

// TestKnowledgeIndexingUsesDocumentRole pins the indexing side: card/passage
// text stored for later retrieval must be embedded under RoleDocument so it
// lands in the same space the RoleQuery vectors are compared against.
//
// Note this does NOT assert "indexing uses RoleDocument only". Saving a source
// also runs topic linking (text.go -> refreshSourceTopicLinksFast), which uses
// the new source's *title* as a query against the already-indexed corpus and so
// legitimately embeds under RoleQuery. That pair is matched — a RoleQuery probe
// against RoleDocument documents — and asserting the absence of RoleQuery here
// would be wrong.
func TestKnowledgeIndexingUsesDocumentRole(t *testing.T) {
	ctx := context.Background()
	store, rec := newRoleRecordingStore(t)

	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "文档侧", Text: "入库文本应当使用 RoleDocument。"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	store.WaitBackground()

	if !rec.sawRole(embedding.RoleDocument) {
		t.Fatalf("indexing embedded stored text under %v, want RoleDocument", rec.snapshot())
	}
	assertNoRolelessEmbedding(t, rec, "indexing")
}

// TestEmbedderSwitchBackfillUsesDocumentRole pins the migration path.
//
// Switching the embedder rewrites every stored vector, and that rewrite must land
// in the document space. This is the one path where a missing role is not merely
// a ranking regression: it would rewrite the whole index into the query space,
// so every later search compares query vectors against query vectors — the
// ModelID filter would happily certify the result as consistent.
//
// A second embedder with a different ModelID is what makes every stored vector
// stale and therefore launches the backfill.
func TestEmbedderSwitchBackfillUsesDocumentRole(t *testing.T) {
	ctx := context.Background()
	store, _ := newRoleRecordingStore(t)

	if _, err := store.SaveText(ctx, TextSaveRequest{Title: "迁移", Text: "换模型后必须按 RoleDocument 重写向量。"}); err != nil {
		t.Fatalf("SaveText: %v", err)
	}
	store.WaitBackground()

	replacement := &recordingRoleEmbedder{modelID: "recording-role-embedder:2:replacement"}
	store.SetEmbedder(replacement)
	store.WaitBackground()

	if !replacement.sawRole(embedding.RoleDocument) {
		t.Fatalf("embedder switch rewrote vectors under %v, want RoleDocument", replacement.snapshot())
	}
	assertNoRolelessEmbedding(t, replacement, "embedder-switch backfill")
}
