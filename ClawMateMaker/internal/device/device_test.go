package device

import "testing"

func TestLikelyESPCandidates(t *testing.T) {
	if !isLikelyESP("303a", "1001") {
		t.Fatal("Espressif USB should be candidate")
	}
	if !isLikelyESP("10c4", "ea60") {
		t.Fatal("CP210x should be candidate")
	}
	if isLikelyESP("ffff", "0001", "ordinary device") {
		t.Fatal("unknown device must not be inferred")
	}
}
