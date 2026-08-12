package ocr

import (
	"fmt"
	"image"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

// recognizeLine runs the recognition model on one cropped text line and
// returns the decoded text plus the mean per-token confidence (RapidOCR /
// CTCLabelDecode convention). The rec graph ends with Softmax, so the classic
// path's outputs are already probabilities (verified: output rows sum to 1).
// When onnxrt detects the fusable CTC head (MatMul -> Add -> Softmax), the
// fused path decodes per-frame argmax ids and their probabilities directly
// without materializing the [T, vocab] tensor; any fused-run failure falls
// back to the classic path, preserving the previous behavior. sc may be nil;
// when set, the rec input tensor aliases scratch memory (consumed by
// Graph.Run / Graph.RunCTC).
func recognizeLine(rec *onnxrt.Graph, crop *image.RGBA, sc *engineScratch) (string, float64, error) {
	input := recPreprocessS(crop, sc)
	if rec.HasCTCHead() {
		ids, probs, vocab, err := rec.RunCTC(map[string]*onnxrt.Tensor{"x": input})
		if err == nil {
			dict, err := DictForVocab(vocab)
			if err != nil {
				return "", 0, err
			}
			text, conf := ctcGreedyDecodeIDs(ids, probs, dict)
			return text, conf, nil
		}
	}
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
	dict, err := DictForVocab(vocab)
	if err != nil {
		return "", 0, err
	}
	text, conf := ctcGreedyDecode(logits.F32, tlen, vocab, dict)
	return text, conf, nil
}

// recognizeBatch performs one dynamic-batch rec inference for same-width
// crops. The fused CTC tail returns a contiguous sequence per sample, so each
// sample is decoded with the same greedy semantics as recognizeLine.
const recBatchMaxLines = 16

func recognizeBatch(rec *onnxrt.Graph, src *image.RGBA, boxes []DetBox, idxs []int, rw int, sc *engineScratch, texts []string, confs []float64) error {
	if len(idxs) == 1 {
		text, conf, err := recognizeLine(rec, cropBoxS(src, boxes[idxs[0]].Points, sc), sc)
		if err != nil {
			return err
		}
		texts[idxs[0]], confs[idxs[0]] = text, conf
		return nil
	}
	if !rec.HasCTCHead() {
		for _, i := range idxs {
			text, conf, err := recognizeLine(rec, cropBoxS(src, boxes[i].Points, sc), sc)
			if err != nil {
				return err
			}
			texts[i], confs[i] = text, conf
		}
		return nil
	}
	per := 3 * recHeight * rw
	buf := f32Scratch(&sc.recBatch, len(idxs)*per)
	for bi, i := range idxs {
		crop := cropBoxS(src, boxes[i].Points, sc)

		resized := resizeRGBAS(crop, rw, recHeight, &sc.recImg)
		recPreprocessInto(resized, buf[bi*per:(bi+1)*per], rw)
	}
	input := &onnxrt.Tensor{Shape: []int{len(idxs), 3, recHeight, rw}, DType: onnxrt.DFloat32, F32: buf}
	ids, probs, vocab, err := rec.RunCTC(map[string]*onnxrt.Tensor{"x": input})
	if err != nil {
		return fmt.Errorf("ocr: batched rec run: %w", err)
	}
	dict, err := DictForVocab(vocab)
	if err != nil {
		return err
	}
	frames := len(ids) / len(idxs)
	if frames*len(idxs) != len(ids) || len(probs) != len(ids) {
		return fmt.Errorf("ocr: batched rec output has %d ids for batch %d", len(ids), len(idxs))
	}
	for bi, i := range idxs {
		texts[i], confs[i] = ctcGreedyDecodeIDs(ids[bi*frames:(bi+1)*frames], probs[bi*frames:(bi+1)*frames], dict)
	}
	return nil
}

// recognizeBatchPadded batches widths that have the same CTC frame count.
// Each crop is resized at its own width and copied into the batch tensor;
// only the unused right edge is padded with the normalized black value (-1).
func recognizeBatchPadded(rec *onnxrt.Graph, src *image.RGBA, boxes []DetBox, widths, idxs []int, rw int, sc *engineScratch, texts []string, confs []float64) error {
	if len(idxs) == 1 || !rec.HasCTCHead() {
		return recognizeBatch(rec, src, boxes, idxs, rw, sc, texts, confs)
	}
	per := 3 * recHeight * rw
	buf := f32Scratch(&sc.recBatch, len(idxs)*per)
	for bi, i := range idxs {
		w := widths[i]
		crop := cropBoxS(src, boxes[i].Points, sc)
		if got := recWidth(crop); got != w {
			return fmt.Errorf("ocr: crop width changed while batching: got %d, want %d", got, w)
		}
		resized := resizeRGBAS(crop, w, recHeight, &sc.recImg)
		sample := buf[bi*per : (bi+1)*per]
		// Normalize directly into its final padded tensor planes. This avoids
		// both the batch-wide fill and the compact-then-copy pass while keeping
		// the model's required normalized-black (-1) padding.
		recPreprocessPaddedInto(resized, sample, w, rw)
	}
	input := &onnxrt.Tensor{Shape: []int{len(idxs), 3, recHeight, rw}, DType: onnxrt.DFloat32, F32: buf}
	ids, probs, vocab, err := rec.RunCTC(map[string]*onnxrt.Tensor{"x": input})
	if err != nil {
		return fmt.Errorf("ocr: batched rec run: %w", err)
	}
	dict, err := DictForVocab(vocab)
	if err != nil {
		return err
	}
	frames := len(ids) / len(idxs)
	if frames*len(idxs) != len(ids) || len(probs) != len(ids) {
		return fmt.Errorf("ocr: batched rec output has %d ids for batch %d", len(ids), len(idxs))
	}
	for bi, i := range idxs {
		valid := (widths[i] + 7) / 8
		if valid > frames {
			valid = frames
		}
		texts[i], confs[i] = ctcGreedyDecodeIDs(ids[bi*frames:bi*frames+valid], probs[bi*frames:bi*frames+valid], dict)
	}
	return nil
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

// ctcGreedyDecodeIDs is ctcGreedyDecode on pre-decoded per-frame argmax ids
// and their probabilities (fused CTC head path); the collapse and confidence
// logic is identical: repeats and the blank id 0 are removed, confidence is
// the mean of the kept tokens' max probabilities, empty text yields 0.
func ctcGreedyDecodeIDs(ids []int, probs []float32, dict []string) (string, float64) {
	var out []rune
	var confSum float64
	var confN int
	prev := -1
	for t, best := range ids {
		if best != 0 && best != prev {
			if best < len(dict) {
				out = append(out, []rune(dict[best])...)
			}
			confSum += float64(probs[t])
			confN++
		}
		prev = best
	}
	if confN == 0 {
		return "", 0
	}
	return string(out), confSum / float64(confN)
}
