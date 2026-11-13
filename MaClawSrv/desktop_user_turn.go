package main

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// One bot's open task. A run that stopped to ask keeps its goal here so the
// person's answer continues that goal. A secret value lives only in this
// map and in the bash environment; it is not put back into the chat.
type desktopBotTurn struct {
	mu      sync.Mutex
	goal    string
	waiting bool
	secrets map[string]string
}

var (
	desktopBotTurns sync.Map // bot id -> *desktopBotTurn
	desktopBotInst  sync.Map // instance id -> bot id
)

func desktopBotTurnOf(botID string) *desktopBotTurn {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil
	}
	if v, ok := desktopBotTurns.Load(botID); ok {
		return v.(*desktopBotTurn)
	}
	turn := &desktopBotTurn{secrets: map[string]string{}}
	actual, _ := desktopBotTurns.LoadOrStore(botID, turn)
	return actual.(*desktopBotTurn)
}

func desktopNoteBotInstance(instanceID, botID string) {
	instanceID = strings.TrimSpace(instanceID)
	botID = strings.TrimSpace(botID)
	if instanceID == "" || botID == "" {
		return
	}
	desktopBotInst.Store(instanceID, botID)
}

func desktopBotOfInstance(instanceID string) string {
	v, ok := desktopBotInst.Load(strings.TrimSpace(instanceID))
	if !ok {
		return ""
	}
	return v.(string)
}

func desktopMarkWaiting(botID string) {
	turn := desktopBotTurnOf(botID)
	if turn == nil {
		return
	}
	turn.mu.Lock()
	turn.waiting = true
	turn.mu.Unlock()
}

func desktopClearWaiting(botID string) {
	turn := desktopBotTurnOf(botID)
	if turn == nil {
		return
	}
	turn.mu.Lock()
	turn.waiting = false
	turn.mu.Unlock()
}

var (
	desktopSavedSecretRe  = regexp.MustCompile(`^已在本机保存 ([A-Z][A-Z0-9_]{0,63})。$`)
	desktopInlineSecretRe = regexp.MustCompile(`(?i)(?:^|[\s,，;；])((?:[A-Z][A-Z0-9_]{0,40})?(?:PASSWORD|PASSWD|SECRET|TOKEN)|密码|口令)\s*[:：=]\s*([^\s,，;；。]+)`)
)

// desktopPrepareUserTurn redacts a secret the person already typed and, when
// they are answering a run that stopped to ask, puts the original goal back
// in front of that answer. The secret value does not remain in the text.
func desktopPrepareUserTurn(botID, content string, meta map[string]string) string {
	turn := desktopBotTurnOf(botID)
	if turn == nil {
		return content
	}
	content = strings.TrimSpace(content)
	if meta != nil {
		name := strings.TrimSpace(meta["bot_secret_name"])
		value := strings.TrimSpace(meta["bot_secret_value"])
		delete(meta, "bot_secret_value")
		if agent.ValidSecretName(name) && value != "" {
			if turn.secrets == nil {
				turn.secrets = map[string]string{}
			}
			turn.mu.Lock()
			turn.secrets[name] = value
			turn.mu.Unlock()
		}
	}
	redacted, found := desktopCaptureInlineSecrets(content)
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.secrets == nil {
		turn.secrets = map[string]string{}
	}
	for name, value := range found {
		turn.secrets[name] = value
	}
	if name := desktopSavedSecretRe.FindStringSubmatch(redacted); turn.waiting && turn.goal != "" && name != nil {
		use := name[1]
		if _, ok := turn.secrets[use]; !ok {
			for key := range turn.secrets {
				use = key
				break
			}
		}
		if _, ok := turn.secrets[use]; ok {
			return desktopResumeText(turn.goal, use)
		}
		return "继续原来的任务，不要另起一件。\n原来的任务：\n" + turn.goal + "\n用户说已经保存了 " + name[1] + "，但这台运行环境里没有这个值。不要声称它已经在环境里，也不要去其他机器人的目录里找。用一句话说明还需要这个人再提供一次。"
	}
	if !desktopSavedSecretRe.MatchString(redacted) {
		turn.goal = redacted
	}
	return redacted
}

func desktopResumeText(goal, name string) string {
	return "继续原来的任务，不要另起一件。\n原来的任务：\n" + goal +
		"\n用户已经把 " + name + " 保存在这台电脑上。它在环境变量 " + name +
		" 里。用 $" + name + "，不要打印它的值，不要在磁盘或其他机器人的目录里寻找它，也不要再向用户索要。"
}

// desktopCaptureInlineSecrets removes secret values from text and returns
// the names it kept. A value is never part of the redacted text.
func desktopCaptureInlineSecrets(text string) (string, map[string]string) {
	found := map[string]string{}
	redacted := desktopInlineSecretRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := desktopInlineSecretRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		name := strings.ToUpper(strings.TrimSpace(parts[1]))
		name = strings.Map(func(r rune) rune {
			switch r {
			case '密', '码', '口', '令':
				return -1
			}
			return r
		}, name)
		name = strings.Trim(name, "_")
		if name == "" || name == "PASSWORD" || name == "PASSWD" || name == "SECRET" || name == "TOKEN" {
			name = "SSH_PASSWORD"
		}
		if !agent.ValidSecretName(name) {
			name = "SSH_PASSWORD"
		}
		value := strings.Trim(parts[2], `"'`)
		if value == "" || value == "***" {
			return match
		}
		found[name] = value
		prefix := match[:strings.Index(match, parts[1])]
		return prefix + name + "=（已保存为 " + name + "，在环境变量里，不要再索要，不要打印）"
	})
	return redacted, found
}

func desktopSecretsForScope(scope agentruntime.Scope) map[string]string {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		bot = desktopBotOfInstance(scope.InstanceID)
	}
	turn := desktopBotTurnOf(bot)
	if turn == nil {
		return nil
	}
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if len(turn.secrets) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range turn.secrets {
		out[k] = v
	}
	return out
}

func bashSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// desktopExportSecrets prefixes a command with exports. The values are not
// written into logs that only keep the model's own command.
func desktopExportSecrets(command string, secrets map[string]string) string {
	if len(secrets) == 0 {
		return command
	}
	var b strings.Builder
	for name, value := range secrets {
		if !agent.ValidSecretName(name) || value == "" {
			continue
		}
		b.WriteString("export ")
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(bashSingleQuote(value))
		b.WriteString("; ")
	}
	if b.Len() == 0 {
		return command
	}
	b.WriteString(command)
	return b.String()
}

func desktopScrubSecrets(text string, secrets map[string]string) string {
	for _, value := range secrets {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[set]")
	}
	return text
}

var desktopForeignTreeRe = regexp.MustCompile(`(?:/home/desktop/work|~/素材|/home/desktop/素材|\$HOME/素材|\$\{HOME\}/素材)/([^/\s;&|'"\\]+)`)

// desktopBashConfined rejects a command that lists another bot's project
// tree or the shared git credential file. This bot's own folder is allowed.
func desktopBashConfined(scope agentruntime.Scope, command string) error {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		bot = desktopBotOfInstance(scope.InstanceID)
	}
	if bot == "" {
		return nil
	}
	safe := desktopWorkdirSafe.ReplaceAllString(bot, "_")
	if strings.Contains(command, ".git-credentials") {
		return errDesktopBashConfined
	}
	for _, m := range desktopForeignTreeRe.FindAllStringSubmatch(command, -1) {
		seg := m[1]
		if seg != bot && seg != safe && seg != "." && seg != ".." {
			return errDesktopBashConfined
		}
	}
	flat := strings.Join(strings.Fields(command), " ")
	for _, parent := range []string{"/home/desktop/work", "~/素材", "/home/desktop/素材"} {
		if strings.Contains(flat, parent) && !strings.Contains(flat, parent+"/"+bot) && !strings.Contains(flat, parent+"/"+safe) {
			return errDesktopBashConfined
		}
	}
	return nil
}

var errDesktopBashConfined = errors.New("this bot can only read its own project folder")
