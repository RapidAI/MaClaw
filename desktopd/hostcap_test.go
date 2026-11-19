package desktopd

import "testing"

func TestParseMemTotal(t *testing.T) {
	got := ParseMemTotal("MemFree: 1 kB\nMemTotal:       16777216 kB\nSwapTotal: 0 kB\n")
	if got != 16<<30 {
		t.Fatalf("mem = %d", got)
	}
	if ParseMemTotal("MemFree: 1 kB\n") != 0 {
		t.Fatal("missing MemTotal parsed as present")
	}
}
