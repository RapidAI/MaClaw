package desktopbot

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// WakeSystemFault is the tool result when the computer could not be woken.
// It is a system fault. It does not ask the person to start or confirm anything.
const WakeSystemFault = "系统故障：电脑没能唤醒，这一步没做成。这不是需要这个人处理的事。已完成的写作和资料不受影响。"

// phaseMemoryTTL matches the continuation window the desktop turn already uses.
const phaseMemoryTTL = 3 * time.Hour

// sameTurnWindow collapses repeated reads of one turn (prompt, then tools)
// onto the phase already chosen for that message.
const sameTurnWindow = 2 * time.Minute

// needsPlanWord is the irreversible-order test. Publishing is not in it:
// a post has its own confirmation gate and an ordinary publish starts in execute.
var needsPlanWord = regexp.MustCompile(`(?i)(^|[^a-z])(install|uninstall|delete|remove|wipe|format|rm -rf|drop table|pay|purchase|checkout|transfer money)([^a-z]|$)`)

var handBackPhaseRe = regexp.MustCompile(`桌面交还给你|请继续刚才的任务|hand(ed)? (the desktop )?back`)

// NeedsPlan reports an order the server keeps in plan until the person
// confirms it with an execute action. Fetching, writing, and publishing
// are not in this set.
func NeedsPlan(text string) bool {
	for _, w := range []string{"安装", "卸载", "删除", "刪除", "付款", "支付", "清空", "格式化", "转账"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return needsPlanWord.MatchString(text)
}

// DecidePhase is the stateless half of the phase choice.
// hint is the client's bot_phase (plan, execute, or empty). An explicit
// execute is a button that already confirmed the work. A plan hint from an
// older GUI is only a hint: an ordinary order still executes. askReply is
// the person's answer to an ask_user from a plan turn; that answer runs.
func DecidePhase(hint, content string, askReply bool) string {
	hint = strings.TrimSpace(hint)
	content = strings.TrimSpace(content)
	if askReply || hint == "execute" {
		return "execute"
	}
	if content != "" && NeedsPlan(content) {
		return "plan"
	}
	if hint == "plan" {
		if content == "" {
			return "plan"
		}
		return "execute"
	}
	return ""
}

type phaseMemory struct {
	phase   string
	phaseAt time.Time
	sig     string
	ask     bool
}

var (
	phaseMu  sync.Mutex
	phaseMem = map[string]*phaseMemory{}
)

// PlanAskKey is the conversation a plan-phase ask_user belongs to.
// An empty bot id cannot be remembered.
func PlanAskKey(tenant, user, bot string) string {
	bot = strings.TrimSpace(bot)
	if bot == "" {
		return ""
	}
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(user) + "\x00" + bot
}

// NotePlanAsk marks that this bot's plan turn asked the person something.
// The next message in that conversation runs in execute.
func NotePlanAsk(tenant, user, bot string) {
	key := PlanAskKey(tenant, user, bot)
	if key == "" {
		return
	}
	phaseMu.Lock()
	defer phaseMu.Unlock()
	mem := phaseMem[key]
	if mem == nil {
		mem = &phaseMemory{}
		phaseMem[key] = mem
	}
	mem.ask = true
}

// ResetPhaseMemory drops continuation and ask-reply state. Tests call it.
func ResetPhaseMemory() {
	phaseMu.Lock()
	phaseMem = map[string]*phaseMemory{}
	phaseMu.Unlock()
}

// ResolvePhase chooses plan or execute for one bot turn and remembers it.
// A repeated call with the same hint and content inside one turn returns
// the phase already chosen. An empty bot id is decided without memory.
func ResolvePhase(tenant, user, bot, hint, content string) string {
	hint = strings.TrimSpace(hint)
	content = strings.TrimSpace(content)
	key := PlanAskKey(tenant, user, bot)
	if key == "" {
		if phase := DecidePhase(hint, content, false); phase != "" {
			return phase
		}
		if handBackPhaseRe.MatchString(content) {
			return "execute"
		}
		return "execute"
	}
	now := time.Now()
	phaseMu.Lock()
	defer phaseMu.Unlock()
	mem := phaseMem[key]
	if mem == nil {
		mem = &phaseMemory{}
		phaseMem[key] = mem
	}
	sig := hint + "\x00" + content
	if !mem.ask && mem.sig == sig && (mem.phase == "plan" || mem.phase == "execute") && now.Sub(mem.phaseAt) < sameTurnWindow {
		return mem.phase
	}
	ask := mem.ask
	mem.ask = false
	decided := decideRemembered(hint, content, ask, mem, now)
	mem.phase = decided
	mem.phaseAt = now
	mem.sig = sig
	return decided
}

func decideRemembered(hint, content string, ask bool, mem *phaseMemory, now time.Time) string {
	if phase := DecidePhase(hint, content, ask); phase != "" {
		return phase
	}
	// No client phase. Inherit a turn already under way, unless this message
	// is the person handing the desktop back.
	if mem.phase == "execute" && now.Sub(mem.phaseAt) < phaseMemoryTTL {
		return "execute"
	}
	if handBackPhaseRe.MatchString(content) {
		return "execute"
	}
	if mem.phase == "plan" && now.Sub(mem.phaseAt) < phaseMemoryTTL {
		return "plan"
	}
	return "execute"
}

// IsDesktopAsleep reports an error from a desktop container that was not
// running, or from a wake that has not finished. Callers wake and retry
// once; they do not show this text to the model as a question.
func IsDesktopAsleep(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, part := range []string{
		"desktop_asleep",
		"DESKTOP_ASLEEP",
		"容器未运行",
		"is not running",
		"not running",
		"桌面在休眠",
		"桌面正在唤醒",
	} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// AsWakeFault turns a power or wake failure into WakeSystemFault.
// Any other error is returned unchanged. A nil error stays nil.
func AsWakeFault(err error) error {
	if err == nil || !IsDesktopAsleep(err) {
		return err
	}
	if strings.Contains(err.Error(), "系统故障") {
		return err
	}
	return errors.New(WakeSystemFault)
}
