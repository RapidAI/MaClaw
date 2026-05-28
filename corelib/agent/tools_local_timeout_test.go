package agent

import "testing"

func TestResolveBashTimeoutClampsToAgentRange(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{name: "default", args: map[string]interface{}{}, want: 240},
		{name: "below min", args: map[string]interface{}{"timeout": float64(120)}, want: 240},
		{name: "valid", args: map[string]interface{}{"timeout": float64(360)}, want: 360},
		{name: "above max", args: map[string]interface{}{"timeout": float64(900)}, want: 600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveBashTimeout(tt.args, "echo ok"); got != tt.want {
				t.Fatalf("ResolveBashTimeout() = %d, want %d", got, tt.want)
			}
		})
	}
}
