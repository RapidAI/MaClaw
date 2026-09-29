package embedding

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// withPrompts pins the process-wide prompt switch for the duration of a test.
func withPrompts(t *testing.T, on bool) {
	t.Helper()
	previous := PromptsEnabled()
	SetPromptsEnabled(on)
	t.Cleanup(func() { SetPromptsEnabled(previous) })
}

func TestPromptPrefixMatchesModelCard(t *testing.T) {
	cases := []struct {
		role Role
		want string
	}{
		{RoleQuery, "task: search result | query: "},
		{RoleDocument, "title: none | text: "},
		{RoleSimilarity, "task: sentence similarity | query: "},
		{RoleClassification, "task: classification | query: "},
		{RoleClustering, "task: clustering | query: "},
		{RoleCodeRetrieval, "task: code retrieval | query: "},
		{RoleQuestionAnswering, "task: question answering | query: "},
		{RoleFactChecking, "task: fact checking | query: "},
		{RoleNone, ""},
	}
	for _, tc := range cases {
		if got := PromptPrefix(tc.role); got != tc.want {
			t.Errorf("PromptPrefix(%d) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestApplyPromptPrependsTemplate(t *testing.T) {
	withPrompts(t, true)
	if got, want := ApplyPrompt("杭州天气", RoleQuery),
		"task: search result | query: 杭州天气"; got != want {
		t.Errorf("ApplyPrompt query = %q, want %q", got, want)
	}
	if got, want := ApplyPrompt("hello", RoleSimilarity),
		"task: sentence similarity | query: hello"; got != want {
		t.Errorf("ApplyPrompt similarity = %q, want %q", got, want)
	}
}

func TestApplyPromptRoleNoneIsIdentity(t *testing.T) {
	withPrompts(t, true)
	if got := ApplyPrompt("keep me as-is", RoleNone); got != "keep me as-is" {
		t.Errorf("RoleNone rewrote text: %q", got)
	}
}

func TestApplyPromptDisabledIsIdentity(t *testing.T) {
	withPrompts(t, false)
	for _, role := range []Role{RoleQuery, RoleDocument, RoleSimilarity, RoleClassification} {
		if got := ApplyPrompt("unchanged", role); got != "unchanged" {
			t.Errorf("role %d rewrote text while prompts disabled: %q", role, got)
		}
	}
}

// allRoles lists every role so property tests cannot silently skip a new one.
func allRoles() []Role {
	return []Role{
		RoleNone, RoleQuery, RoleDocument, RoleSimilarity, RoleClassification,
		RoleClustering, RoleCodeRetrieval, RoleQuestionAnswering, RoleFactChecking,
	}
}

// The batch fast path is only safe while NeedsPrompt agrees with ApplyPrompt.
// If the two ever disagree, a caller that trusts NeedsPrompt would skip the
// prefix and silently embed into the un-prompted space — the exact failure this
// feature exists to prevent.
func TestNeedsPromptMatchesApplyPrompt(t *testing.T) {
	for _, on := range []bool{true, false} {
		withPrompts(t, on)
		for _, role := range allRoles() {
			changed := ApplyPrompt("probe", role) != "probe"
			if got := NeedsPrompt(role); got != changed {
				t.Errorf("prompts=%v role=%d: NeedsPrompt=%v but ApplyPrompt changed the text=%v",
					on, role, got, changed)
			}
		}
	}
}

// legacyEmbedder implements only the base Embedder, like every test double and
// remote embedding service in the tree.
type legacyEmbedder struct{ seen []string }

func (l *legacyEmbedder) Embed(text string) ([]float32, error) {
	l.seen = append(l.seen, text)
	return []float32{1}, nil
}

func (l *legacyEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	l.seen = append(l.seen, texts...)
	return make([][]float32, len(texts)), nil
}

func (*legacyEmbedder) Dim() int { return 1 }
func (*legacyEmbedder) Close()   {}

func TestEmbedAsFallsBackForPlainEmbedders(t *testing.T) {
	withPrompts(t, true)
	legacy := &legacyEmbedder{}
	if _, err := EmbedAs(legacy, "raw text", RoleQuery); err != nil {
		t.Fatalf("EmbedAs: %v", err)
	}
	if len(legacy.seen) != 1 || legacy.seen[0] != "raw text" {
		t.Fatalf("fallback should pass text through untouched, saw %q", legacy.seen)
	}
	if _, err := EmbedBatchAs(legacy, []string{"a", "b"}, RoleDocument); err != nil {
		t.Fatalf("EmbedBatchAs: %v", err)
	}
	if len(legacy.seen) != 3 {
		t.Fatalf("EmbedBatchAs should forward both texts, saw %q", legacy.seen)
	}
}

func TestEmbedAsIsNilSafe(t *testing.T) {
	if vec, err := EmbedAs(nil, "x", RoleQuery); vec != nil || err != nil {
		t.Fatalf("EmbedAs(nil) = %v, %v; want nil, nil", vec, err)
	}
	if vecs, err := EmbedBatchAs(NoopEmbedder{}, []string{"x"}, RoleQuery); vecs != nil || err != nil {
		t.Fatalf("EmbedBatchAs(noop) = %v, %v; want nil, nil", vecs, err)
	}
}

func TestGemmaEmbedderImplementsRoleEmbedder(t *testing.T) {
	// Compile-time guarantee that the production embedder understands prompts;
	// without it EmbedAs would silently degrade to the un-prompted space.
	var _ RoleEmbedder = (*GemmaEmbedder)(nil)
}

func TestGemmaModelIDTracksPromptRegime(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	withPrompts(t, false)
	rawID := emb.ModelID()
	withPrompts(t, true)
	promptID := emb.ModelID()
	t.Logf("ModelID raw=%q prompt=%q", rawID, promptID)

	if rawID == promptID {
		t.Fatalf("ModelID must differ across prompt regimes, both %q", rawID)
	}
	if !strings.HasSuffix(promptID, ":"+promptRecipeVersion) {
		t.Errorf("prompt ModelID %q should end with the recipe version", promptID)
	}
	if !strings.HasSuffix(rawID, ":raw") {
		t.Errorf("un-prompted ModelID %q should end with :raw", rawID)
	}
	if !strings.Contains(rawID, ":768:") {
		t.Errorf("ModelID %q should carry the output dimension", rawID)
	}
}

// TestGemmaModelIDCarriesGGUFIdentity pins the reason ModelID reads the model
// name from the GGUF header: without it, two different checkpoints that share an
// output dimension would be treated as the same vector space, and a model swap
// would keep serving similarity scores computed across two spaces.
func TestGemmaModelIDCarriesGGUFIdentity(t *testing.T) {
	path := findModel(t)

	mf, err := gguf.OpenMmap(path)
	if err != nil {
		t.Fatalf("open mmap: %v", err)
	}
	wantName := strings.TrimSpace(gguf.GetMetaStr(mf.Meta, "general.name"))
	mf.CloseMmap()
	if wantName == "" {
		t.Skip("model declares no general.name")
	}

	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()

	if got := emb.ModelID(); !strings.HasPrefix(got, wantName+":") {
		t.Errorf("ModelID %q should start with the GGUF model name %q", got, wantName)
	}
}

func TestGemmaRoleChangesTokenSequence(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()
	withPrompts(t, true)

	raw := len(emb.tokenizer.Encode("hello world"))
	asQuery := len(emb.tokenizer.Encode(ApplyPrompt("hello world", RoleQuery)))
	if asQuery <= raw {
		t.Fatalf("query prompt did not extend the token sequence: raw=%d prompted=%d", raw, asQuery)
	}
}

func TestGemmaRoleProducesDifferentVectors(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()
	withPrompts(t, true)

	queryVec, err := emb.EmbedWithRole("如何优化数据库查询", RoleQuery)
	if err != nil {
		t.Fatal(err)
	}
	docVec, err := emb.EmbedWithRole("如何优化数据库查询", RoleDocument)
	if err != nil {
		t.Fatal(err)
	}
	if len(queryVec) != len(docVec) {
		t.Fatalf("dimension mismatch: %d vs %d", len(queryVec), len(docVec))
	}
	if cosine32(queryVec, docVec) > 0.999 {
		t.Fatalf("query and document prompts produced identical vectors (cos=%.6f); "+
			"the prompt is probably not reaching the model", cosine32(queryVec, docVec))
	}
}

// TestGemmaBatchRoleShortCircuitsWhenDisabled guards the no-op fast path: with
// prompts off the batch result must be bit-identical to the plain batch, so the
// extra slice copy cannot silently change behaviour.
func TestGemmaBatchRoleShortCircuitsWhenDisabled(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()
	withPrompts(t, false)

	texts := []string{"alpha", "beta 中文", "gamma"}
	plain, err := emb.EmbedBatch(texts)
	if err != nil {
		t.Fatal(err)
	}
	viaRole, err := emb.EmbedBatchWithRole(texts, RoleQuery)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != len(viaRole) {
		t.Fatalf("batch length mismatch: %d vs %d", len(plain), len(viaRole))
	}
	for i := range plain {
		if cosine32(plain[i], viaRole[i]) < 0.999999 {
			t.Fatalf("row %d differs between plain and role batch (cos=%.8f)",
				i, cosine32(plain[i], viaRole[i]))
		}
	}
}

// TestGemmaBatchRoleDiffersWhenEnabled is the counterpart: with prompts on the
// role batch must not collapse back onto the plain batch.
func TestGemmaBatchRoleDiffersWhenEnabled(t *testing.T) {
	path := findModel(t)
	emb, err := NewGemmaEmbedder(path, 768)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	defer emb.Close()
	withPrompts(t, true)

	texts := []string{"alpha", "beta 中文"}
	plain, err := emb.EmbedBatch(texts)
	if err != nil {
		t.Fatal(err)
	}
	viaRole, err := emb.EmbedBatchWithRole(texts, RoleQuery)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plain {
		if cosine32(plain[i], viaRole[i]) > 0.999 {
			t.Fatalf("row %d identical with prompts enabled (cos=%.6f); prompt not applied",
				i, cosine32(plain[i], viaRole[i]))
		}
	}
}
