package moa

import "testing"

func TestEstimateWaveMinUSD(t *testing.T) {
	if got := EstimateWaveMinUSD(0); got <= 0 {
		t.Fatalf("agg-only should be >0, got %v", got)
	}
	one := EstimateWaveMinUSD(1)
	two := EstimateWaveMinUSD(2)
	if two <= one {
		t.Fatalf("2 refs should cost more than 1: one=%v two=%v", one, two)
	}
	if one < 0.001 || one > 0.5 {
		t.Fatalf("1-ref estimate out of expected band: %v", one)
	}
}
