package desktopbot

import (
	"errors"
	"strings"
	"sync"
)

// WakeSystemFault is the tool result when the computer could not be woken.
// It is a system fault. It does not ask the person to start or confirm anything.
const WakeSystemFault = "系统故障：电脑没能唤醒，这一步没做成。这不是需要这个人处理的事。已完成的写作和资料不受影响。"

const (
	// CodeDesktopAsleep is desktopd's 409 when a call would docker-exec a
	// stopped desktop and no workspace is available.
	CodeDesktopAsleep = "DESKTOP_ASLEEP"
	// CodeDesktopWaking is Hub's 503 when a warm start is still unfinished.
	CodeDesktopWaking = "DESKTOP_WAKING"
	// CodeBotScreenClosed is desktopd's 400 when an isolated bot's own screen
	// is not open yet. The container may already be running. It is not an
	// outage: open the screen and retry the same step once.
	CodeBotScreenClosed = "BOT_SCREEN_CLOSED"
)

// AsleepError is a coded refusal to exec a stopped desktop container.
type AsleepError struct{}

func (AsleepError) Error() string { return "desktop_asleep" }
func (AsleepError) Code() string  { return CodeDesktopAsleep }

// WakingError is a warm start that has not finished. It stays typed so a
// caller can wait and retry instead of treating it as a rejected request.
type WakingError struct{}

func (WakingError) Error() string    { return "桌面正在唤醒，再试一次。" }
func (WakingError) Retryable() bool  { return true }
func (WakingError) UserBubble() bool { return false }
func (WakingError) Code() string     { return CodeDesktopWaking }

// IsWaking reports a warm start that should be waited out.
func IsWaking(err error) bool {
	var waking WakingError
	return errors.As(err, &waking)
}

// IsBotScreenClosed reports desktopd's coded 400, or the sentence an older
// desktopd returns before it learned the code.
func IsBotScreenClosed(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, CodeBotScreenClosed) || strings.Contains(s, "desktop is not open yet")
}

// IsDesktopAsleep reports a coded asleep error, or the narrow docker message
// an older desktopd returns when exec hits a stopped container. "docker
// daemon is not running" is not that message.
func IsDesktopAsleep(err error) bool {
	if err == nil {
		return false
	}
	var asleep AsleepError
	if errors.As(err, &asleep) {
		return true
	}
	s := err.Error()
	if strings.Contains(s, CodeDesktopAsleep) {
		return true
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "docker daemon") || strings.Contains(lower, "cannot connect to the docker daemon") {
		return false
	}
	return strings.Contains(lower, "container") && strings.Contains(lower, "is not running")
}

// AsWakeFault turns a coded asleep or waking failure into WakeSystemFault
// after the caller has finished waiting. Any other error is unchanged.
func AsWakeFault(err error) error {
	if err == nil || (!IsDesktopAsleep(err) && !IsWaking(err)) {
		return err
	}
	if strings.Contains(err.Error(), "系统故障") {
		return err
	}
	return errors.New(WakeSystemFault)
}

// TurnPhaseInput is what the server already decided. There is no keyword
// list here. An empty intake phase means execute. Button is the arrange/run
// control. AskID and AskRun match one plan-phase ask_user, not the next
// message of the bot.
type TurnPhaseInput struct {
	Button bool
	Intake string
	AskID  string
	AskRun string
	Tenant string
	User   string
	Bot    string
}

// ApplyTurnPhase chooses plan or execute from the intake and from one
// matching ask. The client's phase is not an input.
func ApplyTurnPhase(in TurnPhaseInput) string {
	if in.Button {
		return "execute"
	}
	if ConsumePlanAsk(in.Tenant, in.User, in.Bot, in.AskID, in.AskRun) {
		return "execute"
	}
	if strings.TrimSpace(in.Intake) == "plan" {
		return "plan"
	}
	return "execute"
}

type planAsk struct {
	id  string
	run string
}

// planAsks is one pending plan-phase question, keyed by bot. The next
// message runs in execute only when it names this ask id or this run id.
var planAsks sync.Map

func planAskKey(tenant, user, bot string) string {
	bot = strings.TrimSpace(bot)
	if bot == "" {
		return ""
	}
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(user) + "\x00" + bot
}

// NotePlanAsk records the ask_user that a plan turn is waiting on.
// A later message matches only this id or this run id.
func NotePlanAsk(tenant, user, bot, askID, runID string) {
	key := planAskKey(tenant, user, bot)
	askID, runID = strings.TrimSpace(askID), strings.TrimSpace(runID)
	if key == "" || (askID == "" && runID == "") {
		return
	}
	planAsks.Store(key, planAsk{id: askID, run: runID})
}

// ConsumePlanAsk reports that this message is the answer to the pending
// ask, and forgets that ask. A different message leaves the ask in place.
func ConsumePlanAsk(tenant, user, bot, askID, runID string) bool {
	key := planAskKey(tenant, user, bot)
	if key == "" {
		return false
	}
	value, ok := planAsks.Load(key)
	if !ok {
		return false
	}
	got, _ := value.(planAsk)
	askID, runID = strings.TrimSpace(askID), strings.TrimSpace(runID)
	match := (askID != "" && askID == got.id) || (runID != "" && runID == got.run)
	if !match {
		return false
	}
	planAsks.Delete(key)
	return true
}

// ResetPhaseMemory drops pending asks. Tests call it.
func ResetPhaseMemory() {
	planAsks.Range(func(key, _ any) bool {
		planAsks.Delete(key)
		return true
	})
}
