package guiapp

import (
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// remoteControlAttachTarget answers where a phone may place an attachment for
// one task: the task directory as the bot's computer sees it, and the bot that
// owns that computer.
//
// A task on a bot belongs to that bot's chat, and `remoteControlKnownTask`
// only accepts the bot's task directory, which is
// /home/desktop/.maclaw/data/tasks/<slug> inside the bot's container. The Hub
// writes the file there through the bot's desktop, so this desktop reports the
// container path, never the desktop app's own home path.
//
// An empty BotID means the task does not belong to a bot: there is no computer
// the Hub could write to, and the attachment is refused rather than dropped
// somewhere unrelated.
func (a *App) remoteControlAttachTarget(taskPath, resolved string) RemoteControlAttachTarget {
	path := strings.TrimSpace(resolved)
	if path == "" {
		path = strings.TrimSpace(taskPath)
	}
	if a == nil || path == "" {
		return RemoteControlAttachTarget{}
	}
	botID := desktopBotIDForTaskPath(path)
	if botID == "" {
		return RemoteControlAttachTarget{TaskDir: filepath.ToSlash(path)}
	}
	return RemoteControlAttachTarget{
		TaskDir: a.containerTaskDir(path),
		BotID:   botID,
		NodeID:  a.remoteControlBotNodeID(botID),
	}
}

// desktopBotIDForTaskPath reads the bot a task directory belongs to. Bot
// tasks live under <data dir>/bots/<bot id>/tasks/<slug>, which is what
// desktopBotIdentity uses for its session key; a plain user task has no
// "bots" segment and answers "".
func desktopBotIDForTaskPath(path string) string {
	slash := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(slash, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != "bots" || parts[i+1] == "" {
			continue
		}
		if parts[i+2] == "tasks" {
			return parts[i+1]
		}
	}
	return ""
}

// containerTaskDir maps this desktop's data directory onto the bot computer's
// home, the same mount the bot's chat writes its files into.
func (a *App) containerTaskDir(local string) string {
	dataDir := strings.TrimSpace(a.GetDataDir())
	slash := filepath.ToSlash(filepath.Clean(local))
	if dataDir != "" {
		data := filepath.ToSlash(filepath.Clean(dataDir))
		if rel, ok := strings.CutPrefix(slash, data); ok && (strings.HasPrefix(rel, "/") || rel == "") {
			return desktop.ContainerHome + "/.maclaw/data" + rel
		}
	}
	return slash
}

// remoteControlBotNodeID is the computer a bot runs on; empty means the cloud
// computer.
func (a *App) remoteControlBotNodeID(botID string) string {
	bots, err := a.ListDesktopBots()
	if err != nil {
		return ""
	}
	for _, bot := range bots {
		if strings.TrimSpace(bot.ID) == botID {
			return strings.TrimSpace(bot.NodeID)
		}
	}
	return ""
}
