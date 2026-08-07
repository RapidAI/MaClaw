package ocr

import (
	"math"
	"sort"
	"sync"
)

// Pure-Go port of PaddleOCR's DBPostProcess (score_mode="fast",
// box_type="quad") as used by the official PP-OCRv6 det pipeline:
//
//	binarize at thresh -> cv2.findContours(RETR_LIST, CHAIN_APPROX_SIMPLE)
//	-> approxPolyDP(0.002*perimeter) -> box_score_fast (mean prob inside
//	polygon) -> filter by box_thresh -> pyclipper unclip (JT_ROUND,
//	distance = area*unclipRatio/perimeter) -> cv2.minAreaRect + ordering
//	-> scale back to the original image size and clip.
//
// Contour extraction is a Suzuki-Abe border-following implementation and the
// polygon offset is a parallel-edge shift with round joins at convex
// vertices; neither is bit-identical to OpenCV/pyclipper but they agree on
// the roughly-rectangular blobs DB produces for text lines.

const (
	dbThresh        = 0.2
	dbBoxThresh     = 0.45
	dbUnclipRatio   = 1.4
	dbMaxCandidates = 3000
	dbMinSize       = 3
)

// DetBox is one detected text region in original image coordinates.
// Points are ordered top-left, top-right, bottom-right, bottom-left.
type DetBox struct {
	Points [4][2]float32
	Score  float32
}

type point struct{ x, y float64 }

// dbPostProcess runs DB post-processing on an HxW probability map and returns
// boxes mapped to the (destW, destH) original image coordinate space.
func dbPostProcess(prob []float32, width, height, destW, destH int) []DetBox {
	bitmap := make([]uint8, width*height)
	for i, p := range prob {
		if p > dbThresh {
			bitmap[i] = 1
		}
	}
	contours := findContours(bitmap, width, height)
	if len(contours) > dbMaxCandidates {
		contours = contours[:dbMaxCandidates]
	}

	widthScale := float64(destW) / float64(width)
	heightScale := float64(destH) / float64(height)

	var boxes []DetBox
	for _, contour := range contours {
		epsilon := 0.002 * arcLength(contour)
		approx := approxPolyDP(contour, epsilon)
		if len(approx) < 4 {
			continue
		}
		score := boxScoreFast(prob, width, height, approx)
		if score < dbBoxThresh {
			continue
		}
		area := polygonArea(approx)
		distance := area * dbUnclipRatio / arcLength(approx)
		expanded, ok := offsetPolygon(approx, distance)
		if !ok {
			continue // offset split into multiple polygons or degenerated
		}
		quad, sside := getMiniBoxes(expanded)
		if sside < dbMinSize+2 {
			continue
		}
		var box DetBox
		box.Score = score
		for i := 0; i < 4; i++ {
			px := math.RoundToEven(quad[i].x * widthScale)
			py := math.RoundToEven(quad[i].y * heightScale)
			box.Points[i][0] = float32(min(max(int(px), 0), destW))
			box.Points[i][1] = float32(min(max(int(py), 0), destH))
		}
		boxes = append(boxes, box)
	}
	return boxes
}

// sortBoxesReadingOrder orders boxes like PaddleOCR's sorted_boxes: primarily
// top-to-bottom by the top-left corner, then bubbles left-to-right within a
// line (y difference below a fixed tolerance).
func sortBoxesReadingOrder(boxes []DetBox) {
	sort.SliceStable(boxes, func(i, j int) bool {
		a, b := boxes[i].Points[0], boxes[j].Points[0]
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		return a[0] < b[0]
	})
	for i := 0; i+1 < len(boxes); i++ {
		for j := i; j >= 0; j-- {
			ydiff := boxes[j+1].Points[0][1] - boxes[j].Points[0][1]
			if ydiff < 0 {
				ydiff = -ydiff
			}
			if ydiff < 10 && boxes[j+1].Points[0][0] < boxes[j].Points[0][0] {
				boxes[j], boxes[j+1] = boxes[j+1], boxes[j]
			} else {
				break
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Suzuki-Abe contour extraction (RETR_LIST equivalent)
// ---------------------------------------------------------------------------

// 8-neighborhood offsets in clockwise order starting from the left pixel.
// OpenCV stores contour points as (x, y); internally we keep (x, y) too.
var nbd8 = [8][2]int{{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}}

// findContours returns all borders (outer and hole) of the binary image,
// compressed like CHAIN_APPROX_SIMPLE (collinear intermediate points dropped).
func findContours(bitmap []uint8, w, h int) [][]point {
	// labeled grid: 0 background, 1 unvisited foreground, >1 assigned border id,
	// negative values mark pixels visited with a 0 right-neighbor.
	f := make([]int32, w*h)
	for i, v := range bitmap {
		if v != 0 {
			f[i] = 1
		}
	}
	at := func(x, y int) int32 {
		if x < 0 || x >= w || y < 0 || y >= h {
			return 0
		}
		return f[y*w+x]
	}

	var contours [][]point
	var nbd int32 = 1

	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			cur := at(sx, sy)
			if cur == 0 {
				continue
			}
			isOuter := cur == 1 && at(sx-1, sy) == 0
			isHole := cur >= 1 && at(sx+1, sy) == 0
			if !isOuter && !isHole {
				continue
			}
			nbd++
			var nb [2]int // neighbor to start the search from
			if isOuter {
				nb = [2]int{sx - 1, sy}
			} else {
				nb = [2]int{sx + 1, sy}
			}

			// 3.1: find a nonzero neighbor of the start pixel; if none, the
			// pixel is isolated — mark it and move on.
			idx := neighborIndex(nb[0]-sx, nb[1]-sy)
			found := -1
			for k := 0; k < 8; k++ {
				ni := (idx + k) % 8 // clockwise search
				nx, ny := sx+nbd8[ni][0], sy+nbd8[ni][1]
				if at(nx, ny) != 0 {
					found = ni
					break
				}
			}
			if found < 0 {
				f[sy*w+sx] = -nbd
				continue
			}

			// 3.2-3.5: follow the border.
			first := [2]int{sx + nbd8[found][0], sy + nbd8[found][1]}
			prev := first
			curr := [2]int{sx, sy}
			var pts []point
			for {
				pts = append(pts, point{float64(curr[0]), float64(curr[1])})
				// Search counterclockwise around curr, starting after prev
				// (nbd8 is clockwise, so step backwards).
				si := neighborIndex(prev[0]-curr[0], prev[1]-curr[1])
				examinedRightZero := false
				next := -1
				for k := 1; k <= 8; k++ {
					ni := (si - k + 16) % 8 // counterclockwise
					nx, ny := curr[0]+nbd8[ni][0], curr[1]+nbd8[ni][1]
					if curr[0]+1 == nx && curr[1] == ny && at(nx, ny) == 0 {
						examinedRightZero = true
					}
					if at(nx, ny) != 0 {
						next = ni
						break
					}
				}
				// Marking rules (3.4).
				ci := curr[1]*w + curr[0]
				if examinedRightZero {
					f[ci] = -nbd
				} else if f[ci] == 1 {
					f[ci] = nbd
				}
				if next < 0 {
					break
				}
				nxt := [2]int{curr[0] + nbd8[next][0], curr[1] + nbd8[next][1]}
				if nxt == [2]int{sx, sy} && curr == first {
					break // back at the start
				}
				prev, curr = curr, nxt
			}
			contours = append(contours, chainApproxSimple(pts))
		}
	}
	return contours
}

// neighborIndex maps a unit 8-neighborhood delta to its nbd8 index.
func neighborIndex(dx, dy int) int {
	for i, d := range nbd8 {
		if d[0] == dx && d[1] == dy {
			return i
		}
	}
	return 0
}

// chainApproxSimple drops intermediate points on straight (horizontal,
// vertical, diagonal) runs, keeping only direction changes.
func chainApproxSimple(pts []point) []point {
	n := len(pts)
	if n <= 2 {
		return pts
	}
	dir := func(a, b point) (int, int) {
		dx, dy := sign(int(b.x-a.x)), sign(int(b.y-a.y))
		return dx, dy
	}
	keep := make([]bool, n)
	keep[0] = true
	pdx, pdy := dir(pts[0], pts[1])
	for i := 1; i+1 < n; i++ {
		dx, dy := dir(pts[i], pts[i+1])
		if dx != pdx || dy != pdy {
			keep[i] = true
			pdx, pdy = dx, dy
		}
	}
	// Closing edge must not make the last point redundant.
	ldx, ldy := dir(pts[n-1], pts[0])
	if ldx != pdx || ldy != pdy {
		keep[n-1] = true
	}
	out := make([]point, 0, n)
	for i, p := range pts {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// ---------------------------------------------------------------------------
// polygon metrics + Douglas-Peucker
// ---------------------------------------------------------------------------

// polygonArea returns the absolute polygon area (cv2.contourArea default).
func polygonArea(pts []point) float64 {
	n := len(pts)
	if n < 3 {
		return 0
	}
	var a float64
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += pts[i].x*pts[j].y - pts[j].x*pts[i].y
	}
	return math.Abs(a) / 2
}

func signedArea(pts []point) float64 {
	n := len(pts)
	var a float64
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += pts[i].x*pts[j].y - pts[j].x*pts[i].y
	}
	return a / 2
}

// arcLength returns the closed polygon perimeter (cv2.arcLength closed=true).
func arcLength(pts []point) float64 {
	n := len(pts)
	if n < 2 {
		return 0
	}
	var l float64
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		l += math.Hypot(pts[j].x-pts[i].x, pts[j].y-pts[i].y)
	}
	return l
}

// approxPolyDP is Douglas-Peucker simplification for a closed polygon,
// following OpenCV's split strategy (leftmost point + farthest point).
func approxPolyDP(pts []point, epsilon float64) []point {
	n := len(pts)
	if n < 4 {
		return append([]point(nil), pts...)
	}
	// Leftmost (min x, then min y) point.
	i0 := 0
	for i := 1; i < n; i++ {
		if pts[i].x < pts[i0].x || (pts[i].x == pts[i0].x && pts[i].y < pts[i0].y) {
			i0 = i
		}
	}
	// Farthest point from i0.
	i1, best := i0, -1.0
	for i := 0; i < n; i++ {
		d := math.Hypot(pts[i].x-pts[i0].x, pts[i].y-pts[i0].y)
		if d > best {
			best, i1 = d, i
		}
	}
	if i1 == i0 {
		return []point{pts[i0]}
	}
	arc1 := dpArc(pts, i0, i1, epsilon)
	arc2 := dpArc(pts, i1, i0+n, epsilon)
	// Concatenate without duplicating the shared endpoints.
	out := make([]point, 0, len(arc1)+len(arc2)-2)
	out = append(out, arc1[:len(arc1)-1]...)
	out = append(out, arc2[:len(arc2)-1]...)
	return out
}

// dpArc runs recursive Douglas-Peucker over the closed-index arc [a, b]
// (indices mod len(pts)) and returns the kept endpoints including a and b.
func dpArc(pts []point, a, b int, epsilon float64) []point {
	n := len(pts)
	pa, pb := pts[a%n], pts[b%n]
	if (b-a+n)%n <= 1 {
		return []point{pa, pb}
	}
	dx, dy := pb.x-pa.x, pb.y-pa.y
	norm := math.Hypot(dx, dy)
	maxDist, maxIdx := -1.0, -1
	for i := a + 1; i < b; i++ {
		p := pts[i%n]
		var d float64
		if norm == 0 {
			d = math.Hypot(p.x-pa.x, p.y-pa.y)
		} else {
			d = math.Abs(dy*p.x-dx*p.y+pb.x*pa.y-pb.y*pa.x) / norm
		}
		if d > maxDist {
			maxDist, maxIdx = d, i
		}
	}
	if maxDist <= epsilon {
		return []point{pa, pb}
	}
	left := dpArc(pts, a, maxIdx, epsilon)
	right := dpArc(pts, maxIdx, b, epsilon)
	out := make([]point, 0, len(left)+len(right)-1)
	out = append(out, left[:len(left)-1]...)
	out = append(out, right...)
	return out
}

// ---------------------------------------------------------------------------
// box score (cv2.fillPoly + cv2.mean equivalent)
// ---------------------------------------------------------------------------

// boxScoreFast computes the mean probability inside the polygon, restricted
// to its bounding box (PaddleOCR box_score_fast).
func boxScoreFast(prob []float32, w, h int, poly []point) float32 {
	xmin, xmax := math.MaxInt, math.MinInt
	ymin, ymax := math.MaxInt, math.MinInt
	for _, p := range poly {
		xmin = min(xmin, int(math.Floor(p.x)))
		xmax = max(xmax, int(math.Ceil(p.x)))
		ymin = min(ymin, int(math.Floor(p.y)))
		ymax = max(ymax, int(math.Ceil(p.y)))
	}
	xmin = min(max(xmin, 0), w-1)
	xmax = min(max(xmax, 0), w-1)
	ymin = min(max(ymin, 0), h-1)
	ymax = min(max(ymax, 0), h-1)

	mw, mh := xmax-xmin+1, ymax-ymin+1
	mask := getMaskScratch(mw * mh)
	fillPoly(mask, mw, mh, poly, xmin, ymin)

	var sum float64
	var cnt int
	for y := 0; y < mh; y++ {
		for x := 0; x < mw; x++ {
			if mask[y*mw+x] != 0 {
				sum += float64(prob[(ymin+y)*w+(xmin+x)])
				cnt++
			}
		}
	}
	if cnt == 0 {
		putMaskScratch(mask)
		return 0
	}
	putMaskScratch(mask)
	return float32(sum / float64(cnt))
}

// maskScratchPool recycles boxScoreFast raster masks; with up to
// dbMaxCandidates contours per frame the per-candidate make() churn shows
// up in GC profiles on dense pages.
var maskScratchPool = sync.Pool{
	New: func() any { return make([]uint8, 0, 4096) },
}

func getMaskScratch(n int) []uint8 {
	b := maskScratchPool.Get().([]uint8)
	if cap(b) < n {
		b = make([]uint8, n)
	}
	b = b[:n]
	clear(b) // pooled buffers are dirty; fillPoly only sets 1s
	return b
}

func putMaskScratch(b []uint8) { maskScratchPool.Put(b[:0]) }

// fillPoly rasterizes a polygon (even-odd scanline) into mask; coordinates
// are shifted by (ox, oy). Matches cv2.fillPoly for simple polygons.
func fillPoly(mask []uint8, w, h int, poly []point, ox, oy int) {
	n := len(poly)
	if n < 3 {
		return
	}
	xs := make([]float64, 0, n) // scanline intersections; reused across rows
	for y := 0; y < h; y++ {
		yc := float64(y+oy) + 0.5 // pixel center
		xs = xs[:0]
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			y0, y1 := poly[i].y, poly[j].y
			if (y0 <= yc && y1 > yc) || (y1 <= yc && y0 > yc) {
				t := (yc - y0) / (y1 - y0)
				xs = append(xs, poly[i].x+t*(poly[j].x-poly[i].x))
			}
		}
		sort.Float64s(xs)
		for k := 0; k+1 < len(xs); k += 2 {
			x0 := int(math.Ceil(xs[k] - 0.5 - float64(ox)))
			x1 := int(math.Ceil(xs[k+1]-0.5-float64(ox))) - 1
			x0 = max(x0, 0)
			x1 = min(x1, w-1)
			for x := x0; x <= x1; x++ {
				mask[y*w+x] = 1
			}
		}
	}
}

// ---------------------------------------------------------------------------
// polygon offset (pyclipper JT_ROUND equivalent) + min-area rect
// ---------------------------------------------------------------------------

// offsetPolygon expands a polygon outward by dist with round joins at convex
// vertices. ok is false when the result degenerates or splits (PaddleOCR
// skips boxes whose offset produces more than one polygon).
func offsetPolygon(pts []point, dist float64) ([]point, bool) {
	n := len(pts)
	if n < 3 || dist <= 0 || math.IsNaN(dist) || math.IsInf(dist, 0) {
		// dist<=0: zero-area contour (no expansion). NaN/Inf: degenerate
		// contour with zero perimeter (area*ratio/perimeter divided by 0) —
		// skip instead of emitting non-finite box coordinates.
		return nil, false
	}
	p := append([]point(nil), pts...)
	if signedArea(p) < 0 {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			p[i], p[j] = p[j], p[i]
		}
	}
	// Edge unit directions and outward normals (interior is on the left).
	dirs := make([]point, n)
	norms := make([]point, n)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		dx, dy := p[j].x-p[i].x, p[j].y-p[i].y
		l := math.Hypot(dx, dy)
		if l == 0 {
			return nil, false
		}
		dirs[i] = point{dx / l, dy / l}
		norms[i] = point{dy / l, -dx / l}
	}
	var out []point
	for i := 0; i < n; i++ {
		pi := (i - 1 + n) % n // previous edge index
		v := p[i]
		// Offset endpoints of the two edges adjacent to vertex i.
		a := point{v.x + norms[pi].x*dist, v.y + norms[pi].y*dist}
		b := point{v.x + norms[i].x*dist, v.y + norms[i].y*dist}
		cross := dirs[pi].x*dirs[i].y - dirs[pi].y*dirs[i].x
		if cross > 1e-9 {
			// Convex vertex: round join — arc from a to b around v.
			a0 := math.Atan2(a.y-v.y, a.x-v.x)
			a1 := math.Atan2(b.y-v.y, b.x-v.x)
			for a1 < a0 {
				a1 += 2 * math.Pi
			}
			steps := int(math.Ceil((a1 - a0) / 0.35))
			if steps < 1 {
				steps = 1
			}
			for s := 0; s <= steps; s++ {
				ang := a0 + (a1-a0)*float64(s)/float64(steps)
				out = append(out, point{v.x + dist*math.Cos(ang), v.y + dist*math.Sin(ang)})
			}
		} else {
			// Concave or straight vertex: intersect the two offset edge lines.
			out = append(out, offsetEdgeIntersect(v, dirs[pi], norms[pi], dirs[i], norms[i], dist, b))
		}
	}
	if len(out) < 3 || polygonArea(out) < polygonArea(p) {
		return nil, false
	}
	return out, true
}

// offsetEdgeIntersect intersects the two offset edge lines meeting at vertex
// v: the previous edge (direction dp, normal np) and the current edge
// (direction dc, normal nc), both pushed outward by dist. Falls back to the
// shifted vertex b when the edges are parallel.
func offsetEdgeIntersect(v, dp, np, dc, nc point, dist float64, b point) point {
	a := point{v.x + np.x*dist, v.y + np.y*dist}
	denom := dp.x*dc.y - dp.y*dc.x
	if math.Abs(denom) < 1e-12 {
		return b
	}
	bx, by := v.x+nc.x*dist, v.y+nc.y*dist
	t := ((bx-a.x)*dc.y - (by-a.y)*dc.x) / denom
	return point{a.x + t*dp.x, a.y + t*dp.y}
}

// getMiniBoxes computes the min-area rect of the polygon and returns its four
// corners ordered top-left, top-right, bottom-right, bottom-left, plus the
// shorter side length (PaddleOCR get_mini_boxes).
func getMiniBoxes(pts []point) ([4]point, float64) {
	cx, cy, rw, rh, angle := minAreaRect(pts)
	// cv2.boxPoints
	ca, sa := math.Cos(angle), math.Sin(angle)
	hw, hh := rw/2, rh/2
	corners := [4]point{
		{cx - hw*ca + hh*sa, cy - hw*sa - hh*ca},
		{cx + hw*ca + hh*sa, cy + hw*sa - hh*ca},
		{cx + hw*ca - hh*sa, cy + hw*sa + hh*ca},
		{cx - hw*ca - hh*sa, cy - hw*sa + hh*ca},
	}
	// Order by x, then split left/right pair by y (PaddleOCR ordering).
	order := []int{0, 1, 2, 3}
	sort.SliceStable(order, func(i, j int) bool { return corners[order[i]].x < corners[order[j]].x })
	i1, i4 := order[0], order[1]
	if corners[order[1]].y > corners[order[0]].y {
		i1, i4 = order[0], order[1]
	} else {
		i1, i4 = order[1], order[0]
	}
	i2, i3 := order[2], order[3]
	if corners[order[3]].y > corners[order[2]].y {
		i2, i3 = order[2], order[3]
	} else {
		i2, i3 = order[3], order[2]
	}
	return [4]point{corners[i1], corners[i2], corners[i3], corners[i4]}, min(rw, rh)
}

// minAreaRect finds the minimum-area rotated rectangle enclosing pts using
// the convex hull and rotating calipers. Returns center, size and the angle
// of the first size component (matching cv2.minAreaRect semantics loosely —
// exact tie-breaking does not matter because callers reorder the corners).
func minAreaRect(pts []point) (cx, cy, w, h, angle float64) {
	hull := convexHull(pts)
	if len(hull) == 0 {
		return 0, 0, 0, 0, 0
	}
	if len(hull) == 1 {
		return hull[0].x, hull[0].y, 0, 0, 0
	}
	bestArea := math.MaxFloat64
	n := len(hull)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		ex, ey := hull[j].x-hull[i].x, hull[j].y-hull[i].y
		l := math.Hypot(ex, ey)
		if l == 0 {
			continue
		}
		ux, uy := ex/l, ey/l // edge direction
		vx, vy := -uy, ux    // normal
		minU, maxU := math.MaxFloat64, -math.MaxFloat64
		minV, maxV := math.MaxFloat64, -math.MaxFloat64
		for _, p := range hull {
			du := p.x*ux + p.y*uy
			dv := p.x*vx + p.y*vy
			minU, maxU = min(minU, du), max(maxU, du)
			minV, maxV = min(minV, dv), max(maxV, dv)
		}
		wi, hi := maxU-minU, maxV-minV
		if area := wi * hi; area < bestArea {
			bestArea = area
			cu, cv := (minU+maxU)/2, (minV+maxV)/2
			cx = cu*ux + cv*vx
			cy = cu*uy + cv*vy
			w, h = wi, hi
			angle = math.Atan2(uy, ux)
		}
	}
	return cx, cy, w, h, angle
}

// convexHull returns the convex hull (Andrew monotone chain, CCW, no
// duplicate closing point).
func convexHull(pts []point) []point {
	p := append([]point(nil), pts...)
	sort.Slice(p, func(i, j int) bool {
		if p[i].x != p[j].x {
			return p[i].x < p[j].x
		}
		return p[i].y < p[j].y
	})
	// dedupe
	uniq := p[:0]
	for _, q := range p {
		if len(uniq) == 0 || uniq[len(uniq)-1] != q {
			uniq = append(uniq, q)
		}
	}
	if len(uniq) <= 2 {
		return uniq
	}
	cross := func(o, a, b point) float64 {
		return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x)
	}
	lower := make([]point, 0, len(uniq))
	for _, q := range uniq {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], q) <= 0 {
			lower = lower[:len(lower)-1]
		}
		lower = append(lower, q)
	}
	upper := make([]point, 0, len(uniq))
	for i := len(uniq) - 1; i >= 0; i-- {
		q := uniq[i]
		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], q) <= 0 {
			upper = upper[:len(upper)-1]
		}
		upper = append(upper, q)
	}
	return append(lower[:len(lower)-1], upper[:len(upper)-1]...)
}
