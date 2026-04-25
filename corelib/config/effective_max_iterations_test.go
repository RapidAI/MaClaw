package config

import "testing"

func TestEffectiveMaxIterations(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		want       int
	}{
		{"zero returns default", 0, MaxAgentIterationsCap},
		{"negative returns default", -1, MaxAgentIterationsCap},
		{"large negative returns default", -100, MaxAgentIterationsCap},
		{"below minimum returns minimum", 10, MinAgentIterations},
		{"just below minimum returns minimum", MinAgentIterations - 1, MinAgentIterations},
		{"at minimum returns minimum", MinAgentIterations, MinAgentIterations},
		{"just above minimum returns configured", MinAgentIterations + 1, MinAgentIterations + 1},
		{"middle value returns configured", 100, 100},
		{"just below maximum returns configured", MaxAgentIterationsCap - 1, MaxAgentIterationsCap - 1},
		{"at maximum returns maximum", MaxAgentIterationsCap, MaxAgentIterationsCap},
		{"just above maximum returns maximum", MaxAgentIterationsCap + 1, MaxAgentIterationsCap},
		{"far above maximum returns maximum", 1000, MaxAgentIterationsCap},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveMaxIterations(tt.configured)
			if got != tt.want {
				t.Errorf("EffectiveMaxIterations(%d) = %d, want %d", tt.configured, got, tt.want)
			}
		})
	}
}

func TestEffectiveMaxIterations_Constants(t *testing.T) {
	// Verify the constants are sensible
	if MinAgentIterations <= 0 {
		t.Errorf("MinAgentIterations should be positive, got %d", MinAgentIterations)
	}
	if MaxAgentIterationsCap <= MinAgentIterations {
		t.Errorf("MaxAgentIterationsCap (%d) should be greater than MinAgentIterations (%d)",
			MaxAgentIterationsCap, MinAgentIterations)
	}
}
