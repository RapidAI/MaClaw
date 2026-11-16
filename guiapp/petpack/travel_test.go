package petpack

import (
	"math"
	"testing"
	"time"
)

func TestDesktopRoamCoversHorizontalVerticalAndDiagonal(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s, x, y := StepDesktopRoam(DesktopRoam{}, 1, 0, 1, 100*time.Millisecond, 10, 20, 0, 400, 0, 300, now)
	if x <= 10 || y != 20 {
		t.Fatalf("horizontal step = (%d,%d), want x to increase and y to stay 20", x, y)
	}
	if s.FaceLeft {
		t.Fatal("rightward heading should face right")
	}

	later := now.Add(RoamRetargetEvery)
	s, x2, y2 := StepDesktopRoam(s, 1, 0, 1, 100*time.Millisecond, x, y, 0, 400, 0, 300, later)
	if x2 == x || y2 == y {
		t.Fatalf("diagonal step stayed on one axis: (%d,%d) -> (%d,%d)", x, y, x2, y2)
	}

	s.Heading = math.Pi / 2
	s.Retarget = later
	s.FaceLeft = true
	s, x3, y3 := StepDesktopRoam(s, 1, 0, 1, 100*time.Millisecond, x2, y2, 0, 400, 0, 300, later.Add(20*time.Millisecond))
	if x3 != x2 || y3 <= y2 {
		t.Fatalf("vertical step = (%d,%d) from (%d,%d)", x3, y3, x2, y2)
	}
	if !s.FaceLeft {
		t.Fatal("vertical heading should keep the side she was facing")
	}

	s.Heading = 0
	s.Retarget = later
	s.FaceLeft = false
	s, nx, ny := StepDesktopRoam(s, 1, 0, 1, 100*time.Millisecond, 390, 40, 0, 400, 0, 300, later.Add(40*time.Millisecond))
	if nx != 400 || !s.FaceLeft {
		t.Fatalf("right edge x=%d faceLeft=%v heading=%v", nx, s.FaceLeft, s.Heading)
	}
	s, nx2, _ := StepDesktopRoam(s, 1, 0, 1, 100*time.Millisecond, nx, ny, 0, 400, 0, 300, later.Add(60*time.Millisecond))
	if nx2 >= nx {
		t.Fatalf("after the right edge she should walk left, x=%d from %d", nx2, nx)
	}

	s.Heading = -math.Pi / 2
	s.Retarget = later
	s, _, top := StepDesktopRoam(s, 1, 0, 1, 100*time.Millisecond, 40, 4, 0, 400, 0, 300, later.Add(80*time.Millisecond))
	if top != 0 {
		t.Fatalf("top edge y=%d, want 0", top)
	}
	if math.Sin(s.Heading) <= 0.2 {
		t.Fatalf("top bounce heading = %v, want downward", s.Heading)
	}
}

func TestWindowEdgeWalksBackAndForth(t *testing.T) {
	rect := WindowRect{Left: 100, Top: 50, Right: 500, Bottom: 350}
	const petSize = 100
	// Center (60, 250) is nearest the left side. The window origin sits half
	// a pet outside that point.
	got, x, y, took := StepWindowEdge(DesktopRoam{}, 1, 0, 1, 0, 10, 200, petSize, rect, 0, 2000, 0, 2000)
	if !took || !got.EdgeOn || got.EdgeDir != 1 {
		t.Fatalf("first visit = %+v took=%v", got, took)
	}
	if x != 50 || y != 200 {
		t.Fatalf("snap origin = (%d,%d), want (50,200)", x, y)
	}
	if math.Abs(got.Along-1200) > 0.5 {
		t.Fatalf("along = %v, want 1200", got.Along)
	}

	got, x, y, took = StepWindowEdge(got, 1, 0, 1, 100*time.Millisecond, x, y, petSize, rect, 0, 2000, 0, 2000)
	if !took || x != 50 || y != 100 {
		t.Fatalf("clockwise step = (%d,%d) along=%v", x, y, got.Along)
	}
	if got.FaceLeft {
		t.Fatal("vertical side should keep the previous facing")
	}

	got.Along = 1390
	got.EdgeDir = 1
	got, x, y, took = StepWindowEdge(got, 1, 0, 1, 50*time.Millisecond, x, y, petSize, rect, 0, 2000, 0, 2000)
	if got.EdgeDir != -1 || math.Abs(got.Along-1360) > 0.5 || x != 50 || y != 40 {
		t.Fatalf("turn at the end = %+v pos=(%d,%d)", got, x, y)
	}

	back := DesktopRoam{EdgeOn: true, Along: 10, EdgeDir: -1}
	back, _, _, _ = StepWindowEdge(back, 1, 0, 1, 100*time.Millisecond, 0, 0, petSize, rect, 0, 2000, 0, 2000)
	if back.EdgeDir != 1 || math.Abs(back.Along-90) > 0.5 {
		t.Fatalf("turn at the start = %+v", back)
	}

	bottom := DesktopRoam{EdgeOn: true, Along: 800, EdgeDir: 1}
	bottom, bx, by, _ := StepWindowEdge(bottom, 1, 0, 1, 0, 0, 0, petSize, rect, 0, 2000, 0, 2000)
	if !bottom.FaceLeft || bx != 350 || by != 300 {
		t.Fatalf("bottom edge face=%v pos=(%d,%d)", bottom.FaceLeft, bx, by)
	}

	shifted := WindowRect{Left: 130, Top: 50, Right: 530, Bottom: 350}
	stuck := DesktopRoam{EdgeOn: true, Along: 1200, EdgeDir: 1}
	_, sx, sy, _ := StepWindowEdge(stuck, 1, 0, 1, 0, 0, 0, petSize, shifted, 0, 2000, 0, 2000)
	if sx != 80 || sy != 200 {
		t.Fatalf("window move = (%d,%d), want (80,200)", sx, sy)
	}
}

func TestWindowEdgeStaysPutWithoutSpeed(t *testing.T) {
	rect := WindowRect{Left: 100, Top: 50, Right: 500, Bottom: 350}
	got, x, y, took := StepWindowEdge(DesktopRoam{RemX: 3}, 0, 0, 1, 100*time.Millisecond, 10, 20, 100, rect, 0, 800, 0, 600)
	if !took || got.EdgeOn || x != 10 || y != 20 || got.RemX != 0 {
		t.Fatalf("pinned edge = %+v pos=(%d,%d) took=%v", got, x, y, took)
	}
	held := DesktopRoam{EdgeOn: true, Along: 40, EdgeDir: 1}
	got, x, y, took = StepWindowEdge(held, 0, 0, 1, 100*time.Millisecond, 12, 14, 100, rect, 0, 800, 0, 600)
	if !took || !got.EdgeOn || got.Along != 40 || x != 12 || y != 14 {
		t.Fatalf("zero speed moved a patrol: %+v pos=(%d,%d)", got, x, y)
	}

	_, _, _, took = StepWindowEdge(DesktopRoam{}, 1, 0, 1, 100*time.Millisecond, 10, 20, 100, WindowRect{Right: 4, Bottom: 4}, 0, 800, 0, 600)
	if took {
		t.Fatal("tiny window was patrolled")
	}

	now := time.Unix(1_700_000_000, 0)
	left := DesktopRoam{EdgeOn: true, Along: 50, Ready: true, Heading: 0, Retarget: now}
	roam, rx, ry := StepPetTravel(left, 1, 0, 1, 100*time.Millisecond, 10, 20, 100, 0, 400, 0, 300, now, WindowRect{}, false)
	if roam.EdgeOn || rx <= 10 {
		t.Fatalf("hidden window = %+v x=%d y=%d", roam, rx, ry)
	}

	// Center (30, 60) is nearest the top edge. The origin would be (-20, 30)
	// and the work area keeps it on screen.
	clamped, cx, cy, took := StepWindowEdge(DesktopRoam{}, 1, 0, 1, 0, -20, 10, 100, WindowRect{Left: 10, Top: 80, Right: 200, Bottom: 200}, 0, 800, 0, 600)
	if !took || cx != 0 || cy != 30 {
		t.Fatalf("clamped snap = %+v pos=(%d,%d)", clamped, cx, cy)
	}
}

func TestDesktopRoamStopsWithoutTravel(t *testing.T) {
	s := DesktopRoam{RemX: 4, RemY: -3, Ready: true, Heading: 0}
	got, x, y := StepDesktopRoam(s, 0, 0, 1, 100*time.Millisecond, 8, 9, 0, 100, 0, 100, time.Unix(0, 0))
	if x != 8 || y != 9 || got.RemX != 0 || got.RemY != 0 {
		t.Fatalf("stopped roam = %+v pos=(%d,%d)", got, x, y)
	}
}
