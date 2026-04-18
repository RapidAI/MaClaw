package corelib

import "testing"

func TestIsLearnedSource(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		// Learned sources — should return true
		{"learned", true},
		{"crafted", true},

		// Non-learned sources — should return false
		{"manual", false},
		{"hub", false},
		{"file", false},
		{"zip_import", false},
		{"github", false},
		{"clawhub", false},
		{"auto_hub", false},
		{"auto_github", false},
		{"auto_clawhub", false},

		// Unknown / empty — should return false
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			got := IsLearnedSource(tt.source)
			if got != tt.want {
				t.Errorf("IsLearnedSource(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}
