package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
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

// desktopPrepareUserTurn redacts a secret the person already typed and, when
// they are answering a run that stopped to ask, puts the original goal back
// in front of that answer. The secret value does not remain in the text, and
// every secret carried in meta is removed from meta before it is stored.
func desktopPrepareUserTurn(botID, content string, meta map[string]string) string {
	turn := desktopBotTurnOf(botID)
	if turn == nil {
		return content
	}
	content = strings.TrimSpace(content)
	carried := desktopTakeMetaSecrets(meta)
	redacted, found := desktopbot.CaptureInlineSecrets(content)
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if turn.secrets == nil {
		turn.secrets = map[string]string{}
	}
	for name, value := range carried {
		turn.secrets[name] = value
	}
	for name, value := range found {
		turn.secrets[name] = value
	}
	// The answer to a stopped run is the secret the GUI saved and carried
	// with this message (bot_secret_name/value metadata). That structured
	// value, not the wording of the message, resumes the original goal.
	if turn.waiting && turn.goal != "" && len(carried) > 0 {
		names := make([]string, 0, len(carried))
		for name := range carried {
			names = append(names, name)
		}
		sort.Strings(names)
		turn.waiting = false
		return desktopResumeText(turn.goal, names[0], redacted)
	}
	turn.goal = redacted
	return redacted
}

// desktopTakeMetaSecrets removes every secret value from message metadata
// (the GUI's staged secret and Hub's inline captures) and returns them.
func desktopTakeMetaSecrets(meta map[string]string) map[string]string {
	out := map[string]string{}
	if meta == nil {
		return out
	}
	name := strings.TrimSpace(meta["bot_secret_name"])
	value := strings.TrimSpace(meta["bot_secret_value"])
	delete(meta, "bot_secret_value")
	if desktopbot.ValidSecretName(name) && value != "" {
		out[name] = value
	}
	for key, value := range meta {
		if !strings.HasPrefix(key, desktopbot.SecretMetaPrefix) {
			continue
		}
		delete(meta, key)
		name := strings.TrimPrefix(key, desktopbot.SecretMetaPrefix)
		if value = strings.TrimSpace(value); desktopbot.ValidSecretName(name) && value != "" {
			out[name] = value
		}
	}
	return out
}

func desktopResumeText(goal, name, said string) string {
	text := "继续原来的任务，不要另起一件。\n原来的任务：\n" + goal +
		"\n用户已经把 " + name + " 保存在这台电脑上。它在环境变量 " + name +
		" 里。用 $" + name + "，不要打印它的值，不要在磁盘或其他机器人的目录里寻找它，也不要再向用户索要。"
	if said = strings.TrimSpace(said); said != "" {
		text += "\n用户这次说：" + said
	}
	return text
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

// The roots that hold one folder per bot, in every spelling a shell sees.
var (
	desktopForeignTreeRe = regexp.MustCompile(`(?:/home/desktop|~|\$HOME|\$\{HOME\})/(?:work|素材)/([^/\s;&|'"\\)]+)`)
	desktopForeignRootRe = regexp.MustCompile(`(?:/home/desktop|~|\$HOME|\$\{HOME\})/(?:work|素材)/?(?:$|[\s;&|'"\\)*])`)
)

// desktopBashConfined rejects a command that lists another bot's project
// tree, the root that holds every bot's tree, or the shared git credential
// file. This bot's own folders are allowed.
func desktopBashConfined(scope agentruntime.Scope, command string) error {
	_, bot := desktop.SplitDesktopUser(scope.UserID)
	if bot == "" {
		bot = desktopBotOfInstance(scope.InstanceID)
	}
	if bot == "" {
		return nil
	}
	if strings.Contains(command, ".git-credentials") {
		return errDesktopBashConfined
	}
	safe := desktopWorkdirSafe.ReplaceAllString(bot, "_")
	for _, m := range desktopForeignTreeRe.FindAllStringSubmatch(command, -1) {
		if seg := m[1]; seg != bot && seg != safe {
			return errDesktopBashConfined
		}
	}
	if desktopForeignRootRe.MatchString(command) {
		return errDesktopBashConfined
	}
	return nil
}

var errDesktopBashConfined = errors.New("this bot can only read its own project folder")

// A turn ends by declaring one outcome. The person sees Text. Note is the
// model's own compliance remark and is never copied into the sentence.
type desktopFinish struct {
	Outcome string
	Text    string
	Note    string
}

var desktopFinishes sync.Map // run id -> desktopFinish

func desktopFinishTurn(scope agentruntime.Scope, args map[string]any) (string, error) {
	outcome := strings.ToLower(strings.TrimSpace(desktopArg(args, "outcome")))
	switch outcome {
	case "done", "ask_user", "failed":
	default:
		return "", fmt.Errorf("finish requires outcome done, ask_user, or failed")
	}
	runID := strings.TrimSpace(scope.RunID)
	if runID == "" {
		return "", fmt.Errorf("finish requires a run")
	}
	desktopFinishes.Store(runID, desktopFinish{
		Outcome: outcome,
		Text:    strings.TrimSpace(desktopArg(args, "text")),
		Note:    strings.TrimSpace(desktopArg(args, "note")),
	})
	return "finish outcome=" + outcome, nil
}

func desktopDeclaredOutcome(runID string) string {
	v, ok := desktopFinishes.Load(strings.TrimSpace(runID))
	if !ok {
		return ""
	}
	return v.(desktopFinish).Outcome
}

// desktopFinishSentence is the user-facing sentence of a declared finish.
// The note is not part of it. ok is false when this run did not finish.
func desktopFinishSentence(runID string) (string, bool) {
	v, ok := desktopFinishes.Load(strings.TrimSpace(runID))
	if !ok {
		return "", false
	}
	return v.(desktopFinish).Text, true
}
