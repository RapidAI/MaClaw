package main

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// The multibot message state that must outlive a MaClawSrv restart: how
// many notify_main messages each run already sent (the per-run cap), and
// which Hub mailbox ids this server already put in a prompt (consumer-side
// dedupe, so an ack lost on the way to Hub does not show a message twice).
// It is stored in multibot-state.db. multibot-state.json is only the
// pre-migration file. Entries older than two days are dropped.

const multibotStateKeep = 48 * time.Hour

type multibotRunCount struct {
	N  int    `json:"n"`
	At string `json:"at"`
}

type multibotState struct {
	Runs map[string]multibotRunCount `json:"runs"`
	Seen map[string]string           `json:"seen"`
}

var (
	multibotStateMu   sync.Mutex
	multibotStatePath = func() string { return filepath.Join(corelib.MaclawDataDir(), "multibot-state.json") }
)

func loadMultibotState() multibotState {
	st := readMultibotState()
	if st.Runs == nil {
		st.Runs = map[string]multibotRunCount{}
	}
	if st.Seen == nil {
		st.Seen = map[string]string{}
	}
	now := time.Now()
	for k, v := range st.Runs {
		if at, err := time.Parse(time.RFC3339, v.At); err != nil || now.Sub(at) > multibotStateKeep {
			delete(st.Runs, k)
		}
	}
	for k, v := range st.Seen {
		if at, err := time.Parse(time.RFC3339, v); err != nil || now.Sub(at) > multibotStateKeep {
			delete(st.Seen, k)
		}
	}
	return st
}

func saveMultibotState(st multibotState) {
	_ = writeMultibotState(st)
}

// desktopNotifyTake counts one message for runKey; false when the run
// already sent limit.
func desktopNotifyTake(runKey string, limit int) bool {
	multibotStateMu.Lock()
	defer multibotStateMu.Unlock()
	st := loadMultibotState()
	c := st.Runs[runKey]
	if c.N >= limit {
		return false
	}
	c.N++
	c.At = time.Now().UTC().Format(time.RFC3339)
	st.Runs[runKey] = c
	saveMultibotState(st)
	return true
}

func desktopNotifyGiveBack(runKey string) {
	multibotStateMu.Lock()
	defer multibotStateMu.Unlock()
	st := loadMultibotState()
	if c, ok := st.Runs[runKey]; ok && c.N > 0 {
		c.N--
		st.Runs[runKey] = c
		saveMultibotState(st)
	}
}

// desktopInboxFirstSeen marks ids as shown and returns, per id, whether it
// was new.
func desktopInboxFirstSeen(scope string, ids []string) []bool {
	multibotStateMu.Lock()
	defer multibotStateMu.Unlock()
	st := loadMultibotState()
	out := make([]bool, len(ids))
	stamp := time.Now().UTC().Format(time.RFC3339)
	for i, id := range ids {
		k := scope + "|" + id
		if _, ok := st.Seen[k]; !ok {
			out[i] = true
			st.Seen[k] = stamp
		}
	}
	saveMultibotState(st)
	return out
}

// desktopMessageKey is the idempotency key Hub dedupes on: the same run
// sending the same message (a retried tool call) files it once.
func desktopMessageKey(runKey string, parts ...string) string {
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return runKey + ":" + hex.EncodeToString(sum[:8])
}
