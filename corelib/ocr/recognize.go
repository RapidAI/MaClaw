package ocr

import (
	"fmt"
	"image"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

// recognizeLine runs the recognition model on one cropped text line and
// returns the decoded text plus the mean per-token confidence (RapidOCR /
// CTCLabelDecode convention). The rec graph ends with Softmax, so outputs are
// already probabilities (verified: output rows sum to 1).
func recognizeLine(rec *onnxrt.Graph, crop *image.RGBA) (string, float64, error) {
	input := recPreprocess(crop)
	outputs, err := rec.Run(map[string]*onnxrt.Tensor{"x": input})
	if err != nil {
		return "", 0, fmt.Errorf("ocr: rec run: %w", err)
	}
	var logits *onnxrt.Tensor
	for _, name := range rec.OutputNames() {
		if t := outputs[name]; t != nil && t.Rank() == 3 && t.Shape[0] == 1 {
			logits = t
			break
		}
	}
	if logits == nil {
		return "", 0, fmt.Errorf("ocr: rec output tensor not found")
	}
	shape := logits.Shape
	tlen, vocab := shape[1], shape[2]
	if vocab != VocabSize() {
		return "", 0, fmt.Errorf("ocr: rec vocab %d does not match dict size %d", vocab, VocabSize())
	}
	text, conf := ctcGreedyDecode(logits.F32, tlen, vocab, Dict())
	return text, conf, nil
}

// ctcGreedyDecode collapses per-frame argmax ids (removing repeats and the
// blank id 0) and maps them through dict. Confidence is the mean of the
// per-token max probabilities; empty text yields 0.
func ctcGreedyDecode(probs []float32, tlen, vocab int, dict []string) (string, float64) {
	var out []rune
	var confSum float64
	var confN int
	prev := -1
	for t := 0; t < tlen; t++ {
		row := probs[t*vocab : (t+1)*vocab]
		best, bestP := 0, float32(-1)
		for i, p := range row {
			if p > bestP {
				best, bestP = i, p
			}
		}
		if best != 0 && best != prev {
			if best < len(dict) {
				out = append(out, []rune(dict[best])...)
			}
			confSum += float64(bestP)
			confN++
		}
		prev = best
	}
	if confN == 0 {
		return "", 0
	}
	return string(out), confSum / float64(confN)
}
