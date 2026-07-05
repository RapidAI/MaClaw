package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
)

func TestPythonRuntimeReadyRequiresPythonUVAndVenv(t *testing.T) {
	cases := []struct {
		name    string
		status  pyenv.Status
		missing string
		ready   bool
	}{
		{
			name:    "missing python",
			status:  pyenv.Status{},
			missing: "Python",
		},
		{
			name: "missing uv",
			status: pyenv.Status{
				Available: true,
				IsPrivate: true,
			},
			missing: "uv",
		},
		{
			name: "system python only",
			status: pyenv.Status{
				Available: true,
			},
			missing: "Private Python",
		},
		{
			name: "missing venv",
			status: pyenv.Status{
				Available:   true,
				IsPrivate:   true,
				UVAvailable: true,
				UVIsPrivate: true,
			},
			missing: "Python venv",
		},
		{
			name: "path uv only",
			status: pyenv.Status{
				Available:   true,
				IsPrivate:   true,
				UVAvailable: true,
				VenvReady:   true,
			},
			missing: "Private uv",
		},
		{
			name: "ready",
			status: pyenv.Status{
				Available:   true,
				IsPrivate:   true,
				UVAvailable: true,
				UVIsPrivate: true,
				VenvReady:   true,
			},
			ready: true,
		},
		{
			name: "ready with prior error text",
			status: pyenv.Status{
				Available:   true,
				IsPrivate:   true,
				UVAvailable: true,
				UVIsPrivate: true,
				VenvReady:   true,
				Error:       "previous transient error",
			},
			ready: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingPythonRuntimeComponent(tc.status); got != tc.missing {
				t.Fatalf("missingPythonRuntimeComponent() = %q, want %q", got, tc.missing)
			}
			if got := pythonRuntimeReady(tc.status); got != tc.ready {
				t.Fatalf("pythonRuntimeReady() = %v, want %v", got, tc.ready)
			}
		})
	}
}
