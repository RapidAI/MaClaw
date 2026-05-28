package backgroundrole

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		command string
		want    string
	}{
		{name: "explicit command", role: "command", command: "sleep 60 && tail -20 build.log", want: "command"},
		{name: "explicit monitor", role: " monitor ", command: "docker build .", want: "monitor"},
		{name: "explicit poll", role: "POLL", command: "docker build .", want: "poll"},
		{name: "docker build stays command", command: "docker build --target runner-base .", want: "command"},
		{name: "kill based watcher", command: `bash -c 'PID=123; while kill -0 $PID 2>/dev/null; do sleep 120; echo "building $(date)"; done; tail -15 /tmp/build.log'`, want: "monitor"},
		{name: "sleep tail poll", command: "sleep 60 && tail -20 /tmp/build.log", want: "poll"},
		{name: "powershell process watcher", command: "while (Get-Process -Id 123 -ErrorAction SilentlyContinue) { Start-Sleep -Seconds 30 }; Get-Content -Tail 20 build.log", want: "monitor"},
		{name: "powershell tail poll", command: "Start-Sleep -Seconds 60; Get-Content -Tail 20 build.log", want: "poll"},
		{name: "long install with tail stays command", command: "npm install && sleep 60 && tail -20 npm.log", want: "command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.role, tt.command); got != tt.want {
				t.Fatalf("Normalize(%q, %q) = %q, want %q", tt.role, tt.command, got, tt.want)
			}
		})
	}
}
