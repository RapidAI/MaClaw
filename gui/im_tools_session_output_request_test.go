package main

import "testing"

func TestSessionOutputLineLimit(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{name: "default", args: nil, want: defaultSessionOutputLineLimit},
		{name: "positive", args: map[string]interface{}{"lines": float64(12)}, want: 12},
		{name: "fraction truncates", args: map[string]interface{}{"lines": 12.9}, want: 12},
		{name: "zero uses default", args: map[string]interface{}{"lines": float64(0)}, want: defaultSessionOutputLineLimit},
		{name: "negative uses default", args: map[string]interface{}{"lines": float64(-1)}, want: defaultSessionOutputLineLimit},
		{name: "caps maximum", args: map[string]interface{}{"lines": float64(500)}, want: maxSessionOutputLineLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionOutputLineLimit(tt.args); got != tt.want {
				t.Fatalf("sessionOutputLineLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWaitForSessionStartupOutputSkipsWhenOutputAlreadyExists(t *testing.T) {
	session := &RemoteSession{
		Status:         SessionStarting,
		RawOutputLines: []string{"ready"},
	}
	waitForSessionStartupOutputWithInterval(session, 3, 0)

	session.mu.RLock()
	defer session.mu.RUnlock()
	if len(session.RawOutputLines) == 0 {
		t.Fatal("expected startup wait to observe output")
	}
}

func TestWaitForSessionStartupOutputSkipsNonStartingSession(t *testing.T) {
	session := &RemoteSession{Status: SessionRunning}
	waitForSessionStartupOutputWithInterval(session, 1, 0)
}
