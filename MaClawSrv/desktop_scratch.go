package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Per-bot scratch folder (papers 11-11 d96): isolated bots share their
// owner's container, so /tmp held another bot's or an older paper's
// figures (/tmp/pub, /tmp/fig…) and a run could pick up stale assets. Each
// isolated bot's bash gets TMPDIR set to its own folder, and the first bash
// result of a run names that folder so the model writes its material there.

var desktopScratchSafe = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

var desktopScratchTold sync.Map // run id -> true once the folder was named

// desktopBotScratchDir is the isolated bot's own scratch folder ("" for the
// owner's shared desktop).
func desktopBotScratchDir(scope agentruntime.Scope) string {
	_, bot := desktop.SplitDesktopUser(desktopOwner(scope).UserID)
	if bot == "" {
		return ""
	}
	return "/tmp/maclaw-" + desktopScratchSafe.ReplaceAllString(bot, "_")
}

// desktopBotScratch prefixes an isolated bot's bash command with its
// TMPDIR and returns the note for the result (once per run).
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
	if _, told := desktopScratchTold.LoadOrStore(strings.TrimSpace(scope.RunID), true); told {
		return ""
	}
	return fmt.Sprintf("(your own scratch folder is %s — $TMPDIR in bash; /tmp is shared with other bots and may hold their or older runs' files, so put this run's text, pictures and downloads in %s and pass those paths to file/publish/deliver)\n", dir, dir)
}
