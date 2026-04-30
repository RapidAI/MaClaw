package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	pyPre := loadBin("corelib/tts/testdata/ref_flow_attn_00_pre.bin")
	pyAttnY := loadBin("corelib/tts/testdata/ref_enc_layer_01_attn_y.bin")
	pyAfterNorm1 := loadBin("corelib/tts/testdata/ref_enc_layer_02_after_norm1.bin")
	pyFFNY := loadBin("corelib/tts/testdata/ref_enc_layer_03_ffn_y.bin")
	pyOutput := loadBin("corelib/tts/testdata/ref_enc_layer_04_output.bin")

	hp := tts.DefaultHParams()
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil { fmt.Printf("Load error: %v\n", err); os.Exit(1) }

	T := 33; hidden := 192
	layer := &w.Flow.Layers[3].Enc[0]

	// Run Go's full encoder layer
	goInput := make([]float32, len(pyPre))
	copy(goInput, pyPre)
	goOutput := tts.EncoderLayerForwardExported(goInput, hidden, T, layer, hp)

	compare("full_layer_output", goOutput, pyOutput)

	// The Go function modifies x in-place and returns it.
	// To debug, I need to check intermediate values.
	// Let me manually replicate the Go encoder layer steps.

	// Step 1: Attention (on raw x)
	x := make([]float32, len(pyPre))
	copy(x, pyPre)

	q := tts.Conv1D(x, hidden, T, layer.Attn.ConvQ.Weight, 1, hidden, 1, 0, layer.Attn.ConvQ.Bias)
	k := tts.Conv1D(x, hidden, T, layer.Attn.ConvK.Weight, 1, hidden, 1, 0, layer.Attn.ConvK.Bias)
	v := tts.Conv1D(x, hidden, T, layer.Attn.ConvV.Weight, 1, hidden, 1, 0, layer.Attn.ConvV.Bias)

	// Run attention manually (same as encoderLayerForward)
	nHeads := 2; headDim := 96
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	windowSize := 4; relLen := 9
	padLength := T - (windowSize + 1)
	usedLen := 2*T - 1

	attnOut := make([]float32, hidden*T)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
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

		// Relative position bias (correct implementation from compare_attn)
		if layer.Attn.EmbRelK != nil {
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
				for m := 0; m < usedLen; m++ {
					padded[tq*(usedLen+1)+m] = relLogits[tq*usedLen+m]
				}
			}
			flat := make([]float32, T*(usedLen+1)+T-1)
			copy(flat, padded)
			for tq := 0; tq < T; tq++ {
				for tk := 0; tk < T; tk++ {
					idx := tq*(usedLen) + (T - 1 + tk)
					if idx < len(flat) {
						scores[tq*T+tk] += flat[idx]
					}
				}
			}
		}

		// Softmax + weighted sum
		for tq := 0; tq < T; tq++ {
			row := scores[tq*T : (tq+1)*T]
			softmax(row)
			for d := 0; d < headDim; d++ {
				var sum float32
				for tk := 0; tk < T; tk++ {
					sum += row[tk] * v[(hOff+d)*T+tk]
				}
				attnOut[(hOff+d)*T+tq] = sum
			}

			// Relative value bias
			if layer.Attn.EmbRelV != nil {
				goRelEmbV := make([]float32, usedLen*headDim)
				for m := 0; m < usedLen; m++ {
					srcIdx := m - padLength
					for d := 0; d < headDim; d++ {
						if srcIdx >= 0 && srcIdx < relLen {
							goRelEmbV[m*headDim+d] = layer.Attn.EmbRelV[srcIdx*headDim+d]
						}
					}
				}
				// abs_to_rel: [T] → [2T-1]
				relW := make([]float32, 2*T)
				for tk := 0; tk < T; tk++ {
					relW[tk+T-1] = 0
				}
				// Full abs_to_rel
				rowPadded := make([]float32, T+T-1)
				copy(rowPadded, row)
				// Flatten + pad + reshape
				flat2 := make([]float32, T*T+T*(T-1))
				for tk := 0; tk < T; tk++ {
					for j := 0; j < T+T-1; j++ {
						if j < T {
							flat2[tk*(T+T-1)+j] = row[j] // wrong — need proper padding
						}
					}
				}
				// This is getting complex. Let me use the proper algorithm.
				// _absolute_position_to_relative_position:
				// pad [T, T] → [T, T + T-1]
				xPad := make([]float32, T*(T+T-1))
				for tq2 := 0; tq2 < T; tq2++ {
					// Only this query's row
					if tq2 == 0 {
						for tk := 0; tk < T; tk++ {
							xPad[tk] = row[tk]
						}
					}
				}
				// Actually this needs the full [T,T] attention matrix, not per-row.
				// Skip relative value bias for now — it's a secondary effect.
			}
		}
	}

	// conv_o
	y := tts.Conv1D(attnOut, hidden, T, layer.Attn.ConvO.Weight, 1, hidden, 1, 0, layer.Attn.ConvO.Bias)
	compare("attn_y (no rel_v bias)", y, pyAttnY)

	// Residual + norm1
	for i := range x {
		x[i] = pyPre[i] + y[i]
	}
	applyLayerNormCT(x, hidden, T, layer.Norm1.Weight, layer.Norm1.Bias)
	compare("after_norm1", x, pyAfterNorm1)

	// FFN
	kSize := layer.FFN.Conv1.KSize
	filter := layer.FFN.Conv1.OutCh
	ffn := tts.Conv1D(x, hidden, T, layer.FFN.Conv1.Weight, kSize, filter, 1, (kSize-1)/2, layer.FFN.Conv1.Bias)
	tts.ReLU(ffn)
	kSize2 := layer.FFN.Conv2.KSize
	ffn = tts.Conv1D(ffn, filter, T, layer.FFN.Conv2.Weight, kSize2, hidden, 1, (kSize2-1)/2, layer.FFN.Conv2.Bias)
	compare("ffn_y", ffn, pyFFNY)

	// Residual + norm2
	for i := range x {
		x[i] += ffn[i]
	}
	applyLayerNormCT(x, hidden, T, layer.Norm2.Weight, layer.Norm2.Bias)
	compare("layer_output", x, pyOutput)
}

func applyLayerNormCT(data []float32, C, T int, weight, bias []float32) {
	col := make([]float32, C)
	for t := 0; t < T; t++ {
		for c := 0; c < C; c++ { col[c] = data[c*T+t] }
		n := len(col)
		var mean float32
		for _, v := range col { mean += v }
		mean /= float32(n)
		var variance float32
		for _, v := range col { d := v - mean; variance += d * d }
		variance /= float32(n)
		scale := 1.0 / float32(math.Sqrt(float64(variance+1e-5)))
		for i := 0; i < n; i++ {
			v := (col[i] - mean) * scale
			if weight != nil && i < len(weight) { v *= weight[i] }
			if bias != nil && i < len(bias) { v += bias[i] }
			col[i] = v
		}
		for c := 0; c < C; c++ { data[c*T+t] = col[c] }
	}
}

func softmax(x []float32) {
	max := x[0]
	for _, v := range x { if v > max { max = v } }
	var sum float32
	for i := range x { x[i] = float32(math.Exp(float64(x[i] - max))); sum += x[i] }
	for i := range x { x[i] /= sum }
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path); if err != nil { return nil }
	n := len(data) / 4; r := make([]float32, n)
	for i := 0; i < n; i++ { r[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:])) }
	return r
}

func compare(name string, go_, py []float32) {
	if go_ == nil || py == nil { fmt.Printf("%-25s MISSING DATA\n", name); return }
	if len(go_) != len(py) { fmt.Printf("%-25s SIZE: Go=%d Py=%d\n", name, len(go_), len(py)); return }
	var maxD, sumD float32
	for i := range go_ { d := float32(math.Abs(float64(go_[i]-py[i]))); if d > maxD { maxD = d }; sumD += d }
	fmt.Printf("%-25s maxDiff=%.6f avgDiff=%.6f\n", name, maxD, sumD/float32(len(go_)))
}
