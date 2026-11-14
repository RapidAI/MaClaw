package desktopbot

import "strings"

// WorthSayingPrompt is the stable system text for when a desktop bot may
// speak. It is pinned with the system segment for the whole conversation.
// The routine "no change, stop" line stays on that turn's last message.
const WorthSayingPrompt = "只在两种情况下主动调用 send_to_user：需要这个人做决定，或者整件事做完。\n" +
	"其它进度只写入 PROGRESS.md。不要为单个工具步骤发消息。\n" +
	"每一轮结束前先问：这值得告诉这个人吗？值得，就 send_to_user 到该去的那个表面。不值得，就什么都不发。"

// RoutineQuietPrompt is the last message of a schedule, background, or event
// turn. It is not part of the pinned system segment.
const RoutineQuietPrompt = "这一轮是例行检查，没有人在等回复。有变化、失败、或需要这个人做决定，就调用 send_to_user，channel 指明该去的那个表面。没有变化就结束，不要调用 send_to_user，也不要写「无变化」「no change」或「(无输出)」。"

// SteerOnIt is the short acknowledgement when a person changes work that is
// already going. English is SteerOnItEN. The sentence does not restate the plan.
const (
	SteerOnIt   = "在办"
	SteerOnItEN = "On it"
)

// SteerAck returns the direction-change sentence in the person's language.
func SteerAck(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		return SteerOnItEN
	}
	return SteerOnIt
}

// NotContent reports a body that only occupies a message. Quiet is no outbound
// record. These three phrases are the ones the routine turn is told not to write.
func NotContent(text string) bool {
	switch strings.TrimSpace(text) {
	case "无变化", "(无输出)", "（无输出）":
		return true
	default:
		return strings.EqualFold(strings.TrimSpace(text), "no change")
	}
}

// RecurringWorkPrompt tells the bot to register work that should happen
// again. The judgment is the bot's, from the work and the conversation.
// It is pinned with the system segment for the whole conversation.
const RecurringWorkPrompt = "一件事若要反复做，调用 bot_schedule 登记成定时任务。一次就能做完的事不要登记。\n" +
	"判断来自当前这件事和已经说过的话，不限于这句话写了周期。搜索、查看、汇报、清理，以及其它要隔一段时间再做的事，都一样。\n" +
	"name 是这条定时任务的稳定名字。同一个名字是同一条，instruction 写成以后每一轮要做的整件事。另一件反复的事用另一个名字。\n" +
	"周期写 interval_minutes。钟点要同时写 hour 和 minute。没写的时间沿用这条任务已有的值。新建时必须给出周期，或同时给出 hour 和 minute。对方没说隔多久，就按这件事该隔多久选一个，并在回复里说明选了多久。\n" +
	"登记之后，这一轮对方要你现在做的仍然做。定时任务负责以后的轮次。到点的那一轮不要再次登记同一条，除非工作或周期变了。取消用 action=cancel 并给出 name。列出用 action=list。"

// PinWorthSaying appends the stable interruption rule once.
func PinWorthSaying(prompt string) string {
	if strings.Contains(prompt, WorthSayingPrompt) {
		return prompt
	}
	prompt = strings.TrimRight(prompt, "\n")
	if prompt == "" {
		return WorthSayingPrompt
	}
	return prompt + "\n\n" + WorthSayingPrompt
}

// PinRecurringWork appends the timer rule once.
func PinRecurringWork(prompt string) string {
	if strings.Contains(prompt, RecurringWorkPrompt) {
		return prompt
	}
	prompt = strings.TrimRight(prompt, "\n")
	if prompt == "" {
		return RecurringWorkPrompt
	}
	return prompt + "\n\n" + RecurringWorkPrompt
}

// SpeakInput is one finished round, before any surface is chosen.
type SpeakInput struct {
	Origin       string
	Channel      string
	Surface      string
	Waiting      bool
	Step         bool
	Done         bool
	Changed      bool
	Ask          bool
	AskText      string
	Failed       bool
	FailureText  string
	ProgressAsk  bool
	ProgressFile string
	ProgressCopy Sections
	Awake        bool
	Steer        bool
	Lang         string
	Prose        string
	SendToUser   string
	Handoff      bool
	WhereWeAre   string
}

// SpeakRecord is one outbound sentence. Channel is desktop, mobile, or email.
type SpeakRecord struct {
	Channel string
	Text    string
	Kind    string
}

// SpeakDecision is what this round may send. Report is a desktop-bot-report.
// Phone is a mobile notification. A quiet end clears the work row.
type SpeakDecision struct {
	Records        []SpeakRecord
	Report         bool
	Phone          bool
	UpdateProgress bool
	ClearWorkRow   bool
	WorkRow        string
	QuietSuccess   bool
	MissedReply    bool
}

// Speak decides the outbound records for one round.
// A tool step, an unchanged routine, and a filler body produce none.
func Speak(in SpeakInput) SpeakDecision {
	in.SendToUser = strings.TrimSpace(in.SendToUser)
	in.Prose = strings.TrimSpace(in.Prose)
	in.AskText = strings.TrimSpace(in.AskText)
	in.FailureText = strings.TrimSpace(in.FailureText)
	if NotContent(in.SendToUser) {
		in.SendToUser = ""
	}
	if NotContent(in.Prose) {
		in.Prose = ""
	}
	if NotContent(in.AskText) {
		in.AskText = ""
	}
	d := SpeakDecision{UpdateProgress: in.Step || routineOrigin(in.Origin)}
	if in.Step && !in.Done && !in.Ask && !in.Failed && !in.Steer && !in.ProgressAsk && in.SendToUser == "" {
		d.WorkRow = strings.TrimSpace(in.WhereWeAre)
		return d
	}
	if in.Steer {
		return oneRecord(d, "person", in, SteerAck(in.Lang), "final", true)
	}
	if in.ProgressAsk {
		text := strings.TrimSpace(ReadProgress(in.ProgressFile, in.ProgressCopy, in.Awake).Text)
		if text == "" {
			return d
		}
		out := oneRecord(d, "person", in, text, "reply", true)
		out.Report = false
		out.Phone = false
		return out
	}
	if in.Ask {
		text := in.AskText
		if text == "" {
			text = in.SendToUser
		}
		if text == "" {
			return d
		}
		return oneRecord(d, in.Origin, in, text, "ask", true)
	}
	if in.Failed {
		text := in.FailureText
		if text == "" {
			text = in.SendToUser
		}
		if text == "" {
			text = in.Prose
		}
		out := d
		ch := speakChannel(in.Origin, in.Channel, in.Surface)
		if text != "" {
			out.Records = []SpeakRecord{{Channel: ch, Text: text, Kind: "final"}}
		}
		out.Report = reportOrigin(in.Origin) && ch == "desktop"
		out.Phone = ch == "mobile"
		out.ClearWorkRow = true
		return out
	}
	if in.Handoff {
		ch := speakChannel(in.Origin, in.Channel, in.Surface)
		out := d
		out.Report = reportOrigin(in.Origin) && ch == "desktop"
		out.Phone = ch == "mobile"
		out.ClearWorkRow = true
		if in.SendToUser != "" {
			out.Records = []SpeakRecord{{Channel: ch, Text: in.SendToUser, Kind: "ask"}}
		}
		return out
	}
	sentence := in.SendToUser
	if sentence != "" && worthSentence(in) {
		kind := "final"
		if in.Origin == "person" && !in.Done {
			kind = "reply"
		}
		return oneRecord(d, in.Origin, in, sentence, kind, true)
	}
	d.ClearWorkRow = true
	d.QuietSuccess = !in.Waiting && !in.Handoff && !in.Ask && !in.Failed
	d.MissedReply = in.Waiting && sentence == "" && in.Prose == ""
	return d
}

func worthSentence(in SpeakInput) bool {
	if in.Origin == "person" || in.Origin == "" {
		return true
	}
	return in.Changed
}

func routineOrigin(origin string) bool {
	switch strings.TrimSpace(origin) {
	case "schedule", "event", "background":
		return true
	default:
		return false
	}
}

func reportOrigin(origin string) bool {
	switch strings.TrimSpace(origin) {
	case "schedule", "event":
		return true
	default:
		return false
	}
}

func oneRecord(d SpeakDecision, origin string, in SpeakInput, text, kind string, clear bool) SpeakDecision {
	ch := speakChannel(origin, in.Channel, in.Surface)
	d.Records = []SpeakRecord{{Channel: ch, Text: text, Kind: kind}}
	d.Report = reportOrigin(in.Origin) && ch == "desktop"
	d.Phone = ch == "mobile"
	d.ClearWorkRow = clear
	return d
}

func speakChannel(origin, channel, surface string) string {
	channel = strings.ToLower(strings.TrimSpace(channel))
	surface = strings.ToLower(strings.TrimSpace(surface))
	if origin == "person" {
		if surface == "" {
			return "desktop"
		}
		return surface
	}
	switch channel {
	case "mobile", "email", "desktop":
		return channel
	default:
		return "desktop"
	}
}

// SettleQuietInput is the desktop command body at the end of a run.
type SettleQuietInput struct {
	Waiting    bool
	Handoff    bool
	Ask        bool
	Failed     bool
	Text       string
	SendToUser string
}

// SettleQuietResult splits an empty body into a quiet success and a missed
// reply. A person who is waiting still needs an answer. Nobody waiting does not.
type SettleQuietResult struct {
	Quiet  bool
	Missed bool
}

// SettleQuiet applies the empty-body rule. A filler phrase such as 「无变化」
// is still a body: the caller keeps it on the run record and does not speak it.
// Only a body with nothing in it is quiet, or a missed reply when someone is waiting.
func SettleQuiet(in SettleQuietInput) SettleQuietResult {
	text := strings.TrimSpace(in.Text)
	send := strings.TrimSpace(in.SendToUser)
	if text != "" || send != "" || in.Handoff || in.Ask || in.Failed {
		return SettleQuietResult{}
	}
	if in.Waiting {
		return SettleQuietResult{Missed: true}
	}
	return SettleQuietResult{Quiet: true}
}
