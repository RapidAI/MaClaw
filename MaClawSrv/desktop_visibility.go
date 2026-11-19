package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// What a person can be shown. Process stays on the run record and the
// status line. needs_user and outcome are the only kinds that enter the chat.
const (
	desktopMsgProcess   = "process"
	desktopMsgNeedsUser = "needs_user"
	desktopMsgOutcome   = "outcome"
)

// desktopMessageKind classifies one outward message from where it was
// produced. A model claim does not promote a mid-run note into the chat:
// outcome is only the run's finish, and needs_user only when this turn
// already has a structured ask or takeover.
func desktopMessageKind(claimed string, finishing, needsUser bool) string {
	switch strings.TrimSpace(claimed) {
	case desktopMsgNeedsUser:
		if needsUser {
			return desktopMsgNeedsUser
		}
		return desktopMsgProcess
	case desktopMsgOutcome:
		if finishing {
			return desktopMsgOutcome
		}
		return desktopMsgProcess
	case desktopMsgProcess:
		return desktopMsgProcess
	default:
		if finishing {
			return desktopMsgOutcome
		}
		if needsUser {
			return desktopMsgNeedsUser
		}
		return desktopMsgProcess
	}
}

func desktopChatKind(kind string) bool {
	return kind == desktopMsgNeedsUser || kind == desktopMsgOutcome
}

var (
	desktopProcessMu    sync.Mutex
	desktopProcessLines = map[string][]string{}
)

// desktopRecordProcess keeps one process note on the run. It is not a chat
// message. The newest note is also what the status line shows.
func desktopRecordProcess(runID, text string) {
	runID = strings.TrimSpace(runID)
	text = strings.TrimSpace(text)
	if runID == "" || text == "" {
		return
	}
	desktopProcessMu.Lock()
	lines := append(desktopProcessLines[runID], text)
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	desktopProcessLines[runID] = lines
	desktopProcessMu.Unlock()
	dir := filepath.Join(corelib.MaclawDataDir(), "run-process")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	raw, err := json.Marshal(map[string]string{"kind": desktopMsgProcess, "text": text, "at": time.Now().Format(time.RFC3339)})
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, runID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

func desktopProcessOf(runID string) []string {
	desktopProcessMu.Lock()
	defer desktopProcessMu.Unlock()
	lines := desktopProcessLines[strings.TrimSpace(runID)]
	if len(lines) == 0 {
		return nil
	}
	return append([]string(nil), lines...)
}

func desktopLatestProcess(runID string) string {
	lines := desktopProcessOf(runID)
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
