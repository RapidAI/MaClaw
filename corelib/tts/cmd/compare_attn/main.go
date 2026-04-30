// Compare Go attention internals with Python reference.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	pyPre := loadBin("corelib/tts/testdata/ref_flow_attn_00_pre.bin")
	pyQ := loadBin("corelib/tts/testdata/ref_flow_attn_01_q.bin")
	pyScoresBefore := loadBin("corelib/tts/testdata/ref_flow_attn_02_scores_before_rel.bin")
	pyRelEmb := loadBin("corelib/tts/testdata/ref_flow_attn_03_rel_emb_used.bin")
	pyRelLogits := loadBin("corelib/tts/testdata/ref_flow_attn_04_rel_logits.bin")
	pyScoresLocal := loadBin("corelib/tts/testdata/ref_flow_attn_05_scores_local.bin")
	pyScoresCombined := loadBin("corelib/tts/testdata/ref_flow_attn_06_scores_combined.bin")
	pyAttnOut := loadBin("corelib/tts/testdata/ref_flow_attn_08_attn_output.bin")

	hp := tts.DefaultHParams()
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil {
		fmt.Printf("Load error: %v\n", err)
		os.Exit(1)
	}

	T := 33
	hidden := 192
	nHeads := 2
	headDim := 96

	// Use Python's pre output as input to attention
	layer := &w.Flow.Layers[3].Enc[0] // flow coupling 3, FFT layer 0

	// Q, K, V projections
	q := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvQ.Weight, 1, hidden, 1, 0, layer.Attn.ConvQ.Bias)
	k := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvK.Weight, 1, hidden, 1, 0, layer.Attn.ConvK.Bias)

	compare("Q projection", q, pyQ)

	// Standard attention scores
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	goScores := make([]float32, nHeads*T*T)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * k[(hOff+d)*T+tk]
				}
				goScores[h*T*T+tq*T+tk] = dot * scale
			}
		}
	}
	compare("scores_before_rel", goScores, pyScoresBefore)

	// Now test relative position bias
	// Python: emb_rel_k [1, 9, 96], padded to [1, 65, 96], used [1, 65, 96]
	embRelK := layer.Attn.EmbRelK // raw from GGUF: [1, 9, 96] = 864 floats
	fmt.Printf("\nembRelK: %d elements\n", len(embRelK))

	// Python's _get_relative_embeddings:
	// pad_length = max(33 - 5, 0) = 28
	// padded = F.pad(emb, [0, 0, 28, 28]) → [1, 65, 96]
	// slice [0:65] → [1, 65, 96]
	windowSize := 4
	relLen := 2*windowSize + 1 // 9
	padLength := T - (windowSize + 1) // 28
	usedLen := 2*T - 1 // 65

	// Build padded relative embeddings
	goRelEmb := make([]float32, usedLen*headDim) // [65, 96]
	for m := 0; m < usedLen; m++ {
		srcIdx := m - padLength // index into original [9, 96]
		for d := 0; d < headDim; d++ {
			if srcIdx >= 0 && srcIdx < relLen {
				goRelEmb[m*headDim+d] = embRelK[srcIdx*headDim+d]
			}
			// else: zero (padding)
		}
	}
	compare("rel_emb_used", goRelEmb, pyRelEmb)

	// Q @ rel_K^T: [2, 33, 65]
	goRelLogits := make([]float32, nHeads*T*usedLen)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		for tq := 0; tq < T; tq++ {
			for m := 0; m < usedLen; m++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * scale * goRelEmb[m*headDim+d]
				}
				goRelLogits[h*T*usedLen+tq*usedLen+m] = dot
			}
		}
	}
	compare("rel_logits", goRelLogits, pyRelLogits)

	// _relative_position_to_absolute_position: [2, 33, 65] → [2, 33, 33]
	goScoresLocal := make([]float32, nHeads*T*T)
	for h := 0; h < nHeads; h++ {
		// Pad: [33, 65] → [33, 66]
		padded := make([]float32, T*(usedLen+1))
		for tq := 0; tq < T; tq++ {
			for m := 0; m < usedLen; m++ {
				padded[tq*(usedLen+1)+m] = goRelLogits[h*T*usedLen+tq*usedLen+m]
			}
			// last column is 0 (padding)
		}
		// Flatten: [33*66] = [2178]
		flatLen := T * (usedLen + 1)
		// Pad end: [2178 + 32] = [2210]
		flat := make([]float32, flatLen+T-1)
		copy(flat, padded)
		// Reshape: [34, 65] → slice [:33, 32:]
		rowLen := usedLen // 65 = 2*T-1
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				idx := tq*rowLen + (T - 1 + tk)
				if idx < len(flat) {
					goScoresLocal[h*T*T+tq*T+tk] = flat[idx]
				}
			}
		}
	}
	compare("scores_local", goScoresLocal, pyScoresLocal)

	// Combined
	goCombined := make([]float32, len(goScores))
	for i := range goCombined {
		goCombined[i] = goScores[i] + goScoresLocal[i]
	}
	compare("scores_combined", goCombined, pyScoresCombined)

	// Softmax + value output
	v := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvV.Weight, 1, hidden, 1, 0, layer.Attn.ConvV.Bias)

	goAttnOut := make([]float32, hidden*T)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		for tq := 0; tq < T; tq++ {
			row := make([]float32, T)
			copy(row, goCombined[h*T*T+tq*T:h*T*T+(tq+1)*T])
			tensor.Softmax(row)

			for d := 0; d < headDim; d++ {
				var sum float32
				for tk := 0; tk < T; tk++ {
					sum += row[tk] * v[(hOff+d)*T+tk]
				}
				goAttnOut[(hOff+d)*T+tq] = sum
			}

			// Relative value bias
			if layer.Attn.EmbRelV != nil {
				embRelV := layer.Attn.EmbRelV
				goRelEmbV := make([]float32, usedLen*headDim)
				for m := 0; m < usedLen; m++ {
					srcIdx := m - padLength
					for dd := 0; dd < headDim; dd++ {
						if srcIdx >= 0 && srcIdx < relLen {
							goRelEmbV[m*headDim+dd] = embRelV[srcIdx*headDim+dd]
						}
					}
				}

				// _absolute_position_to_relative_position
				relWeights := make([]float32, T*(2*T))
				for tk := 0; tk < T; tk++ {
					relWeights[tk+T-1] = 0 // will be filled
				}
				// Pad: [33, 33] → [33, 65]
				padRow := make([]float32, T*(T+T-1))
				for tk := 0; tk < T; tk++ {
					padRow[tk] = row[tk]
				}
				// Actually use the full Python algorithm
				// pad [33] → [33 + 32] = [65]
				rowPadded := make([]float32, T+T-1)
				copy(rowPadded, row)
				// This is per-query, need full [T, T] → [T, 2T-1]
				// Skip for now — the key comparison is scores_local
				_ = goRelEmbV
			}
		}
	}

	// Output projection
	ow := layer.Attn.ConvO.Weight
	ob := layer.Attn.ConvO.Bias
	goOut := tts.Conv1D(goAttnOut, hidden, T, ow, 1, hidden, 1, 0, ob)
	_ = goOut
	_ = pyAttnOut

	fmt.Println("\n=== Summary ===")
	fmt.Println("If scores_local matches, the relative position bias is correct.")
	fmt.Println("If scores_combined matches, the full attention scores are correct.")
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path)
	if err != nil { return nil }
	n := len(data) / 4
	r := make([]float32, n)
	for i := 0; i < n; i++ {
		r[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return r
}

func compare(name string, go_, py []float32) {
	if len(go_) != len(py) {
		fmt.Printf("%-25s SIZE MISMATCH: Go=%d Py=%d\n", name, len(go_), len(py))
		return
	}
	var maxD, sumD float32
	for i := range go_ {
		d := float32(math.Abs(float64(go_[i] - py[i])))
		if d > maxD { maxD = d }
		sumD += d
	}
	avgD := sumD / float32(len(go_))
	fmt.Printf("%-25s maxDiff=%.6f avgDiff=%.6f\n", name, maxD, avgD)
}
