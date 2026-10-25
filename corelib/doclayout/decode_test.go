package doclayout

import (
	"math"
	"reflect"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

func TestDecodeReadingOrder(t *testing.T) {
	const queries = 3
	logits := onnxrt.NewFloat(1, queries, len(Labels))
	for i := range logits.F32 {
		logits.F32[i] = -8
	}
	setClass(logits, 0, 22, 8) // text, first in reading order
	setClass(logits, 1, 21, 8) // table
	boxes := onnxrt.NewFloat(1, queries, 4)
	putBox(boxes, 0, 0.25, 0.25, 0.2, 0.2)
	putBox(boxes, 1, 0.75, 0.70, 0.2, 0.2)
	putBox(boxes, 2, 0.5, 0.5, 0.2, 0.2)
	order := onnxrt.NewFloat(1, queries, queries)
	// Query 0 precedes query 1. Query 1 precedes nobody kept.
	order.F32[0*queries+1] = 2
	order.F32[1*queries+0] = -2
	got, err := Decode(logits, boxes, order, 100, 200, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("regions %d", len(got))
	}
	if got[0].Label != "text" || got[0].Order != 0 {
		t.Fatalf("first %+v", got[0])
	}
	if math.Abs(got[0].Box.X0-15) > 1e-6 || math.Abs(got[0].Box.Y0-30) > 1e-6 || math.Abs(got[0].Box.X1-35) > 1e-6 || math.Abs(got[0].Box.Y1-70) > 1e-6 {
		t.Fatalf("box %+v", got[0].Box)
	}
	if got[1].Label != "table" || got[1].Order != 1 {
		t.Fatalf("second %+v", got[1])
	}
	lines := []PixelLine{{X: 70, Y: 120, W: 10, H: 10}, {X: 16, Y: 40, W: 10, H: 10}}
	if orderIdx := ReadingOrder(lines, got); !reflect.DeepEqual(orderIdx, []int{1, 0}) {
		t.Fatalf("order %v", orderIdx)
	}
}

func TestDecodeDropsLowScore(t *testing.T) {
	logits := onnxrt.NewFloat(1, 1, len(Labels))
	for i := range logits.F32 {
		logits.F32[i] = -8
	}
	boxes := onnxrt.NewFloat(1, 1, 4)
	putBox(boxes, 0, 0.5, 0.5, 0.4, 0.4)
	order := onnxrt.NewFloat(1, 1, 1)
	got, err := Decode(logits, boxes, order, 10, 10, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("kept %+v", got)
	}
}

func setClass(t *onnxrt.Tensor, query, class int, logit float32) {
	t.F32[query*len(Labels)+class] = logit
}

func putBox(t *onnxrt.Tensor, query int, cx, cy, w, h float32) {
	base := query * 4
	t.F32[base] = cx
	t.F32[base+1] = cy
	t.F32[base+2] = w
	t.F32[base+3] = h
}
