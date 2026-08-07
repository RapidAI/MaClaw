package ocr

import (
	"math"
	"testing"
)

// mask builds a w x h binary grid from a list of filled rectangles / points.
func mask(w, h int, rects ...[4]int) []uint8 {
	m := make([]uint8, w*h)
	for _, r := range rects {
		for y := r[1]; y <= r[3]; y++ {
			for x := r[0]; x <= r[2]; x++ {
				m[y*w+x] = 1
			}
		}
	}
	return m
}

func contourBBox(pts []point) (minX, minY, maxX, maxY float64) {
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64
	for _, p := range pts {
		minX, minY = math.Min(minX, p.x), math.Min(minY, p.y)
		maxX, maxY = math.Max(maxX, p.x), math.Max(maxY, p.y)
	}
	return
}

func TestFindContours_SingleRect(t *testing.T) {
	m := mask(20, 20, [4]int{3, 4, 12, 11})
	contours := findContours(m, 20, 20)
	if len(contours) != 1 {
		t.Fatalf("contours=%d want 1", len(contours))
	}
	c := contours[0]
	if len(c) != 4 {
		t.Fatalf("rect contour should compress to 4 corners, got %d: %v", len(c), c)
	}
	minX, minY, maxX, maxY := contourBBox(c)
	if minX != 3 || minY != 4 || maxX != 12 || maxY != 11 {
		t.Fatalf("bbox=(%v,%v)-(%v,%v)", minX, minY, maxX, maxY)
	}
}

func TestFindContours_TwoDisjointRects(t *testing.T) {
	m := mask(30, 20, [4]int{2, 2, 8, 8}, [4]int{15, 5, 25, 15})
	contours := findContours(m, 30, 20)
	if len(contours) != 2 {
		t.Fatalf("contours=%d want 2", len(contours))
	}
}

func TestFindContours_Donut(t *testing.T) {
	// Filled 14x14 ring with a 6x6 hole.
	m := mask(20, 20, [4]int{3, 3, 16, 16})
	for y := 7; y <= 12; y++ {
		for x := 7; x <= 12; x++ {
			m[y*20+x] = 0
		}
	}
	contours := findContours(m, 20, 20)
	if len(contours) != 2 {
		t.Fatalf("donut should produce outer+hole contours, got %d", len(contours))
	}
}

func TestFindContours_Concave(t *testing.T) {
	// L-shape: vertical bar + horizontal bar.
	m := mask(20, 20, [4]int{2, 2, 6, 15}, [4]int{7, 11, 15, 15})
	contours := findContours(m, 20, 20)
	if len(contours) != 1 {
		t.Fatalf("contours=%d want 1", len(contours))
	}
	if len(contours[0]) != 7 {
		// 8-connected border: the concave notch adds a diagonal step
		// (6,10)->(7,11), so the L has 7 compressed corners, not 6.
		t.Fatalf("L-shape should have 7 corners, got %d: %v", len(contours[0]), contours[0])
	}
}

func TestMinAreaRect_AxisAligned(t *testing.T) {
	pts := []point{{0, 0}, {10, 0}, {10, 4}, {0, 4}}
	_, _, w, h, _ := minAreaRect(pts)
	if math.Abs(w-10) > 1e-6 || math.Abs(h-4) > 1e-6 {
		t.Fatalf("size=%vx%v want 10x4", w, h)
	}
}

func TestMinAreaRect_Rotated45(t *testing.T) {
	// 10x4 rect rotated 45 degrees about the origin.
	pts := make([]point, 4)
	base := []point{{0, 0}, {10, 0}, {10, 4}, {0, 4}}
	s, c := math.Sin(math.Pi/4), math.Cos(math.Pi/4)
	for i, p := range base {
		pts[i] = point{p.x*c - p.y*s, p.x*s + p.y*c}
	}
	_, _, w, h, _ := minAreaRect(pts)
	got := []float64{w, h}
	if math.Abs(got[0]-10) > 1e-4 || math.Abs(got[1]-4) > 1e-4 {
		if math.Abs(got[0]-4) > 1e-4 || math.Abs(got[1]-10) > 1e-4 {
			t.Fatalf("size=%vx%v want 10x4 (either orientation)", w, h)
		}
	}
}

func TestGetMiniBoxes_Ordering(t *testing.T) {
	pts := []point{{0, 0}, {20, 0}, {20, 8}, {0, 8}}
	quad, sside := getMiniBoxes(pts)
	if sside != 8 {
		t.Fatalf("sside=%v want 8", sside)
	}
	tl, tr, br, bl := quad[0], quad[1], quad[2], quad[3]
	if tl.y > tr.y || tl.x > bl.x {
		t.Fatalf("bad ordering: %v", quad)
	}
	if !(tl.x <= tr.x && bl.x <= br.x && tl.y <= bl.y && tr.y <= br.y) {
		t.Fatalf("not TL,TR,BR,BL: %v", quad)
	}
}

func TestOffsetPolygon_NonFiniteDistance(t *testing.T) {
	sq := []point{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	for _, d := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if out, ok := offsetPolygon(sq, d); ok || out != nil {
			t.Fatalf("offsetPolygon(dist=%v) = (%v, %v), want (nil, false)", d, out, ok)
		}
	}
}

func TestOffsetPolygon_SquareGrows(t *testing.T) {
	sq := []point{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	out, ok := offsetPolygon(sq, 1.0)
	if !ok {
		t.Fatal("offset failed")
	}
	area := polygonArea(out)
	// Ideal round-join expansion: A + P*d + pi*d^2 = 100 + 40 + pi ≈ 143.1
	if area < 140 || area > 145 {
		t.Fatalf("expanded area=%v want ~143.1", area)
	}
	// Expanded square must contain the original corners' bbox expanded by 1.
	minX, minY, maxX, maxY := contourBBox(out)
	if minX > -0.99 || minY > -0.99 || maxX < 10.99 || maxY < 10.99 {
		t.Fatalf("bbox=(%v,%v)-(%v,%v)", minX, minY, maxX, maxY)
	}
}

func TestBoxScoreFast(t *testing.T) {
	// 10x10 prob map, all 0.8; polygon covers left half.
	prob := make([]float32, 100)
	for i := range prob {
		prob[i] = 0.8
	}
	poly := []point{{1, 1}, {4, 1}, {4, 8}, {1, 8}}
	score := boxScoreFast(prob, 10, 10, poly)
	if math.Abs(float64(score)-0.8) > 0.01 {
		t.Fatalf("score=%v want ~0.8", score)
	}
}

func TestDBPostProcess_FindsRect(t *testing.T) {
	// 64x64 prob map with one bright bar; box_thresh/unclip defaults apply.
	w, h := 64, 64
	prob := make([]float32, w*h)
	for y := 20; y <= 30; y++ {
		for x := 8; x <= 55; x++ {
			prob[y*w+x] = 0.9
		}
	}
	boxes := dbPostProcess(prob, w, h, w, h)
	if len(boxes) != 1 {
		t.Fatalf("boxes=%d want 1: %+v", len(boxes), boxes)
	}
	b := boxes[0]
	minX, minY, maxX, maxY := contourBBox([]point{
		{float64(b.Points[0][0]), float64(b.Points[0][1])},
		{float64(b.Points[1][0]), float64(b.Points[1][1])},
		{float64(b.Points[2][0]), float64(b.Points[2][1])},
		{float64(b.Points[3][0]), float64(b.Points[3][1])},
	})
	// Unclip expands the 48x11 blob by distance = area*1.4/perimeter ≈ 5.77.
	if minX < 1 || minX > 4 || maxX < 59 || maxX > 62 || minY < 13 || minY > 16 || maxY < 34 || maxY > 37 {
		t.Fatalf("unexpected box extent (%v,%v)-(%v,%v)", minX, minY, maxX, maxY)
	}
	if b.Score < 0.8 {
		t.Fatalf("score=%v", b.Score)
	}
}
