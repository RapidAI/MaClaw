// Package diarization provides CPU-only, pure-Go speaker diarization for
// meeting recordings. It combines Silero VAD, CAM++ speaker embeddings and
// deterministic cosine clustering; it does not attempt source separation of
// overlapping voices.
package diarization

import (
	"fmt"
	"math"
	"sort"

	"github.com/RapidAI/CodeClaw/corelib/vad"
)

const SampleRate = 16000

// Segment is one non-overlapping time span assigned to a local speaker ID.
// Speaker is stable only within one Diarize call.
type Segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

// Config controls VAD, embedding windows and clustering. Zero values select
// settings matching FunASR's CAM++ diarization recipe where possible.
type Config struct {
	// KnownSpeakers pins the cluster count when the attendee count is known.
	// Zero infers it from the embedding affinities.
	KnownSpeakers int
	// MinSpeakers and MaxSpeakers bound automatic clustering.
	MinSpeakers int
	MaxSpeakers int
	// MergeThreshold is the centroid cosine similarity at which two inferred
	// clusters are merged. FunASR's CAM++ backend uses 0.78.
	MergeThreshold float32
	// SegmentDuration and SegmentShift control overlapping speaker windows.
	SegmentDuration float64
	SegmentShift    float64
	// MaxWindows caps the number of CAM++ embeddings in one recording. Zero
	// selects a CPU-safe default. When a meeting contains more windows, the
	// analysis shift is widened evenly across each VAD-positive span rather
	// than simply dropping the end of the recording.
	MaxWindows int
}

func (c Config) normalized() Config {
	if c.MinSpeakers < 1 {
		c.MinSpeakers = 1
	}
	if c.MaxSpeakers < c.MinSpeakers {
		c.MaxSpeakers = 15
	}
	// KnownSpeakers can originate at an RPC boundary. Keep an accidental
	// negative value in automatic mode, and never allow an unrealistic attendee
	// count to bypass the same upper bound that protects automatic clustering.
	if c.KnownSpeakers < 0 {
		c.KnownSpeakers = 0
	}
	if c.KnownSpeakers > c.MaxSpeakers {
		c.KnownSpeakers = c.MaxSpeakers
	}
	if c.MergeThreshold <= 0 || c.MergeThreshold > 1 {
		c.MergeThreshold = .78
	}
	if c.SegmentDuration <= 0 {
		c.SegmentDuration = 1.5
	}
	if c.SegmentShift <= 0 {
		c.SegmentShift = .75
	}
	if c.MaxWindows <= 0 {
		c.MaxWindows = 240
	}
	return c
}

// Embedder makes an L2-normalized speaker embedding for a 16 kHz mono window.
// It is intentionally small so the diarization logic can be tested without a
// model file and so alternative Go CAM++ runtimes can be substituted.
type Embedder interface {
	Embed([]float32) ([]float32, error)
}

// Diarize runs VAD, crops overlapping 1.5 s windows, embeds them, clusters the
// resulting vectors, and returns smoothed non-overlapping speaker spans.
func Diarize(pcm []float32, embedder Embedder, config Config) ([]Segment, error) {
	if embedder == nil {
		return nil, fmt.Errorf("diarization: nil embedder")
	}
	if len(pcm) < SampleRate/2 {
		return nil, nil
	}
	config = config.normalized()
	voice, err := speechSegments(pcm)
	if err != nil {
		return nil, err
	}
	if len(voice) == 0 {
		return nil, nil
	}
	windows := chunkBounded(voice, pcm, config.SegmentDuration, config.SegmentShift, config.MaxWindows)
	if len(windows) == 0 {
		return nil, nil
	}
	embs := make([][]float32, len(windows))
	for i, w := range windows {
		e, err := embedder.Embed(w.pcm)
		if err != nil {
			return nil, fmt.Errorf("diarization: embed window %d: %w", i, err)
		}
		if len(e) == 0 {
			return nil, fmt.Errorf("diarization: empty embedding for window %d", i)
		}
		normalize(e)
		embs[i] = e
	}
	labels := cluster(embs, config)
	result := make([]Segment, len(windows))
	for i, w := range windows {
		result[i] = Segment{Start: w.start, End: w.end, Speaker: labels[i]}
	}
	return smooth(result), nil
}

type speechSpan struct{ start, end float64 }
type window struct {
	start, end float64
	pcm        []float32
}

func speechSegments(pcm []float32) ([]speechSpan, error) {
	m, err := vad.Load()
	if err != nil {
		return nil, fmt.Errorf("load VAD: %w", err)
	}
	state := m.NewState()
	const win = 512
	var out []speechSpan
	active := -1
	silence := 0
	for off := 0; off < len(pcm); off += win {
		buf := make([]float32, win)
		copy(buf, pcm[off:min(off+win, len(pcm))])
		prob, err := m.Detect(buf, state)
		if err != nil {
			return nil, err
		}
		if prob >= .5 {
			if active < 0 {
				active = off
			}
			silence = 0
			continue
		}
		if active >= 0 {
			silence += win
			if silence >= SampleRate/4 { // retain 250 ms turn-final silence
				end := min(off+win, len(pcm))
				if end-active >= SampleRate/3 {
					out = append(out, speechSpan{float64(active) / SampleRate, float64(end) / SampleRate})
				}
				active = -1
				silence = 0
			}
		}
	}
	if active >= 0 && len(pcm)-active >= SampleRate/3 {
		out = append(out, speechSpan{float64(active) / SampleRate, float64(len(pcm)) / SampleRate})
	}
	return mergeSpans(out, .3), nil
}

func chunk(spans []speechSpan, pcm []float32, duration, shift float64) []window {
	return chunkBounded(spans, pcm, duration, shift, 0)
}

// chunkBounded creates uniformly distributed windows across all speech spans.
// Its optional cap protects CPU-only diarization and the O(n²) clustering step
// on lengthy meetings while retaining coverage of the whole recording.
func chunkBounded(spans []speechSpan, pcm []float32, duration, shift float64, maxWindows int) []window {
	length, step := int(duration*SampleRate), int(shift*SampleRate)
	if length <= 0 || step <= 0 {
		return nil
	}
	if maxWindows > 0 && windowCount(spans, duration, shift) > maxWindows {
		return chunkWithBudget(spans, pcm, length, maxWindows)
	}
	var out []window
	for _, s := range spans {
		lo, hi := int(s.start*SampleRate), int(s.end*SampleRate)
		for pos, last := lo, -1; pos < hi; pos += step {
			end := min(pos+length, hi)
			start := max(lo, end-length)
			if end <= last {
				break
			}
			last = end
			part := make([]float32, length)
			copy(part, pcm[start:end])
			out = append(out, window{float64(start) / SampleRate, float64(end) / SampleRate, part})
			if end >= hi {
				break
			}
			// Always include a tail-aligned window. A fixed shift otherwise can
			// leave up to almost one full shift of speech unrepresented.
			if pos+step+length >= hi {
				tailStart := max(lo, hi-length)
				if tailStart > pos {
					part := make([]float32, length)
					copy(part, pcm[tailStart:hi])
					out = append(out, window{float64(tailStart) / SampleRate, float64(hi) / SampleRate, part})
				}
				break
			}
		}
	}
	return out
}

// chunkWithBudget assigns a strict global window budget before allocating any
// PCM buffers. It keeps first/last coverage for every selected speech span;
// when there are more VAD spans than slots, it samples spans evenly instead of
// allowing many tiny pauses to defeat the CPU safety limit.
func chunkWithBudget(spans []speechSpan, pcm []float32, length, budget int) []window {
	valid := make([]speechSpan, 0, len(spans))
	for _, s := range spans {
		if s.end > s.start {
			valid = append(valid, s)
		}
	}
	if budget <= 0 || len(valid) == 0 {
		return nil
	}
	if len(valid) > budget {
		selected := make([]speechSpan, 0, budget)
		for i := 0; i < budget; i++ {
			index := i * (len(valid) - 1) / max(1, budget-1)
			selected = append(selected, valid[index])
		}
		valid = selected
	}

	// Every remaining span receives one window. Allocate the remaining slots
	// proportionally to speech duration so long speaker turns retain detail.
	slots := make([]int, len(valid))
	for i := range slots {
		slots[i] = 1
	}
	remaining := budget - len(valid)
	if remaining > 0 {
		for remaining > 0 {
			best := 0
			bestNeed := -1.0
			for i, s := range valid {
				need := (s.end - s.start) / float64(slots[i])
				if need > bestNeed {
					best, bestNeed = i, need
				}
			}
			slots[best]++
			remaining--
		}
	}

	out := make([]window, 0, budget)
	for i, s := range valid {
		lo, hi := int(s.start*SampleRate), int(s.end*SampleRate)
		lastStart := max(lo, hi-length)
		for n := 0; n < slots[i]; n++ {
			start := lo
			if slots[i] > 1 {
				start = lo + (lastStart-lo)*n/(slots[i]-1)
			}
			end := min(hi, start+length)
			part := make([]float32, length)
			copy(part, pcm[start:end])
			out = append(out, window{float64(start) / SampleRate, float64(end) / SampleRate, part})
		}
	}
	return out
}

func windowCount(spans []speechSpan, duration, shift float64) int {
	if duration <= 0 || shift <= 0 {
		return 0
	}
	count := 0
	for _, s := range spans {
		if s.end <= s.start {
			continue
		}
		d := s.end - s.start
		if d <= duration {
			count++
			continue
		}
		count += int(math.Ceil((d-duration)/shift)) + 1
	}
	return count
}

func cluster(embs [][]float32, cfg Config) []int {
	n := len(embs)
	labels := make([]int, n)
	if n == 0 {
		return labels
	}
	if cfg.KnownSpeakers == 1 || n == 1 {
		return labels
	}
	// Agglomerative average-linkage is deterministic and avoids scipy/sklearn at
	// runtime. It merges the closest centroids until an oracle count is reached,
	// or until no pair meets FunASR's 0.78 merge rule.
	groups := make([][]int, n)
	for i := range groups {
		groups[i] = []int{i}
	}
	target := cfg.KnownSpeakers
	if target < 1 {
		target = cfg.MinSpeakers
	}
	for len(groups) > target {
		bi, bj, best := -1, -1, float32(-2)
		for i := 0; i < len(groups); i++ {
			ci := centroid(groups[i], embs)
			for j := i + 1; j < len(groups); j++ {
				v := cosine(ci, centroid(groups[j], embs))
				if v > best {
					bi, bj, best = i, j, v
				}
			}
		}
		// Automatic clustering normally stops below the similarity threshold,
		// but MaxSpeakers is a real upper bound, not merely documentation.  In
		// a long/noisy recording every analysis window can otherwise become a
		// distinct "speaker", causing one ASR request per window.
		mustRespectMax := cfg.KnownSpeakers == 0 && len(groups) > cfg.MaxSpeakers
		if bi < 0 || (cfg.KnownSpeakers == 0 && !mustRespectMax && best < cfg.MergeThreshold) {
			break
		}
		groups[bi] = append(groups[bi], groups[bj]...)
		groups = append(groups[:bj], groups[bj+1:]...)
	}
	for id, g := range groups {
		for _, i := range g {
			labels[i] = id
		}
	}
	return labels
}

func centroid(indexes []int, embs [][]float32) []float32 {
	c := make([]float32, len(embs[0]))
	for _, i := range indexes {
		for d, v := range embs[i] {
			c[d] += v
		}
	}
	normalize(c)
	return c
}
func cosine(a, b []float32) float32 {
	var s float32
	for i := range a {
		if i < len(b) {
			s += a[i] * b[i]
		}
	}
	return s
}
func normalize(x []float32) {
	var n float64
	for _, v := range x {
		n += float64(v * v)
	}
	if n == 0 {
		return
	}
	q := float32(1 / math.Sqrt(n))
	for i := range x {
		x[i] *= q
	}
}
func mergeSpans(in []speechSpan, gap float64) []speechSpan {
	if len(in) < 2 {
		return in
	}
	out := []speechSpan{in[0]}
	for _, s := range in[1:] {
		p := &out[len(out)-1]
		if s.start-p.end <= gap {
			p.end = s.end
		} else {
			out = append(out, s)
		}
	}
	return out
}
func smooth(in []Segment) []Segment {
	if len(in) == 0 {
		return nil
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].Start < in[j].Start })
	// Split overlapping analysis windows halfway, then discard/smooth isolated
	// sub-700ms turns as in FunASR's postprocess().
	for i := 1; i < len(in); i++ {
		if in[i-1].End > in[i].Start {
			mid := (in[i-1].End + in[i].Start) / 2
			in[i-1].End = mid
			in[i].Start = mid
		}
	}
	for i := range in {
		if in[i].End-in[i].Start < .7 {
			if i == 0 && len(in) > 1 {
				in[i].Speaker = in[i+1].Speaker
			} else if i == len(in)-1 && i > 0 {
				in[i].Speaker = in[i-1].Speaker
			} else if i > 0 && i+1 < len(in) {
				if in[i].Start-in[i-1].End <= in[i+1].Start-in[i].End {
					in[i].Speaker = in[i-1].Speaker
				} else {
					in[i].Speaker = in[i+1].Speaker
				}
			}
		}
	}
	out := []Segment{in[0]}
	for _, s := range in[1:] {
		p := &out[len(out)-1]
		if s.Speaker == p.Speaker && s.Start <= p.End+.001 {
			p.End = maxf(p.End, s.End)
		} else if s.End > s.Start {
			out = append(out, s)
		}
	}
	return out
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
