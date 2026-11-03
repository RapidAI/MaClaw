package gopdf

import "math"

// RectFrom creates a Rect positioned at (x, y) with width w and height h.
// This is a convenience for working with positioned rectangles.
type RectFrom struct {
	X, Y, W, H float64
}

// Contains returns true if point (px, py) is inside the rectangle.
func (r RectFrom) Contains(px, py float64) bool {
	return px >= r.X && px <= r.X+r.W && py >= r.Y && py <= r.Y+r.H
}

// ContainsRect returns true if other is entirely inside r.
func (r RectFrom) ContainsRect(other RectFrom) bool {
	return other.X >= r.X && other.Y >= r.Y &&
		other.X+other.W <= r.X+r.W && other.Y+other.H <= r.Y+r.H
}

// Intersects returns true if r and other overlap.
func (r RectFrom) Intersects(other RectFrom) bool {
	return r.X < other.X+other.W && r.X+r.W > other.X &&
		r.Y < other.Y+other.H && r.Y+r.H > other.Y
}

// Intersection returns the overlapping area of two rectangles.
// Returns a zero RectFrom if they don't overlap.
func (r RectFrom) Intersection(other RectFrom) RectFrom {
	if !r.Intersects(other) {
		return RectFrom{}
	}
	x := math.Max(r.X, other.X)
	y := math.Max(r.Y, other.Y)
	x2 := math.Min(r.X+r.W, other.X+other.W)
	y2 := math.Min(r.Y+r.H, other.Y+other.H)
	return RectFrom{X: x, Y: y, W: x2 - x, H: y2 - y}
}

// Union returns the smallest rectangle that contains both r and other.
func (r RectFrom) Union(other RectFrom) RectFrom {
	x := math.Min(r.X, other.X)
	y := math.Min(r.Y, other.Y)
	x2 := math.Max(r.X+r.W, other.X+other.W)
	y2 := math.Max(r.Y+r.H, other.Y+other.H)
	return RectFrom{X: x, Y: y, W: x2 - x, H: y2 - y}
}

// IsEmpty returns true if the rectangle has zero or negative area.
func (r RectFrom) IsEmpty() bool {
	return r.W <= 0 || r.H <= 0
}

// Area returns the area of the rectangle.
func (r RectFrom) Area() float64 {
	if r.W <= 0 || r.H <= 0 {
		return 0
	}
	return r.W * r.H
}

// Center returns the center point of the rectangle.
func (r RectFrom) Center() Point {
	return Point{X: r.X + r.W/2, Y: r.Y + r.H/2}
}

// Normalize ensures W and H are positive, adjusting X and Y if needed.
func (r RectFrom) Normalize() RectFrom {
	if r.W < 0 {
		r.X += r.W
		r.W = -r.W
	}
	if r.H < 0 {
		r.Y += r.H
		r.H = -r.H
	}
	return r
}

// Matrix represents a 2D affine transformation matrix [a b c d e f].
// The transformation is:
//
//	x' = a*x + c*y + e
//	y' = b*x + d*y + f
type Matrix struct {
	A, B, C, D, E, F float64
}

// IdentityMatrix returns the identity transformation matrix.
func IdentityMatrix() Matrix {
	return Matrix{A: 1, B: 0, C: 0, D: 1, E: 0, F: 0}
}

// TranslateMatrix returns a translation matrix.
func TranslateMatrix(tx, ty float64) Matrix {
	return Matrix{A: 1, B: 0, C: 0, D: 1, E: tx, F: ty}
}

// ScaleMatrix returns a scaling matrix.
func ScaleMatrix(sx, sy float64) Matrix {
	return Matrix{A: sx, B: 0, C: 0, D: sy, E: 0, F: 0}
}

// RotateMatrix returns a rotation matrix for the given angle in degrees.
func RotateMatrix(degrees float64) Matrix {
	rad := degrees * math.Pi / 180
	cos := math.Cos(rad)
	sin := math.Sin(rad)
	return Matrix{A: cos, B: sin, C: -sin, D: cos, E: 0, F: 0}
}

// Multiply returns the product of m and other (m * other).
func (m Matrix) Multiply(other Matrix) Matrix {
	return Matrix{
		A: m.A*other.A + m.C*other.B,
		B: m.B*other.A + m.D*other.B,
		C: m.A*other.C + m.C*other.D,
		D: m.B*other.C + m.D*other.D,
		E: m.A*other.E + m.C*other.F + m.E,
		F: m.B*other.E + m.D*other.F + m.F,
	}
}

// TransformPoint applies the matrix transformation to a point.
func (m Matrix) TransformPoint(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

// IsIdentity returns true if this is the identity matrix.
func (m Matrix) IsIdentity() bool {
	const eps = 1e-10
	return math.Abs(m.A-1) < eps && math.Abs(m.B) < eps &&
		math.Abs(m.C) < eps && math.Abs(m.D-1) < eps &&
		math.Abs(m.E) < eps && math.Abs(m.F) < eps
}

// Distance returns the Euclidean distance between two points.
func Distance(p1, p2 Point) float64 {
	dx := p2.X - p1.X
	dy := p2.Y - p1.Y
	return math.Sqrt(dx*dx + dy*dy)
}
