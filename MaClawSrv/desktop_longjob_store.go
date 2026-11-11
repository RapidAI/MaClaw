package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

var (
	desktopLongJobMu   sync.Mutex
	desktopLongJobMem  = map[string]*desktopLongJob{}
	desktopLongJobPath = func() string { return filepath.Join(corelib.MaclawDataDir(), "desktop-longjobs.json") }
)

func desktopLongJobSave(j *desktopLongJob) error {
	if j == nil || j.ID == "" {
		return nil
	}
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	cp := *j
	desktopLongJobMem[j.ID] = &cp
	return desktopLongJobFlushLocked()
}

func desktopLongJobGet(id string) *desktopLongJob {
	id = trimJobID(id)
	if id == "" {
		return nil
	}
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	j := desktopLongJobMem[id]
	if j == nil {
		return nil
	}
	cp := *j
	return &cp
}

func desktopLongJobLatestFor(scope agentruntime.Scope) *desktopLongJob {
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	var best *desktopLongJob
	for _, j := range desktopLongJobMem {
		if !desktopLongJobSameOwner(j, scope) {
			continue
		}
		// A job still working outranks a newer one that already ended: the
		// run is "running" while anything it handed off is.
		active := j.Status == "running" || j.Status == "stalled"
		bestActive := best != nil && (best.Status == "running" || best.Status == "stalled")
		if best == nil || (active && !bestActive) || (active == bestActive && j.Started.After(best.Started)) {
			best = j
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

func desktopLongJobListFor(scope agentruntime.Scope) []*desktopLongJob {
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	var out []*desktopLongJob
	for _, j := range desktopLongJobMem {
		if desktopLongJobSameOwner(j, scope) {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out
}

func desktopLongJobRunning() []*desktopLongJob {
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	var out []*desktopLongJob
	for _, j := range desktopLongJobMem {
		if j.Status == "running" || j.Status == "stalled" {
			cp := *j
			out = append(out, &cp)
		}
	}
	return out
}

func desktopLongJobForRun(runID string) *desktopLongJob {
	if runID == "" {
		return nil
	}
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobLoadLocked()
	var best *desktopLongJob
	for _, j := range desktopLongJobMem {
		if j.SourceRun != runID {
			continue
		}
		if best == nil || j.Started.After(best.Started) {
			best = j
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

func desktopLongJobSameOwner(j *desktopLongJob, scope agentruntime.Scope) bool {
	if j == nil {
		return false
	}
	if scope.InstanceID != "" && j.InstanceID == scope.InstanceID {
		return true
	}
	return j.TenantID == scope.TenantID && j.UserID == scope.UserID
}

func trimJobID(id string) string {
	return strings.TrimSpace(id)
}

func desktopLongJobLoadLocked() {
	if len(desktopLongJobMem) > 0 {
		return
	}
	raw, err := os.ReadFile(desktopLongJobPath())
	if err != nil || len(raw) == 0 {
		return
	}
	var list []*desktopLongJob
	if json.Unmarshal(raw, &list) != nil {
		return
	}
	desktopLongJobMem = map[string]*desktopLongJob{}
	for _, j := range list {
		if j != nil && j.ID != "" {
			desktopLongJobMem[j.ID] = j
		}
	}
}

func desktopLongJobFlushLocked() error {
	list := make([]*desktopLongJob, 0, len(desktopLongJobMem))
	for _, j := range desktopLongJobMem {
		cp := *j
		list = append(list, &cp)
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	path := desktopLongJobPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func desktopLongJobResetForTest() {
	desktopLongJobMu.Lock()
	defer desktopLongJobMu.Unlock()
	desktopLongJobMem = map[string]*desktopLongJob{}
}
