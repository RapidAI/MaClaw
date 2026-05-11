package main

import "testing"

func TestParseScreenshotDisplayIndex(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want int
	}{
		{name: "float", raw: float64(2), want: 2},
		{name: "int", raw: 3, want: 3},
		{name: "string", raw: " 4 ", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseScreenshotDisplayIndex(tt.raw)
			if err != nil {
				t.Fatalf("parseScreenshotDisplayIndex() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseScreenshotDisplayIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseScreenshotDisplayIndexRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
	}{
		{name: "trailing text", raw: "1abc"},
		{name: "empty string", raw: ""},
		{name: "unsupported type", raw: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.raw
			if _, err := parseScreenshotDisplayIndex(raw); err == nil {
				t.Fatalf("parseScreenshotDisplayIndex(%#v) error = nil, want error", raw)
			}
		})
	}
}
