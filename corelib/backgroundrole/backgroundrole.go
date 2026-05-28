package backgroundrole

import "strings"

func Normalize(role, command string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "command", "monitor", "poll":
		return strings.ToLower(strings.TrimSpace(role))
	}
	cmd := strings.ToLower(command)
	if looksLikeBackgroundMonitorCommand(cmd) {
		return "monitor"
	}
	if looksLikeBackgroundPollCommand(cmd) && !looksLongRunningBackgroundCommand(cmd) {
		return "poll"
	}
	return "command"
}

func looksLikeBackgroundMonitorCommand(cmd string) bool {
	return (strings.Contains(cmd, "kill -0") || strings.Contains(cmd, "get-process") || strings.Contains(cmd, "test-path")) &&
		(containsTailCommand(cmd) || strings.Contains(cmd, "docker images") || strings.Contains(cmd, "building $(date)"))
}

func looksLikeBackgroundPollCommand(cmd string) bool {
	return (strings.Contains(cmd, "sleep ") || strings.Contains(cmd, "start-sleep")) && containsTailCommand(cmd)
}

func containsTailCommand(cmd string) bool {
	return strings.Contains(cmd, "tail ") || strings.Contains(cmd, "get-content") || strings.Contains(cmd, "gc ")
}

func looksLongRunningBackgroundCommand(cmd string) bool {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	longPatterns := []string{
		"pip install", "pip3 install", "apt install", "apt-get install", "apt update", "apt-get update",
		"npm install", "yarn install", "pnpm install", "docker build", "docker pull", "docker compose", "docker-compose",
		"go build", "go test", "cargo build", "make ", "cmake --build", "git clone", "rsync ", "scp ",
	}
	for _, pattern := range longPatterns {
		if strings.Contains(cmd, pattern) {
			return true
		}
	}
	return cmd == "make"
}
