package main

import (
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Scratch folders.
//
// Per bot (papers 11-11 d96): isolated bots share their owner's container, so
// /tmp held another bot's or an older paper's figures (/tmp/pub, /tmp/fig…)
// and a run could pick up stale assets.
//
// Per run (Daniel 11-12 06:29 「改」): one bot can run several tasks at once,
// and its runs shared /tmp/maclaw-<bot>, so two runs overwrote each other's
// gen.py or report.html. Each run's bash now gets TMPDIR set to
// /tmp/maclaw-<bot>/run-<run>/ ("main" stands for the owner's shared desktop),
// and the first bash result of a run names that folder so the model writes its
// material there. A call without a run id keeps the old per-bot folder (owner:
// no TMPDIR at all). Nothing here deletes a folder: a delivered or published
// file may still be read from it by a later step, and a background job keeps
// its own folder under /tmp/maclaw-<bot>/jobs/.

var desktopScratchSafe = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

var desktopScratchTold sync.Map // run id -> true once the folder was named

// desktopScratchRunPart is the run's folder name under the bot's scratch
// root ("" without a run id). The full run id is kept (sanitized, capped) so
// two runs never share a folder.
func desktopScratchRunPart(runID string) string {
	run := strings.TrimPrefix(strings.TrimSpace(runID), "run_")
	if run == "" {
		return ""
	}
	run = desktopScratchSafe.ReplaceAllString(run, "_")
	if len(run) > 48 {
		run = run[:48]
	}
	return "run-" + run
}

// desktopBotScratchDir is this run's own scratch folder. Without a run id it
// is the isolated bot's folder ("" for the owner's shared desktop), as before.
func desktopBotScratchDir(scope agentruntime.Scope) string {
	_, bot := desktop.SplitDesktopUser(desktopOwner(scope).UserID)
	run := desktopScratchRunPart(scope.RunID)
	if bot == "" {
		if run == "" {
			return ""
		}
		bot = "main"
	}
	root := "/tmp/maclaw-" + desktopScratchSafe.ReplaceAllString(bot, "_")
	if run == "" {
		return root
	}
	return path.Join(root, run)
}

// desktopBotScratch prefixes a bash command with this run's TMPDIR and
// returns the note for the result (once per run).
func desktopBotScratch(scope agentruntime.Scope, args map[string]any) string {
	dir := desktopBotScratchDir(scope)
	if dir == "" || args == nil {
		return ""
	}
	cmd, _ := args["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return ""
	}
	args["command"] = fmt.Sprintf("mkdir -p %s && export TMPDIR=%s; %s", dir, dir, cmd)
	run := strings.TrimSpace(scope.RunID)
	if run == "" {
		return ""
	}
	if _, told := desktopScratchTold.LoadOrStore(run, true); told {
		return ""
	}
	log.Printf("[desktop-scratch] run=%s TMPDIR=%s", run, dir)
	return fmt.Sprintf("(this run's own scratch folder is %s — $TMPDIR in bash. /tmp is shared with other bots, and other tasks of this bot may run at the same time with their own folders, so put this run's text, pictures, scripts and downloads in %s (not in /tmp directly) and pass those paths to file/publish/deliver)\n", dir, dir)
}

// forgetDesktopScratch drops the run's once-per-run note marker.
func forgetDesktopScratch(runID string) {
	if runID = strings.TrimSpace(runID); runID != "" {
		desktopScratchTold.Delete(runID)
	}
}
