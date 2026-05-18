package workflow

import (
	"testing"
	"time"
)

func TestNormalizeAuditTimestamp_ZeroTime(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Millisecond)
	result := NormalizeAuditTimestamp(time.Time{})
	after := time.Now().UTC().Truncate(time.Millisecond)

	if result.Before(before) || result.After(after) {
		t.Errorf("expected timestamp between %v and %v, got %v", before, after, result)
	}
	if result.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", result.Location())
	}
	// Verify millisecond precision (no sub-millisecond component)
	if result.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("expected millisecond precision, got nanosecond remainder %d", result.Nanosecond()%int(time.Millisecond))
	}
}

func TestNormalizeAuditTimestamp_NonZeroTime(t *testing.T) {
	// Create a time with microsecond precision in a non-UTC timezone
	loc := time.FixedZone("Test", 8*3600)
	input := time.Date(2025, 7, 15, 10, 30, 45, 123456789, loc)

	result := NormalizeAuditTimestamp(input)

	// Should be converted to UTC
	if result.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", result.Location())
	}
	// Should be truncated to millisecond precision
	expectedNano := 123000000 // 123ms truncated
	if result.Nanosecond() != expectedNano {
		t.Errorf("expected nanosecond %d, got %d", expectedNano, result.Nanosecond())
	}
	// Verify the time was converted to UTC correctly
	expectedUTC := time.Date(2025, 7, 15, 2, 30, 45, 123000000, time.UTC)
	if !result.Equal(expectedUTC) {
		t.Errorf("expected %v, got %v", expectedUTC, result)
	}
}

func TestNormalizePageSize(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"zero returns default", 0, DefaultAuditPageSize},
		{"negative returns default", -1, DefaultAuditPageSize},
		{"exceeds max returns default", 200, DefaultAuditPageSize},
		{"exactly max returns max", DefaultAuditPageSize, DefaultAuditPageSize},
		{"valid small value", 10, 10},
		{"valid value 50", 50, 50},
		{"valid value 1", 1, 1},
		{"valid value 99", 99, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePageSize(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePageSize(%d) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDefaultAuditPageSize(t *testing.T) {
	if DefaultAuditPageSize != 100 {
		t.Errorf("DefaultAuditPageSize = %d, want 100", DefaultAuditPageSize)
	}
}
