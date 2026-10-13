package asr

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"runtime/debug"
)

// SenseVoice Metal backend selection (darwin only). MACLAW_SENSEVOICE_METAL=0
// (or MACLAW_METAL=0) disables it; init or self-check failure keeps the CPU
// path, and a runtime GPU error falls back to the CPU for this model.

const svMetalSelfCheckMinCos = 0.9999

func metalDisabled(specific string) bool {
	return os.Getenv(specific) == "0" || os.Getenv("MACLAW_METAL") == "0"
}

func (m *SenseVoiceModel) initGPU() {
	if runtime.GOOS != "darwin" {
		return
	}
	if metalDisabled("MACLAW_SENSEVOICE_METAL") {
		m.gpuReason = "disabled by env"
		return
	}
	g, err := newSVGPU(m)
	// The upload staging copies (~2× the weights while growing) are garbage now.
	debug.FreeOSMemory()
	if err != nil {
		m.gpuReason = err.Error()
		return
	}
	if err := m.svMetalSelfCheck(g); err != nil {
		g.release()
		m.gpuReason = "self-check: " + err.Error()
		return
	}
	m.gpu = g
	log.Printf("asr: SenseVoice Metal GPU backend active (%d MB weights)", g.WeightSize>>20)
}

// svMetalSelfCheck compares GPU and CPU CTC logits on deterministic audio.
func (m *SenseVoiceModel) svMetalSelfCheck(g *svGPU) error {
	pcm := make([]float32, 16000*2)
	seed := uint32(12345)
	for i := range pcm {
		seed = seed*1664525 + 1013904223
		t := float64(i) / 16000
		pcm[i] = float32(0.3*math.Sin(2*math.Pi*(220+200*t)*t) + 0.05*(float64(seed>>8)/float64(1<<24)-0.5))
	}
	fb, lfr, n := m.frontend(pcm)
	if n <= 0 {
		return fmt.Errorf("frontend failed")
	}
	_ = fb
	ref := m.encode(lfr, n) // CPU logits [T × vocab]
	refCopy := append([]float32(nil), ref...)
	feats, T := m.prepFeats(lfr, n)
	got, err := g.encode(feats, T, m.hp.FeatsDim)
	if err != nil {
		return err
	}
	V, S := m.hp.VocabSize, g.logitStride()
	minCos := 1.0
	for r := 0; r < T; r++ {
		var d, na, nb float64
		for c := 0; c < V; c++ {
			a, b := float64(refCopy[r*V+c]), float64(got[r*S+c])
			d += a * b
			na += a * a
			nb += b * b
		}
		minCos = math.Min(minCos, d/math.Sqrt(na*nb))
	}
	if !(minCos >= svMetalSelfCheckMinCos) {
		return fmt.Errorf("logit cos %.6f < %.4f", minCos, svMetalSelfCheckMinCos)
	}
	return nil
}

// frontend runs fbank + LFR into model scratch (as Transcribe does).
func (m *SenseVoiceModel) frontend(pcm []float32) (fbank, lfr []float32, lfrFrames int) {
	numFbankFrames := (len(pcm)-svWindowSize)/svHopSize + 1
	if numFbankFrames <= 0 {
		return nil, nil, 0
	}
	lfrFrames = (numFbankFrames-svLFRm)/svLFRn + 1
	if lfrFrames <= 0 {
		lfrFrames = 1
	}
	bufs := m.ensureEncBufs(4 + lfrFrames)
	fbank = make([]float32, numFbankFrames*svNumMels)
	if !svMelFilterbankInto(pcm, fbank) {
		return nil, nil, 0
	}
	_ = bufs
	lfr = make([]float32, lfrFrames*svFeatsDim)
	lfrFrames = svApplyLFRInto(fbank, numFbankFrames, lfr)
	return fbank, lfr, lfrFrames
}

// GPUBackend reports whether the Metal encoder is active and, if not, why.
func (m *SenseVoiceModel) GPUBackend() (bool, string) { return m.gpu != nil, m.gpuReason }

// GPUMemoryBytes reports Metal buffer sizes (weights, activations).
func (m *SenseVoiceModel) GPUMemoryBytes() (int, int) {
	if m.gpu == nil {
		return 0, 0
	}
	return m.gpu.WeightSize, m.gpu.ActBytes()
}

// encodeArgmaxGPU runs the encoder + CTC head on the GPU and returns per-frame
// argmax ids (first maximum, like the CPU kernel). Caller holds m.mu.
func (m *SenseVoiceModel) encodeArgmaxGPU(lfrFeats []float32, lfrFrames int) ([]int, bool) {
	g := m.gpu
	if g == nil {
		return nil, false
	}
	feats, T := m.prepFeats(lfrFeats, lfrFrames)
	logits, err := g.encode(feats, T, m.hp.FeatsDim)
	if err != nil {
		log.Printf("asr: SenseVoice Metal backend failed, falling back to CPU: %v", err)
		g.release()
		m.gpu = nil
		return nil, false
	}
	bufs := m.encBufs
	if cap(bufs.ctcIDs) < T {
		bufs.ctcIDs = make([]int, T)
	}
	ids := bufs.ctcIDs[:T]
	V, S := m.hp.VocabSize, g.logitStride()
	for r := 0; r < T; r++ {
		row := logits[r*S : r*S+V]
		best, bi := row[0], 0
		for c := 1; c < V; c++ {
			if row[c] > best {
				best, bi = row[c], c
			}
		}
		ids[r] = bi
	}
	return ids, true
}
