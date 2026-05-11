package main

import "testing"

func TestIsFatalSessionErrorScansRecentFatalMarkers(t *testing.T) {
	lines := []string{
		"working",
		"temporary timeout",
		"Authentication failed: invalid credentials",
	}
	if !isFatalSessionError(lines) {
		t.Fatal("fatal session error marker was not detected")
	}
}

func TestIsFatalSessionErrorIgnoresOldFatalMarkers(t *testing.T) {
	lines := []string{"Authentication failed: invalid credentials"}
	for i := 0; i < 20; i++ {
		lines = append(lines, "later non-fatal output")
	}
	if isFatalSessionError(lines) {
		t.Fatal("fatal marker outside recent scan window should be ignored")
	}
}

func TestClassifyFatalSessionOutputLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want fatalSessionErrorKind
	}{
		{name: "api key", line: "API key not configured", want: fatalSessionErrorAPIKey},
		{name: "http unauthorized", line: "request failed with status 401", want: fatalSessionErrorHTTPUnauthorized},
		{name: "tool missing", line: "codex: command not found", want: fatalSessionErrorToolMissing},
		{name: "permission", line: "permission denied opening file", want: fatalSessionErrorPermission},
		{name: "transient", line: "rate limit exceeded, retry later", want: fatalSessionErrorNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyFatalSessionOutputLine(tt.line); got != tt.want {
				t.Fatalf("classifyFatalSessionOutputLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
