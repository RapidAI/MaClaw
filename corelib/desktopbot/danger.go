package desktopbot

import (
	"os"
	"strings"
	"sync"
	"time"
)

// DangerWait is how long a held command waits for the person's answer
// (the 确认 / 拒绝 buttons under the ask). 15 s was too short to click.
const DangerWait = 10 * time.Minute

// DangerOptions are the two buttons guiapp shows under a danger ask.
var DangerOptions = []string{"确认", "拒绝"}

const DangerDeclinedError = "这个人没同意，这一条不执行。不要再问，也不要换个办法做同一件事；跳过它，用别的方式把任务做完，做不了就直接说哪一步没做。"

const DangerAskedError = "这一条要先问这个人。在 send_to_user 里用你自己的口吻说清楚要做什么（不要写出密码、token 等秘密）、为什么需要它，然后停下等回答。没有回答就不执行。不要改用别的动作代替。"

const DangerSilenceError = "15 秒没有回答，这一次不执行。不要再自动问。"

// DangerCall is one shell or install the model wants to run.
// Helper is the computer helper. It uses the same gate and does not get a pass.
type DangerCall struct {
	Action  string
	Command string
	Helper  bool
}

// DangerInput is one look at a call. VerdictOK false means the other model
// timed out or the verdict could not be read. That is not a pass.
// A user rule cannot allow the always-ask list.
type DangerInput struct {
	Call      DangerCall
	Verdict   string
	VerdictOK bool
	Key       string
	Now       time.Time
}

// DangerResult is the gate. Run false means the command does not execute.
// Ask is set only the first time this key asks. Error is what the model reads.
type DangerResult struct {
	Run   bool
	Ask   ConnectorAsk
	Error string
}

type dangerHold struct {
	at     time.Time
	answer int // 0 waiting, 1 approved, -1 declined
}

var (
	dangerHolds sync.Map
	dangerAsks  sync.Map
	dangerNowFn = time.Now
)

// DangerNow is the clock the desktop gate uses. Tests replace it.
func DangerNow() time.Time {
	if dangerNowFn == nil {
		return time.Now()
	}
	return dangerNowFn()
}

// SetDangerNow replaces the gate clock. Pass nil to restore time.Now.
func SetDangerNow(now func() time.Time) {
	if now == nil {
		dangerNowFn = time.Now
		return
	}
	dangerNowFn = now
}

// ResetDangerState drops holds and asks and restores the clock.
func ResetDangerState() {
	dangerHolds = sync.Map{}
	dangerAsks = sync.Map{}
	dangerNowFn = time.Now
}

// GateDanger decides one call. git push, docker compose up, install, and
// DROP DATABASE / DROP TABLE ask even when the verdict is allow.
// A readonly command such as ls runs when the verdict allows, or when no
// verdict could be read. A listed danger with no pass does not run.
// An ordinary command runs when no verdict could be read, so a missing
// model does not stop echo. A block verdict does not run.
func GateDanger(in DangerInput) DangerResult {
	call := in.Call
	label, always, listed, readonly := classifyDanger(call)
	// A package install lands in this bot's own isolated desktop container,
	// so it runs without a question unless the review blocks it or
	// MACLAW_DESKTOP_INSTALL_ASK=on asks for the old behaviour.
	if label == "install" && !installAsks() {
		if in.VerdictOK && strings.EqualFold(strings.TrimSpace(in.Verdict), "block") {
			return holdDanger(in, dangerLabel(label, call))
		}
		return DangerResult{Run: true}
	}
	// rm -rf of scratch paths inside the bot's own container (/tmp, a work
	// dir, a subfolder of home, a relative path below the cwd) is routine
	// cleanup, e.g. re-cloning a repo: Bot 5 asked 「我需要执行「rm -rf」才能
	// 继续，可以吗？」 in the middle of a post (10-31). A block still holds it.
	if label == "rm -rf" && rmScratchOnly(call.Command) {
		if in.VerdictOK && strings.EqualFold(strings.TrimSpace(in.Verdict), "block") {
			return holdDanger(in, dangerLabel(label, call))
		}
		return DangerResult{Run: true}
	}
	if always || mustAsk(in, listed, readonly) {
		return holdDanger(in, dangerLabel(label, call))
	}
	return DangerResult{Run: true}
}

func installAsks() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_INSTALL_ASK")), "on")
}

// dangerLabel names what would run in the fallback question. Only an install
// shows its package names; any other command is named by its kind so a secret
// in the command line never reaches the chat.
func dangerLabel(label string, call DangerCall) string {
	if label == "rm -rf" {
		if targets := rmTargets(call.Command); len(targets) > 0 {
			return "rm:" + strings.Join(targets, "、")
		}
		return label
	}
	if label != "install" {
		return label
	}
	command := strings.Join(strings.Fields(call.Command), " ")
	if command == "" {
		return label
	}
	if runes := []rune(command); len(runes) > 120 {
		command = string(runes[:120]) + "…"
	}
	return command
}

func mustAsk(in DangerInput, listed, readonly bool) bool {
	if in.VerdictOK && strings.EqualFold(strings.TrimSpace(in.Verdict), "allow") {
		return false
	}
	if !in.VerdictOK && readonly {
		return false
	}
	if !in.VerdictOK && !listed {
		return false
	}
	return true
}

func holdDanger(in DangerInput, label string) DangerResult {
	now := in.Now
	if now.IsZero() {
		now = DangerNow()
	}
	key := in.Key
	if key == "" {
		key = label
	}
	ask := ConnectorAsk{Kind: "ask", Text: dangerAskText(label)}
	if value, ok := dangerHolds.Load(key); ok {
		held, _ := value.(dangerHold)
		switch held.answer {
		case 1:
			dangerHolds.Delete(key) // one-shot: this run of this command
			return DangerResult{Run: true}
		case -1:
			return DangerResult{Error: DangerDeclinedError}
		}
		if !now.Before(held.at.Add(DangerWait)) {
			return DangerResult{Error: DangerSilenceError}
		}
		return DangerResult{Error: DangerAskedError}
	}
	dangerHolds.Store(key, dangerHold{at: now})
	return DangerResult{Ask: ask, Error: DangerAskedError}
}

func dangerAskText(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "这一条命令"
	}
	if paths, ok := strings.CutPrefix(label, "rm:"); ok {
		return "我想把 " + paths + " 整个删掉再继续，可以吗？"
	}
	if label == "这一条命令" {
		return "我想在桌面上跑一条命令，可能会改动东西，先问你一下，可以吗？"
	}
	return "我想执行「" + label + "」再继续，可以吗？"
}

// rmTargets are the paths of every rm -rf in command, a secret-looking one
// (key=value, a long token) shortened so it never reaches the chat.
func rmTargets(cmd string) []string {
	var out []string
	for _, seg := range splitShell(cmd) {
		if !rmRecursiveForce(seg) {
			continue
		}
		fields := strings.Fields(seg)
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				continue
			}
			out = append(out, redactPath(strings.Trim(f, `"'`)))
		}
	}
	return out
}

func redactPath(p string) string {
	if strings.Contains(p, "=") {
		return "…"
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if len(part) >= 24 && !strings.ContainsAny(part, ".-_ ") {
			parts[i] = "…"
		}
	}
	return strings.Join(parts, "/")
}

// rmScratchOnly: every rm -rf target is a scratch path in the container.
func rmScratchOnly(cmd string) bool {
	any := false
	for _, seg := range splitShell(cmd) {
		if !rmRecursiveForce(seg) {
			continue
		}
		fields := strings.Fields(seg)
		targets := 0
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "-") {
				continue
			}
			targets++
			if !rmScratchPath(strings.Trim(f, `"'`)) {
				return false
			}
		}
		if targets == 0 {
			return false
		}
		any = true
	}
	return any
}

var rmScratchRoots = []string{"/tmp/", "/var/tmp/", "/root/work/", "/root/tmp/", "/workspace/", "~/", "$HOME/", "/root/"}

var rmSystemUnderRoot = []string{".ssh", ".config", ".local", ".cache/chromium", ".mozilla", ".gnupg", "Desktop", "素材", "chrome", "browser"}

func rmScratchPath(p string) bool {
	if p == "" || strings.Contains(p, "..") || strings.Contains(p, "`") {
		return false
	}
	if strings.Contains(p, "$") && !strings.HasPrefix(p, "$HOME/") {
		return false
	}
	clean := strings.TrimRight(p, "/")
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~") && !strings.HasPrefix(p, "$") {
		// relative to the cwd: a named subfolder, not . or *
		return clean != "" && clean != "." && !strings.HasPrefix(clean, "*")
	}
	for _, root := range rmScratchRoots {
		if !strings.HasPrefix(clean+"/", root) || len(clean)+1 <= len(root) {
			continue
		}
		rest := strings.TrimPrefix(clean+"/", root)
		if strings.HasPrefix(rest, "*") {
			return false
		}
		if root == "~/" || root == "$HOME/" || root == "/root/" {
			for _, keep := range rmSystemUnderRoot {
				if strings.HasPrefix(rest, keep+"/") || strings.HasPrefix(rest, keep) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// AnswerDanger records the person's 确认 (yes) or 拒绝 for every held command
// whose key starts with prefix and still waits. It reports how many it
// answered.
func AnswerDanger(prefix string, yes bool, now time.Time) int {
	if now.IsZero() {
		now = DangerNow()
	}
	n := 0
	dangerHolds.Range(func(k, v any) bool {
		key, _ := k.(string)
		held, _ := v.(dangerHold)
		if strings.HasPrefix(key, prefix) && held.answer == 0 && now.Before(held.at.Add(DangerWait)) {
			held.answer = -1
			if yes {
				held.answer = 1
			}
			dangerHolds.Store(key, held)
			n++
		}
		return true
	})
	return n
}

// DangerAnswer reads a short chat reply as yes/no to a held ask.
func DangerAnswer(text string) (yes, ok bool) {
	t := strings.ToLower(strings.Trim(strings.TrimSpace(text), "。.!！~ "))
	switch t {
	case "确认", "確認", "可以", "同意", "好", "好的", "行", "执行", "继续", "yes", "y", "ok", "okay", "approve":
		return true, true
	case "拒绝", "拒絕", "不行", "不要", "不可以", "不同意", "取消", "别", "no", "n", "deny", "decline":
		return false, true
	}
	return false, false
}

// NoteDangerAsk stores the first ask for each key. A later ask does not replace it.
func NoteDangerAsk(keys []string, ask ConnectorAsk) {
	if strings.TrimSpace(ask.Text) == "" {
		return
	}
	if ask.Kind == "" {
		ask.Kind = "ask"
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dangerAsks.LoadOrStore(key, ask)
	}
}

// PeekDangerAsk returns the first stored ask among keys.
func PeekDangerAsk(keys ...string) (ConnectorAsk, bool) {
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, ok := dangerAsks.Load(key)
		if !ok {
			continue
		}
		ask, _ := value.(ConnectorAsk)
		if strings.TrimSpace(ask.Text) != "" {
			return ask, true
		}
	}
	return ConnectorAsk{}, false
}

// ApplyDangerAsk copies the first ask onto send_to_user when that field is empty.
func ApplyDangerAsk(metadata map[string]string, keys ...string) {
	if metadata == nil || strings.TrimSpace(metadata["send_to_user"]) != "" {
		return
	}
	ask, ok := PeekDangerAsk(keys...)
	if !ok {
		return
	}
	metadata["send_to_user"] = ask.Text
	kind := ask.Kind
	if kind == "" {
		kind = "ask"
	}
	metadata["send_to_user_kind"] = kind
	ApplyConfirmButtons(metadata)
}

// ApplyConfirmButtons puts 确认 / 拒绝 under a yes/no ask that has no
// choices of its own (guiapp renders ask_user_options_json as buttons).
func ApplyConfirmButtons(metadata map[string]string) {
	if metadata == nil || strings.TrimSpace(metadata["send_to_user_kind"]) != "ask" || strings.TrimSpace(metadata["ask_user_options_json"]) != "" {
		return
	}
	if strings.TrimSpace(metadata["ask_user_question"]) == "" {
		metadata["ask_user_question"] = metadata["send_to_user"]
	}
	metadata["ask_user_options_json"] = `["确认","拒绝"]`
}

func classifyDanger(call DangerCall) (label string, always, listed, readonly bool) {
	action := strings.ToLower(strings.TrimSpace(call.Action))
	command := call.Command
	switch {
	case action == "install":
		return "install", true, true, false
	case sqlDrop(command):
		return "DROP", true, true, false
	case hasGitPush(command):
		return "git push", true, true, false
	case hasComposeUp(command):
		return "docker compose up", true, true, false
	case rmRecursiveForce(command):
		return "rm -rf", false, true, false
	case pipeToShell(command):
		if strings.Contains(strings.ToLower(command), "wget") && !strings.Contains(strings.ToLower(command), "curl") {
			return "wget|sh", false, true, false
		}
		return "curl|sh", false, true, false
	case npmInstall(command) || chmodExec(command):
		return "这一条命令", false, true, false
	case readonlyShell(command):
		return "ls", false, false, true
	default:
		return "这一条命令", false, false, false
	}
}

func readonlyShell(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "|&;<>`$()") || strings.Contains(command, "\n") {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	switch baseName(fields[0]) {
	case "ls", "pwd", "cat", "head", "tail", "wc", "stat", "identify":
		return true
	default:
		return false
	}
}

func sqlDrop(command string) bool {
	folded := strings.Join(strings.Fields(strings.ToLower(command)), " ")
	return strings.Contains(folded, "drop database") || strings.Contains(folded, "drop table")
}

func hasGitPush(command string) bool {
	for _, seg := range splitShell(command) {
		seenGit := false
		for _, field := range strings.Fields(seg) {
			base := baseName(strings.ToLower(field))
			if base == "git" {
				seenGit = true
				continue
			}
			if seenGit && base == "push" {
				return true
			}
		}
	}
	return false
}

func hasComposeUp(command string) bool {
	for _, seg := range splitShell(command) {
		fields := strings.Fields(strings.ToLower(seg))
		for i := 0; i < len(fields); i++ {
			base := baseName(fields[i])
			if base == "docker-compose" && i+1 < len(fields) && baseName(fields[i+1]) == "up" {
				return true
			}
			if base == "docker" && i+2 < len(fields) && baseName(fields[i+1]) == "compose" && baseName(fields[i+2]) == "up" {
				return true
			}
		}
	}
	return false
}

func rmRecursiveForce(command string) bool {
	for _, seg := range splitShell(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 || baseName(strings.ToLower(fields[0])) != "rm" {
			continue
		}
		recursive, force := false, false
		for _, field := range fields[1:] {
			lower := strings.ToLower(field)
			if lower == "--recursive" {
				recursive = true
			}
			if lower == "--force" {
				force = true
			}
			if strings.HasPrefix(field, "-") && !strings.HasPrefix(field, "--") {
				if strings.ContainsAny(field, "rR") {
					recursive = true
				}
				if strings.Contains(strings.ToLower(field), "f") {
					force = true
				}
			}
		}
		if recursive && force {
			return true
		}
	}
	return false
}

func pipeToShell(command string) bool {
	parts := strings.Split(command, "|")
	if len(parts) < 2 {
		return false
	}
	head := strings.ToLower(parts[0])
	if !strings.Contains(head, "curl") && !strings.Contains(head, "wget") {
		return false
	}
	tail := strings.Fields(parts[len(parts)-1])
	if len(tail) == 0 {
		return false
	}
	switch baseName(strings.ToLower(tail[0])) {
	case "sh", "bash", "zsh":
		return true
	default:
		return false
	}
}

func npmInstall(command string) bool {
	for _, seg := range splitShell(command) {
		fields := strings.Fields(strings.ToLower(seg))
		for i := 0; i < len(fields); i++ {
			if baseName(fields[i]) == "npm" && i+1 < len(fields) && fields[i+1] == "install" {
				return true
			}
		}
	}
	return false
}

func chmodExec(command string) bool {
	for _, seg := range splitShell(command) {
		fields := strings.Fields(seg)
		if len(fields) == 0 || baseName(strings.ToLower(fields[0])) != "chmod" {
			continue
		}
		for _, field := range fields[1:] {
			if strings.Contains(field, "+x") {
				return true
			}
		}
	}
	return false
}

func splitShell(command string) []string {
	raw := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n", "&", "\n").Replace(command)
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func baseName(path string) string {
	path = strings.TrimSpace(path)
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		path = path[i+1:]
	}
	return path
}
