package llmpool

import (
	"testing"
	"time"
)

func TestNormalizeServeWindowsEmptyStaysAlways(t *testing.T) {
	windows, ok := NormalizeServeWindows(nil)
	if !ok || windows != nil {
		t.Fatalf("nil policy = %#v ok=%v, want nil true", windows, ok)
	}
	// Every day 00:00-24:00 reads as always open and collapses the policy.
	windows, ok = NormalizeServeWindows([]TokenBankShareWindow{{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "00:00", End: "24:00"}})
	if !ok || windows != nil {
		t.Fatalf("always-open window = %#v ok=%v, want nil true", windows, ok)
	}
}

func TestNormalizeServeWindowsCanonicalizesAndRejectsBrokenClocks(t *testing.T) {
	windows, ok := NormalizeServeWindows([]TokenBankShareWindow{
		{Days: []int{1, 1, 7, 3}, Start: "09:5", End: "18:00"},
	})
	if !ok {
		t.Fatalf("valid windows rejected: %#v", windows)
	}
	if len(windows) != 1 || windows[0].Start != "09:05" || windows[0].End != "18:00" || len(windows[0].Days) != 2 {
		t.Fatalf("normalized = %#v", windows)
	}
	if _, ok := NormalizeServeWindows([]TokenBankShareWindow{{Start: "25:00", End: "26:00"}}); ok {
		t.Fatal("a window that cannot be read must fail the whole policy")
	}
	if _, ok := NormalizeServeWindows([]TokenBankShareWindow{{Start: "08:00", End: "08:00"}}); ok {
		t.Fatal("start equal to end must fail the whole policy")
	}
	if _, ok := NormalizeServeWindows([]TokenBankShareWindow{{Days: []int{9}, Start: "08:00", End: "09:00"}}); ok {
		t.Fatal("every listed day outside 0..6 must fail the whole policy")
	}
}

func TestServeWindowsAllowsUnionsTheWindows(t *testing.T) {
	loc := loadCreditMultiplierLocation(DefaultCreditMultiplierTimezone)
	wednesdayNoon := time.Date(2026, 9, 30, 12, 0, 0, 0, loc)
	wednesdayNight := time.Date(2026, 9, 30, 23, 0, 0, 0, loc)
	fridayNight := time.Date(2026, 10, 2, 23, 0, 0, 0, loc)
	saturdayNoon := time.Date(2026, 10, 3, 12, 0, 0, 0, loc)
	if wednesdayNoon.Weekday() != time.Wednesday || fridayNight.Weekday() != time.Friday {
		t.Fatalf("fixture weekdays wednesday=%s friday=%s", wednesdayNoon.Weekday(), fridayNight.Weekday())
	}
	windows := []TokenBankShareWindow{
		{Days: []int{3}, Start: "09:00", End: "18:00"},
		{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "08:00"},
	}
	if !ServeWindowsAllows(windows, wednesdayNoon) || !ServeWindowsAllows(windows, wednesdayNight) || !ServeWindowsAllows(windows, fridayNight) {
		t.Fatal("a time inside any window opens the provider")
	}
	if ServeWindowsAllows(windows, saturdayNoon) {
		t.Fatal("Saturday noon is outside every window")
	}
	if !ServeWindowsAllows(nil, saturdayNoon) {
		t.Fatal("an empty policy is always allowed")
	}
	// A policy with one always-open window is always open.
	always := []TokenBankShareWindow{{Days: []int{1}, Start: "09:00", End: "18:00"}, {Days: []int{6}, Start: "00:00", End: "24:00"}}
	if !ServeWindowsAllows(always, saturdayNoon) {
		t.Fatal("the all-day Saturday window opens the policy")
	}
}
