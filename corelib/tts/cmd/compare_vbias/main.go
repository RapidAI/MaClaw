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
	pyNoVBias := loadBin("corelib/tts/testdata/ref_attn_out_no_vbias.bin")
	pyWithVBias := loadBin("corelib/tts/testdata/ref_attn_out_with_vbias.bin")

	hp := tts.DefaultHParams()
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil { fmt.Printf("Load error: %v\n", err); os.Exit(1) }

	T := 33; hidden := 192; nHeads := 2; headDim := 96
	windowSize := 4; relLen := 9
	padLength := T - (windowSize + 1) // 28
	usedLen := 2*T - 1 // 65

	layer := &w.Flow.Layers[3].Enc[0]

	q := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvQ.Weight, 1, hidden, 1, 0, layer.Attn.ConvQ.Bias)
	k := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvK.Weight, 1, hidden, 1, 0, layer.Attn.ConvK.Bias)
	v := tts.Conv1D(pyPre, hidden, T, layer.Attn.ConvV.Weight, 1, hidden, 1, 0, layer.Attn.ConvV.Bias)

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))

	attnOut := make([]float32, hidden*T)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim

		// Scores + relative key bias (verified correct)
		scores := make([]float32, T*T)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * k[(hOff+d)*T+tk]
				}
				scores[tq*T+tk] = dot * scale
			}
		}

		// Relative key bias
		goRelEmb := make([]float32, usedLen*headDim)
		for m := 0; m < usedLen; m++ {
			srcIdx := m - padLength
			for d := 0; d < headDim; d++ {
				if srcIdx >= 0 && srcIdx < relLen {
					goRelEmb[m*headDim+d] = layer.Attn.EmbRelK[srcIdx*headDim+d]
				}
			}
		}
		relLogits := make([]float32, T*usedLen)
		for tq := 0; tq < T; tq++ {
			for m := 0; m < usedLen; m++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * scale * goRelEmb[m*headDim+d]
				}
				relLogits[tq*usedLen+m] = dot
			}
		}
		// rel_to_abs
		padded := make([]float32, T*(usedLen+1))
		for tq := 0; tq < T; tq++ {
			copy(padded[tq*(usedLen+1):], relLogits[tq*usedLen:(tq+1)*usedLen])
		}
		flat := make([]float32, T*(usedLen+1)+T-1)
		copy(flat, padded)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				idx := tq*usedLen + (T - 1 + tk)
				if idx < len(flat) {
					scores[tq*T+tk] += flat[idx]
				}
			}
		}

		// Softmax
		for tq := 0; tq < T; tq++ {
			row := scores[tq*T : (tq+1)*T]
			tensor.Softmax(row)
		}

		// Weighted sum of values (no value bias)
		for tq := 0; tq < T; tq++ {
			for d := 0; d < headDim; d++ {
				var sum float32
				for tk := 0; tk < T; tk++ {
					sum += scores[tq*T+tk] * v[(hOff+d)*T+tk]
				}
				attnOut[(hOff+d)*T+tq] = sum
			}
		}
	}

	compare("attn_out (no vbias)", attnOut, pyNoVBias)

	// Now add value bias using the new implementation
	attnOutWithV := make([]float32, len(attnOut))
	copy(attnOutWithV, attnOut)

	for h := 0; h < nHeads; h++ {
		hOff := h * headDim

		// Recompute softmax'd scores for this head
		scoresH := make([]float32, T*T)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * k[(hOff+d)*T+tk]
				}
				scoresH[tq*T+tk] = dot * scale
			}
		}
		// Add key bias
		goRelEmbK := make([]float32, usedLen*headDim)
		for m := 0; m < usedLen; m++ {
			srcIdx := m - padLength
			for d := 0; d < headDim; d++ {
				if srcIdx >= 0 && srcIdx < relLen {
					goRelEmbK[m*headDim+d] = layer.Attn.EmbRelK[srcIdx*headDim+d]
				}
			}
		}
		relLogitsH := make([]float32, T*usedLen)
		for tq := 0; tq < T; tq++ {
			for m := 0; m < usedLen; m++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[(hOff+d)*T+tq] * scale * goRelEmbK[m*headDim+d]
				}
				relLogitsH[tq*usedLen+m] = dot
			}
		}
		paddedH := make([]float32, T*(usedLen+1))
		for tq := 0; tq < T; tq++ {
			copy(paddedH[tq*(usedLen+1):], relLogitsH[tq*usedLen:(tq+1)*usedLen])
		}
		flatH := make([]float32, T*(usedLen+1)+T-1)
		copy(flatH, paddedH)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				idx := tq*usedLen + (T - 1 + tk)
				if idx < len(flatH) {
					scoresH[tq*T+tk] += flatH[idx]
				}
			}
		}
		for tq := 0; tq < T; tq++ {
			tensor.Softmax(scoresH[tq*T : (tq+1)*T])
		}

		// Value bias: abs_to_rel on softmax'd scores, then matmul with rel_v
		relEmbV := make([]float32, usedLen*headDim)
		for m := 0; m < usedLen; m++ {
			srcIdx := m - padLength
			for d := 0; d < headDim; d++ {
				if srcIdx >= 0 && srcIdx < relLen {
					relEmbV[m*headDim+d] = layer.Attn.EmbRelV[srcIdx*headDim+d]
				}
			}
		}

		// _absolute_position_to_relative_position per head
		rowPadded := T + T - 1
		flatLen := T * rowPadded
		totalLen := T + flatLen
		flatV := make([]float32, totalLen)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				flatV[T+tq*rowPadded+tk] = scoresH[tq*T+tk]
			}
		}
		reshapeRow := 2 * T
		for tq := 0; tq < T; tq++ {
			for m := 0; m < usedLen; m++ {
				idx := tq*reshapeRow + m + 1
				if idx >= totalLen { continue }
				w := flatV[idx]
				if w == 0 { continue }
				for d := 0; d < headDim; d++ {
					attnOutWithV[(hOff+d)*T+tq] += w * relEmbV[m*headDim+d]
				}
			}
		}
	}

	compare("attn_out (with vbias)", attnOutWithV, pyWithVBias)
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path); if err != nil { return nil }
	n := len(data) / 4; r := make([]float32, n)
	for i := 0; i < n; i++ { r[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:])) }
	return r
}

func compare(name string, go_, py []float32) {
	if len(go_) != len(py) { fmt.Printf("%-30s SIZE: Go=%d Py=%d\n", name, len(go_), len(py)); return }
	var maxD, sumD float32
	for i := range go_ { d := float32(math.Abs(float64(go_[i]-py[i]))); if d > maxD { maxD = d }; sumD += d }
	fmt.Printf("%-30s maxDiff=%.6f avgDiff=%.6f\n", name, maxD, sumD/float32(len(go_)))
}
