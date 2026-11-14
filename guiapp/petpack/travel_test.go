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

func TestDesktopRoamStopsWithoutTravel(t *testing.T) {
	s := DesktopRoam{RemX: 4, RemY: -3, Ready: true, Heading: 0}
	got, x, y := StepDesktopRoam(s, 0, 0, 1, 100*time.Millisecond, 8, 9, 0, 100, 0, 100, time.Unix(0, 0))
	if x != 8 || y != 9 || got.RemX != 0 || got.RemY != 0 {
		t.Fatalf("stopped roam = %+v pos=(%d,%d)", got, x, y)
	}
}
