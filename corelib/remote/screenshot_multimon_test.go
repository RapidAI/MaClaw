package remote

import "testing"

func TestOrderDisplaysPrimaryFirst(t *testing.T) {
	got := orderDisplays([]DisplayInfo{
		{Name: "left", X: -200, Y: 0, Width: 100, Height: 100},
		{Name: "primary", X: 0, Y: 0, Width: 100, Height: 100, Primary: true},
		{Name: "lower", X: 0, Y: 100, Width: 100, Height: 100},
	})
	if len(got) != 3 || got[0].Name != "primary" || got[0].Index != 0 || !got[0].Primary {
		t.Fatalf("primary first: %+v", got)
	}
	if got[1].Name != "left" || got[1].Index != 1 || got[2].Name != "lower" || got[2].Index != 2 {
		t.Fatalf("position order: %+v", got)
	}
}
