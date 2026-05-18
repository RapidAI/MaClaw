package needleruntime

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
)

type Q8Predictor struct {
	tokenizer *SimpleTokenizer
	labels    []string
	weights   *Q8Weights
	identity  bool
	scratch   sync.Pool
}

type q8Scratch struct {
	buf    *q8ScratchBuffer
	pooled []int32
	logits []float64
	ok     bool
}

type q8ScratchBuffer struct {
	pooled []int32
	logits []float64
	ids    []int
	active []int
}

func NewQ8Predictor(tokenizer *SimpleTokenizer, labels []string, weights *Q8Weights) (*Q8Predictor, error) {
	if tokenizer == nil {
		return nil, fmt.Errorf("needleruntime: q8 predictor missing tokenizer")
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("needleruntime: q8 predictor missing labels")
	}
	if weights == nil || weights.Header == nil {
		return nil, fmt.Errorf("needleruntime: q8 predictor missing weights")
	}
	if int(weights.Header.NumLabels) > len(labels) {
		return nil, fmt.Errorf("needleruntime: labels=%d smaller than weight labels=%d", len(labels), weights.Header.NumLabels)
	}
	if maxID := tokenizer.MaxID(); maxID >= int(weights.Header.VocabSize) {
		return nil, fmt.Errorf("needleruntime: tokenizer max id %d exceeds weight vocab size %d", maxID, weights.Header.VocabSize)
	}
	if weights.Header.Flags&WeightFlagSparseHashHead != 0 && !tokenizer.isHashingTokenizer() {
		return nil, fmt.Errorf("needleruntime: sparse hash head weights require a hashing tokenizer")
	}
	if weights.Header.Flags&WeightFlagSparseHashHead != 0 && !tokenizer.hasCompleteHashVocab() {
		return nil, fmt.Errorf("needleruntime: sparse hash head weights require a complete __hN hashing tokenizer vocabulary")
	}
	if weights.Header.Flags&WeightFlagSparseHashHead != 0 && weights.Header.VocabSize != weights.Header.HiddenSize {
		return nil, fmt.Errorf("needleruntime: sparse hash head weights require vocab_size == hidden_size")
	}
	if weights.Header.Flags&WeightFlagSparseHashHead != 0 && tokenizer.HashDim != int(weights.Header.VocabSize) {
		return nil, fmt.Errorf("needleruntime: sparse hash head tokenizer dim %d does not match weight vocab size %d", tokenizer.HashDim, weights.Header.VocabSize)
	}
	identity := weights.Header.Flags&(WeightFlagIdentityEmbedding|WeightFlagSparseHashHead) != 0 && weights.Header.VocabSize == weights.Header.HiddenSize
	return &Q8Predictor{tokenizer: tokenizer, labels: labels, weights: weights, identity: identity}, nil
}

func (p *Q8Predictor) Predict(ctx context.Context, req Request) (needledata.Decision, error) {
	select {
	case <-ctx.Done():
		return needledata.Decision{}, ctx.Err()
	default:
	}
	if p == nil || p.tokenizer == nil || p.weights == nil {
		return needledata.Decision{}, fmt.Errorf("needleruntime: q8 predictor is not initialized")
	}
	scratch := p.logitsForRequest(req)
	if !scratch.ok {
		return needledata.Decision{Name: fallbackLabel(p.labels), Confidence: 0.0, Source: "needle_q8"}, nil
	}
	idx, conf := argmaxSoftmax(scratch.logits)
	p.releaseScratch(scratch)
	if idx < 0 || idx >= len(p.labels) {
		return needledata.Decision{}, fmt.Errorf("needleruntime: predicted label index %d out of range", idx)
	}
	return needledata.Decision{Name: p.labels[idx], Confidence: conf, Source: "needle_q8"}, nil
}

func (p *Q8Predictor) logitsForRequest(req Request) q8Scratch {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	if p.identity && p.tokenizer != nil && p.tokenizer.isHashingTokenizer() {
		return p.logitsForSparseHashRequest(req, p.getScratchNoClear(h, n, 0))
	}
	buf := p.getScratch(h, n, 32)
	if p.tokenizer != nil && p.tokenizer.isHashingTokenizer() {
		return p.logitsForHashRequest(req, buf)
	}
	ids := buf.ids[:0]
	ids = p.tokenizer.EncodeInto(ids, RenderPrompt(req))
	if len(ids) == 0 {
		p.putScratch(buf)
		return q8Scratch{}
	}
	return p.logitsForIDsWithScratch(ids, buf)
}

func (p *Q8Predictor) logitsForSparseHashRequest(req Request, buf *q8ScratchBuffer) q8Scratch {
	n := int(p.weights.Header.NumLabels)
	dim := p.tokenizer.HashDim
	pooled := buf.pooled[:dim]
	for _, bucket := range buf.active {
		pooled[bucket] = 0
	}
	active := buf.active[:0]
	count := 0
	active, count = appendHashTokenCount(pooled, active, count, dim, "Task")
	active, count = appendHashedTextCounts(pooled, active, count, dim, req.Task)
	active, count = appendHashTokenCount(pooled, active, count, dim, "Choices")
	if len(req.Choices) == 0 {
		active, count = appendHashTokenCount(pooled, active, count, dim, "none")
	} else {
		for _, choice := range req.Choices {
			active, count = appendHashedTextCounts(pooled, active, count, dim, choice)
		}
	}
	active, count = appendHashTokenCount(pooled, active, count, dim, "User")
	active, count = appendHashedTextCounts(pooled, active, count, dim, req.Text)
	if count == 0 {
		p.putScratch(buf)
		return q8Scratch{}
	}
	logits := buf.logits[:n]
	p.fillSparseLogits(logits, pooled, active, count)
	buf.active = active
	return q8Scratch{buf: buf, pooled: pooled, logits: logits, ok: true}
}

func (p *Q8Predictor) logitsForHashRequest(req Request, buf *q8ScratchBuffer) q8Scratch {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	pooled := buf.pooled[:h]
	dim := p.tokenizer.HashDim
	count := 0
	count += addHashTokenToPooled(pooled, p.weights.Embeddings, h, dim, "Task")
	count += addHashedTextToPooled(pooled, p.weights.Embeddings, h, dim, req.Task)
	count += addHashTokenToPooled(pooled, p.weights.Embeddings, h, dim, "Choices")
	if len(req.Choices) == 0 {
		count += addHashTokenToPooled(pooled, p.weights.Embeddings, h, dim, "none")
	} else {
		for _, choice := range req.Choices {
			count += addHashedTextToPooled(pooled, p.weights.Embeddings, h, dim, choice)
		}
	}
	count += addHashTokenToPooled(pooled, p.weights.Embeddings, h, dim, "User")
	count += addHashedTextToPooled(pooled, p.weights.Embeddings, h, dim, req.Text)
	if count == 0 {
		p.putScratch(buf)
		return q8Scratch{}
	}
	logits := buf.logits[:n]
	p.fillLogits(logits, pooled, count)
	return q8Scratch{buf: buf, pooled: pooled, logits: logits, ok: true}
}

func (p *Q8Predictor) logitsForIDs(ids []int) q8Scratch {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	buf := p.getScratch(h, n, 0)
	return p.logitsForIDsWithScratch(ids, buf)
}

func (p *Q8Predictor) logitsForIDsWithScratch(ids []int, buf *q8ScratchBuffer) q8Scratch {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	vocab := int(p.weights.Header.VocabSize)
	pooled := buf.pooled[:h]
	count := 0
	for _, id := range ids {
		if id < 0 || id >= vocab {
			continue
		}
		row := id * h
		addEmbeddingRow(pooled, p.weights.Embeddings[row:row+h])
		count++
	}
	if count == 0 {
		p.putScratch(buf)
		return q8Scratch{}
	}
	logits := buf.logits[:n]
	p.fillLogits(logits, pooled, count)
	return q8Scratch{buf: buf, pooled: pooled, logits: logits, ok: true}
}

func (p *Q8Predictor) fillLogits(logits []float64, pooled []int32, count int) {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	inv := 1.0 / float64(count)
	for label := 0; label < n; label++ {
		row := label * h
		acc := dotInt32Int8(pooled, p.weights.Head[row:row+h])
		logits[label] = float64(p.weights.Bias[label]) + float64(acc)*inv
	}
}

func (p *Q8Predictor) fillSparseLogits(logits []float64, counts []int32, active []int, total int) {
	n := int(p.weights.Header.NumLabels)
	h := int(p.weights.Header.HiddenSize)
	inv := 1.0 / float64(total)
	for label := 0; label < n; label++ {
		row := label * h
		var acc int64
		for _, bucket := range active {
			acc += int64(counts[bucket]) * int64(p.weights.Head[row+bucket])
		}
		logits[label] = float64(p.weights.Bias[label]) + float64(acc)*inv
	}
}

func (p *Q8Predictor) releaseScratch(s q8Scratch) {
	if s.buf != nil {
		p.putScratch(s.buf)
	}
}

func (p *Q8Predictor) getScratch(hidden, labels, idsCap int) *q8ScratchBuffer {
	buf := p.getScratchNoClear(hidden, labels, idsCap)
	clear(buf.pooled)
	buf.active = buf.active[:0]
	return buf
}

func (p *Q8Predictor) getScratchNoClear(hidden, labels, idsCap int) *q8ScratchBuffer {
	var buf *q8ScratchBuffer
	if v := p.scratch.Get(); v != nil {
		buf, _ = v.(*q8ScratchBuffer)
	}
	if buf == nil {
		buf = &q8ScratchBuffer{}
	}
	if cap(buf.pooled) < hidden {
		buf.pooled = make([]int32, hidden)
	} else {
		buf.pooled = buf.pooled[:hidden]
	}
	if cap(buf.logits) < labels {
		buf.logits = make([]float64, labels)
	} else {
		buf.logits = buf.logits[:labels]
		clear(buf.logits)
	}
	if idsCap > 0 && cap(buf.ids) < idsCap {
		buf.ids = make([]int, 0, idsCap)
	} else {
		buf.ids = buf.ids[:0]
	}
	if cap(buf.active) < hidden {
		buf.active = make([]int, 0, hidden)
	}
	return buf
}

func (p *Q8Predictor) putScratch(buf *q8ScratchBuffer) {
	if buf != nil {
		p.scratch.Put(buf)
	}
}

func addEmbeddingRow(dst []int32, row []int8) {
	j := 0
	for ; j+8 <= len(dst); j += 8 {
		dst[j+0] += int32(row[j+0])
		dst[j+1] += int32(row[j+1])
		dst[j+2] += int32(row[j+2])
		dst[j+3] += int32(row[j+3])
		dst[j+4] += int32(row[j+4])
		dst[j+5] += int32(row[j+5])
		dst[j+6] += int32(row[j+6])
		dst[j+7] += int32(row[j+7])
	}
	for ; j < len(dst); j++ {
		dst[j] += int32(row[j])
	}
}

func addHashTokenToPooled(pooled []int32, embeddings []int8, h, dim int, token string) int {
	id := hashPieceID(token, dim)
	addEmbeddingRow(pooled, embeddings[id*h:id*h+h])
	return 1
}

func appendHashTokenCount(counts []int32, active []int, total, dim int, token string) ([]int, int) {
	id := hashPieceID(token, dim)
	if counts[id] == 0 {
		active = append(active, id)
	}
	counts[id]++
	return active, total + 1
}

func appendHashedTextCounts(counts []int32, active []int, total, dim int, text string) ([]int, int) {
	var hash uint64
	inToken := false
	for i := 0; i < len(text); {
		b := text[i]
		if b < utf8.RuneSelf {
			i++
			if isASCIISep(b) {
				if inToken {
					id := int(hash % uint64(dim))
					if counts[id] == 0 {
						active = append(active, id)
					}
					counts[id]++
					total++
					inToken = false
				}
				continue
			}
			if !inToken {
				hash = fnv64Offset
				inToken = true
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			hash ^= uint64(b)
			hash *= fnv64Prime
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 {
			break
		}
		i += size
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if inToken {
				id := int(hash % uint64(dim))
				if counts[id] == 0 {
					active = append(active, id)
				}
				counts[id]++
				total++
				inToken = false
			}
			continue
		}
		if !inToken {
			hash = fnv64Offset
			inToken = true
		}
		hash = hashRuneLower(hash, r)
	}
	if inToken {
		id := int(hash % uint64(dim))
		if counts[id] == 0 {
			active = append(active, id)
		}
		counts[id]++
		total++
	}
	return active, total
}

func addHashedTextToPooled(pooled []int32, embeddings []int8, h, dim int, text string) int {
	var hash uint64
	inToken := false
	count := 0
	for i := 0; i < len(text); {
		b := text[i]
		if b < utf8.RuneSelf {
			i++
			if isASCIISep(b) {
				if inToken {
					id := int(hash % uint64(dim))
					addEmbeddingRow(pooled, embeddings[id*h:id*h+h])
					count++
					inToken = false
				}
				continue
			}
			if !inToken {
				hash = fnv64Offset
				inToken = true
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			hash ^= uint64(b)
			hash *= fnv64Prime
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 {
			break
		}
		i += size
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if inToken {
				id := int(hash % uint64(dim))
				addEmbeddingRow(pooled, embeddings[id*h:id*h+h])
				count++
				inToken = false
			}
			continue
		}
		if !inToken {
			hash = fnv64Offset
			inToken = true
		}
		hash = hashRuneLower(hash, r)
	}
	if inToken {
		id := int(hash % uint64(dim))
		addEmbeddingRow(pooled, embeddings[id*h:id*h+h])
		count++
	}
	return count
}

func dotInt32Int8(a []int32, b []int8) int64 {
	var s0, s1, s2, s3, s4, s5, s6, s7 int64
	var s8, s9, s10, s11, s12, s13, s14, s15 int64
	j := 0
	for ; j+16 <= len(a); j += 16 {
		s0 += int64(a[j+0]) * int64(b[j+0])
		s1 += int64(a[j+1]) * int64(b[j+1])
		s2 += int64(a[j+2]) * int64(b[j+2])
		s3 += int64(a[j+3]) * int64(b[j+3])
		s4 += int64(a[j+4]) * int64(b[j+4])
		s5 += int64(a[j+5]) * int64(b[j+5])
		s6 += int64(a[j+6]) * int64(b[j+6])
		s7 += int64(a[j+7]) * int64(b[j+7])
		s8 += int64(a[j+8]) * int64(b[j+8])
		s9 += int64(a[j+9]) * int64(b[j+9])
		s10 += int64(a[j+10]) * int64(b[j+10])
		s11 += int64(a[j+11]) * int64(b[j+11])
		s12 += int64(a[j+12]) * int64(b[j+12])
		s13 += int64(a[j+13]) * int64(b[j+13])
		s14 += int64(a[j+14]) * int64(b[j+14])
		s15 += int64(a[j+15]) * int64(b[j+15])
	}
	sum := s0 + s1 + s2 + s3 + s4 + s5 + s6 + s7 + s8 + s9 + s10 + s11 + s12 + s13 + s14 + s15
	for ; j < len(a); j++ {
		sum += int64(a[j]) * int64(b[j])
	}
	return sum
}

func (p *Q8Predictor) pool(ids []int) []float32 {
	h := int(p.weights.Header.HiddenSize)
	vocab := int(p.weights.Header.VocabSize)
	out := make([]float32, h)
	count := 0
	for _, id := range ids {
		if id < 0 || id >= vocab {
			continue
		}
		row := id * h
		for j := 0; j < h; j++ {
			out[j] += float32(p.weights.Embeddings[row+j])
		}
		count++
	}
	if count == 0 {
		return out
	}
	inv := float32(1.0 / float64(count))
	for j := range out {
		out[j] *= inv
	}
	return out
}

func (p *Q8Predictor) logits(pooled []float32) []float64 {
	h := int(p.weights.Header.HiddenSize)
	n := int(p.weights.Header.NumLabels)
	out := make([]float64, n)
	for label := 0; label < n; label++ {
		sum := float64(p.weights.Bias[label])
		row := label * h
		for j := 0; j < h; j++ {
			sum += float64(pooled[j]) * float64(p.weights.Head[row+j])
		}
		out[label] = sum
	}
	return out
}

func argmaxSoftmax(logits []float64) (int, float64) {
	switch len(logits) {
	case 0:
		return -1, 0
	case 1:
		return 0, 1
	case 2:
		if logits[0] >= logits[1] {
			return 0, 1 / (1 + math.Exp(logits[1]-logits[0]))
		}
		return 1, 1 / (1 + math.Exp(logits[0]-logits[1]))
	}
	best := 0
	maxLogit := logits[0]
	for i, v := range logits[1:] {
		if v > maxLogit {
			best = i + 1
			maxLogit = v
		}
	}
	var denom float64
	for _, v := range logits {
		denom += math.Exp(v - maxLogit)
	}
	if denom == 0 {
		return best, 0
	}
	return best, 1 / denom
}

func fallbackLabel(labels []string) string {
	for _, label := range labels {
		if strings.EqualFold(label, "other") || strings.EqualFold(label, "no_call") || strings.EqualFold(label, "no_extract") {
			return label
		}
	}
	if len(labels) > 0 {
		return labels[0]
	}
	return ""
}
