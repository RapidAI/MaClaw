package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// codingWorkbenchHooks is an optional .maclaw/hooks.json for pure-coding lifecycle.
// Example:
//
//	{
//	  "pre_step": ["echo pre"],
//	  "post_step": ["echo post"],
//	  "pre_plan": [],
//	  "post_turn": [],
//	  "pre_verify": [],
//	  "post_verify": [],
//	  "pre_checkpoint": [],
//	  "post_checkpoint": [],
//	  "on_conflict": [],
//	  "fail_on_error": false
//	}
type codingWorkbenchHooks struct {
	PreStep        []string `json:"pre_step"`
	PostStep       []string `json:"post_step"`
	PrePlan        []string `json:"pre_plan"`
	PostTurn       []string `json:"post_turn"`
	PreVerify      []string `json:"pre_verify"`
	PostVerify     []string `json:"post_verify"`
	PreCheckpoint  []string `json:"pre_checkpoint"`
	PostCheckpoint []string `json:"post_checkpoint"`
	OnConflict     []string `json:"on_conflict"`
	// FailOnError aborts the gated phase when a pre_* (or on_conflict) command exits non-zero.
	// Default false: failures are reported but do not stop the workbench.
	FailOnError bool `json:"fail_on_error"`
}

// codingHookRunResult is the outcome of running one hooks phase.
type codingHookRunResult struct {
	Phase  string
	Report string
	Failed bool
	Ran    int
}

// codingHooksCacheTTL bounds status-poll disk reads of .maclaw/hooks.json.
const codingHooksCacheTTL = 2 * time.Second

type codingHooksCacheEntry struct {
	hooks    codingWorkbenchHooks
	modUnix  int64 // 0 when file missing
	exists   bool
	loadedAt time.Time
}

var codingHooksCache sync.Map // projectPath -> codingHooksCacheEntry

func loadCodingWorkbenchHooks(projectPath string) codingWorkbenchHooks {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return codingWorkbenchHooks{}
	}
	path := filepath.Join(projectPath, ".maclaw", "hooks.json")
	info, statErr := os.Stat(path)
	modUnix := int64(0)
	exists := false
	if statErr == nil && !info.IsDir() {
		exists = true
		modUnix = info.ModTime().UnixNano()
	}
	if raw, ok := codingHooksCache.Load(projectPath); ok {
		ent := raw.(codingHooksCacheEntry)
		if time.Since(ent.loadedAt) < codingHooksCacheTTL && ent.exists == exists && ent.modUnix == modUnix {
			return ent.hooks
		}
	}
	if !exists {
		ent := codingHooksCacheEntry{loadedAt: time.Now(), exists: false}
		codingHooksCache.Store(projectPath, ent)
		return codingWorkbenchHooks{}
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		ent := codingHooksCacheEntry{loadedAt: time.Now(), exists: true, modUnix: modUnix}
		codingHooksCache.Store(projectPath, ent)
		return codingWorkbenchHooks{}
	}
	var h codingWorkbenchHooks
	if err := json.Unmarshal(data, &h); err != nil {
		log.Printf("[coding-hooks] parse %s: %v", path, err)
		ent := codingHooksCacheEntry{loadedAt: time.Now(), exists: true, modUnix: modUnix}
		codingHooksCache.Store(projectPath, ent)
		return codingWorkbenchHooks{}
	}
	codingHooksCache.Store(projectPath, codingHooksCacheEntry{
		hooks:    h,
		modUnix:  modUnix,
		exists:   true,
		loadedAt: time.Now(),
	})
	return h
}

// codingWorkbenchHooksSummary returns active phase names and total command count in one pass.
func codingWorkbenchHooksSummary(h codingWorkbenchHooks) (phases []string, count int) {
	type pair struct {
		name string
		cmds []string
	}
	pairs := []pair{
		{"pre_plan", h.PrePlan},
		{"pre_step", h.PreStep},
		{"post_step", h.PostStep},
		{"pre_verify", h.PreVerify},
		{"post_verify", h.PostVerify},
		{"post_turn", h.PostTurn},
		{"pre_checkpoint", h.PreCheckpoint},
		{"post_checkpoint", h.PostCheckpoint},
		{"on_conflict", h.OnConflict},
	}
	for _, p := range pairs {
		n := 0
		for _, c := range p.cmds {
			if strings.TrimSpace(c) != "" {
				n++
			}
		}
		if n > 0 {
			phases = append(phases, p.name)
			count += n
		}
	}
	return phases, count
}

// codingWorkbenchHooksActivePhases lists phase names that have at least one command.
func codingWorkbenchHooksActivePhases(h codingWorkbenchHooks) []string {
	phases, _ := codingWorkbenchHooksSummary(h)
	return phases
}

// codingWorkbenchHooksCommandCount returns total non-empty hook commands.
func codingWorkbenchHooksCommandCount(h codingWorkbenchHooks) int {
	_, n := codingWorkbenchHooksSummary(h)
	return n
}

// runCodingWorkbenchHookPhase runs one named phase from loaded hooks.
// When hooks.FailOnError is set, stops after the first failing command in the phase.
func runCodingWorkbenchHookPhase(projectPath string, hooks codingWorkbenchHooks, phase string) codingHookRunResult {
	phase = strings.ToLower(strings.TrimSpace(phase))
	var cmds []string
	switch phase {
	case "pre_plan":
		cmds = hooks.PrePlan
	case "pre_step":
		cmds = hooks.PreStep
	case "post_step":
		cmds = hooks.PostStep
	case "pre_verify":
		cmds = hooks.PreVerify
	case "post_verify":
		cmds = hooks.PostVerify
	case "post_turn":
		cmds = hooks.PostTurn
	case "pre_checkpoint":
		cmds = hooks.PreCheckpoint
	case "post_checkpoint":
		cmds = hooks.PostCheckpoint
	case "on_conflict":
		cmds = hooks.OnConflict
	default:
		return codingHookRunResult{Phase: phase}
	}
	return runCodingWorkbenchHooksOpts(projectPath, cmds, phase, hooks.FailOnError)
}

// runCodingWorkbenchHookCommands runs shell snippets and returns a report string
// (legacy helper; prefer runCodingWorkbenchHooks when fail detection is needed).
func runCodingWorkbenchHookCommands(projectPath string, cmds []string, phase string) string {
	return runCodingWorkbenchHooks(projectPath, cmds, phase).Report
}

func runCodingWorkbenchHooks(projectPath string, cmds []string, phase string) codingHookRunResult {
	return runCodingWorkbenchHooksOpts(projectPath, cmds, phase, false)
}

func runCodingWorkbenchHooksOpts(projectPath string, cmds []string, phase string, stopOnFail bool) codingHookRunResult {
	res := codingHookRunResult{Phase: phase}
	if len(cmds) == 0 {
		return res
	}
	projectPath = strings.TrimSpace(projectPath)
	var reports []string
	for i, raw := range cmds {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Safety: only allow relatively short shell snippets.
		if len(raw) > 2000 {
			reports = append(reports, fmt.Sprintf("%s[%d]: skipped (command too long)", phase, i+1))
			continue
		}
		res.Ran++
		out, err := execHookCommand(projectPath, raw, 60*time.Second)
		if err != nil {
			res.Failed = true
			reports = append(reports, fmt.Sprintf("%s[%d] FAIL: %s — %v\n%s", phase, i+1, raw, err, truncateRunesForSubAgent(out, 300)))
			log.Printf("[coding-hooks] %s cmd failed project=%s err=%v", phase, projectPath, err)
			if stopOnFail {
				break
			}
			continue
		}
		if strings.TrimSpace(out) != "" {
			reports = append(reports, fmt.Sprintf("%s[%d] OK: %s\n%s", phase, i+1, raw, truncateRunesForSubAgent(out, 200)))
		} else {
			reports = append(reports, fmt.Sprintf("%s[%d] OK: %s", phase, i+1, raw))
		}
	}
	res.Report = strings.Join(reports, "\n")
	return res
}

// codingHookShouldAbort reports whether FailOnError should stop the gated phase.
func codingHookShouldAbort(hooks codingWorkbenchHooks, res codingHookRunResult) bool {
	return hooks.FailOnError && res.Failed
}

// fireCodingOnConflictHook runs on_conflict lifecycle hooks for projectPath
// (falls back to sticky ProjectPath when empty).
func (h *IMMessageHandler) fireCodingOnConflictHook(userID, projectPath string) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" && h != nil {
		projectPath = strings.TrimSpace(h.getStickyCodingWorkbenchMemory(userID).ProjectPath)
	}
	if projectPath == "" {
		return
	}
	hooks := loadCodingWorkbenchHooks(projectPath)
	if len(hooks.OnConflict) == 0 {
		return
	}
	if res := runCodingWorkbenchHookPhase(projectPath, hooks, "on_conflict"); res.Report != "" {
		log.Printf("[coding-hooks] on_conflict: %s", truncateRunesForSubAgent(res.Report, 160))
	}
}

func execHookCommand(projectPath, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	hideCommandWindow(cmd)
	if projectPath != "" {
		cmd.Dir = projectPath
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
