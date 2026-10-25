package doclayout

import (
	"fmt"
	"math"
	"sort"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

// Box is an axis-aligned region in the source image, origin at the top-left.
type Box struct {
	X0, Y0, X1, Y1 float64
}

// Width returns the box width in image pixels.
func (b Box) Width() float64 { return b.X1 - b.X0 }

// Height returns the box height in image pixels.
func (b Box) Height() float64 { return b.Y1 - b.Y0 }

// Contains reports whether the point lies inside the half-open box.
func (b Box) Contains(x, y float64) bool {
	return x >= b.X0 && x < b.X1 && y >= b.Y0 && y < b.Y1
}

// Region is one kept layout query.
type Region struct {
	Label string
	Score float64
	Box   Box
	Order int
}

// PixelLine is a text line in the same image space as Region.Box.
type PixelLine struct {
	X, Y, W, H float64
}

// Decode turns one PP-DocLayoutV3 forward into reading order.
// Class scores are sigmoid, not softmax. A query is kept when its best
// class score is at least threshold. Boxes are cx, cy, w, h normalized to
// the stretched model input, mapped back onto origW by origH. orderLogits[i, j]
// greater than zero means query i precedes query j. Rank is the number of
// other kept queries each box precedes.
func Decode(logits, boxes, orderLogits *onnxrt.Tensor, origW, origH int, threshold float64) ([]Region, error) {
	if origW < 1 || origH < 1 {
		return nil, fmt.Errorf("doclayout: image size %dx%d", origW, origH)
	}
	if threshold <= 0 || threshold >= 1 {
		return nil, fmt.Errorf("doclayout: threshold %v", threshold)
	}
	classN := len(Labels)
	scores, err := plane(logits, classN)
	if err != nil {
		return nil, fmt.Errorf("doclayout: logits: %w", err)
	}
	coords, err := plane(boxes, 4)
	if err != nil {
		return nil, fmt.Errorf("doclayout: boxes: %w", err)
	}
	order, err := square(orderLogits)
	if err != nil {
		return nil, fmt.Errorf("doclayout: order: %w", err)
	}
	queries := len(scores) / classN
	if queries == 0 || len(coords)/4 != queries || len(order) != queries*queries {
		return nil, fmt.Errorf("doclayout: query count logits %d boxes %d order %d", queries, len(coords)/4, int(math.Sqrt(float64(len(order)))))
	}
	type kept struct {
		region Region
		query  int
		wins   int
	}
	var rows []kept
	for q := 0; q < queries; q++ {
		bestC := 0
		best := sigmoid(scores[q*classN])
		for c := 1; c < classN; c++ {
			s := sigmoid(scores[q*classN+c])
			if s > best {
				best = s
				bestC = c
			}
		}
		if best < threshold {
			continue
		}
		cx := float64(coords[q*4])
		cy := float64(coords[q*4+1])
		bw := float64(coords[q*4+2])
		bh := float64(coords[q*4+3])
		box := Box{
			X0: (cx - bw/2) * float64(origW),
			Y0: (cy - bh/2) * float64(origH),
			X1: (cx + bw/2) * float64(origW),
			Y1: (cy + bh/2) * float64(origH),
		}
		if !finiteBox(box) {
			continue
		}
		box = clipBox(box, origW, origH)
		if box.Width() < 1 || box.Height() < 1 {
			continue
		}
		rows = append(rows, kept{region: Region{Label: Labels[bestC], Score: best, Box: box}, query: q})
	}
	for i := range rows {
		wins := 0
		for j := range rows {
			if i == j {
				continue
			}
			if order[rows[i].query*queries+rows[j].query] > 0 {
				wins++
			}
		}
		rows[i].wins = wins
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].wins != rows[j].wins {
			return rows[i].wins > rows[j].wins
		}
		if rows[i].region.Box.Y0 != rows[j].region.Box.Y0 {
			return rows[i].region.Box.Y0 < rows[j].region.Box.Y0
		}
		return rows[i].region.Box.X0 < rows[j].region.Box.X0
	})
	out := make([]Region, len(rows))
	for i := range rows {
		rows[i].region.Order = i
		out[i] = rows[i].region
	}
	return out, nil
}

// ReadingOrder returns line indexes grouped by region reading order.
// A line belongs to the earliest region that contains its center. Lines
// outside every region keep their relative order after the grouped lines.
func ReadingOrder(lines []PixelLine, regions []Region) []int {
	sorted := append([]Region(nil), regions...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	buckets := make([][]int, len(sorted)+1)
	for i, line := range lines {
		cx := line.X + line.W/2
		cy := line.Y + line.H/2
		bin := len(sorted)
		for r, region := range sorted {
			if region.Box.Contains(cx, cy) {
				bin = r
				break
			}
		}
		buckets[bin] = append(buckets[bin], i)
	}
	out := make([]int, 0, len(lines))
	for _, bucket := range buckets {
		out = append(out, bucket...)
	}
	return out
}

func plane(t *onnxrt.Tensor, last int) ([]float32, error) {
	if t == nil || t.DType != onnxrt.DFloat32 || len(t.Shape) < 2 || t.Shape[len(t.Shape)-1] != last {
		if t == nil {
			return nil, fmt.Errorf("missing")
		}
		return nil, fmt.Errorf("shape %v dtype %v", t.Shape, t.DType)
	}
	n := 1
	for _, d := range t.Shape[:len(t.Shape)-1] {
		n *= d
	}
	if n < 1 || len(t.F32) != n*last {
		return nil, fmt.Errorf("shape %v holds %d values", t.Shape, len(t.F32))
	}
	return t.F32, nil
}

func square(t *onnxrt.Tensor) ([]float32, error) {
	if t == nil || t.DType != onnxrt.DFloat32 || len(t.Shape) < 2 {
		if t == nil {
			return nil, fmt.Errorf("missing")
		}
		return nil, fmt.Errorf("shape %v", t.Shape)
	}
	n := t.Shape[len(t.Shape)-1]
	if t.Shape[len(t.Shape)-2] != n || n < 1 {
		return nil, fmt.Errorf("shape %v", t.Shape)
	}
	need := n * n
	batch := 1
	for _, d := range t.Shape[:len(t.Shape)-2] {
		batch *= d
	}
	if batch != 1 || len(t.F32) != need {
		return nil, fmt.Errorf("shape %v", t.Shape)
	}
	return t.F32, nil
}

func sigmoid(x float32) float64 {
	v := float64(x)
	if v >= 0 {
		z := math.Exp(-v)
		return 1 / (1 + z)
	}
	z := math.Exp(v)
	return z / (1 + z)
}

func finiteBox(b Box) bool {
	return !math.IsNaN(b.X0) && !math.IsNaN(b.Y0) && !math.IsNaN(b.X1) && !math.IsNaN(b.Y1) &&
		!math.IsInf(b.X0, 0) && !math.IsInf(b.Y0, 0) && !math.IsInf(b.X1, 0) && !math.IsInf(b.Y1, 0)
}

func clipBox(b Box, w, h int) Box {
	maxX := float64(w)
	maxY := float64(h)
	if b.X0 < 0 {
		b.X0 = 0
	}
	if b.Y0 < 0 {
		b.Y0 = 0
	}
	if b.X1 > maxX {
		b.X1 = maxX
	}
	if b.Y1 > maxY {
		b.Y1 = maxY
	}
	return b
}
