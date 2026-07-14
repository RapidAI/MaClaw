package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// detectRemoteProjectVerifyCommand probes the remote project via SSH for a
// suitable verify command (go.mod / package.json / Cargo.toml / …).
func detectRemoteProjectVerifyCommand(h *IMMessageHandler, sessionID, projectDir string) string {
	if h == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	projectDir = strings.TrimSpace(projectDir)
	if sessionID == "" || projectDir == "" {
		return ""
	}
	// Single remote probe: print the first matching marker.
	// shell: portable enough for Linux remote coding hosts.
	probe := fmt.Sprintf(
		`cd %s 2>/dev/null || exit 0; `+
			`if [ -f go.mod ]; then echo GO; `+
			`elif [ -f Cargo.toml ]; then echo CARGO; `+
			`elif [ -f package.json ]; then echo NODE; `+
			`elif [ -f pyproject.toml ] || [ -f requirements.txt ]; then echo PY; `+
			`elif [ -f Makefile ]; then echo MAKE; `+
			`elif [ -f CMakeLists.txt ]; then echo CMAKE; `+
			`else echo NONE; fi`,
		remoteShellQuote(projectDir),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      probe,
		"wait_seconds": float64(20),
	})
	marker := strings.ToUpper(extractRemoteProbeMarker(out))
	switch marker {
	case "GO":
		return "go build ./... && go vet ./..."
	case "CARGO":
		return "cargo check"
	case "NODE":
		// Prefer typecheck/build/lint if scripts exist — best-effort npm run.
		return detectRemoteNodeVerifyCommand(h, sessionID, projectDir)
	case "PY":
		return "python3 -m py_compile $(find . -name '*.py' -not -path './.git/*' | head -n 40) 2>/dev/null || python3 -m pytest --co -q 2>/dev/null || true"
	case "MAKE":
		return "make -n check 2>/dev/null || make -n test 2>/dev/null || make -n build 2>/dev/null || true"
	case "CMAKE":
		return "cmake --build build 2>/dev/null || true"
	default:
		return ""
	}
}

func detectRemoteNodeVerifyCommand(h *IMMessageHandler, sessionID, projectDir string) string {
	probe := fmt.Sprintf(
		`cd %s 2>/dev/null || exit 0; `+
			`node -e "try{const s=require('./package.json').scripts||{}; if(s.typecheck)console.log('typecheck'); else if(s.build)console.log('build'); else if(s.lint)console.log('lint'); else if(s.test)console.log('test'); else console.log('none')}catch(e){console.log('none')}"`,
		remoteShellQuote(projectDir),
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      probe,
		"wait_seconds": float64(20),
	})
	script := strings.ToLower(extractRemoteProbeMarker(out))
	switch script {
	case "typecheck", "build", "lint", "test":
		return "npm run " + script
	default:
		return ""
	}
}

func extractRemoteProbeMarker(sshOutput string) string {
	// Prefer last non-empty non-maclaw line.
	lines := strings.Split(sshOutput, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "[maclaw]") || strings.HasPrefix(line, "错误") {
			continue
		}
		// Take first token (GO, CARGO, typecheck, …).
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Keep original case for npm script names; callers upper/lower as needed.
		return fields[0]
	}
	return ""
}

// runCodingWorkbenchRemoteStepVerify runs project verification over SSH.
func runCodingWorkbenchRemoteStepVerify(
	h *IMMessageHandler,
	sessionID, projectDir string,
) (ok bool, cmd, output string, skipped bool) {
	if h == nil {
		return true, "", "", true
	}
	sessionID = strings.TrimSpace(sessionID)
	projectDir = strings.TrimSpace(projectDir)
	if sessionID == "" || projectDir == "" {
		return true, "", "", true
	}
	cmd = detectRemoteProjectVerifyCommand(h, sessionID, projectDir)
	if strings.TrimSpace(cmd) == "" {
		return true, "", "", true
	}
	// Capture exit code in output so we can parse success without relying on
	// sshExec's soft success heuristics alone.
	wrapped := fmt.Sprintf(
		`cd %s && { %s ; }; echo "__MACLAW_VERIFY_EXIT:$?__"`,
		remoteShellQuote(projectDir),
		cmd,
	)
	out := h.sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      wrapped,
		"wait_seconds": float64(180),
	})
	out = strings.TrimSpace(out)
	if utf8.RuneCountInString(out) > 2000 {
		out = truncateRunesForSubAgent(out, 2000)
	}
	// Tool-level hard failure.
	if remoteCodingToolOutcome(out) != "success" && !strings.Contains(out, "__MACLAW_VERIFY_EXIT:") {
		return false, cmd, out, false
	}
	exitCode := parseRemoteVerifyExitCode(out)
	// Strip marker from user-facing output.
	clean := strings.TrimSpace(stripRemoteVerifyExitMarker(out))
	if clean == "" {
		clean = "(remote verify completed with no output)"
	}
	if exitCode != 0 {
		return false, cmd, clean, false
	}
	return true, cmd, clean, false
}

func parseRemoteVerifyExitCode(out string) int {
	const marker = "__MACLAW_VERIFY_EXIT:"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		// No marker: treat tool success as pass, else fail.
		if remoteCodingToolOutcome(out) == "success" {
			return 0
		}
		return 1
	}
	rest := out[idx+len(marker):]
	end := strings.IndexAny(rest, "_\n\r ")
	if end > 0 {
		rest = rest[:end]
	}
	rest = strings.Trim(rest, "_ \t")
	code := 1
	fmt.Sscanf(rest, "%d", &code)
	return code
}

func stripRemoteVerifyExitMarker(out string) string {
	const marker = "__MACLAW_VERIFY_EXIT:"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		return out
	}
	// Drop the marker line.
	before := out[:idx]
	after := out[idx:]
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		after = after[nl+1:]
	} else {
		after = ""
	}
	return strings.TrimSpace(before + after)
}
