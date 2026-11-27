package store

import "testing"
import "time"

func TestParseStoredClockReadsRegistrationLayouts(t *testing.T) {
	offset := time.Date(2026, 11, 27, 2, 5, 31, 0, time.FixedZone("CST", 8*3600))
	if got := ParseStoredClock("2026-11-27T02:05:31+08:00"); !got.Equal(offset) {
		t.Fatalf("offset clock = %s", got)
	}
	sqlite := ParseStoredClock("2026-11-02 12:00:00")
	if !sqlite.Equal(time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("sqlite clock = %s", sqlite)
	}
	nano := ParseStoredClock("2026-11-02T12:00:00.5Z")
	if nano.IsZero() || nano.UTC().Second() != 0 {
		t.Fatalf("fractional clock = %s", nano)
	}
	if !ParseStoredClock("").IsZero() || !ParseStoredClock("soon").IsZero() {
		t.Fatal("unreadable clocks must stay zero")
	}
	if !ParseStoredClock("0001-01-01T00:00:00Z").IsZero() {
		t.Fatal("year 1 is the zero clock")
	}
}
