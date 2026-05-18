package needleruntime

import (
	"math"
	"testing"
	"time"
)

func TestQ8FusedLogitsMatchPoolAndLogits(t *testing.T) {
	weights := &Q8Weights{
		Header: &WeightHeader{VocabSize: 4, HiddenSize: 4, NumLabels: 2},
		Embeddings: []int8{
			1, 2, 3, 4,
			-2, 1, 0, 3,
			0, -1, 2, 1,
			1, 1, 1, 1,
		},
		Head: []int8{
			1, 0, -1, 2,
			-1, 3, 1, 0,
		},
		Bias: []float32{0.25, -0.5},
	}
	p := &Q8Predictor{weights: weights}
	ids := []int{0, 1, 99, 2}
	pooled := p.pool(ids)
	want := p.logits(pooled)
	scratch := p.logitsForIDs(ids)
	if !scratch.ok {
		t.Fatal("logitsForIDs returned ok=false")
	}
	defer p.releaseScratch(scratch)
	got := scratch.logits
	if len(got) != len(want) {
		t.Fatalf("logit len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if diff := got[i] - want[i]; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("logit[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestQ8SparseHashLogitsMatchDenseIdentity(t *testing.T) {
	h := 16
	n := 3
	emb := make([]int8, h*h)
	head := make([]int8, n*h)
	bias := []float32{0.25, -0.5, 0.75}
	for i := 0; i < h; i++ {
		emb[i*h+i] = 1
	}
	for i := range head {
		head[i] = int8(i%9 - 4)
	}
	vocab := make(map[string]int, h)
	for i := 0; i < h; i++ {
		vocab["__h"+itoa(i)] = i
	}
	weights := &Q8Weights{Header: &WeightHeader{VocabSize: uint32(h), HiddenSize: uint32(h), NumLabels: uint32(n), Flags: WeightFlagIdentityEmbedding}, Embeddings: emb, Head: head, Bias: bias}
	p, err := NewQ8Predictor(&SimpleTokenizer{Vocab: vocab, HashDim: h}, []string{"a", "b", "c"}, weights)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Task: "workflow_review", Text: "Approve this workflow review and continue", Choices: []string{"approve", "reject", "clarify"}}
	ids := p.tokenizer.EncodeRequestInto(nil, req)
	dense := p.logitsForIDs(ids)
	if !dense.ok {
		t.Fatal("dense logits returned ok=false")
	}
	defer p.releaseScratch(dense)
	buf := p.getScratch(h, n, 0)
	sparse := p.logitsForSparseHashRequest(req, buf)
	if !sparse.ok {
		t.Fatal("sparse logits returned ok=false")
	}
	defer p.releaseScratch(sparse)
	for i := range dense.logits {
		if diff := dense.logits[i] - sparse.logits[i]; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("logit[%d] = %v, want %v", i, sparse.logits[i], dense.logits[i])
		}
	}
	activeSeen := make(map[int]bool, len(sparse.buf.active))
	for _, bucket := range sparse.buf.active {
		if bucket < 0 || bucket >= h {
			t.Fatalf("active bucket out of range: %d", bucket)
		}
		if activeSeen[bucket] {
			t.Fatalf("duplicate active bucket: %d", bucket)
		}
		activeSeen[bucket] = true
	}
	for bucket, count := range sparse.pooled {
		if count != 0 && !activeSeen[bucket] {
			t.Fatalf("bucket %d has count %d but is absent from active list", bucket, count)
		}
	}
}

func BenchmarkQ8FusedLogits(b *testing.B) {
	h := 128
	n := 8
	vocab := 256
	emb := make([]int8, vocab*h)
	head := make([]int8, n*h)
	bias := make([]float32, n)
	for i := range emb {
		emb[i] = int8(i%17 - 8)
	}
	for i := range head {
		head[i] = int8(i%13 - 6)
	}
	p := &Q8Predictor{weights: &Q8Weights{Header: &WeightHeader{VocabSize: uint32(vocab), HiddenSize: uint32(h), NumLabels: uint32(n)}, Embeddings: emb, Head: head, Bias: bias}}
	ids := make([]int, 64)
	for i := range ids {
		ids[i] = i % vocab
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		scratch := p.logitsForIDs(ids)
		if !scratch.ok {
			b.Fatal("empty logits")
		}
		p.releaseScratch(scratch)
	}
}

func BenchmarkQ8Predict(b *testing.B) {
	h := 128
	n := 8
	vocab := 256
	emb := make([]int8, vocab*h)
	head := make([]int8, n*h)
	bias := make([]float32, n)
	for i := range emb {
		emb[i] = int8(i%17 - 8)
	}
	for i := range head {
		head[i] = int8(i%13 - 6)
	}
	v := make(map[string]int, vocab)
	for i := 0; i < vocab; i++ {
		v["__h"+itoa(i)] = i
	}
	p := &Q8Predictor{tokenizer: &SimpleTokenizer{Vocab: v, HashDim: vocab}, labels: []string{"a", "b", "c", "d", "e", "f", "g", "h"}, weights: &Q8Weights{Header: &WeightHeader{VocabSize: uint32(vocab), HiddenSize: uint32(h), NumLabels: uint32(n)}, Embeddings: emb, Head: head, Bias: bias}}
	req := Request{Task: "workflow_review", Text: "looks good continue and move to next step with approval", Choices: []string{"a", "b", "c", "d"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := p.Predict(nilContext{}, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQ8PredictSparseIdentity(b *testing.B) {
	h := 128
	n := 8
	emb := make([]int8, h*h)
	head := make([]int8, n*h)
	bias := make([]float32, n)
	for i := 0; i < h; i++ {
		emb[i*h+i] = 1
	}
	for i := range head {
		head[i] = int8(i%13 - 6)
	}
	v := make(map[string]int, h)
	for i := 0; i < h; i++ {
		v["__h"+itoa(i)] = i
	}
	p, err := NewQ8Predictor(&SimpleTokenizer{Vocab: v, HashDim: h}, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, &Q8Weights{Header: &WeightHeader{VocabSize: uint32(h), HiddenSize: uint32(h), NumLabels: uint32(n), Flags: WeightFlagIdentityEmbedding}, Embeddings: emb, Head: head, Bias: bias})
	if err != nil {
		b.Fatal(err)
	}
	req := Request{Task: "workflow_review", Text: "looks good continue and move to next step with approval", Choices: []string{"a", "b", "c", "d"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := p.Predict(nilContext{}, req); err != nil {
			b.Fatal(err)
		}
	}
}

type nilContext struct{}

func (nilContext) Deadline() (deadline time.Time, ok bool) { return time.Time{}, false }
func (nilContext) Done() <-chan struct{}                   { return nil }
func (nilContext) Err() error                              { return nil }
func (nilContext) Value(key any) any                       { return nil }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestQ8SparseHashRequestClearsPreviousActiveBuckets(t *testing.T) {
	h := 32
	n := 2
	vocab := make(map[string]int, h)
	for i := 0; i < h; i++ {
		vocab["__h"+itoa(i)] = i
	}
	head := make([]int8, n*h)
	for i := range head {
		head[i] = int8(i%11 - 5)
	}
	p, err := NewQ8Predictor(&SimpleTokenizer{Vocab: vocab, HashDim: h}, []string{"a", "b"}, &Q8Weights{Header: &WeightHeader{VocabSize: uint32(h), HiddenSize: uint32(h), NumLabels: uint32(n), Flags: WeightFlagSparseHashHead}, Head: head, Bias: []float32{0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	buf := p.getScratchNoClear(h, n, 0)
	first := p.logitsForSparseHashRequest(Request{Task: "workflow_review", Text: "alpha beta gamma", Choices: []string{"a", "b"}}, buf)
	if !first.ok {
		t.Fatal("first sparse logits returned ok=false")
	}
	p.releaseScratch(first)
	second := p.logitsForSparseHashRequest(Request{Task: "workflow_review", Text: "delta", Choices: []string{"a"}}, p.getScratchNoClear(h, n, 0))
	if !second.ok {
		t.Fatal("second sparse logits returned ok=false")
	}
	defer p.releaseScratch(second)
	activeSeen := map[int]bool{}
	for _, bucket := range second.buf.active {
		activeSeen[bucket] = true
	}
	for bucket, count := range second.pooled {
		if count != 0 && !activeSeen[bucket] {
			t.Fatalf("stale bucket %d kept count %d outside active set %#v", bucket, count, second.buf.active)
		}
	}
}

func TestArgmaxSoftmaxBinaryFastPathMatchesGeneralSoftmax(t *testing.T) {
	cases := [][]float64{{2, 1}, {-1, 4}, {0, 0}, {120, 119}}
	for _, logits := range cases {
		idx, conf := argmaxSoftmax(logits)
		maxLogit := logits[0]
		if logits[1] > maxLogit {
			maxLogit = logits[1]
		}
		want0 := math.Exp(logits[0] - maxLogit)
		want1 := math.Exp(logits[1] - maxLogit)
		denom := want0 + want1
		wantIdx := 0
		wantConf := want0 / denom
		if want1 > want0 {
			wantIdx = 1
			wantConf = want1 / denom
		}
		if idx != wantIdx || math.Abs(conf-wantConf) > 1e-12 {
			t.Fatalf("argmaxSoftmax(%v) = %d %.12f, want %d %.12f", logits, idx, conf, wantIdx, wantConf)
		}
	}
}
