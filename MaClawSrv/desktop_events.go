package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"

	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Desktop run event stream (docs/desktop-observability.md). One JSONL file
// per run under <data root>/desktop-runs/<tenant>/<bot>/<run>.jsonl. Lines
// carry no typed text, scripts, cookies or keys: arguments go through the
// same summary as the trajectory (free text becomes a length), errors and
// reasons are clipped. Per file 2 MB; whole tree pruned to 14 days / 300 MB.
// MACLAW_DESKTOP_EVENTS=off disables it.

const (
	desktopEventsEnv       = "MACLAW_DESKTOP_EVENTS"
	desktopEventsFileMax   = 2 << 20
	desktopEventsTreeMax   = 300 << 20
	desktopEventsKeepDays  = 14
	desktopEventsPruneEach = 10 * time.Minute
)

// desktopEventsRoot overrides the directory in tests.
var desktopEventsRoot = ""

var (
	desktopEventsMu        sync.Mutex
	desktopEventsLastPrune time.Time
)

type desktopEvent struct {
	TS          string            `json:"ts"`
	Event       string            `json:"event"`
	RunID       string            `json:"run_id"`
	Tenant      string            `json:"tenant,omitempty"`
	Bot         string            `json:"bot,omitempty"`
	UserKey     string            `json:"user_key,omitempty"`
	Instance    string            `json:"instance,omitempty"`
	Step        int               `json:"step,omitempty"`
	Model       string            `json:"model,omitempty"`
	Action      string            `json:"action,omitempty"`
	Args        map[string]string `json:"args,omitempty"`
	Result      string            `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	ThinkMS     int64             `json:"think_ms,omitempty"`
	ExecMS      int64             `json:"exec_ms,omitempty"`
	ImageTokens int               `json:"image_tokens,omitempty"`
	Actions     int               `json:"actions,omitempty"`
	MaxActions  int               `json:"max_actions,omitempty"`
	MaxTokens   int               `json:"max_image_tokens,omitempty"`
	Site        string            `json:"site,omitempty"`
	Lease       string            `json:"lease,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Trajectory  string            `json:"trajectory,omitempty"`
}

func desktopEventsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopEventsEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

func desktopEventsDir() string {
	if desktopEventsRoot != "" {
		return desktopEventsRoot
	}
	return filepath.Join(defaultDataRoot(), "desktop-runs")
}

func desktopEventSafe(s string) string {
	s = desktopTrajectorySafe(s)
	if s == "" {
		return "_"
	}
	return s
}

// desktopEventIdentity fills run, tenant, bot and the hashed user key.
func desktopEventIdentity(ev *desktopEvent, scope agentruntime.Scope) {
	owner, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		bot = "owner"
	}
	sum := sha256.Sum256([]byte(scope.TenantID + "\x00" + owner))
	ev.RunID = scope.RunID
	ev.Tenant = scope.TenantID
	ev.Bot = bot
	ev.UserKey = hex.EncodeToString(sum[:6])
	ev.Instance = scope.InstanceID
}

// desktopEmit appends one event to its run file. Never fails the caller.
func desktopEmit(scope agentruntime.Scope, ev desktopEvent) {
	if !desktopEventsEnabled() || strings.TrimSpace(scope.RunID) == "" {
		return
	}
	if ev.TS == "" {
		ev.TS = time.Now().Format(time.RFC3339Nano)
	}
	desktopEventIdentity(&ev, scope)
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	dir := filepath.Join(desktopEventsDir(), desktopEventSafe(ev.Tenant), desktopEventSafe(ev.Bot))
	path := filepath.Join(dir, desktopEventSafe(ev.RunID)+".jsonl")
	desktopEventsMu.Lock()
	defer desktopEventsMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size() > desktopEventsFileMax {
		return
	} else if err != nil {
		desktopEventsPruneLocked()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// desktopEventsPruneLocked drops run files older than the retention, then
// the oldest until the tree fits. At most once per desktopEventsPruneEach.
func desktopEventsPruneLocked() {
	now := time.Now()
	if now.Sub(desktopEventsLastPrune) < desktopEventsPruneEach {
		return
	}
	desktopEventsLastPrune = now
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	_ = filepath.WalkDir(desktopEventsDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if now.Sub(info.ModTime()) > desktopEventsKeepDays*24*time.Hour {
			_ = os.Remove(path)
			return nil
		}
		files = append(files, file{path, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= desktopEventsTreeMax {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// desktopResultClass names a step outcome for the event stream.
func desktopResultClass(out string, err error, refusal string) string {
	switch refusal {
	case "":
	case "guard":
		return "guard"
	default:
		return "refused"
	}
	if class := desktopErrorClass(out, err); class != "" {
		return class
	}
	if err != nil {
		return "error"
	}
	return "ok"
}

// desktopEmitStep records one desktop tool call. refusal is "refused"
// (repeat guard), "guard" (failure-class guard) or "budget", else "".
func desktopEmitStep(scope, owner agentruntime.Scope, args map[string]any, model, out string, err error, took time.Duration, refusal string) {
	if !desktopEventsEnabled() {
		return
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if !desktopActionAllowed(action) {
		action = "invalid"
	}
	ev := desktopEvent{Event: "step", Model: model, Action: action, Args: desktopTrajectoryArgs(args), ExecMS: took.Milliseconds(),
		Result: desktopResultClass(out, err, refusal), MaxActions: desktopRunMaxActions(), MaxTokens: desktopRunMaxImageTokens()}
	if err != nil {
		ev.Error = desktopStepClip(err.Error(), 200)
	}
	if refusal != "" && refusal != "guard" {
		ev.Reason = refusal
	}
	if st := desktopRunFor(owner); st != nil {
		now := time.Now()
		st.mu.Lock()
		ev.Step, ev.Actions, ev.ImageTokens, ev.Site = st.seq, st.actions, st.imageTokens, st.site
		if !st.lastEventEnd.IsZero() {
			// Time between the previous call's end and this call's start:
			// mostly the model deciding the next step.
			ev.ThinkMS = now.Add(-took).Sub(st.lastEventEnd).Milliseconds()
			if ev.ThinkMS < 0 {
				ev.ThinkMS = 0
			}
		}
		st.lastEventEnd = now
		if st.dir != "" && refusal == "" && st.seq > 0 {
			ev.Trajectory = fmt.Sprintf("%s/step-%03d.json", st.dir, st.seq)
		}
		st.mu.Unlock()
	}
	desktopEmit(scope, ev)
}

// desktopEmitNote records a non-step event (takeover, lease, ...).
func desktopEmitNote(scope agentruntime.Scope, event, lease, reason string) {
	desktopEmit(scope, desktopEvent{Event: event, Lease: lease, Reason: desktopStepClip(reason, 160)})
}

// desktopNoteLease emits a lease event when the bot_state lease changes.
func desktopNoteLease(scope agentruntime.Scope, lease string, state map[string]any) {
	if lease == "" {
		return
	}
	st := desktopRunFor(desktopOwner(scope))
	if st == nil {
		return
	}
	st.mu.Lock()
	changed := st.lastLease != lease
	st.lastLease = lease
	st.mu.Unlock()
	if !changed {
		return
	}
	reason, _ := state["reason"].(string)
	if takeover, _ := state["takeover"].(bool); takeover && reason == "" {
		reason = "takeover"
	}
	desktopEmitNote(scope, "lease", lease, reason)
}
