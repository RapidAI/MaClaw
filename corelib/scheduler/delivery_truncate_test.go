package scheduler

import (
	"strings"
	"testing"
)

func TestTruncateDeliveryBody(t *testing.T) {
	if TruncateDeliveryBody("  hi  ") != "hi" {
		t.Fatal("trim")
	}
	long := strings.Repeat("字", MaxDeliveryBodyRunes+50)
	got := TruncateDeliveryBody(long)
	if len([]rune(got)) > MaxDeliveryBodyRunes {
		t.Fatalf("len=%d", len([]rune(got)))
	}
}
