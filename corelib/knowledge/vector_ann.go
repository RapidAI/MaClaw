package knowledge

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// vectorANNStore is an in-process, model-isolated approximate candidate index.
// It uses random-hyperplane locality-sensitive hashing (LSH): each signature
// bucket is searched first, then exact cosine ranking is applied to the union
// of candidates. It is deliberately opt-in; search always falls back to a full
// exact scan when disabled or when a bucket is sparse. This gives deployments a
// safe acceleration seam for a future persisted HNSW backend.
//
// The index deliberately contains no tenancy/ACL state. All access filtering
// remains in SQL before indexing and again before returning candidates.
type vectorANNStore struct {
	mu     sync.RWMutex
	spaces map[string]*vectorANNSpace
	clock  uint64
}

type vectorANNSpace struct {
	generation uint64
	buckets    map[uint64][]int
	lastUsed   uint64
}

type vectorANNVector struct {
	key    string
	vector []float32
}

const vectorANNSignatureBits = 16

func newVectorANNStore() *vectorANNStore {
	return &vectorANNStore{spaces: make(map[string]*vectorANNSpace)}
}

func (s *SQLiteStore) invalidateVectorANN() {
	if s == nil || s.vectorANN == nil {
		return
	}
	s.vectorANN.mu.Lock()
	s.vectorANN.spaces = make(map[string]*vectorANNSpace)
	s.vectorANN.mu.Unlock()
}

// VectorIndexStats reports the active vector retrieval mode. CandidateCount is
// intentionally omitted: it is query/filter-dependent and reporting a global
// count could accidentally reveal other tenants' corpus sizes.
type VectorIndexStats struct {
	Enabled  bool   `json:"enabled"`
	Backend  string `json:"backend"`
	Fallback string `json:"fallback"`
}

func (s *SQLiteStore) VectorIndexStats() VectorIndexStats {
	if s != nil && s.approximateVectorSearchEnabled() {
		return VectorIndexStats{Enabled: true, Backend: "lsh_candidate", Fallback: "exact_cosine"}
	}
	return VectorIndexStats{Enabled: false, Backend: "exact_cosine", Fallback: "exact_cosine"}
}

func (s *SQLiteStore) vectorANNCandidates(spaceKey string, generation uint64, values []vectorANNVector, query []float32, want int) []int {
	if len(values) == 0 || !validEmbeddingVector(query, 0) {
		return nil
	}
	if want <= 0 {
		want = 20
	}
	if s == nil || s.vectorANN == nil || !s.approximateVectorSearchEnabled() || len(values) < 256 {
		return exactVectorCandidateIndexes(values, query, want)
	}
	// Candidate rows are already ACL-filtered. Reuse an LSH space only when the
	// complete ordered candidate identity matches; this preserves the strict
	// scope isolation of the exact path while avoiding repeated rebuilds for
	// identical queries.
	key := vectorANNSpaceKey(spaceKey, generation, values)
	// An LRU policy keeps frequently used scopes warm. Unlike map iteration,
	// eviction is deterministic and does not randomly discard a hot tenant.
	s.vectorANN.mu.Lock()
	s.vectorANN.clock++
	space := s.vectorANN.spaces[key]
	if space == nil {
		if len(s.vectorANN.spaces) >= vectorANNMaxCachedSpaces {
			var oldestKey string
			var oldestUsed uint64
			for cachedKey, cachedSpace := range s.vectorANN.spaces {
				if oldestKey == "" || cachedSpace.lastUsed < oldestUsed || (cachedSpace.lastUsed == oldestUsed && cachedKey < oldestKey) {
					oldestKey, oldestUsed = cachedKey, cachedSpace.lastUsed
				}
			}
			delete(s.vectorANN.spaces, oldestKey)
		}
		space = buildVectorANNSpace(generation, values)
		s.vectorANN.spaces[key] = space
	}
	space.lastUsed = s.vectorANN.clock
	s.vectorANN.mu.Unlock()
	signature := vectorANNSignature(query)
	candidateSet := make(map[int]struct{}, want*3)
	for _, pos := range space.buckets[signature] {
		candidateSet[pos] = struct{}{}
	}
	// Probe signatures at Hamming distance one for stable recall near a plane.
	for bit := 0; bit < vectorANNSignatureBits && len(candidateSet) < want*4; bit++ {
		for _, pos := range space.buckets[signature^(1<<bit)] {
			candidateSet[pos] = struct{}{}
		}
	}
	// LSH is only an optimization. Build the actual scoreable set before deciding
	// whether the bucket is sufficiently populated: corrupt or stale vectors can
	// hash into a bucket, but exact ranking will reject them. Counting those rows
	// here could suppress the exact fallback and return fewer valid results.
	candidates := make([]vectorANNVector, 0, len(candidateSet))
	positions := make([]int, 0, len(candidateSet))
	for pos := range candidateSet {
		if pos >= 0 && pos < len(values) && validEmbeddingVector(values[pos].vector, len(query)) {
			positions = append(positions, pos)
			candidates = append(candidates, values[pos])
		}
	}
	// LSH is only an optimization. Sparse scoreable candidate sets cannot prove
	// absence of relevant content, so use the exact path under the configured bar.
	if len(candidates) < want*2 {
		return exactVectorCandidateIndexes(values, query, want)
	}
	order := exactVectorCandidateIndexes(candidates, query, want)
	result := make([]int, 0, len(order))
	for _, candidate := range order {
		result = append(result, positions[candidate])
	}
	return result
}

const vectorANNMaxCachedSpaces = 16

func vectorANNSpaceKey(spaceKey string, generation uint64, values []vectorANNVector) string {
	// Values are already constrained by model and ACL predicates. Including the
	// ordered IDs and vector signatures makes a cached space unusable whenever a
	// candidate set, its ordering, or its vector payload changes. Every field is
	// length-prefixed because callers may supply IDs and space names containing
	// separators. A delimiter-only encoding could otherwise make two distinct
	// scopes share one cached index.
	var b strings.Builder
	b.Grow(len(spaceKey) + len(values)*32)
	b.WriteString("v1;")
	writeVectorANNSpaceKeyField(&b, spaceKey)
	writeVectorANNSpaceKeyField(&b, strconv.FormatUint(generation, 10))
	for _, value := range values {
		writeVectorANNSpaceKeyField(&b, value.key)
		writeVectorANNSpaceKeyField(&b, strconv.FormatUint(vectorFingerprint(value.vector), 16))
	}
	return b.String()
}

func writeVectorANNSpaceKeyField(b *strings.Builder, value string) {
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteByte(':')
	b.WriteString(value)
	b.WriteByte(';')
}

func vectorFingerprint(vector []float32) uint64 {
	var h uint64 = 1469598103934665603
	for _, value := range vector {
		h ^= uint64(math.Float32bits(value))
		h *= 1099511628211
	}
	return h
}

func buildVectorANNSpace(generation uint64, values []vectorANNVector) *vectorANNSpace {
	space := &vectorANNSpace{generation: generation, buckets: make(map[uint64][]int)}
	for i, value := range values {
		space.buckets[vectorANNSignature(value.vector)] = append(space.buckets[vectorANNSignature(value.vector)], i)
	}
	return space
}

func vectorANNSignature(vector []float32) uint64 {
	var signature uint64
	for bit := 0; bit < vectorANNSignatureBits; bit++ {
		var dot float64
		if len(vector) == 0 {
			continue
		}
		dimension := (bit*1103515245 + 12345) % len(vector)
		dot = float64(vector[dimension])
		if dot >= 0 {
			signature |= 1 << bit
		}
	}
	return signature
}

func exactVectorCandidateIndexes(values []vectorANNVector, query []float32, want int) []int {
	type scored struct {
		index int
		score float64
		key   string
	}
	scores := make([]scored, 0, len(values))
	for i, value := range values {
		if !validEmbeddingVector(value.vector, len(query)) {
			continue
		}
		scores = append(scores, scored{index: i, score: cosineSimilarity(query, value.vector), key: value.key})
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].key < scores[j].key
		}
		return scores[i].score > scores[j].score
	})
	if want > len(scores) {
		want = len(scores)
	}
	result := make([]int, want)
	for i := range result {
		result[i] = scores[i].index
	}
	return result
}
