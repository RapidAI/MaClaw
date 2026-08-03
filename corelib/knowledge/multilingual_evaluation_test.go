package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// multilingualEvaluationEmbedder is a deterministic stand-in for a genuine
// multilingual encoder. Its purpose is to make the retrieval harness stable:
// model upgrades can run the same labelled cases with a real encoder and
// compare Recall@K/MRR without changing evaluation mechanics.
type multilingualEvaluationEmbedder struct{}

func (multilingualEvaluationEmbedder) Embed(text string) ([]float32, error) {
	text = strings.ToLower(normalizeKnowledgeText(text))
	for i, keywords := range [][]string{
		{"量子", "quantum", "量子通信"},
		{"vaccine", "疫苗", "ワクチン"},
		{"cloud", "클라우드", "云"},
		{"flood", "ระบบเตือนภัย", "洪水"},
		{"agriculture", "زراعة", "农业"},
		{"privacy", "隐私", "プライバシー"},
	} {
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				vector := make([]float32, 6)
				vector[i] = 1
				return vector, nil
			}
		}
	}
	return make([]float32, 6), nil
}

func (e multilingualEvaluationEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i], _ = e.Embed(text)
	}
	return vectors, nil
}

func (multilingualEvaluationEmbedder) Dim() int { return 6 }
func (multilingualEvaluationEmbedder) Close()   {}
func (multilingualEvaluationEmbedder) ModelID() string {
	return "multilingual-evaluation-v1"
}

type multilingualEvaluationCase struct {
	name       string
	language   string
	document   string
	query      string
	expectedID string
}

func TestMultilingualRetrievalEvaluationBaseline(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetEmbedder(multilingualEvaluationEmbedder{})

	cases := []multilingualEvaluationCase{
		{name: "zh_to_en", language: "zh", document: "量子通信利用纠缠分发密钥。", query: "quantum communication security", expectedID: "src-zh"},
		{name: "en_to_ja", language: "ja", document: "ワクチン接種は地域の感染症対策を支える。", query: "vaccine public health", expectedID: "src-ja"},
		{name: "ko_to_en", language: "ko", document: "클라우드 서비스는 탄력적 확장을 제공한다.", query: "cloud scaling", expectedID: "src-ko"},
		{name: "th_to_en", language: "th", document: "ระบบเตือนภัยน้ำท่วมช่วยลดความเสียหายของชุมชน", query: "flood early warning", expectedID: "src-th"},
		{name: "ar_to_en", language: "ar", document: "الزراعة الدقيقة تقلل استخدام المياه وتحسن الإنتاج.", query: "agriculture water efficiency", expectedID: "src-ar"},
		{name: "ja_to_zh", language: "ja", document: "プライバシー保護にはデータ最小化が重要です。", query: "隐私 数据最小化", expectedID: "src-privacy"},
	}
	for _, tc := range cases {
		if err := store.SaveSource(ctx, Source{ID: tc.expectedID, Kind: SourceKindText, URI: "evaluation://" + tc.expectedID, Title: tc.name, Status: StatusParsed}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDocumentNode(ctx, DocumentNode{ID: "node-" + tc.expectedID, SourceID: tc.expectedID, Type: "paragraph", Text: tc.document, Metadata: map[string]string{"language": tc.language}}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveCard(ctx, Card{ID: "card-" + tc.expectedID, SourceID: tc.expectedID, NodeID: "node-" + tc.expectedID, Title: tc.name, Claim: tc.document, Embedding: mustEmbedEvaluation(t, tc.document)}); err != nil {
			t.Fatal(err)
		}
	}

	const k = 3
	recall, mrr := 0.0, 0.0
	for _, tc := range cases {
		results, err := store.Search(ctx, SearchOptions{Query: tc.query, Limit: k, PreferEmbedding: true})
		if err != nil {
			t.Fatalf("%s search: %v", tc.name, err)
		}
		rank := expectedSourceRank(results, tc.expectedID)
		if rank == 0 {
			t.Logf("%s no expected source in results: %#v", tc.name, results)
		}
		if rank > 0 && rank <= k {
			recall++
			mrr += 1 / float64(rank)
		}
	}
	recall /= float64(len(cases))
	mrr /= float64(len(cases))
	if recall < 1 || mrr < 1 {
		t.Fatalf("multilingual baseline regression: Recall@%d=%.3f MRR@%d=%.3f", k, recall, k, mrr)
	}
}

func mustEmbedEvaluation(t *testing.T, text string) []float32 {
	t.Helper()
	vector, err := (multilingualEvaluationEmbedder{}).Embed(text)
	if err != nil {
		t.Fatal(err)
	}
	return vector
}

func expectedSourceRank(results []SearchResult, sourceID string) int {
	for i, result := range results {
		if result.Source.ID == sourceID {
			return i + 1
		}
	}
	return 0
}
