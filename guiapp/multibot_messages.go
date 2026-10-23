package guiapp

// multibot_messages.go is the teammate → main bot direction. Remote desktop
// bots report through Hub (kind "report" cross notes, posted by MaClawSrv's
// desktop notify_main action or by any bot through POST /bots/{id}/cross);
// the watcher pulls them, and run transitions it already sees (finished,
// blocked, failed, file delivered) become reports too. Every message goes
// through one botregistry.Bus (dedupe, rate limit, ping-pong guard) and one
// Inbox: an idle main bot is woken with a synthetic turn ("multibot-wake",
// sent by the frontend into the main chat); a busy one gets them when its
// turn ends. The main bot answers with send_to_bot(reply_to=...).

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botregistry"
)

// multiBotWakeDebounce batches a burst (finished + file delivered) into one wake.
const multiBotWakeDebounce = 1500 * time.Millisecond

// multiBotNoteHub is the optional Hub surface for reports. appMultiBotHub
// implements it; a hub without it simply never yields reports.
type multiBotNoteHub interface {
	notes(ctx context.Context) ([]DesktopBotCrossNote, error)
	report(ctx context.Context, fromID, targetID, topic, text, replyTo string, artifacts []string) (DesktopBotCrossNote, error)
}

// MultiBotWake is the payload of the "multibot-wake" event.
type MultiBotWake struct {
	Prompt  string   `json:"prompt"`
	Display string   `json:"display"`
	IDs     []string `json:"ids"`
}

func isTeammateWake(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "[队友消息")
}

func (mb *multiBot) initMessages() {
	mb.bus = botregistry.NewBus(botregistry.BusLimits{})
	mb.inbox = botregistry.NewInbox(mb.queueWake)
	mb.seenNotes = map[string]bool{}
	mb.reg.Subscribe(mb.onRegistryEvent)
}

func (mb *multiBot) hubNotes() multiBotNoteHub {
	if mb == nil || mb.hub == nil {
		return nil
	}
	h, _ := mb.hub.(multiBotNoteHub)
	return h
}

// receive submits one message addressed to the main bot and delivers it.
func (mb *multiBot) receive(m botregistry.Message) (botregistry.Message, error) {
	if m.FromName == "" {
		if b, ok := mb.reg.Get(m.From); ok {
			m.FromName = b.Name
		}
	}
	m.To = multiBotSelfID
	stored, err := mb.bus.Submit(m)
	if err != nil {
		if !errors.Is(err, botregistry.ErrDuplicate) {
			log.Printf("[multibot] drop message from %s: %v", m.From, err)
		}
		return stored, err
	}
	if stored.Via != "watch" {
		mb.reg.Notify(botregistry.EventMessage, stored.From, string(stored.Kind)+": "+clipRunes(stored.Text, 200))
	}
	mb.inbox.Deliver(stored)
	return stored, nil
}

// queueWake is the Inbox's wake: it debounces, then emits one event.
func (mb *multiBot) queueWake(msgs []botregistry.Message) {
	mb.mu.Lock()
	mb.wakeBuf = append(mb.wakeBuf, msgs...)
	if mb.wakeTimer == nil {
		mb.wakeTimer = time.AfterFunc(multiBotWakeDebounce, mb.flushWake)
	}
	mb.mu.Unlock()
}

func (mb *multiBot) flushWake() {
	mb.mu.Lock()
	msgs := mb.wakeBuf
	mb.wakeBuf = nil
	mb.wakeTimer = nil
	mb.mu.Unlock()
	if len(msgs) == 0 || mb.emit == nil {
		return
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	mb.emit("multibot-wake", MultiBotWake{Prompt: botregistry.WakePrompt(msgs), Display: botregistry.WakeDisplay(msgs), IDs: ids})
}

// onRegistryEvent turns a teammate's run transition into a report.
func (mb *multiBot) onRegistryEvent(ev botregistry.Event) {
	if ev.BotID == multiBotSelfID || ev.BotID == "" {
		return
	}
	var kind botregistry.MessageKind
	text := strings.TrimSpace(ev.Detail)
	switch ev.Kind {
	case botregistry.EventFinished:
		kind, text = botregistry.MsgDone, firstNonEmptyMB(text, "任务完成 / run finished")
	case botregistry.EventNeedsUser:
		kind, text = botregistry.MsgBlocked, firstNonEmptyMB(text, "需要人处理 / needs the person")
	case botregistry.EventFailed:
		kind, text = botregistry.MsgBlocked, "失败 / failed: "+firstNonEmptyMB(text, "unknown error")
	case botregistry.EventArtifact:
		kind = botregistry.MsgArtifact
	default:
		return
	}
	if ev.Task != "" && ev.Kind != botregistry.EventArtifact {
		text = clipRunes(ev.Task, 80) + " — " + text
	}
	m := botregistry.Message{From: ev.BotID, FromName: ev.Name, Kind: kind, Text: text, RunID: ev.RunID, Via: "watch",
		ExternalID: "ev:" + string(ev.Kind) + ":" + ev.BotID + ":" + ev.RunID + ":" + ev.Detail}
	if kind == botregistry.MsgArtifact {
		if id := strings.Fields(text); len(id) > 0 {
			m.Artifacts = []string{id[0]}
		}
	}
	// Registry callbacks run under no lock of ours, but keep Notify re-entry
	// (receive emits EventMessage only for non-watch messages) off this path.
	go func() { _, _ = mb.receive(m) }()
}

func firstNonEmptyMB(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// pollReports pulls Hub report notes addressed to the account primary (the
// relay that stands for this main bot). Notes already on Hub when the GUI
// starts are marked seen, not replayed.
func (mb *multiBot) pollReports(ctx context.Context) {
	h := mb.hubNotes()
	if h == nil {
		return
	}
	notes, err := h.notes(ctx)
	if err != nil {
		return
	}
	relay := ""
	if r, err := mb.hubRelay(ctx); err == nil {
		relay = r
	}
	mb.mu.Lock()
	first := !mb.notesInit
	mb.notesInit = true
	fresh := []DesktopBotCrossNote{}
	for _, n := range notes {
		if n.Kind != "report" || mb.seenNotes[n.ID] {
			continue
		}
		mb.seenNotes[n.ID] = true
		if !first {
			fresh = append(fresh, n)
		}
	}
	if len(mb.seenNotes) > 500 {
		keep := map[string]bool{}
		for _, n := range notes {
			keep[n.ID] = mb.seenNotes[n.ID]
		}
		mb.seenNotes = keep
	}
	mb.mu.Unlock()
	for _, n := range fresh {
		if relay != "" && n.Target != relay {
			continue // a report to another teammate; its own chat shows it
		}
		if n.From == relay {
			continue // the main bot's own send
		}
		_, _ = mb.receive(botregistry.Message{
			From: n.From, Kind: botregistry.NormalizeKind(n.Topic), Text: n.Text, Artifacts: n.Artifacts,
			RunID: n.RunID, ReplyTo: n.ReplyTo, Via: "hub", ExternalID: "note:" + n.ID,
		})
	}
}

// startTeamInbox starts the watcher at app start so reports wake the main
// bot even before it has used a team tool. Safe to call more than once.
func (a *App) startTeamInbox() {
	if mb := a.multiBot(); mb != nil {
		mb.ensureWatch()
	}
}

func (h appMultiBotHub) notes(ctx context.Context) ([]DesktopBotCrossNote, error) {
	return h.a.PullDesktopBotCrossNotes()
}

func (h appMultiBotHub) report(ctx context.Context, fromID, targetID, topic, text, replyTo string, artifacts []string) (DesktopBotCrossNote, error) {
	return h.a.postDesktopBotReport(fromID, targetID, topic, text, replyTo, artifacts)
}
