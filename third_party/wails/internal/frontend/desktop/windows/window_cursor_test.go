//go:build windows

package windows

import "testing"

// TestRestoreCursorDisplayCount checks the probe-and-undo sequence. Win32 has
// no cursor-count read, so a hidden count must end at zero with one show per
// decrement, and an already visible count must be left exactly as it was.
func TestRestoreCursorDisplayCount(t *testing.T) {
	cases := []struct {
		name         string
		start        int32
		wantCount    int32
		wantOriginal int32
		wantCalls    []bool
	}{
		{name: "hidden once", start: -1, wantCount: 0, wantOriginal: -1, wantCalls: []bool{true}},
		{name: "hidden three times", start: -3, wantCount: 0, wantOriginal: -3, wantCalls: []bool{true, true, true}},
		{name: "visible at zero", start: 0, wantCount: 0, wantOriginal: 0, wantCalls: []bool{true, false}},
		{name: "visible above zero", start: 2, wantCount: 2, wantOriginal: 2, wantCalls: []bool{true, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := tc.start
			var calls []bool
			original := restoreCursorDisplayCountWith(func(show bool) int32 {
				calls = append(calls, show)
				if show {
					count++
				} else {
					count--
				}
				return count
			})
			if original != tc.wantOriginal {
				t.Fatalf("original = %d, want %d", original, tc.wantOriginal)
			}
			if count != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			if len(calls) != len(tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
			}
			for i := range calls {
				if calls[i] != tc.wantCalls[i] {
					t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
				}
			}
		})
	}
}

// TestRestoreCursorDisplayCountStopsAtTheBound keeps an absurdly negative
// count from spinning forever. The helper gives up and leaves the count as it
// found it plus the bounded number of increments.
func TestRestoreCursorDisplayCountStopsAtTheBound(t *testing.T) {
	count := int32(-100)
	calls := 0
	original := restoreCursorDisplayCountWith(func(show bool) int32 {
		calls++
		if show {
			count++
		} else {
			count--
		}
		return count
	})
	if original != -100 {
		t.Fatalf("original = %d, want -100", original)
	}
	if calls > 33 {
		t.Fatalf("calls = %d, want at most 33", calls)
	}
	if count >= 0 {
		t.Fatalf("count = %d, want it still hidden after the bound", count)
	}
}
